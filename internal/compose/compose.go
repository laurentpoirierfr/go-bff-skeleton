// Package compose contient les API propres au BFF : elles appellent plusieurs
// backends en parallèle, et composent la réponse dont l'écran a besoin, en une
// seule requête. C'est ce qui distingue un BFF d'une simple passerelle.
//
//   - GET /api/dashboard : tous les utilisateurs et toutes les commandes,
//     résumés ; sans l'une des deux sources, la réponse serait fausse, donc
//     une erreur ;
//   - GET /api/profiles/{id} : un utilisateur et ses commandes ; si les
//     commandes manquent, la réponse est dégradée (partial) mais utile.
package compose

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/laurentpoirierfr/go-bff-skeleton/internal/proxy"
)

// Backend est un service appelé, avec le délai de sa route.
type Backend struct {
	Name    string
	URL     *url.URL // http://users:8080/api/v1/users
	Timeout time.Duration
}

// Composer appelle les backends users et orders.
type Composer struct {
	users, orders Backend
	client        *http.Client
	log           *slog.Logger
}

// New crée le composeur ; transport est celui du proxy (instrumenté).
func New(users, orders Backend, transport http.RoundTripper, log *slog.Logger) *Composer {
	return &Composer{users: users, orders: orders, client: &http.Client{Transport: transport}, log: log}
}

// Register ajoute les routes au mux. Elles sont plus précises que « /api/ »
// (la réponse 404 du proxy) : le mux les choisit.
func (c *Composer) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/dashboard", c.dashboard)
	mux.HandleFunc("GET /api/profiles/{id}", c.profile)
}

type user struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type order struct {
	ID         int    `json:"id"`
	UserID     int    `json:"user_id"`
	Product    string `json:"product"`
	TotalCents int64  `json:"total_cents"`
	Status     string `json:"status"`
}

// UserSummary est une ligne du tableau de bord.
type UserSummary struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	OrderCount int    `json:"order_count"`
	TotalCents int64  `json:"total_cents"`
}

// Dashboard est la réponse de GET /api/dashboard.
type Dashboard struct {
	UserCount  int           `json:"user_count"`
	OrderCount int           `json:"order_count"`
	TotalCents int64         `json:"total_cents"`
	TopUsers   []UserSummary `json:"top_users"` // par montant décroissant
}

func (c *Composer) dashboard(w http.ResponseWriter, r *http.Request) {
	var (
		users  []user
		orders []order
	)
	g, ctx := errgroup.WithContext(r.Context())
	g.Go(func() error { return c.get(ctx, c.users, "", &users) })
	g.Go(func() error { return c.get(ctx, c.orders, "", &orders) })
	if err := g.Wait(); err != nil {
		c.fail(w, r, err)
		return
	}
	d := Dashboard{UserCount: len(users), OrderCount: len(orders), TopUsers: []UserSummary{}}
	byUser := map[int]*UserSummary{}
	for _, u := range users {
		byUser[u.ID] = &UserSummary{ID: u.ID, Name: u.Name}
	}
	for _, o := range orders {
		d.TotalCents += o.TotalCents
		if s := byUser[o.UserID]; s != nil {
			s.OrderCount++
			s.TotalCents += o.TotalCents
		}
	}
	for _, u := range users {
		d.TopUsers = append(d.TopUsers, *byUser[u.ID])
	}
	slices.SortStableFunc(d.TopUsers, func(a, b UserSummary) int { return int(b.TotalCents - a.TotalCents) })
	reply(w, d)
}

// Profile est la réponse de GET /api/profiles/{id}.
type Profile struct {
	User     user     `json:"user"`
	Orders   []order  `json:"orders"`             // null si le service des commandes n'a pas répondu
	Partial  bool     `json:"partial"`            // vrai si une partie manque
	Warnings []string `json:"warnings,omitempty"` // ce qui manque, et pourquoi
}

func (c *Composer) profile(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id < 1 {
		proxy.Problem(w, http.StatusNotFound, "user not found")
		return
	}
	var (
		p                  Profile
		userErr, ordersErr error
		orders             []order
	)
	// Les deux appels partent ensemble ; l'échec de l'un n'annule pas l'autre.
	done := make(chan struct{}, 2)
	go func() { userErr = c.get(r.Context(), c.users, "/"+strconv.Itoa(id), &p.User); done <- struct{}{} }()
	go func() {
		ordersErr = c.get(r.Context(), c.orders, "?user_id="+strconv.Itoa(id), &orders)
		done <- struct{}{}
	}()
	<-done
	<-done
	if userErr != nil { // sans l'utilisateur, il n'y a pas de profil
		c.fail(w, r, userErr)
		return
	}
	if ordersErr != nil {
		c.log.WarnContext(r.Context(), "partial profile", "user_id", id, "err", ordersErr)
		p.Partial = true
		p.Warnings = []string{public(ordersErr)}
	} else {
		p.Orders = orders
	}
	reply(w, p)
}

// callError décrit l'échec d'un appel à un backend. Error() donne le détail
// (adresse, erreur réseau), pour le journal ; public() un message sans
// détail interne, pour le client.
type callError struct {
	backend string
	timeout time.Duration // non nul : délai dépassé
	code    int           // non nul : le backend a répondu une erreur
	err     error
}

func (e *callError) Error() string {
	switch {
	case e.code != 0:
		return fmt.Sprintf("backend %s answered %d", e.backend, e.code)
	case e.timeout != 0:
		return fmt.Sprintf("backend %s did not answer within %s: %v", e.backend, e.timeout, e.err)
	}
	return fmt.Sprintf("backend %s unavailable: %v", e.backend, e.err)
}

func (e *callError) Unwrap() error { return e.err }

func (e *callError) public() string {
	switch {
	case e.code != 0:
		return fmt.Sprintf("backend %s answered %d", e.backend, e.code)
	case e.timeout != 0:
		return fmt.Sprintf("backend %s did not answer within %s", e.backend, e.timeout)
	}
	return fmt.Sprintf("backend %s unavailable", e.backend)
}

// public renvoie le message d'une erreur montrable au client.
func public(err error) string {
	var ce *callError
	if errors.As(err, &ce) {
		return ce.public()
	}
	return "backend error"
}

// get appelle un backend, dans le délai de sa route, et décode sa réponse :
// l'objet lui-même, ou le champ items d'une liste.
func (c *Composer) get(ctx context.Context, b Backend, suffix string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, b.Timeout)
	defer cancel()
	u := b.URL.String() + suffix
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return &callError{backend: b.Name, timeout: b.Timeout, err: err}
		}
		return &callError{backend: b.Name, err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &callError{backend: b.Name, code: resp.StatusCode}
	}
	if suffix == "" || suffix[0] == '?' {
		var list struct {
			Items json.RawMessage `json:"items"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
			return &callError{backend: b.Name, err: fmt.Errorf("invalid response: %w", err)}
		}
		err = json.Unmarshal(list.Items, out)
	} else {
		err = json.NewDecoder(resp.Body).Decode(out)
	}
	if err != nil {
		return &callError{backend: b.Name, err: fmt.Errorf("invalid response: %w", err)}
	}
	return nil
}

// fail traduit l'échec d'un appel : 404 si l'objet n'existe pas, 504 pour un
// délai dépassé, 502 sinon. Le détail (adresses, erreur réseau) reste dans le
// journal : le client reçoit un message sans information interne.
func (c *Composer) fail(w http.ResponseWriter, r *http.Request, err error) {
	var ce *callError
	switch {
	case errors.As(err, &ce) && ce.code == http.StatusNotFound:
		proxy.Problem(w, http.StatusNotFound, "not found")
		return
	case errors.As(err, &ce) && ce.timeout != 0:
		proxy.Problem(w, http.StatusGatewayTimeout, public(err))
	default:
		proxy.Problem(w, http.StatusBadGateway, public(err))
	}
	c.log.WarnContext(r.Context(), "composition failed", "err", err)
}

func reply(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

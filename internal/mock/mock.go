// Package mock contient deux services simulés, users et orders, pour montrer
// le BFF en fonctionnement : ses routes, son agrégation, et ses réponses quand
// un backend est lent ou en panne. Les données sont fixes ; les utilisateurs
// créés sont gardés en mémoire.
package mock

import (
	"encoding/json"
	"math/rand/v2"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"
)

// Options simule les défauts d'un backend.
type Options struct {
	Delay    time.Duration // ajouté à chaque réponse
	FailRate float64       // part des requêtes en échec (503), de 0 à 1
}

// User est un utilisateur.
type User struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Order est une commande d'un utilisateur.
type Order struct {
	ID         int    `json:"id"`
	UserID     int    `json:"user_id"`
	Product    string `json:"product"`
	TotalCents int64  `json:"total_cents"`
	Status     string `json:"status"`
}

// Users renvoie le service des utilisateurs :
//
//	GET  /api/v1/users        la liste
//	GET  /api/v1/users/{id}   un utilisateur (404 s'il n'existe pas)
//	POST /api/v1/users        crée un utilisateur ({"name", "email"})
func Users(o Options) http.Handler {
	var (
		mu    sync.Mutex
		users = []User{
			{1, "Ada Lovelace", "ada@example.com"},
			{2, "Grace Hopper", "grace@example.com"},
			{3, "Margaret Hamilton", "margaret@example.com"},
		}
	)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/users", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		reply(w, http.StatusOK, map[string]any{"items": slices.Clone(users)})
	})
	mux.HandleFunc("GET /api/v1/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("id"))
		mu.Lock()
		defer mu.Unlock()
		i := slices.IndexFunc(users, func(u User) bool { return u.ID == id })
		if i < 0 {
			problem(w, http.StatusNotFound, "user not found")
			return
		}
		reply(w, http.StatusOK, users[i])
	})
	mux.HandleFunc("POST /api/v1/users", func(w http.ResponseWriter, r *http.Request) {
		var u User
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&u); err != nil || u.Name == "" || u.Email == "" {
			problem(w, http.StatusUnprocessableEntity, "name and email are required")
			return
		}
		mu.Lock()
		u.ID = users[len(users)-1].ID + 1
		users = append(users, u)
		mu.Unlock()
		w.Header().Set("Location", "/api/v1/users/"+strconv.Itoa(u.ID))
		reply(w, http.StatusCreated, u)
	})
	return faults(o, "users", mux)
}

// Orders renvoie le service des commandes :
//
//	GET /api/v1/orders?user_id=1   la liste, éventuellement filtrée
//	GET /api/v1/orders/{id}        une commande (404 si elle n'existe pas)
func Orders(o Options) http.Handler {
	orders := []Order{
		{101, 1, "Analytical engine", 120000, "shipped"},
		{102, 1, "Punched cards", 2500, "placed"},
		{103, 2, "Compiler", 54000, "shipped"},
		{104, 3, "Apollo guidance computer", 990000, "placed"},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/orders", func(w http.ResponseWriter, r *http.Request) {
		items := orders
		if s := r.URL.Query().Get("user_id"); s != "" {
			uid, _ := strconv.Atoi(s)
			items = slices.DeleteFunc(slices.Clone(orders), func(o Order) bool { return o.UserID != uid })
		}
		reply(w, http.StatusOK, map[string]any{"items": items})
	})
	mux.HandleFunc("GET /api/v1/orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("id"))
		i := slices.IndexFunc(orders, func(o Order) bool { return o.ID == id })
		if i < 0 {
			problem(w, http.StatusNotFound, "order not found")
			return
		}
		reply(w, http.StatusOK, orders[i])
	})
	return faults(o, "orders", mux)
}

// faults ajoute le délai et les pannes simulés, et l'en-tête X-Backend qui
// montre quel service a répondu.
func faults(o Options, name string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Backend", "mock-"+name)
		if o.Delay > 0 {
			select {
			case <-time.After(o.Delay):
			case <-r.Context().Done():
				return
			}
		}
		if o.FailRate > 0 && rand.Float64() < o.FailRate { // simulation : un aléa non cryptographique suffit
			problem(w, http.StatusServiceUnavailable, "simulated failure")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func reply(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func problem(w http.ResponseWriter, code int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"title": http.StatusText(code), "status": code, "detail": detail})
}

// Package health expose les sondes de l'orchestrateur :
//
//   - /health/live : le processus répond (sinon, il faut le redémarrer) ;
//   - /health/ready : il peut recevoir du trafic (ses dépendances répondent,
//     et il n'est pas en train de s'arrêter).
//
// Les deux sont distinctes : une base indisponible doit retirer l'instance du
// trafic, pas la redémarrer en boucle.
package health

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Check teste une dépendance, en respectant le délai du contexte.
type Check func(ctx context.Context) error

// Health regroupe les vérifications d'un binaire.
type Health struct {
	timeout      time.Duration
	mu           sync.Mutex
	names        []string
	checks       map[string]Check
	shuttingDown atomic.Bool
}

// New crée les sondes ; chaque vérification dispose de timeout.
func New(timeout time.Duration) *Health {
	return &Health{timeout: timeout, checks: map[string]Check{}}
}

// Add enregistre la vérification d'une dépendance (base, NATS…).
func (h *Health) Add(name string, c Check) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.checks[name]; !exists {
		h.names = append(h.names, name)
	}
	h.checks[name] = c
}

// ShuttingDown signale l'arrêt en cours : /health/ready répond 503, et
// l'orchestrateur cesse d'envoyer du trafic pendant que l'instance termine
// ses requêtes.
func (h *Health) ShuttingDown() { h.shuttingDown.Store(true) }

// Register ajoute les deux routes au mux.
func (h *Health) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
		reply(w, http.StatusOK, map[string]any{"status": "ok"})
	})
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		if h.shuttingDown.Load() {
			reply(w, http.StatusServiceUnavailable, map[string]any{"status": "shutting_down"})
			return
		}
		results, ok := h.run(r.Context())
		code, status := http.StatusOK, "ok"
		if !ok {
			code, status = http.StatusServiceUnavailable, "unavailable"
		}
		reply(w, code, map[string]any{"status": status, "checks": results})
	})
}

// run lance les vérifications en parallèle : la sonde répond dans le délai
// d'une seule, et non de leur somme.
func (h *Health) run(ctx context.Context) (map[string]string, bool) {
	h.mu.Lock()
	names := append([]string(nil), h.names...)
	checks := make([]Check, len(names))
	for i, n := range names {
		checks[i] = h.checks[n]
	}
	h.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()
	errs := make([]error, len(names))
	var wg sync.WaitGroup
	for i := range names {
		wg.Go(func() { errs[i] = checks[i](ctx) })
	}
	wg.Wait()

	results, ok := make(map[string]string, len(names)), true
	for i, n := range names {
		results[n] = "ok"
		if errs[i] != nil {
			results[n], ok = errs[i].Error(), false
		}
	}
	return results, ok
}

func reply(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

// Package web contient les routes HTTP du service.
package web

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// Routes renvoie le handler du service, avec la journalisation des requêtes.
func Routes(log *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /sante", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"statut": "ok"})
	})
	return journal(log, mux)
}

// statut retient le code de réponse, pour le journal.
type statut struct {
	http.ResponseWriter
	code int
}

func (s *statut) WriteHeader(code int) {
	s.code = code
	s.ResponseWriter.WriteHeader(code)
}

func journal(log *slog.Logger, suivant http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		debut := time.Now()
		s := &statut{ResponseWriter: w, code: http.StatusOK}
		suivant.ServeHTTP(s, r)
		log.InfoContext(r.Context(), "requête", "methode", r.Method, "chemin", r.URL.Path,
			"statut", s.code, "duree", time.Since(debut))
	})
}

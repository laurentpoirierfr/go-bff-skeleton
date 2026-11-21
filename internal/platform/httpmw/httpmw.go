// Package httpmw contient les middlewares HTTP communs : identifiant de
// requête, journal d'accès et récupération des paniques.
package httpmw

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Chain applique les middlewares : le premier est le plus extérieur.
func Chain(h http.Handler, mws ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

type requestIDKey struct{}

// RequestIDHeader porte l'identifiant de la requête, d'un service à l'autre.
const RequestIDHeader = "X-Request-ID"

// Un identifiant reçu n'est repris que s'il est raisonnable : il finit dans
// les journaux.
var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// RequestID reprend l'identifiant reçu, ou en crée un, le renvoie dans la
// réponse et le range dans le contexte.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if !validRequestID.MatchString(id) {
			id = uuid.NewString()
		}
		w.Header().Set(RequestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}

// RequestIDFrom lit l'identifiant de la requête dans le contexte.
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// recorder retient le statut et la taille de la réponse.
type recorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *recorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// Unwrap laisse http.ResponseController atteindre la réponse d'origine.
func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// AccessLog journalise chaque requête, avec son identifiant. Les sondes de
// santé, interrogées en boucle, sont au niveau debug.
func AccessLog(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &recorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			level := slog.LevelInfo
			switch {
			case strings.HasPrefix(r.URL.Path, "/health/"):
				// Une sonde en échec (503) n'est pas une erreur du service : son
				// état est déjà dans la réponse, et elle est interrogée en boucle.
				level = slog.LevelDebug
			case rec.status >= 500:
				level = slog.LevelError
			}
			log.Log(r.Context(), level, "request",
				"request_id", RequestIDFrom(r.Context()), "method", r.Method, "path", r.URL.Path,
				"status", rec.status, "bytes", rec.bytes, "duration", time.Since(start))
		})
	}
}

// Recover transforme une panique en réponse 500 (RFC 9457), et la
// journalise : un bogue dans un handler ne coupe pas la connexion sans
// réponse.
func Recover(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				v := recover()
				if v == nil {
					return
				}
				if v == http.ErrAbortHandler { //nolint:errorlint // valeur sentinelle de net/http, comparée telle quelle
					panic(v)
				}
				log.ErrorContext(r.Context(), "panic", "request_id", RequestIDFrom(r.Context()), "value", v)
				w.Header().Set("Content-Type", "application/problem+json")
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"title":"Internal Server Error","status":500,"detail":"internal error"}`))
			}()
			next.ServeHTTP(w, r)
		})
	}
}

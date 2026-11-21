// Package proxy relaie les appels de l'application vers les backends, selon
// la table de routage : /api/users/42 → http://users/api/v1/users/42. Seules
// les routes déclarées sont relayées : le BFF n'est jamais un proxy ouvert.
package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Route relaie les chemins qui commencent par Prefix vers Target.
type Route struct {
	Name    string
	Prefix  string   // /api/users
	Target  *url.URL // http://users:8080/api/v1/users
	Timeout time.Duration
}

// Transport renvoie le transport HTTP vers les backends : un span par appel,
// et l'en-tête traceparent transmis, pour que la trace continue chez le
// backend.
func Transport() http.RoundTripper {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConnsPerHost = 64 // le BFF parle beaucoup à peu de backends
	return otelhttp.NewTransport(t)
}

// Options règle le proxy.
type Options struct {
	MaxBodyBytes int64             // taille maximale d'un corps relayé
	Transport    http.RoundTripper // nil : http.DefaultTransport
	Log          *slog.Logger
}

// Register enregistre les routes sur mux, ainsi qu'une réponse 404 pour tout
// autre chemin sous /api/ (sans quoi il serait servi par l'application web).
func Register(mux *http.ServeMux, routes []Route, o Options) {
	if o.Transport == nil {
		o.Transport = http.DefaultTransport
	}
	for _, r := range routes {
		h := handler(r, o)
		// /api/users et tout ce qui est dessous ; pas /api/usersX.
		mux.Handle(r.Prefix, h)
		mux.Handle(r.Prefix+"/", h)
	}
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		Problem(w, http.StatusNotFound, "no route for this path")
	})
}

func handler(r Route, o Options) http.Handler {
	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			// Le reste du chemin, après le préfixe, s'ajoute tel quel au chemin
			// de la cible : /api/users → /api/v1/users (sans barre finale, que
			// SetURL ajouterait), /api/users/42 → /api/v1/users/42. La requête
			// (?query) est conservée.
			path := strings.TrimSuffix(r.Target.Path, "/") + strings.TrimPrefix(pr.In.URL.Path, r.Prefix)
			if path == "" {
				path = "/"
			}
			pr.Out.URL.Scheme, pr.Out.URL.Host = r.Target.Scheme, r.Target.Host
			pr.Out.URL.Path, pr.Out.URL.RawPath = path, ""
			pr.Out.Host = "" // l'en-tête Host suit la cible
			pr.SetXForwarded()
			// Les cookies du navigateur restent au BFF : un backend ne doit pas
			// recevoir la session de l'utilisateur.
			pr.Out.Header.Del("Cookie")
		},
		Transport: o.Transport,
		ErrorHandler: func(w http.ResponseWriter, req *http.Request, err error) {
			switch {
			case errors.Is(err, context.Canceled) && req.Context().Err() != nil && !errors.Is(context.Cause(req.Context()), errTimeout):
				// Le client est parti : personne ne lira la réponse.
				o.Log.DebugContext(req.Context(), "client gone", "route", r.Name)
			case errors.Is(err, context.DeadlineExceeded) || errors.Is(context.Cause(req.Context()), errTimeout):
				o.Log.WarnContext(req.Context(), "backend timeout", "route", r.Name, "timeout", r.Timeout)
				Problem(w, http.StatusGatewayTimeout, fmt.Sprintf("backend %s did not answer within %s", r.Name, r.Timeout))
			case isTooLarge(err):
				Problem(w, http.StatusRequestEntityTooLarge, "request body too large")
			default:
				o.Log.WarnContext(req.Context(), "backend unavailable", "route", r.Name, "err", err)
				Problem(w, http.StatusBadGateway, fmt.Sprintf("backend %s unavailable", r.Name))
			}
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx, cancel := context.WithTimeoutCause(req.Context(), r.Timeout, errTimeout)
		defer cancel()
		if req.Body != nil {
			req.Body = http.MaxBytesReader(w, req.Body, o.MaxBodyBytes)
		}
		rp.ServeHTTP(w, req.WithContext(ctx))
	})
}

var errTimeout = errors.New("route timeout")

func isTooLarge(err error) bool {
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr)
}

// Problem écrit une erreur au format RFC 9457 (application/problem+json).
func Problem(w http.ResponseWriter, code int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"title": http.StatusText(code), "status": code, "detail": detail})
}

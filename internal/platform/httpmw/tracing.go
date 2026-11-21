package httpmw

import (
	"net/http"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Tracing crée un span par requête, et reprend le contexte de trace reçu
// (en-tête traceparent). Le span est nommé par la route (« POST
// /v1/orders/{id}/cancellation ») et non par l'URL, qui contient des
// identifiants : un nom par route garde les traces regroupables. Les sondes de
// santé, interrogées en boucle, ne sont pas tracées.
func Tracing(next http.Handler) http.Handler {
	return otelhttp.NewHandler(next, "http",
		// Appelé au début (route encore inconnue), puis par otelhttp après le
		// routage, une fois r.Pattern renseigné par ServeMux.
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			switch {
			case r.Pattern == "":
				return r.Method
			case strings.Contains(r.Pattern, " "):
				return r.Pattern
			}
			return r.Method + " " + r.Pattern
		}),
		otelhttp.WithFilter(func(r *http.Request) bool { return !strings.HasPrefix(r.URL.Path, "/health/") }),
	)
}

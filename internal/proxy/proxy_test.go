package proxy

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/laurentpoirierfr/go-bff-skeleton/internal/platform/telemetry"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// echo renvoie ce que le backend a reçu.
func echo(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"method": r.Method, "path": r.URL.Path, "query": r.URL.RawQuery, "body": string(body), "host": r.Host,
			"cookie": r.Header.Get("Cookie"), "xff": r.Header.Get("X-Forwarded-For"), "xfh": r.Header.Get("X-Forwarded-Host"),
			"traceparent": r.Header.Get("Traceparent"),
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func route(t *testing.T, name, prefix, target string, timeout time.Duration) Route {
	t.Helper()
	u, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	return Route{Name: name, Prefix: prefix, Target: u, Timeout: timeout}
}

func newMux(routes ...Route) *http.ServeMux {
	mux := http.NewServeMux()
	Register(mux, routes, Options{MaxBodyBytes: 64, Log: quiet})
	return mux
}

func do(t *testing.T, h http.Handler, method, target, body string, hdr ...string) (int, map[string]any, http.Header) {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	var m map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &m)
	return w.Code, m, w.Header()
}

func TestRewrite(t *testing.T) {
	backend := echo(t)
	mux := newMux(route(t, "users", "/api/users", backend.URL+"/api/v1/users", time.Second))
	for in, want := range map[string]string{
		"/api/users":        "/api/v1/users",
		"/api/users/42":     "/api/v1/users/42",
		"/api/users/42/x/y": "/api/v1/users/42/x/y",
	} {
		code, m, _ := do(t, mux, "GET", in+"?page=2&q=a%20b", "")
		if code != 200 || m["path"] != want || m["query"] != "page=2&q=a%20b" {
			t.Errorf("%s: %d %v", in, code, m)
		}
	}
}

func TestRequestForwarding(t *testing.T) {
	backend := echo(t)
	mux := newMux(route(t, "users", "/api/users", backend.URL+"/api/v1/users", time.Second))
	code, m, _ := do(t, mux, "POST", "/api/users", `{"name":"Ada"}`, "Cookie", "session=secret", "Content-Type", "application/json")
	host := strings.TrimPrefix(backend.URL, "http://")
	if code != 200 || m["method"] != "POST" || m["body"] != `{"name":"Ada"}` || m["host"] != host {
		t.Errorf("%d %v", code, m)
	}
	if m["cookie"] != "" {
		t.Errorf("the browser cookie reached the backend: %v", m["cookie"])
	}
	if m["xff"] == "" || m["xfh"] != "example.com" {
		t.Errorf("X-Forwarded-*: %v", m)
	}
}

// Un chemin voisin ne déclenche pas la route, et un chemin inconnu sous
// /api/ reçoit 404, jamais l'application web.
func TestUnknownPaths(t *testing.T) {
	backend := echo(t)
	mux := newMux(route(t, "users", "/api/users", backend.URL+"/api/v1/users", time.Second))
	for _, p := range []string{"/api/usersX", "/api/other", "/api/"} {
		if code, m, h := do(t, mux, "GET", p, ""); code != 404 || h.Get("Content-Type") != "application/problem+json" || m["detail"] != "no route for this path" {
			t.Errorf("%s: %d %v", p, code, m)
		}
	}
}

func TestBackendErrors(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(time.Second):
		case <-r.Context().Done():
		}
	}))
	defer slow.Close()
	down := httptest.NewServer(http.NotFoundHandler())
	down.Close() // plus personne n'écoute
	backend := echo(t)
	mux := newMux(
		route(t, "slow", "/api/slow", slow.URL, 50*time.Millisecond),
		route(t, "down", "/api/down", down.URL, time.Second),
		route(t, "users", "/api/users", backend.URL, time.Second),
	)
	start := time.Now()
	if code, m, _ := do(t, mux, "GET", "/api/slow", ""); code != http.StatusGatewayTimeout || !strings.Contains(m["detail"].(string), "did not answer within 50ms") {
		t.Errorf("timeout: %d %v", code, m)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Errorf("the timeout was not applied: %v", time.Since(start))
	}
	if code, m, _ := do(t, mux, "GET", "/api/down", ""); code != http.StatusBadGateway || m["detail"] != "backend down unavailable" {
		t.Errorf("down: %d %v", code, m)
	}
	if code, _, _ := do(t, mux, "POST", "/api/users", strings.Repeat("x", 100)); code != http.StatusRequestEntityTooLarge {
		t.Errorf("too large: %d", code)
	}
}

// Les réponses d'erreur du backend sont relayées telles quelles.
func TestBackendStatus(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Backend", "test")
		http.Error(w, "nope", http.StatusTeapot)
	}))
	defer backend.Close()
	mux := newMux(route(t, "tea", "/api/tea", backend.URL, time.Second))
	if code, _, h := do(t, mux, "GET", "/api/tea", ""); code != http.StatusTeapot || h.Get("X-Backend") != "test" {
		t.Errorf("%d %v", code, h)
	}
}

// Une cible sans chemin, ou avec une barre finale, se joint correctement.
func TestTargetPaths(t *testing.T) {
	backend := echo(t)
	for _, tc := range []struct{ target, in, want string }{
		{backend.URL, "/api/x", "/"},
		{backend.URL, "/api/x/1", "/1"},
		{backend.URL + "/v1/", "/api/x", "/v1"},
		{backend.URL + "/v1/", "/api/x/1", "/v1/1"},
	} {
		mux := newMux(route(t, "x", "/api/x", tc.target, time.Second))
		if _, m, _ := do(t, mux, "GET", tc.in, ""); m["path"] != tc.want {
			t.Errorf("%s + %s: %v, want %s", tc.target, tc.in, m["path"], tc.want)
		}
	}
}

// Le contexte de trace de la requête entrante est transmis au backend.
func TestTracePropagation(t *testing.T) {
	if _, err := telemetry.Setup(context.Background(), telemetry.Options{}); err != nil { // propagateur W3C
		t.Fatal(err)
	}
	backend := echo(t)
	mux := http.NewServeMux()
	Register(mux, []Route{route(t, "users", "/api/users", backend.URL, time.Second)}, Options{MaxBodyBytes: 64, Log: quiet, Transport: Transport()})
	ctx, span := sdktrace.NewTracerProvider().Tracer("test").Start(context.Background(), "incoming")
	defer span.End()
	req := httptest.NewRequest("GET", "/api/users", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var m map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &m)
	if !strings.Contains(m["traceparent"], span.SpanContext().TraceID().String()) {
		t.Errorf("traceparent %q, want trace %s", m["traceparent"], span.SpanContext().TraceID())
	}
}

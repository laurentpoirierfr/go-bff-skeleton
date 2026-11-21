package compose

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/laurentpoirierfr/go-bff-skeleton/internal/mock"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// backend démarre un service simulé, et renvoie sa description.
func backend(t *testing.T, name string, h http.Handler, path string, timeout time.Duration) Backend {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL + path)
	return Backend{Name: name, URL: u, Timeout: timeout}
}

func newMux(users, orders Backend) *http.ServeMux {
	mux := http.NewServeMux()
	New(users, orders, http.DefaultTransport, quiet).Register(mux)
	return mux
}

func get(t *testing.T, h http.Handler, path string, out any) int {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	if out != nil {
		if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
			t.Fatalf("%s: %v: %s", path, err, w.Body)
		}
	}
	return w.Code
}

func usersBackend(t *testing.T, o mock.Options, timeout time.Duration) Backend {
	return backend(t, "users", mock.Users(o), "/api/v1/users", timeout)
}

func ordersBackend(t *testing.T, o mock.Options, timeout time.Duration) Backend {
	return backend(t, "orders", mock.Orders(o), "/api/v1/orders", timeout)
}

func TestDashboard(t *testing.T) {
	mux := newMux(usersBackend(t, mock.Options{}, time.Second), ordersBackend(t, mock.Options{}, time.Second))
	var d Dashboard
	if code := get(t, mux, "/api/dashboard", &d); code != 200 {
		t.Fatalf("%d", code)
	}
	if d.UserCount != 3 || d.OrderCount != 4 || d.TotalCents != 120000+2500+54000+990000 {
		t.Errorf("%+v", d)
	}
	// Margaret Hamilton (990 000) d'abord, puis Ada (122 500), puis Grace.
	if len(d.TopUsers) != 3 || d.TopUsers[0].Name != "Margaret Hamilton" || d.TopUsers[1].OrderCount != 2 || d.TopUsers[1].TotalCents != 122500 {
		t.Errorf("%+v", d.TopUsers)
	}
}

// Les deux appels partent en parallèle : la réponse arrive dans le délai du
// plus lent, pas dans leur somme.
func TestDashboardParallel(t *testing.T) {
	delay := 150 * time.Millisecond
	mux := newMux(usersBackend(t, mock.Options{Delay: delay}, time.Second), ordersBackend(t, mock.Options{Delay: delay}, time.Second))
	start := time.Now()
	if code := get(t, mux, "/api/dashboard", nil); code != 200 {
		t.Fatalf("%d", code)
	}
	if d := time.Since(start); d >= 2*delay {
		t.Errorf("%v : les appels ne sont pas parallèles", d)
	}
}

// Sans l'une des deux sources, le tableau de bord serait faux : erreur.
func TestDashboardFailures(t *testing.T) {
	var p map[string]any
	mux := newMux(usersBackend(t, mock.Options{}, time.Second), ordersBackend(t, mock.Options{FailRate: 1}, time.Second))
	if code := get(t, mux, "/api/dashboard", &p); code != http.StatusBadGateway || !strings.Contains(p["detail"].(string), "orders") {
		t.Errorf("failure: %d %v", code, p)
	}
	mux = newMux(usersBackend(t, mock.Options{Delay: time.Second}, 50*time.Millisecond), ordersBackend(t, mock.Options{}, time.Second))
	if code := get(t, mux, "/api/dashboard", &p); code != http.StatusGatewayTimeout || !strings.Contains(p["detail"].(string), "users did not answer within 50ms") {
		t.Errorf("timeout: %d %v", code, p)
	}
}

func TestProfile(t *testing.T) {
	mux := newMux(usersBackend(t, mock.Options{}, time.Second), ordersBackend(t, mock.Options{}, time.Second))
	var p Profile
	if code := get(t, mux, "/api/profiles/1", &p); code != 200 || p.User.Name != "Ada Lovelace" || len(p.Orders) != 2 || p.Partial {
		t.Errorf("%d %+v", code, p)
	}
	if code := get(t, mux, "/api/profiles/99", nil); code != http.StatusNotFound {
		t.Errorf("missing user: %d", code)
	}
	if code := get(t, mux, "/api/profiles/abc", nil); code != http.StatusNotFound {
		t.Errorf("invalid id: %d", code)
	}
}

// Sans les commandes, le profil reste utile : réponse dégradée, signalée.
func TestProfileDegraded(t *testing.T) {
	mux := newMux(usersBackend(t, mock.Options{}, time.Second), ordersBackend(t, mock.Options{FailRate: 1}, time.Second))
	var raw map[string]any
	if code := get(t, mux, "/api/profiles/2", &raw); code != 200 {
		t.Fatalf("%d %v", code, raw)
	}
	if raw["orders"] != nil || raw["partial"] != true || raw["warnings"].([]any)[0] != "backend orders answered 503" {
		t.Errorf("%v", raw)
	}
	if user, _ := raw["user"].(map[string]any); user["name"] != "Grace Hopper" {
		t.Errorf("%v", raw["user"])
	}
}

// Une réponse d'erreur ne révèle rien d'interne : ni adresse, ni port, ni
// erreur réseau. Le détail reste dans le journal.
func TestNoInternalDetails(t *testing.T) {
	down := httptest.NewServer(http.NotFoundHandler())
	addr := strings.TrimPrefix(down.URL, "http://")
	down.Close()
	u, _ := url.Parse(down.URL + "/api/v1/orders")
	orders := Backend{Name: "orders", URL: u, Timeout: time.Second}
	mux := newMux(usersBackend(t, mock.Options{}, time.Second), orders)
	for _, path := range []string{"/api/dashboard", "/api/profiles/1"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		body := w.Body.String()
		if strings.Contains(body, addr) || strings.Contains(body, "dial") || strings.Contains(body, "127.0.0.1") || !strings.Contains(body, "backend orders unavailable") {
			t.Errorf("%s: %s", path, body)
		}
	}
}

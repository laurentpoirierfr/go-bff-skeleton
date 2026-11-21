package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func probe(t *testing.T, h *Health, path string) (int, map[string]any) {
	t.Helper()
	mux := http.NewServeMux()
	h.Register(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("%v: %s", err, w.Body)
	}
	return w.Code, body
}

func TestLive(t *testing.T) {
	h := New(time.Second)
	h.Add("database", func(context.Context) error { return errors.New("down") })
	// Une dépendance en panne ne rend pas le processus « mort ».
	if code, _ := probe(t, h, "/health/live"); code != http.StatusOK {
		t.Errorf("live: %d", code)
	}
}

func TestReady(t *testing.T) {
	h := New(time.Second)
	h.Add("database", func(context.Context) error { return nil })
	if code, body := probe(t, h, "/health/ready"); code != http.StatusOK || body["status"] != "ok" {
		t.Errorf("%d %v", code, body)
	}
	h.Add("nats", func(context.Context) error { return errors.New("connection refused") })
	code, body := probe(t, h, "/health/ready")
	checks, _ := body["checks"].(map[string]any)
	if code != http.StatusServiceUnavailable || checks["database"] != "ok" || checks["nats"] != "connection refused" {
		t.Errorf("%d %v", code, body)
	}
}

// Une vérification bloquée est interrompue par le délai, et les autres
// s'exécutent en parallèle : la sonde répond dans le délai d'une seule.
func TestTimeout(t *testing.T) {
	h := New(50 * time.Millisecond)
	for _, name := range []string{"a", "b", "c"} {
		h.Add(name, func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() })
	}
	start := time.Now()
	if code, _ := probe(t, h, "/health/ready"); code != http.StatusServiceUnavailable {
		t.Errorf("code %d", code)
	}
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Errorf("the probe took %v", d)
	}
}

func TestShuttingDown(t *testing.T) {
	h := New(time.Second)
	h.ShuttingDown()
	if code, body := probe(t, h, "/health/ready"); code != http.StatusServiceUnavailable || body["status"] != "shutting_down" {
		t.Errorf("%d %v", code, body)
	}
}

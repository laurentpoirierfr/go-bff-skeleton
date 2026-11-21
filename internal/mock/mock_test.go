package mock

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func get(t *testing.T, h http.Handler, method, path, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
	var m map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &m)
	return w, m
}

func TestUsers(t *testing.T) {
	h := Users(Options{})
	if w, m := get(t, h, "GET", "/api/v1/users", ""); w.Code != 200 || len(m["items"].([]any)) != 3 || w.Header().Get("X-Backend") != "mock-users" {
		t.Errorf("list: %d %v", w.Code, m)
	}
	if w, m := get(t, h, "GET", "/api/v1/users/2", ""); w.Code != 200 || m["name"] != "Grace Hopper" {
		t.Errorf("get: %d %v", w.Code, m)
	}
	if w, _ := get(t, h, "GET", "/api/v1/users/99", ""); w.Code != 404 {
		t.Errorf("missing: %d", w.Code)
	}
	w, m := get(t, h, "POST", "/api/v1/users", `{"name":"Barbara Liskov","email":"barbara@example.com"}`)
	if w.Code != 201 || m["id"] != 4.0 || w.Header().Get("Location") != "/api/v1/users/4" {
		t.Errorf("create: %d %v", w.Code, m)
	}
	if w, _ := get(t, h, "POST", "/api/v1/users", `{}`); w.Code != 422 {
		t.Errorf("invalid: %d", w.Code)
	}
}

func TestOrders(t *testing.T) {
	h := Orders(Options{})
	if _, m := get(t, h, "GET", "/api/v1/orders", ""); len(m["items"].([]any)) != 4 {
		t.Errorf("all: %v", m)
	}
	if _, m := get(t, h, "GET", "/api/v1/orders?user_id=1", ""); len(m["items"].([]any)) != 2 {
		t.Errorf("filtered: %v", m)
	}
	if w, m := get(t, h, "GET", "/api/v1/orders/103", ""); w.Code != 200 || m["product"] != "Compiler" {
		t.Errorf("get: %d %v", w.Code, m)
	}
}

func TestFaults(t *testing.T) {
	if w, _ := get(t, Orders(Options{FailRate: 1}), "GET", "/api/v1/orders", ""); w.Code != http.StatusServiceUnavailable {
		t.Errorf("failure: %d", w.Code)
	}
	start := time.Now()
	get(t, Users(Options{Delay: 50 * time.Millisecond}), "GET", "/api/v1/users", "")
	if time.Since(start) < 50*time.Millisecond {
		t.Error("delay not applied")
	}
}

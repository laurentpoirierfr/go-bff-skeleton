package httpmw

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestID(t *testing.T) {
	var seen string
	h := RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { seen = RequestIDFrom(r.Context()) }))
	for _, tc := range []struct{ in, want string }{{"abc-123", "abc-123"}, {"", ""}, {"bad value\n", ""}} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set(RequestIDHeader, tc.in)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		got := w.Header().Get(RequestIDHeader)
		if got != seen || got == "" || (tc.want != "" && got != tc.want) || (tc.want == "" && got == tc.in) {
			t.Errorf("in %q: header %q, context %q", tc.in, got, seen)
		}
	}
}

func TestAccessLogAndRecover(t *testing.T) {
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("hello")) })
	mux.HandleFunc("/boom", func(http.ResponseWriter, *http.Request) { panic("bug") })
	h := Chain(mux, RequestID, AccessLog(log), Recover(log))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/ok", nil))
	if w.Code != 200 || !strings.Contains(logs.String(), "level=INFO msg=request") || !strings.Contains(logs.String(), "path=/ok status=200 bytes=5") {
		t.Errorf("%d\n%s", w.Code, logs.String())
	}

	logs.Reset()
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/boom", nil))
	if w.Code != 500 || w.Header().Get("Content-Type") != "application/problem+json" || strings.Contains(w.Body.String(), "bug") {
		t.Errorf("%d %s %q", w.Code, w.Header(), w.Body)
	}
	if !strings.Contains(logs.String(), "msg=panic") || !strings.Contains(logs.String(), "value=bug") || !strings.Contains(logs.String(), "level=ERROR msg=request") {
		t.Errorf("logs:\n%s", logs.String())
	}
}

// Une sonde en échec reste au niveau debug : ce n'est pas une erreur.
func TestAccessLogHealthProbe(t *testing.T) {
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	h := AccessLog(log)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/health/ready", nil))
	if !strings.Contains(logs.String(), "level=DEBUG msg=request") {
		t.Errorf("%s", logs.String())
	}
}

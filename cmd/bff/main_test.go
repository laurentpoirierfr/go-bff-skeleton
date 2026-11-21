package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuffer est un tampon sûr en concurrence, pour capturer le journal.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}
func (s *syncBuffer) String() string { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

// Le binaire démarre, répond à ses sondes, puis s'arrête proprement.
func TestRun(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	t.Setenv("HTTP_ADDRESS", addr)
	t.Setenv("TELEMETRY_ENABLED", "false") // ne pas écrire dans le collecteur de développement
	var logs syncBuffer
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- run(ctx, []string{"-config", "../../configs/bff.yaml"}, &logs) }()

	var r *http.Response
	for range 100 {
		if r, err = http.Get("http://" + addr + "/health/ready"); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Errorf("status %d", r.StatusCode)
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("shutdown: %v\n%s", err, logs.String())
	}
}

func TestInvalidConfiguration(t *testing.T) {
	t.Setenv("PROXY_DEFAULT_TIMEOUT", "0s")
	err := run(context.Background(), []string{"-config", "../../configs/bff.yaml"}, &syncBuffer{})
	if err == nil || !strings.Contains(err.Error(), "proxy.default_timeout") {
		t.Errorf("err = %v", err)
	}
}

func TestPrintConfig(t *testing.T) {
	t.Setenv("ROUTES_USERS_TARGET", "http://users.prod/api/v1/users")
	var out syncBuffer
	if err := run(context.Background(), []string{"-config", "../../configs/bff.yaml", "-print-config"}, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`target: "http://users.prod/api/v1/users"  # env ROUTES_USERS_TARGET`, `prefix: "/api/orders"  # fichier`} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("%q missing from:\n%s", want, out.String())
		}
	}
}

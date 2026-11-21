package runner

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// waiter tourne jusqu'à l'annulation, et note qu'il s'est arrêté.
func waiter(stopped *atomic.Bool) Component {
	return Component{Name: "waiter", Run: func(ctx context.Context) error {
		<-ctx.Done()
		stopped.Store(true)
		return nil
	}}
}

func TestCancel(t *testing.T) {
	var a, b atomic.Bool
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- Run(ctx, quiet, waiter(&a), waiter(&b)) }()
	cancel()
	if err := <-done; err != nil || !a.Load() || !b.Load() {
		t.Errorf("err %v, stopped %v %v", err, a.Load(), b.Load())
	}
}

// L'échec d'un composant arrête les autres, et son erreur est renvoyée.
func TestFailure(t *testing.T) {
	var a atomic.Bool
	broken := Component{Name: "broken", Run: func(context.Context) error { return errors.New("connection lost") }}
	err := Run(context.Background(), quiet, waiter(&a), broken)
	if err == nil || err.Error() != "broken: connection lost" || !a.Load() {
		t.Errorf("err %v, other stopped %v", err, a.Load())
	}
}

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

// Une requête en cours se termine avant l'arrêt du serveur, et beforeShutdown
// est appelée avant.
func TestHTTPServerGracefulShutdown(t *testing.T) {
	addr := freePort(t)
	inFlight := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/slow", func(w http.ResponseWriter, _ *http.Request) {
		close(inFlight)
		time.Sleep(200 * time.Millisecond)
		w.Write([]byte("done"))
	})
	var notified atomic.Bool
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() {
		done <- Run(ctx, quiet, HTTPServer(&http.Server{Addr: addr, Handler: mux}, 5*time.Second, func() { notified.Store(true) }))
	}()

	response := make(chan string)
	go func() {
		for range 50 {
			r, err := http.Get("http://" + addr + "/slow")
			if err == nil {
				b, _ := io.ReadAll(r.Body)
				r.Body.Close()
				response <- string(b)
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		response <- "never started"
	}()
	<-inFlight
	cancel() // arrêt pendant la requête
	if r := <-response; r != "done" {
		t.Errorf("response %q", r)
	}
	if err := <-done; err != nil || !notified.Load() {
		t.Errorf("err %v, notified %v", err, notified.Load())
	}
}

func TestHTTPServerAddressInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if err := Run(context.Background(), quiet, HTTPServer(&http.Server{Addr: ln.Addr().String()}, time.Second, nil)); err == nil {
		t.Error("an address in use must fail the start")
	}
}

// Package e2e teste les binaires comme ils sont déployés : le BFF et les deux
// services simulés, compilés, lancés comme des processus, le BFF configuré par
// ses seules variables d'environnement. Sauté avec go test -short.
package e2e_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

var binaries string

func TestMain(m *testing.M) {
	flag.Parse() // testing.Short() lit les options de la ligne de commande
	code := func() int {
		if testing.Short() {
			return m.Run()
		}
		dir, err := os.MkdirTemp("", "bff-e2e-")
		if err != nil {
			panic(err)
		}
		defer os.RemoveAll(dir)
		binaries = dir
		for _, b := range []string{"bff", "mock-users", "mock-orders"} {
			out, err := exec.Command("go", "build", "-o", filepath.Join(dir, b), "github.com/laurentpoirierfr/go-bff-skeleton/cmd/"+b).CombinedOutput()
			if err != nil {
				panic(fmt.Sprintf("build %s: %v\n%s", b, err, out))
			}
		}
		return m.Run()
	}()
	os.Exit(code)
}

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

type process struct {
	name string
	cmd  *exec.Cmd
	out  *syncBuffer
	done chan error
}

// start lance un binaire depuis un répertoire vide : sans configs/bff.yaml,
// seul l'environnement compte, comme en production.
func start(t *testing.T, name string, env map[string]string, args ...string) *process {
	t.Helper()
	cmd := exec.Command(filepath.Join(binaries, name), args...)
	cmd.Dir = t.TempDir()
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	p := &process{name: name, cmd: cmd, out: &syncBuffer{}, done: make(chan error, 1)}
	cmd.Stdout, cmd.Stderr = p.out, p.out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { p.done <- cmd.Wait() }()
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			<-p.done
		}
		if t.Failed() {
			t.Logf("journal de %s :\n%s", name, p.out.String())
		}
	})
	return p
}

func (p *process) stop(t *testing.T) {
	t.Helper()
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-p.done:
		if err != nil {
			t.Errorf("%s: arrêt en erreur : %v", p.name, err)
		}
	case <-time.After(20 * time.Second):
		t.Errorf("%s: pas arrêté en 20 s", p.name)
	}
}

func freeAddress(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

func waitHTTP(t *testing.T, p *process, url string) {
	t.Helper()
	for range 200 {
		if r, err := http.Get(url); err == nil {
			r.Body.Close()
			if r.StatusCode < 500 {
				return
			}
		}
		select {
		case err := <-p.done:
			t.Fatalf("%s s'est arrêté : %v", p.name, err)
		case <-time.After(50 * time.Millisecond):
		}
	}
	t.Fatalf("%s ne répond pas : %s", p.name, url)
}

type response struct {
	code int
	hdr  http.Header
	body []byte
}

func (r response) json(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(r.body, &m); err != nil {
		t.Fatalf("%v : %s", err, r.body)
	}
	return m
}

func call(t *testing.T, method, url, body string) response {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var b bytes.Buffer
	_, _ = b.ReadFrom(r.Body)
	return response{code: r.StatusCode, hdr: r.Header, body: b.Bytes()}
}

func TestBFF(t *testing.T) {
	if testing.Short() {
		t.Skip("compile et lance les binaires")
	}
	usersAddr, ordersAddr, bffAddr := freeAddress(t), freeAddress(t), freeAddress(t)
	mockUsers := start(t, "mock-users", nil, "-address", usersAddr)
	mockOrders := start(t, "mock-orders", nil, "-address", ordersAddr)
	bff := start(t, "bff", map[string]string{
		"HTTP_ADDRESS":         bffAddr,
		"ROUTES_USERS_PREFIX":  "/api/users",
		"ROUTES_USERS_TARGET":  "http://" + usersAddr + "/api/v1/users",
		"ROUTES_USERS_TIMEOUT": "500ms",
		"ROUTES_ORDERS_PREFIX": "/api/orders",
		"ROUTES_ORDERS_TARGET": "http://" + ordersAddr + "/api/v1/orders",
		"TELEMETRY_ENABLED":    "false",
		"SHUTDOWN_TIMEOUT":     "5s",
	})
	waitHTTP(t, mockUsers, "http://"+usersAddr+"/api/v1/users")
	waitHTTP(t, mockOrders, "http://"+ordersAddr+"/api/v1/orders")
	waitHTTP(t, bff, "http://"+bffAddr+"/health/ready")
	base := "http://" + bffAddr

	t.Run("proxy", func(t *testing.T) {
		if r := call(t, "GET", base+"/api/users", ""); r.code != 200 || len(r.json(t)["items"].([]any)) < 3 || r.hdr.Get("X-Backend") != "mock-users" {
			t.Errorf("liste : %d %s", r.code, r.body)
		}
		if r := call(t, "POST", base+"/api/users", `{"name":"Barbara Liskov","email":"barbara@example.com"}`); r.code != 201 || r.json(t)["name"] != "Barbara Liskov" {
			t.Errorf("création : %d %s", r.code, r.body)
		}
		if r := call(t, "GET", base+"/api/orders?user_id=1", ""); r.code != 200 || len(r.json(t)["items"].([]any)) != 2 {
			t.Errorf("commandes filtrées : %d %s", r.code, r.body)
		}
		if r := call(t, "GET", base+"/api/nothing", ""); r.code != 404 || r.hdr.Get("Content-Type") != "application/problem+json" {
			t.Errorf("route inconnue : %d %s", r.code, r.body)
		}
	})

	t.Run("composition", func(t *testing.T) {
		r := call(t, "GET", base+"/api/dashboard", "")
		if d := r.json(t); r.code != 200 || d["order_count"] != 4.0 || d["user_count"].(float64) < 3 {
			t.Errorf("tableau de bord : %d %s", r.code, r.body)
		}
		r = call(t, "GET", base+"/api/profiles/1", "")
		if p := r.json(t); r.code != 200 || p["partial"] != false || len(p["orders"].([]any)) != 2 {
			t.Errorf("profil : %d %s", r.code, r.body)
		}
	})

	t.Run("frontend", func(t *testing.T) {
		if _, err := os.Stat("../../frontend/dist/index.html"); err != nil {
			t.Skip("frontend non construit (make web) : le binaire sert la page d'avertissement")
		}
		r := call(t, "GET", base+"/users/1", "")
		if r.code != 200 || !strings.Contains(string(r.body), `<div id="root">`) || r.hdr.Get("Content-Security-Policy") == "" {
			t.Errorf("lien direct : %d %v", r.code, r.hdr)
		}
	})

	t.Run("pannes", func(t *testing.T) {
		mockOrders.stop(t)
		r := call(t, "GET", base+"/api/profiles/1", "")
		if p := r.json(t); r.code != 200 || p["partial"] != true || p["orders"] != nil {
			t.Errorf("profil dégradé : %d %s", r.code, r.body)
		}
		if r := call(t, "GET", base+"/api/dashboard", ""); r.code != http.StatusBadGateway {
			t.Errorf("tableau de bord sans commandes : %d %s", r.code, r.body)
		}
		// mock-users répond désormais en 2 s, au-delà du délai de sa route (500 ms).
		mockUsers.stop(t)
		slow := start(t, "mock-users", nil, "-address", usersAddr, "-delay", "2s")
		waitHTTP(t, slow, "http://"+usersAddr+"/nothing")
		start := time.Now()
		if r := call(t, "GET", base+"/api/users", ""); r.code != http.StatusGatewayTimeout {
			t.Errorf("backend lent : %d %s", r.code, r.body)
		}
		if d := time.Since(start); d > 1500*time.Millisecond {
			t.Errorf("le délai de la route n'a pas été appliqué : %v", d)
		}
		slow.stop(t)
	})

	bff.stop(t)
}

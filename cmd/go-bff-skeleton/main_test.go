package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"
)

// Le serveur démarre, répond, puis s'arrête proprement quand le contexte est
// annulé.
func TestSert(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0") // un port libre
	if err != nil {
		t.Fatal(err)
	}
	adresse := ln.Addr().String()
	ln.Close()

	ctx, annule := context.WithCancel(context.Background())
	fini := make(chan error, 1)
	go func() { fini <- sert(ctx, adresse, slog.New(slog.NewTextHandler(io.Discard, nil))) }()

	var reponse *http.Response
	for range 50 { // le serveur démarre en arrière-plan
		if reponse, err = http.Get("http://" + adresse + "/sante"); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	reponse.Body.Close()
	if reponse.StatusCode != http.StatusOK {
		t.Errorf("statut %d", reponse.StatusCode)
	}

	annule()
	if err := <-fini; err != nil {
		t.Errorf("arrêt : %v", err)
	}
}

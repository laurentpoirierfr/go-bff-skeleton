package web

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSante(t *testing.T) {
	var journal bytes.Buffer
	h := Routes(slog.New(slog.NewTextHandler(&journal, nil)))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/sante", nil))
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != `{"statut":"ok"}` {
		t.Errorf("%d %s", w.Code, w.Body)
	}
	if !strings.Contains(journal.String(), "chemin=/sante statut=200") {
		t.Errorf("journal : %s", journal.String())
	}

	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/sante", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST : %d", w.Code)
	}
}

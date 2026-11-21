package web

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

var build = fstest.MapFS{
	"index.html":          {Data: []byte("<!doctype html><div id=root></div>")},
	"assets/index-abc.js": {Data: []byte(strings.Repeat("console.log('gopher');", 100))},
	"favicon.svg":         {Data: []byte("<svg></svg>")},
	".gitkeep":            {Data: nil},
}

func serveReq(t *testing.T, h http.Handler, method, path string, hdr ...string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func handler(t *testing.T) http.Handler {
	t.Helper()
	h, err := Handler(build, Options{CSP: "default-src 'self'"})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// Les chemins de l'application reçoivent index.html, jamais mis en cache, avec
// sa politique de sécurité.
func TestSPAFallback(t *testing.T) {
	h := handler(t)
	for _, p := range []string{"/", "/users", "/users/42", "/index.html"} {
		w := serveReq(t, h, "GET", p)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "id=root") || w.Header().Get("Cache-Control") != "no-cache" ||
			w.Header().Get("Content-Security-Policy") != "default-src 'self'" || w.Header().Get("Content-Type") != "text/html; charset=utf-8" {
			t.Errorf("%s: %d %v", p, w.Code, w.Header())
		}
	}
}

func TestAssets(t *testing.T) {
	h := handler(t)
	w := serveReq(t, h, "GET", "/assets/index-abc.js")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" ||
		!strings.Contains(w.Header().Get("Content-Type"), "javascript") || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("%d %v", w.Code, w.Header())
	}
	// Un fichier absent : 404, et non index.html.
	if w := serveReq(t, h, "GET", "/assets/old-123.js"); w.Code != 404 || strings.Contains(w.Body.String(), "id=root") {
		t.Errorf("missing asset: %d %s", w.Code, w.Body)
	}
	// .gitkeep n'est pas servi.
	if w := serveReq(t, h, "GET", "/.gitkeep"); w.Code != 404 {
		t.Errorf(".gitkeep: %d", w.Code)
	}
	if w := serveReq(t, h, "POST", "/"); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: %d", w.Code)
	}
}

func TestGzipAndETag(t *testing.T) {
	h := handler(t)
	w := serveReq(t, h, "GET", "/assets/index-abc.js", "Accept-Encoding", "gzip, br")
	if w.Header().Get("Content-Encoding") != "gzip" || w.Body.Len() >= len(build["assets/index-abc.js"].Data) {
		t.Fatalf("not compressed: %v, %d bytes", w.Header(), w.Body.Len())
	}
	zr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := io.ReadAll(zr); string(b) != string(build["assets/index-abc.js"].Data) {
		t.Error("gzip content differs")
	}
	etag := w.Header().Get("ETag")
	if w := serveReq(t, h, "GET", "/assets/index-abc.js", "If-None-Match", etag); w.Code != http.StatusNotModified || w.Body.Len() != 0 {
		t.Errorf("If-None-Match: %d", w.Code)
	}
	// Un petit fichier n'est pas compressé.
	if w := serveReq(t, h, "GET", "/favicon.svg", "Accept-Encoding", "gzip"); w.Header().Get("Content-Encoding") != "" {
		t.Errorf("small file compressed")
	}
}

// Sans build, une page explique quoi faire.
func TestNotBuilt(t *testing.T) {
	h, err := Handler(fstest.MapFS{".gitkeep": {}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if w := serveReq(t, h, "GET", "/"); w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "make web") {
		t.Errorf("%d %s", w.Code, w.Body)
	}
}

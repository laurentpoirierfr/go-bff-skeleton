// Package web sert l'application React embarquée : les fichiers du build, et
// index.html pour les chemins de l'application (/users/1), que le routeur du
// navigateur traite.
package web

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

// Options règle le service des pages.
type Options struct {
	CSP string // Content-Security-Policy des pages
}

type file struct {
	data, gz    []byte // le contenu, et sa version gzip (nil si inutile)
	contentType string
	etag        string
}

// Handler charge le build en mémoire, une fois (il ne change pas), et le sert.
func Handler(fsys fs.FS, o Options) (http.Handler, error) {
	files := map[string]*file{}
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path.Base(p) == ".gitkeep" {
			return err
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		f := &file{data: data, contentType: mime.TypeByExtension(path.Ext(p)), etag: `"` + hex.EncodeToString(sum[:8]) + `"`}
		if f.contentType == "" {
			f.contentType = http.DetectContentType(data)
		}
		if compressible(f.contentType) && len(data) > 1024 {
			f.gz = gzipped(data)
		}
		files["/"+p] = f
		return nil
	})
	if err != nil {
		return nil, err
	}
	index := files["/index.html"]
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		p := path.Clean(r.URL.Path)
		if f, ok := files[p]; ok && p != "/index.html" {
			cache := "public, max-age=3600"
			if strings.HasPrefix(p, "/assets/") {
				// Le nom porte une empreinte du contenu : il ne change jamais.
				cache = "public, max-age=31536000, immutable"
			}
			serve(w, r, f, cache)
			return
		}
		if path.Ext(p) != "" && p != "/index.html" {
			// Un fichier absent (un ancien script…) : une vraie 404, et non la
			// page de l'application, que le navigateur prendrait pour du JS.
			http.NotFound(w, r)
			return
		}
		if index == nil {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("frontend not built: run « make web », then rebuild the BFF\n"))
			return
		}
		// Un chemin de l'application : index.html, jamais mis en cache, pour
		// qu'un déploiement soit vu aussitôt.
		w.Header().Set("Content-Security-Policy", o.CSP)
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		serve(w, r, index, "no-cache")
	}), nil
}

// serve écrit un fichier, compressé si le client l'accepte, et répond 304 si
// le client en a déjà la version courante.
func serve(w http.ResponseWriter, r *http.Request, f *file, cache string) {
	h := w.Header()
	h.Set("Content-Type", f.contentType)
	h.Set("Cache-Control", cache)
	h.Set("ETag", f.etag)
	h.Add("Vary", "Accept-Encoding")
	if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, f.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	body := f.data
	if f.gz != nil && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		h.Set("Content-Encoding", "gzip")
		body = f.gz
	}
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(body)
}

func compressible(contentType string) bool {
	for _, t := range []string{"text/", "javascript", "json", "svg", "xml"} {
		if strings.Contains(contentType, t) {
			return true
		}
	}
	return false
}

func gzipped(data []byte) []byte {
	var b bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&b, gzip.BestCompression)
	_, _ = zw.Write(data)
	_ = zw.Close()
	return b.Bytes()
}

package web

import (
	"crypto/rand"
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var embeddedAssets embed.FS

const placeholderIndex = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>CouchFusion</title></head><body><div id="__nuxt">CouchFusion UI</div></body></html>`

func staticHandler() http.Handler {
	dist, _ := fs.Sub(embeddedAssets, "dist")
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			http.Error(writer, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean(request.URL.Path), "/")
		if name != "." && name != "" && !strings.HasPrefix(path.Base(name), ".") {
			if data, err := fs.ReadFile(dist, name); err == nil {
				if path.Ext(name) == ".html" {
					writeSPAHTML(writer, request, data)
					return
				}
				if contentType := mime.TypeByExtension(path.Ext(name)); contentType != "" {
					writer.Header().Set("Content-Type", contentType)
				}
				writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				writer.WriteHeader(http.StatusOK)
				if request.Method != http.MethodHead {
					_, _ = writer.Write(data)
				}
				return
			}
			if path.Ext(name) != "" {
				http.NotFound(writer, request)
				return
			}
		}
		index, err := fs.ReadFile(dist, "index.html")
		if err != nil {
			index = []byte(placeholderIndex)
		}
		writeSPAHTML(writer, request, index)
	})
}

func writeSPAHTML(writer http.ResponseWriter, request *http.Request, document []byte) {
	nonce, err := randomToken(rand.Reader)
	if err != nil {
		http.Error(writer, "Could not secure UI response", http.StatusInternalServerError)
		return
	}
	document = []byte(strings.ReplaceAll(string(document), "<script", `<script nonce="`+nonce+`"`))
	writer.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; frame-ancestors 'none'; frame-src http://127.0.0.1:* http://localhost:*; object-src 'none'; connect-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self' 'nonce-"+nonce+"'")
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.WriteHeader(http.StatusOK)
	if request.Method != http.MethodHead {
		_, _ = writer.Write(document)
	}
}

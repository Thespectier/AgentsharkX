// Package web serves the production console from the AgentsharkX binary.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed static
var staticFiles embed.FS

type handler struct {
	api   http.Handler
	root  fs.FS
	files http.Handler
}

func New(api http.Handler) http.Handler {
	root, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic(err)
	}
	return &handler{api: api, root: root, files: http.FileServer(http.FS(root))}
}

func (handler *handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.URL.Path == "/healthz" || strings.HasPrefix(request.URL.Path, "/api/") {
		handler.api.ServeHTTP(writer, request)
		return
	}
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		handler.api.ServeHTTP(writer, request)
		return
	}

	requested := strings.TrimPrefix(path.Clean("/"+request.URL.Path), "/")
	if requested != "." && requested != "" {
		if info, err := fs.Stat(handler.root, requested); err == nil && !info.IsDir() {
			if strings.HasPrefix(requested, "assets/") {
				writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			handler.files.ServeHTTP(writer, request)
			return
		}
	}

	index, err := fs.ReadFile(handler.root, "index.html")
	if err != nil {
		http.Error(writer, "console asset unavailable", http.StatusServiceUnavailable)
		return
	}
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	if request.Method != http.MethodHead {
		_, _ = writer.Write(index)
	}
}

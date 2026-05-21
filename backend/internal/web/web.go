package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// Dist contains the production web client. The release build scripts replace
// backend/internal/web/dist with apps/web/dist before compiling the Go binary.
//
//go:embed dist
var Dist embed.FS

func Handler() http.Handler {
	distFS, err := fs.Sub(Dist, "dist")
	if err != nil {
		panic(err)
	}

	fileServer := http.FileServer(http.FS(distFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}

		cleanPath := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if cleanPath != "." && cleanPath != "" {
			file, err := distFS.Open(cleanPath)
			if err == nil {
				defer file.Close()
				info, statErr := file.Stat()
				if statErr == nil && !info.IsDir() {
					fileServer.ServeHTTP(w, r)
					return
				}
			}
		}

		spaRequest := new(http.Request)
		*spaRequest = *r
		spaURL := *r.URL
		spaURL.Path = "/"
		spaRequest.URL = &spaURL
		fileServer.ServeHTTP(w, spaRequest)
	})
}

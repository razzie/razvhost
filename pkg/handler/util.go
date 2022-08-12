package handler

import (
	"net/http"
	"strings"

	"github.com/razzie/razvhost/pkg/stream"
)

func handlePathCombinations(handler http.Handler, hostname, hostPath, targetPath string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = strings.TrimPrefix(r.URL.Path, hostPath)
		r.URL.RawPath = strings.TrimPrefix(r.URL.RawPath, hostPath)
		if !strings.HasPrefix(r.URL.Path, "/") {
			r.URL.Path = "/" + r.URL.Path
			r.URL.RawPath = "/" + r.URL.RawPath
		}
		ww := stream.NewPathPrefixHTMLResponseWriter(hostname, hostPath, targetPath, w)
		defer ww.Close()
		handler.ServeHTTP(ww, r)
	})
}

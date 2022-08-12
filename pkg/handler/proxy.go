package handler

import (
	"net/http"
	"net/http/httputil"
	"net/url"
)

func newProxyHandler(hostname, hostPath string, target url.URL) http.Handler {
	handler := httputil.NewSingleHostReverseProxy(&target)
	return handlePathCombinations(handler, hostname, hostPath, target.Path)
}

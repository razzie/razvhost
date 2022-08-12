package handler

import (
	"net/http"
	"net/url"
	"strings"
)

func newRedirectHandler(hostname, hostPath string, target url.URL) http.Handler {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetQuery := target.RawQuery
		redirURL := *r.URL
		redirURL.Host = target.Host
		redirURL.Path, redirURL.RawPath = joinURLPath(&target, &redirURL)
		if targetQuery == "" || redirURL.RawQuery == "" {
			redirURL.RawQuery = targetQuery + redirURL.RawQuery
		} else {
			redirURL.RawQuery = targetQuery + "&" + redirURL.RawQuery
		}
		http.Redirect(w, r, redirURL.String(), http.StatusSeeOther)
	})
	return handlePathCombinations(handler, "", hostPath, "")
}

func joinURLPath(a, b *url.URL) (path, rawpath string) {
	if a.RawPath == "" && b.RawPath == "" {
		return singleJoiningSlash(a.Path, b.Path), ""
	}

	apath := a.EscapedPath()
	bpath := b.EscapedPath()

	aslash := strings.HasSuffix(apath, "/")
	bslash := strings.HasPrefix(bpath, "/")

	switch {
	case aslash && bslash:
		return a.Path + b.Path[1:], apath + bpath[1:]
	case !aslash && !bslash:
		return a.Path + "/" + b.Path, apath + "/" + bpath
	}
	return a.Path + b.Path, apath + bpath
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}
	return a + b
}

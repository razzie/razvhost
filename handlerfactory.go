package razvhost

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/yookoala/gofast"
)

// HandlerFactory ...
type HandlerFactory struct {
	phpClientFactory gofast.ClientFactory
}

// NewHandlerFactory returns a new HandlerFactory
func NewHandlerFactory(phpaddr *url.URL) *HandlerFactory {
	hf := &HandlerFactory{}
	if phpaddr != nil {
		hf.setupPHP(phpaddr)
	}
	return hf
}

// Handler ...
func (hf *HandlerFactory) Handler(hostname string, target url.URL) (handler http.Handler, err error) {
	hostname, hostPath := splitHostnameAndPath(hostname)
	switch target.Scheme {
	case "file":
		handler = hf.newFileServer(hostname, hostPath, target.Host+target.Path)
	case "http", "https":
		handler = hf.newProxyHandler(hostname, hostPath, target)
	case "redirect":
		handler = hf.newRedirectHandler(hostname, hostPath, target)
	case "php":
		handler, err = hf.newPHPHandler(hostname, hostPath, target.Host+target.Path)
	default:
		err = fmt.Errorf("unknown target URL scheme: %s", target.Scheme)
	}
	return
}

func (hf *HandlerFactory) newFileServer(hostname, hostPath, dir string) http.Handler {
	handler := FileServer(Directory(dir))
	return handlePathCombinations(handler, hostname, hostPath, "")
}

func (hf *HandlerFactory) newProxyHandler(hostname, hostPath string, target url.URL) http.Handler {
	handler := httputil.NewSingleHostReverseProxy(&target)
	return handlePathCombinations(handler, hostname, hostPath, target.Path)
}

func (hf *HandlerFactory) newRedirectHandler(hostname, hostPath string, target url.URL) http.Handler {
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
	return handlePathCombinations(handler, hostname, hostPath, "")
}

func (hf *HandlerFactory) setupPHP(cgiaddr *url.URL) {
	network := cgiaddr.Scheme
	address := cgiaddr.Host
	if len(address) == 0 {
		address = cgiaddr.Path
	}
	connFactory := gofast.SimpleConnFactory(network, address)
	hf.phpClientFactory = gofast.SimpleClientFactory(connFactory, 0)
}

func (hf *HandlerFactory) newPHPHandler(hostname, hostPath, endpoint string) (http.Handler, error) {
	if hf.phpClientFactory == nil {
		return nil, fmt.Errorf("PHP not configured")
	}
	fi, err := os.Stat(endpoint)
	if err != nil {
		return nil, err
	}
	endpoint, err = filepath.Abs(endpoint)
	if err != nil {
		return nil, err
	}
	var targetPath string
	var sessHandler gofast.SessionHandler
	if fi.IsDir() {
		sessHandler = gofast.NewPHPFS(endpoint)(gofast.BasicSession)
		targetPath = endpoint
	} else {
		sessHandler = gofast.NewFileEndpoint(endpoint)(gofast.BasicSession)
		targetPath = filepath.Dir(endpoint)
	}
	handler := gofast.NewHandler(sessHandler, hf.phpClientFactory)
	return handlePathCombinations(handler, hostname, hostPath, targetPath), nil
}

func handlePathCombinations(handler http.Handler, hostname, hostPath, targetPath string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = strings.TrimPrefix(r.URL.Path, hostPath)
		r.URL.RawPath = strings.TrimPrefix(r.URL.RawPath, hostPath)
		if !strings.HasPrefix(r.URL.Path, "/") {
			r.URL.Path = "/" + r.URL.Path
			r.URL.RawPath = "/" + r.URL.RawPath
		}
		ww := NewPathPrefixHTMLResponseWriter(hostname, hostPath, targetPath, w, nil)
		defer ww.Close()
		handler.ServeHTTP(ww, r)
	})
}

func splitHostnameAndPath(hostname string) (string, string) {
	i := strings.Index(hostname, "/")
	if i == -1 {
		return hostname, ""
	}
	return hostname[:i], hostname[i:]
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

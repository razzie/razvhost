package razvhost

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
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
	hostname, path := splitHostnameAndPath(hostname)
	switch target.Scheme {
	case "file":
		handler = FileServer(Directory(target.Host+target.Path), path)
	case "http", "https":
		handler = hf.newProxyHandler(path, target)
	case "redirect":
		handler = hf.newRedirectHandler(target)
	case "php":
		handler, err = hf.newPHPHandler(path, target.Host+target.Path)
	default:
		err = fmt.Errorf("unknown target URL scheme: %s", target.Scheme)
	}
	return
}

func (hf *HandlerFactory) newProxyHandler(path string, target url.URL) http.Handler {
	targetQuery := target.RawQuery
	director := func(req *http.Request) {
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.URL.Path, req.URL.RawPath = joinURLPath(&target, req.URL)
		if targetQuery == "" || req.URL.RawQuery == "" {
			req.URL.RawQuery = targetQuery + req.URL.RawQuery
		} else {
			req.URL.RawQuery = targetQuery + "&" + req.URL.RawQuery
		}
		if _, ok := req.Header["User-Agent"]; !ok {
			// explicitly disable User-Agent so it's not set to default value
			req.Header.Set("User-Agent", "")
		}
	}
	var modifyResponse func(*http.Response) error
	if len(path) > 0 {
		modifyResponse = func(resp *http.Response) error {
			if ctype := resp.Header.Get("Content-Type"); strings.HasPrefix(ctype, "text/html") {
				resp.Header.Del("Content-Length")
				resp.ContentLength = -1
				resp.Body = NewPathPrefixHTMLStreamer(path, resp.Body)
			}
			if location := resp.Header.Get("Location"); len(location) > 0 {
				resp.Header.Set("Location", path+location)
			}
			return nil
		}
	}
	return &httputil.ReverseProxy{
		Director:       director,
		ModifyResponse: modifyResponse,
	}
}

func (hf *HandlerFactory) newRedirectHandler(target url.URL) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

func (hf *HandlerFactory) newPHPHandler(path, endpoint string) (http.Handler, error) {
	if hf.phpClientFactory == nil {
		return nil, fmt.Errorf("PHP not configured")
	}
	fi, err := os.Stat(endpoint)
	if err != nil {
		return nil, err
	}
	var sessHandler gofast.SessionHandler
	if fi.IsDir() {
		sessHandler = gofast.NewPHPFS(endpoint)(gofast.BasicSession)
	} else {
		sessHandler = gofast.NewFileEndpoint(endpoint)(gofast.BasicSession)
	}
	handler := gofast.NewHandler(sessHandler, hf.phpClientFactory)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := NewPathPrefixHTMLResponseWriter(path, w)
		defer ww.Close()
		handler.ServeHTTP(ww, r)
	}), nil
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

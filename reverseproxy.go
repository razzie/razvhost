package razvhost

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
)

// ProxyEvent ...
type ProxyEvent struct {
	Hostname string
	Target   url.URL
	Up       bool
}

// ReverseProxy ...
type ReverseProxy struct {
	mtx     sync.Mutex
	proxies map[string]*mux
}

// Listen listens to proxy events
func (p *ReverseProxy) Listen(events <-chan ProxyEvent) {
	p.mtx.Lock()
	if p.proxies == nil {
		p.proxies = make(map[string]*mux)
	}
	p.mtx.Unlock()

	for e := range events {
		p.processEvent(e)
	}
}

// Process processes a list of proxy events
func (p *ReverseProxy) Process(events []ProxyEvent) {
	p.mtx.Lock()
	if p.proxies == nil {
		p.proxies = make(map[string]*mux)
	}
	p.mtx.Unlock()

	for _, e := range events {
		p.processEvent(e)
	}
}

func (p *ReverseProxy) processEvent(e ProxyEvent) {
	host, path := splitHostnameAndPath(e.Hostname)

	if !e.Up {
		p.mtx.Lock()
		m := p.proxies[host]
		p.mtx.Unlock()
		if m != nil {
			m.remove(path, e.Target)
		}
		return
	}

	_, handler, err := newHandler(e.Hostname, e.Target)
	if err != nil {
		log.Println(err)
		return
	}

	p.mtx.Lock()
	m := p.proxies[host]
	if m == nil {
		m = new(mux)
		p.proxies[host] = m
	}
	p.mtx.Unlock()

	m.add(path, handler, e.Target)
}

// ValidateHost implements autocert.HostPolicy
func (p *ReverseProxy) ValidateHost(ctx context.Context, host string) error {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	if _, ok := p.proxies[host]; !ok {
		return fmt.Errorf("unknown hostname: %s", host)
	}
	return nil
}

func (p *ReverseProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Println(r.RemoteAddr, "->", r.Method, r.Host, r.RequestURI)

	p.mtx.Lock()
	m, ok := p.proxies[r.Host]
	p.mtx.Unlock()
	if !ok {
		http.Error(w, "Unknown hostname in request: "+r.Host, http.StatusForbidden)
		return
	}
	if handler := m.handler(r.URL.Path); handler != nil {
		handler.ServeHTTP(w, r)
		return
	}
	http.Error(w, "Cannot serve path: "+r.URL.Path, http.StatusForbidden)
}

var junkProxyHeaders = []string{
	"x-client-ip",
	"cf-connecting-ip",
	"fastly-client-ip",
	"true-client-ip",
	"x-real-ip",
	"x-cluster-client-ip",
	"x-forwarded",
	"forwarded-for",
	"forwarded",
}

func newDirector(target url.URL) func(req *http.Request) {
	return func(req *http.Request) {
		for _, h := range junkProxyHeaders {
			req.Header.Del(h)
		}
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
	}
}

type redirectHandler struct {
	targetURL url.URL
}

func (redir *redirectHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	target := redir.targetURL
	target.Scheme = r.URL.Scheme
	target.Path = r.URL.Path
	http.Redirect(w, r, target.String(), http.StatusSeeOther)
}

func splitHostnameAndPath(hostname string) (string, string) {
	i := strings.Index(hostname, "/")
	if i == -1 {
		return hostname, ""
	}
	return hostname[:i], hostname[i:]
}

func newHandler(hostname string, target url.URL) (path string, handler http.Handler, err error) {
	hostname, path = splitHostnameAndPath(hostname)

	switch target.Scheme {
	case "file":
		handler = http.FileServer(http.Dir(target.Path))

	case "http", "https":
		if len(target.Path) > 1 {
			err = fmt.Errorf("paths are unsupported in http/https target URLs (%v)", target)
			return
		}
		rproxy := httputil.NewSingleHostReverseProxy(&target)
		rproxy.Director = newDirector(target)
		handler = rproxy

	case "redirect":
		if len(target.Path) > 1 {
			err = fmt.Errorf("paths are unsupported in redirect target URLs (%v)", target)
			return
		}
		handler = &redirectHandler{targetURL: target}

	default:
		err = fmt.Errorf("unknown target URL scheme: %s", target.Scheme)
	}

	return
}

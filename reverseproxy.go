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

// DefaultDiscardHeaders ...
var DefaultDiscardHeaders = []string{
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

// ProxyEvent ...
type ProxyEvent struct {
	Hostname string
	Target   url.URL
	Up       bool
}

func (e ProxyEvent) String() string {
	str := e.Hostname + " -> " + e.Target.String()
	if e.Up {
		str += " [UP]"
	} else {
		str += " [DOWN]"
	}
	return str
}

// ReverseProxy ...
type ReverseProxy struct {
	mtx            sync.Mutex
	proxies        map[string]*mux
	DiscardHeaders []string
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
	log.Println("proxy event:", e.String())
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

	_, handler, err := p.newHandler(e.Hostname, e.Target)
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

type redirectHandler struct {
	targetURL url.URL
}

func (redir *redirectHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	target := redir.targetURL
	targetQuery := target.RawQuery
	req.URL.Scheme = target.Scheme
	req.URL.Host = target.Host
	req.URL.Path, req.URL.RawPath = joinURLPath(&target, req.URL)
	if targetQuery == "" || req.URL.RawQuery == "" {
		req.URL.RawQuery = targetQuery + req.URL.RawQuery
	} else {
		req.URL.RawQuery = targetQuery + "&" + req.URL.RawQuery
	}
	http.Redirect(w, req, req.URL.String(), http.StatusSeeOther)
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

func (p *ReverseProxy) newDirector(target url.URL) func(req *http.Request) {
	targetQuery := target.RawQuery
	return func(req *http.Request) {
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.URL.Path, req.URL.RawPath = joinURLPath(&target, req.URL)
		if targetQuery == "" || req.URL.RawQuery == "" {
			req.URL.RawQuery = targetQuery + req.URL.RawQuery
		} else {
			req.URL.RawQuery = targetQuery + "&" + req.URL.RawQuery
		}
		req.Header.Set("razvhost-remoteaddr", req.RemoteAddr)
		for _, h := range p.DiscardHeaders {
			req.Header.Del(h)
		}
		if _, ok := req.Header["User-Agent"]; !ok {
			// explicitly disable User-Agent so it's not set to default value
			req.Header.Set("User-Agent", "")
		}
	}
}

func (p *ReverseProxy) newHandler(hostname string, target url.URL) (path string, handler http.Handler, err error) {
	hostname, path = splitHostnameAndPath(hostname)

	switch target.Scheme {
	case "file":
		handler = http.FileServer(http.Dir(target.Path))

	case "http", "https":
		handler = &httputil.ReverseProxy{Director: p.newDirector(target)}

	case "redirect":
		handler = &redirectHandler{targetURL: target}

	default:
		err = fmt.Errorf("unknown target URL scheme: %s", target.Scheme)
	}

	return
}

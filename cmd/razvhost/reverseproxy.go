package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
)

// ReverseProxy ...
type ReverseProxy struct {
	mtx        sync.Mutex
	proxies    map[string]http.Handler
	proxyLists []ProxyList
}

// NewReverseProxy ...
func NewReverseProxy(l ...ProxyList) *ReverseProxy {
	return &ReverseProxy{
		proxies:    make(map[string]http.Handler),
		proxyLists: l,
	}
}

// AddProxyList ...
func (p *ReverseProxy) AddProxyList(l ProxyList) {
	p.proxyLists = append(p.proxyLists, l)
}

func (p *ReverseProxy) getTargetURL(hostname string) *url.URL {
	for _, proxyList := range p.proxyLists {
		target, ok := proxyList.GetProxy(hostname)
		if ok {
			targetURL, _ := url.Parse(target)
			return targetURL
		}
	}
	return nil
}

func (p *ReverseProxy) getProxy(hostname string) http.Handler {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	proxy := p.proxies[hostname]
	if proxy == nil {
		targetURL := p.getTargetURL(hostname)
		if targetURL == nil {
			return nil
		}
		switch targetURL.Scheme {
		case "file":
			proxy = http.FileServer(http.Dir(targetURL.Path))
		case "http", "https":
			if len(targetURL.Path) > 1 {
				panic(fmt.Errorf("paths are unsupported in http/https target URLs (%v)", targetURL))
			}
			rproxy := httputil.NewSingleHostReverseProxy(targetURL)
			rproxy.Director = makeDirector(targetURL)
			proxy = rproxy
		case "redirect":
			if len(targetURL.Path) > 1 {
				panic(fmt.Errorf("paths are unsupported in redirect target URLs (%v)", targetURL))
			}
			proxy = &redirectHandler{targetURL: *targetURL}
		default:
			panic(fmt.Errorf("unknown target URL scheme: %s", targetURL.Scheme))
		}
		if proxy != nil {
			p.proxies[hostname] = proxy
		}
	}
	return proxy
}

// ValidateHost implements autocert.HostPolicy
func (p *ReverseProxy) ValidateHost(ctx context.Context, host string) error {
	url := p.getTargetURL(host)
	if url == nil {
		return fmt.Errorf("unknown hostname: %s", host)
	}
	return nil
}

func (p *ReverseProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	proxy := p.getProxy(r.Host)
	if proxy == nil {
		http.Error(w, "Unknown hostname in request: "+r.Host, http.StatusNotFound)
		return
	}
	proxy.ServeHTTP(w, r)
}

func makeDirector(target *url.URL) func(req *http.Request) {
	return func(req *http.Request) {
		checklist := []string{
			"x-client-ip",         // Standard headers used by Amazon EC2, Heroku, and others.
			"x-forwarded-for",     // Load-balancers (AWS ELB) or proxies.
			"cf-connecting-ip",    // @see https://support.cloudflare.com/hc/en-us/articles/200170986-How-does-Cloudflare-handle-HTTP-Request-headers-
			"fastly-client-ip",    // Fastly and Firebase hosting header (When forwared to cloud function)
			"true-client-ip",      // Akamai and Cloudflare: True-Client-IP.
			"x-real-ip",           // Default nginx proxy/fcgi; alternative to x-forwarded-for, used by some proxies.
			"x-cluster-client-ip", // (Rackspace LB and Riverbed's Stingray) http://www.rackspace.com/knowledge_center/article/controlling-access-to-linux-cloud-sites-based-on-the-client-ip-address
			"x-forwarded",
			"forwarded-for",
			"forwarded",
		}

		for _, h := range checklist {
			req.Header.Del(h)
		}
		req.Header.Add("X-Forwarded-For", req.RemoteAddr)
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
	http.Redirect(w, r, target.String(), http.StatusSeeOther)
}

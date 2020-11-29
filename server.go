package razvhost

import (
	"context"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/mssola/user_agent"
	"golang.org/x/crypto/acme/autocert"
)

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

// ServerConfig ...
type ServerConfig struct {
	ConfigFile        string
	CertsDir          string
	NoCert            bool
	WatchDockerEvents bool
	EnableHTTP2       bool
	DiscardHeaders    []string
	ExtraHeaders      map[string]string
	PHPAddr           string
}

// Server ...
type Server struct {
	mtx     sync.RWMutex
	proxies map[string]*Mux
	config  ServerConfig
	factory *HandlerFactory
}

// NewServer ...
func NewServer(cfg *ServerConfig) *Server {
	phpaddr, err := url.Parse(cfg.PHPAddr)
	if err != nil {
		log.Println(err)
	}
	srv := &Server{
		config:  *cfg,
		factory: NewHandlerFactory(phpaddr),
	}
	if len(cfg.ConfigFile) > 0 {
		if err := srv.loadConfig(); err != nil {
			log.Println(err)
		}
	}
	if cfg.WatchDockerEvents {
		if err := srv.watchDockerEvents(); err != nil {
			log.Println(err)
		}
	}
	return srv
}

// Listen listens to proxy events
func (s *Server) Listen(events <-chan ProxyEvent) {
	s.mtx.Lock()
	if s.proxies == nil {
		s.proxies = make(map[string]*Mux)
	}
	s.mtx.Unlock()

	for e := range events {
		s.processEvent(e)
	}
}

// Process processes a list of proxy events
func (s *Server) Process(events []ProxyEvent) {
	s.mtx.Lock()
	if s.proxies == nil {
		s.proxies = make(map[string]*Mux)
	}
	s.mtx.Unlock()

	for _, e := range events {
		s.processEvent(e)
	}
}

func (s *Server) processEvent(e ProxyEvent) {
	log.Println("CONFIG:", e.String())
	host, path := splitHostnameAndPath(e.Hostname)

	if !e.Up {
		s.mtx.RLock()
		m := s.proxies[host]
		s.mtx.RUnlock()
		if m != nil {
			m.Remove(path, e.Target.String())
		}
		return
	}

	handler, err := s.factory.Handler(e.Hostname, e.Target)
	if err != nil {
		log.Println(err)
		return
	}

	s.mtx.Lock()
	m := s.proxies[host]
	if m == nil {
		m = new(Mux)
		s.proxies[host] = m
	}
	s.mtx.Unlock()

	m.Add(path, handler, e.Target.String())
}

// ValidateHost implements autocert.HostPolicy
func (s *Server) ValidateHost(ctx context.Context, host string) error {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	if _, ok := s.proxies[host]; !ok {
		return fmt.Errorf("unknown hostname: %s", host)
	}
	return nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mtx.RLock()
	m, ok := s.proxies[r.Host]
	s.mtx.RUnlock()
	if !ok {
		http.Error(w, "Unknown hostname in request: "+r.Host, http.StatusForbidden)
		return
	}
	if handler := m.Handler(r.URL.Path); handler != nil {
		s.updateHeaders(w, r)
		handler.ServeHTTP(w, r)
		return
	}
	http.Error(w, "Cannot serve path: "+r.URL.Path, http.StatusForbidden)
}

// Serve ...
func (s *Server) Serve() error {
	certManager := &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		Cache:      autocert.DirCache(s.config.CertsDir),
		HostPolicy: s.ValidateHost,
	}
	server := &http.Server{
		Addr:    ":443",
		Handler: LoggerMiddleware(s),
		TLSConfig: &tls.Config{
			GetCertificate: certManager.GetCertificate,
		},
	}
	if !s.config.EnableHTTP2 {
		server.TLSNextProto = make(map[string]func(*http.Server, *tls.Conn, http.Handler))
	}

	if s.config.NoCert {
		server.Addr = ":80"
		return server.ListenAndServe()
	}

	errChan := make(chan error, 1)
	go func() {
		acmeHandler := certManager.HTTPHandler(nil)
		errChan <- http.ListenAndServe(":80", LoggerMiddleware(acmeHandler))
	}()
	go func() {
		errChan <- server.ListenAndServeTLS("", "")
	}()
	return <-errChan
}

// Debug ...
func (s *Server) Debug(addr string) error {
	log.Println("Debug interface listening on", addr)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uri := strings.SplitN(r.URL.Path, "/", 3)
		if len(uri) < 2 {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		r.Host = uri[1]
		r.URL.Path = strings.TrimPrefix(r.URL.Path, "/"+r.Host)
		r.URL.RawPath = strings.TrimPrefix(r.URL.RawPath, "/"+r.Host)
		ww := NewPathPrefixHTMLResponseWriter(r.URL.Host, "/"+r.Host, "", w)
		defer ww.Close()
		s.ServeHTTP(ww, r)
	})
	return http.ListenAndServe(addr, LoggerMiddleware(handler))
}

func (s *Server) updateHeaders(w http.ResponseWriter, r *http.Request) {
	r.Header.Set("X-Forwarded-Host", r.Host)
	r.Header.Set("x-razvhost-remoteaddr", r.RemoteAddr)
	for _, h := range s.config.DiscardHeaders {
		r.Header.Del(h)
	}
	for h, value := range s.config.ExtraHeaders {
		r.Header.Add(h, value)
		w.Header().Add(h, value)
	}
}

func (s *Server) loadConfig() error {
	if len(s.config.ConfigFile) == 0 {
		return nil
	}

	events, err := ReadConfigFile(s.config.ConfigFile)
	if err != nil {
		if os.IsNotExist(err) {
			if err := ioutil.WriteFile(s.config.ConfigFile, []byte(ExampleConfig), 0777); err == nil {
				log.Println("created demo config:", s.config.ConfigFile)
			}
			return nil
		}
		return err
	}

	s.Process(events)
	return nil
}

func (s *Server) watchDockerEvents() error {
	docker, err := NewDockerWatch()
	if err != nil {
		return err
	}

	events, err := docker.GetActiveContainers()
	if err != nil {
		return err
	}
	s.Process(events)

	eventsCh, err := docker.GetProxyEvents()
	if err != nil {
		return err
	}
	go s.Listen(eventsCh)

	return nil
}

// LoggerMiddleware ...
func LoggerMiddleware(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua := user_agent.New(r.UserAgent())
		browser, ver := ua.Browser()
		defer log.Printf("%s %s%s - %s (%s %s %s)",
			r.Method, r.Host, r.URL.RequestURI(),
			r.RemoteAddr, ua.OS(), browser, ver)
		handler.ServeHTTP(w, r)
	})
}

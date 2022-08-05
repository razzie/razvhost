package razvhost

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/crypto/acme/autocert"
)

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

type Server struct {
	mux     Mux
	config  ServerConfig
	factory *HandlerFactory
}

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
func (s *Server) Listen(events <-chan []ProxyEvent) {
	for e := range events {
		s.ProcessEvents(e)
	}
}

// ProcessEvents processes a list of proxy events
func (s *Server) ProcessEvents(events []ProxyEvent) {
	for _, e := range events {
		s.ProcessEvent(e)
	}
}

// ProcessEvent processes a single proxy event
func (s *Server) ProcessEvent(e ProxyEvent) {
	log.Println("CONFIG:", e.String())

	if !e.Up {
		s.mux.Remove(e.Hostname, e.Target.String())
		return
	}

	handler, err := s.factory.Handler(e.Hostname, e.Target)
	if err != nil {
		log.Println(err)
		e.Up = false
		log.Println("CONFIG:", e.String())
		return
	}

	s.mux.Add(e.Hostname, handler, e.Target.String())
}

// ValidateHost implements autocert.HostPolicy
func (s *Server) ValidateHost(ctx context.Context, host string) error {
	if !s.mux.ContainsHost(host) {
		return fmt.Errorf("unknown hostname: %s", host)
	}
	return nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if handler := s.mux.Handler(r.Host + r.URL.Path); handler != nil {
		s.updateHeaders(w, r)
		handler.ServeHTTP(w, r)
		return
	}
	http.Error(w, "Cannot serve path: "+r.Host+r.URL.Path, http.StatusForbidden)
}

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

	cfg, err := NewConfig(s.config.ConfigFile)
	if err != nil {
		return err
	}
	go s.Listen(cfg.C)

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
	s.ProcessEvents(events)

	eventsCh, err := docker.GetProxyEvents()
	if err != nil {
		return err
	}
	go s.Listen(eventsCh)

	return nil
}

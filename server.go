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
	mux            Mux
	config         ServerConfig
	internalServer *http.Server
	certManager    *autocert.Manager
	factory        *HandlerFactory
}

func NewServer(cfg ServerConfig) *Server {
	s := &Server{
		config: cfg,
	}

	// set up internal server
	s.certManager = &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		Cache:      autocert.DirCache(s.config.CertsDir),
		HostPolicy: s.ValidateHost,
	}
	s.internalServer = &http.Server{
		Addr:    ":443",
		Handler: LoggerMiddleware(s),
		TLSConfig: &tls.Config{
			GetCertificate: s.certManager.GetCertificate,
		},
	}
	if !cfg.EnableHTTP2 {
		s.internalServer.TLSNextProto = make(map[string]func(*http.Server, *tls.Conn, http.Handler))
	}

	// set up handler factory
	phpaddr, err := url.Parse(cfg.PHPAddr)
	if err != nil {
		log.Println(err)
	}
	s.factory = NewHandlerFactory(phpaddr)

	// get config
	if len(cfg.ConfigFile) > 0 {
		if err := s.loadConfig(); err != nil {
			log.Println(err)
		}
	}
	if cfg.WatchDockerEvents {
		if err := s.watchDockerEvents(); err != nil {
			log.Println(err)
		}
	}

	return s
}

// Listen listens to config events
func (s *Server) Listen(events <-chan []ConfigEvent) {
	for e := range events {
		s.ProcessEvents(e)
	}
}

// ProcessEvents processes a list of config events
func (s *Server) ProcessEvents(events []ConfigEvent) {
	for _, e := range events {
		s.ProcessEvent(e)
	}
}

// ProcessEvent processes a single config event
func (s *Server) ProcessEvent(e ConfigEvent) {
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
	if s.config.NoCert {
		s.internalServer.Addr = ":80"
		return s.internalServer.ListenAndServe()
	}

	errChan := make(chan error, 1)
	go func() {
		acmeHandler := s.certManager.HTTPHandler(nil)
		errChan <- http.ListenAndServe(":80", LoggerMiddleware(acmeHandler))
	}()
	go func() {
		errChan <- s.internalServer.ListenAndServeTLS("", "")
	}()
	return <-errChan
}

func (s *Server) Shutdown() error {
	return s.internalServer.Shutdown(context.Background())
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

	eventsCh, err := docker.GetConfigEvents()
	if err != nil {
		return err
	}
	go s.Listen(eventsCh)

	return nil
}

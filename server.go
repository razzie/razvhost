package razvhost

import (
	"crypto/tls"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"

	"golang.org/x/crypto/acme/autocert"
)

// ServerConfig ...
type ServerConfig struct {
	ConfigFile        string
	CertsDir          string
	WatchDockerEvents bool
	EnableHTTP2       bool
	DiscardHeaders    []string
}

// Server ...
type Server struct {
	config      ServerConfig
	proxies     *ReverseProxy
	certManager *autocert.Manager
}

// NewServer ...
func NewServer(cfg *ServerConfig) *Server {
	proxies := &ReverseProxy{
		DiscardHeaders: cfg.DiscardHeaders,
	}
	certManager := &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		Cache:      autocert.DirCache(cfg.CertsDir),
		HostPolicy: proxies.ValidateHost,
	}
	srv := &Server{
		config:      *cfg,
		proxies:     proxies,
		certManager: certManager,
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

// Serve ...
func (s *Server) Serve() error {
	server := &http.Server{
		Addr:    ":443",
		Handler: s.proxies,
		TLSConfig: &tls.Config{
			GetCertificate: s.certManager.GetCertificate,
		},
	}
	if !s.config.EnableHTTP2 {
		server.TLSNextProto = make(map[string]func(*http.Server, *tls.Conn, http.Handler))
	}

	errChan := make(chan error, 1)
	go func() {
		errChan <- http.ListenAndServe(":80", s.certManager.HTTPHandler(nil))
	}()
	go func() {
		errChan <- server.ListenAndServeTLS("", "")
	}()
	return <-errChan
}

// Debug ...
func (s *Server) Debug(addr string) error {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.SplitN(r.URL.Path, "/", 3)
		if len(parts) < 2 {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		r.Host = parts[1]
		r.URL.Path = strings.TrimPrefix(r.URL.Path, "/"+r.Host)
		s.proxies.ServeHTTP(&redirectHook{w: w, prefix: "/" + r.Host}, r)
	})
	return http.ListenAndServe(addr, handler)
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

	s.proxies.Process(events)
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
	s.proxies.Process(events)

	eventsCh, err := docker.GetProxyEvents()
	if err != nil {
		return err
	}
	go s.proxies.Listen(eventsCh)

	return nil
}

type redirectHook struct {
	w      http.ResponseWriter
	prefix string
}

func (h *redirectHook) Header() http.Header {
	return h.w.Header()
}

func (h *redirectHook) Write(buf []byte) (int, error) {
	return h.w.Write(buf)
}

func (h *redirectHook) WriteHeader(statusCode int) {
	if location := h.w.Header().Get("Location"); len(location) > 0 {
		h.w.Header().Set("Location", h.prefix+location)
	}
	h.w.WriteHeader(statusCode)
}

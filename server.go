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
}

// Server ...
type Server struct {
	config      *Config
	docker      *DockerWatch
	proxies     *ReverseProxy
	certsDir    string
	certManager *autocert.Manager
	server      *http.Server
}

// NewServer ...
func NewServer(cfg *ServerConfig) *Server {
	var config *Config
	if len(cfg.ConfigFile) > 0 {
		config = NewConfig()
		err := config.ReadFromFile(cfg.ConfigFile)
		if err != nil {
			if os.IsNotExist(err) {
				exampleConfig := []string{
					"example.com example2.com -> http://localhost:8080",
					"fileexample.com -> file:///var/www/public/",
					"redirect.com -> redirect://github.com",
				}
				if err := ioutil.WriteFile(cfg.ConfigFile, []byte(strings.Join(exampleConfig, "\n")), 0777); err != nil {
					log.Println("created demo config:", cfg.ConfigFile)
				}
			} else {
				log.Println(err)
			}
		}
	}

	var docker *DockerWatch
	if cfg.WatchDockerEvents {
		var err error
		docker, err = NewDockerWatch()
		if err != nil {
			log.Println(err)
		}
	}

	proxies := NewReverseProxy()
	if config != nil {
		proxies.AddProxyList(config)
	}
	if docker != nil {
		proxies.AddProxyList(docker)
	}

	certManager := &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		Cache:      autocert.DirCache(cfg.CertsDir),
		HostPolicy: proxies.ValidateHost,
	}

	server := &http.Server{
		Addr:    ":443",
		Handler: proxies,
		TLSConfig: &tls.Config{
			GetCertificate: certManager.GetCertificate,
		},
	}

	if !cfg.EnableHTTP2 {
		server.TLSNextProto = make(map[string]func(*http.Server, *tls.Conn, http.Handler))
	}

	return &Server{
		config:      config,
		docker:      docker,
		proxies:     proxies,
		certsDir:    cfg.CertsDir,
		certManager: certManager,
		server:      server,
	}
}

// Serve ...
func (s *Server) Serve() error {
	errChan := make(chan error, 1)
	go func() {
		errChan <- http.ListenAndServe(":80", s.certManager.HTTPHandler(nil))
	}()
	go func() {
		errChan <- s.server.ListenAndServeTLS("", "")
	}()
	return <-errChan
}

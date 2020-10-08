package main

import (
	"crypto/tls"
	"log"
	"net/http"

	"golang.org/x/crypto/acme/autocert"
)

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
func NewServer(certsDir string, dockerWatch bool) *Server {
	config := NewConfig()
	err := config.ReadFromFile("config")
	if err != nil {
		log.Println(err)
	}

	var docker *DockerWatch
	if dockerWatch {
		docker, err = NewDockerWatch()
		if err != nil {
			log.Println(err)
		}
	}

	proxies := NewReverseProxy(config)
	if docker != nil {
		proxies.AddProxyList(docker)
	}

	certManager := &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		Cache:      autocert.DirCache(certsDir),
		HostPolicy: proxies.ValidateHost,
	}

	server := &http.Server{
		Addr:    ":443",
		Handler: proxies,
		TLSConfig: &tls.Config{
			GetCertificate: certManager.GetCertificate,
		},
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
	}

	return &Server{
		config:      config,
		docker:      docker,
		proxies:     proxies,
		certsDir:    certsDir,
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

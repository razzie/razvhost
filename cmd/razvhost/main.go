package main

import (
	"flag"
)

// command line args
var (
	CertsDir          string
	WatchDockerEvents bool
	EnableHTTP2       bool
)

func init() {
	flag.StringVar(&CertsDir, "certs", "certs", "Directory to store certificates in")
	flag.BoolVar(&WatchDockerEvents, "docker", false, "Watch Docker events to find containers with VIRTUAL_HOST")
	flag.BoolVar(&EnableHTTP2, "http2", false, "Enable HTTP2 requests towards internal ports")
	flag.Parse()
}

func main() {
	cfg := &ServerConfig{
		CertsDir:          CertsDir,
		WatchDockerEvents: WatchDockerEvents,
		EnableHTTP2:       EnableHTTP2,
	}
	if err := NewServer(cfg).Serve(); err != nil {
		panic(err)
	}
}

package main

import (
	"flag"
)

// command line args
var (
	EnableHTTP2 bool
)

func init() {
	flag.BoolVar(&EnableHTTP2, "http2", false, "Enable HTTP2 requests towards internal ports")
	flag.Parse()
}

func main() {
	cfg := &ServerConfig{
		CertsDir:          "certs",
		WatchDockerEvents: true,
		EnableHTTP2:       EnableHTTP2,
	}
	if err := NewServer(cfg).Serve(); err != nil {
		panic(err)
	}
}

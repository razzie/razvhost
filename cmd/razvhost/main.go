package main

import (
	"flag"
	"log"
	"os"
	"strings"

	"github.com/razzie/razvhost"
)

// command line args
var (
	ConfigFile        string
	CertsDir          string
	WatchDockerEvents bool
	EnableHTTP2       bool
	DiscardHeaders    string
)

func init() {
	flag.StringVar(&ConfigFile, "cfg", "config", "Config file")
	flag.StringVar(&CertsDir, "certs", "certs", "Directory to store certificates in")
	flag.BoolVar(&WatchDockerEvents, "docker", false, "Watch Docker events to find containers with VIRTUAL_HOST")
	flag.BoolVar(&EnableHTTP2, "http2", false, "Enable HTTP2 requests towards internal ports")
	flag.StringVar(&DiscardHeaders, "discard-headers", "", "Comma separated list of http headers to discard")
	flag.Parse()

	log.SetOutput(os.Stdout)
}

func main() {
	cfg := &razvhost.ServerConfig{
		ConfigFile:        ConfigFile,
		CertsDir:          CertsDir,
		WatchDockerEvents: WatchDockerEvents,
		EnableHTTP2:       EnableHTTP2,
		DiscardHeaders:    append(strings.Split(DiscardHeaders, ","), razvhost.DefaultDiscardHeaders...),
	}
	if err := razvhost.NewServer(cfg).Serve(); err != nil {
		panic(err)
	}
}

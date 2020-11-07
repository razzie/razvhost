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
	NoCert            bool
	WatchDockerEvents bool
	EnableHTTP2       bool
	DiscardHeaders    string
	DebugAddr         string
)

func init() {
	flag.StringVar(&ConfigFile, "cfg", "config", "Config file")
	flag.StringVar(&CertsDir, "certs", "certs", "Directory to store certificates in")
	flag.BoolVar(&NoCert, "nocert", false, "Disable HTTPS and certificate handling")
	flag.BoolVar(&WatchDockerEvents, "docker", false, "Watch Docker events to find containers with VIRTUAL_HOST")
	flag.BoolVar(&EnableHTTP2, "http2", false, "Enable HTTP2 requests towards internal ports")
	flag.StringVar(&DiscardHeaders, "discard-headers", "", "Comma separated list of http headers to discard")
	flag.StringVar(&DebugAddr, "debug", "", "Debug listener address, where hostname is the first part of the URL")
	flag.Parse()

	log.SetOutput(os.Stdout)
}

func main() {
	cfg := &razvhost.ServerConfig{
		ConfigFile:        ConfigFile,
		CertsDir:          CertsDir,
		NoCert:            NoCert,
		WatchDockerEvents: WatchDockerEvents,
		EnableHTTP2:       EnableHTTP2,
		DiscardHeaders:    append(strings.Split(DiscardHeaders, ","), razvhost.DefaultDiscardHeaders...),
	}
	srv := razvhost.NewServer(cfg)
	if len(DebugAddr) > 0 {
		go log.Fatal(srv.Debug(DebugAddr))
	}
	log.Fatal(srv.Serve())
}

package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/razzie/razvhost/pkg/server"
)

// command line args
var (
	ConfigFile        string
	CertsDir          string
	NoCert            bool
	NoServerHeader    bool
	WatchDockerEvents bool
	EnableHTTP2       bool
	DiscardHeaders    string
	PHPAddr           string
	DebugAddr         string
)

var version string

var defaultDiscardHeaders = []string{
	"x-client-ip",
	"cf-connecting-ip",
	"fastly-client-ip",
	"true-client-ip",
	"x-real-ip",
	"x-cluster-client-ip",
	"x-forwarded",
	"forwarded-for",
	"forwarded",
}

func init() {
	flag.StringVar(&ConfigFile, "cfg", "config", "Config file")
	flag.StringVar(&CertsDir, "certs", "certs", "Directory to store certificates in")
	flag.BoolVar(&NoCert, "nocert", false, "Disable HTTPS and certificate handling")
	flag.BoolVar(&NoServerHeader, "no-server-header", false, "Disable 'Server: razvhost/<version>' header in responses")
	flag.BoolVar(&WatchDockerEvents, "docker", false, "Watch Docker events to find containers with VIRTUAL_HOST")
	flag.BoolVar(&EnableHTTP2, "http2", false, "Enable HTTP2")
	flag.StringVar(&DiscardHeaders, "discard-headers", "", "Comma separated list of http headers to discard")
	flag.StringVar(&PHPAddr, "php-addr", "unix:///var/run/php/php-fpm.sock", "PHP CGI address")
	flag.StringVar(&DebugAddr, "debug", "", "Debug listener address, where hostname is the first part of the URL")
	flag.Parse()

	log.SetOutput(os.Stdout)
}

func waitForSignal() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	<-sigChan
}

func main() {
	var serverHeader map[string]string
	if !NoServerHeader {
		if len(version) > 0 {
			serverHeader = map[string]string{"Server": "razvhost/" + version}
		} else {
			serverHeader = map[string]string{"Server": "razvhost"}
		}
	}

	log.Println("Starting razvhost", version)
	cfg := server.ServerConfig{
		ConfigFile:        ConfigFile,
		CertsDir:          CertsDir,
		NoCert:            NoCert,
		WatchDockerEvents: WatchDockerEvents,
		EnableHTTP2:       EnableHTTP2,
		DiscardHeaders:    append(strings.Split(DiscardHeaders, ","), defaultDiscardHeaders...),
		ExtraHeaders:      serverHeader,
		PHPAddr:           PHPAddr,
	}
	srv := server.NewServer(cfg)
	if len(DebugAddr) > 0 {
		go func() {
			if err := srv.Debug(DebugAddr); err != nil {
				log.Fatal(err)
			}
		}()
	}
	go func() {
		if err := srv.Shutdown(); err != nil {
			log.Fatal(err)
		}
	}()

	waitForSignal()

	log.Println("Shutdown")
	if err := srv.Shutdown(); err != nil {
		log.Fatal(err)
	}
}

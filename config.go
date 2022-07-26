package razvhost

import (
	"bufio"
	"io"
	"log"
	"net/url"
	"os"
	"strings"
)

// DefaultDiscardHeaders ...
var DefaultDiscardHeaders = []string{
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

// ExampleConfig ...
const ExampleConfig = `# comment
example.com alias.com -> http://localhost:8080
example.com/files -> file:///var/www/public/
loadbalance.com -> http://localhost:8081 http://localhost:8082
fileserver.com -> file:///var/www/public/
redirect.com -> redirect://github.com/razzie/razvhost
phpexample.com -> php:///var/www/index.php
phpexample2.com -> php:///var/www/mysite/`

// ConfigEntry ...
type ConfigEntry struct {
	Hostnames []string
	Targets   []url.URL
}

// ConfigEntryList ...
type ConfigEntryList []ConfigEntry

// ReadConfigFile ...
func ReadConfigFile(filename string) (ConfigEntryList, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return ReadConfig(file)
}

// ReadConfig ...
func ReadConfig(config io.Reader) (ConfigEntryList, error) {
	var entries ConfigEntryList
	scanner := bufio.NewScanner(config)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) == 0 || strings.HasPrefix(line, "#") {
			continue
		}
		items := strings.SplitN(line, "->", 2)
		if len(items) < 2 {
			log.Println("bad config line:", line)
			continue
		}
		hostnames := strings.Fields(items[0])
		targets := strings.Fields(items[1])
		entry := ConfigEntry{
			Hostnames: hostnames,
			Targets:   make([]url.URL, 0, len(targets)),
		}
		for _, target := range targets {
			targetURL, err := url.Parse(target)
			if err != nil {
				log.Println("bad target:", err)
				continue
			}
			entry.Targets = append(entry.Targets, *targetURL)
		}
		entries = append(entries, entry)
	}

	return entries, scanner.Err()
}

// ToProxyEvents ...
func (entry ConfigEntry) ToProxyEvents() []ProxyEvent {
	proxies := make([]ProxyEvent, 0, len(entry.Hostnames)*len(entry.Targets))
	for _, hostname := range entry.Hostnames {
		for _, target := range entry.Targets {
			proxies = append(proxies, ProxyEvent{
				Hostname: hostname,
				Target:   target,
				Up:       true,
			})
		}
	}
	return proxies
}

// ToProxyEvents ...
func (entries ConfigEntryList) ToProxyEvents() (proxies []ProxyEvent) {
	for _, entry := range entries {
		proxies = append(proxies, entry.ToProxyEvents()...)
	}
	return
}

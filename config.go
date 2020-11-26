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
var ExampleConfig = `# comment
example.com alias.com -> http://localhost:8080
example.com/files -> file:///var/www/public/
loadbalance.com -> http://localhost:8081 http://localhost:8082
fileserver.com -> file:///var/www/public/
redirect.com -> redirect://github.com/razzie/razvhost
phpexample.com -> php:///var/www/index.php
phpexample2.com -> php:///var/www/mysite/`

// ReadConfigFile ...
func ReadConfigFile(filename string) ([]ProxyEvent, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return ReadConfig(file)
}

// ReadConfig ...
func ReadConfig(config io.Reader) ([]ProxyEvent, error) {
	var proxies []ProxyEvent
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
		for _, hostname := range hostnames {
			for _, target := range targets {
				targetURL, err := url.Parse(target)
				if err != nil {
					log.Println("bad target:", err)
					continue
				}
				proxies = append(proxies, ProxyEvent{
					Hostname: hostname,
					Target:   *targetURL,
					Up:       true,
				})
			}
		}
	}

	return proxies, scanner.Err()
}

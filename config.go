package razvhost

import (
	"bufio"
	"io"
	"log"
	"net/url"
	"os"
	"strings"
)

// ExampleConfig ...
var ExampleConfig = strings.Join([]string{
	"# comment",
	"example.com example2.com -> http://localhost:8080",
	"fileexample.com -> file:///var/www/public/",
	"redirect.com -> redirect://github.com",
}, "\n")

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
		if strings.HasPrefix(line, "#") {
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

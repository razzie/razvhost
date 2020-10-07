package main

import (
	"bufio"
	"log"
	"os"
	"strings"
	"sync"
)

// Config ...
type Config struct {
	mtx     sync.Mutex
	proxies map[string]string
}

// NewConfig ...
func NewConfig() *Config {
	return &Config{
		proxies: make(map[string]string),
	}
}

// AddProxy ...
func (c *Config) AddProxy(hostname, target string) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.proxies[hostname] = target
}

// GetProxy ...
func (c *Config) GetProxy(hostname string) (string, bool) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	target, ok := c.proxies[hostname]
	return target, ok
}

// GetProxies ...
func (c *Config) GetProxies() map[string]string {
	proxies := make(map[string]string)
	c.mtx.Lock()
	defer c.mtx.Unlock()
	for hostname, target := range c.proxies {
		proxies[hostname] = target
	}
	return proxies
}

// ReadFromFile ...
func (c *Config) ReadFromFile(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	c.mtx.Lock()
	defer c.mtx.Unlock()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		items := strings.SplitN(scanner.Text(), "->", 2)
		if len(items) < 2 {
			log.Println("bad config line:", scanner.Text())
			continue
		}
		hostname := strings.TrimSpace(items[0])
		target := strings.TrimSpace(items[1])
		c.proxies[hostname] = target
	}

	return scanner.Err()
}

package razvhost

import (
	"bufio"
	"io"
	"log"
	"net/url"
	"os"
	"strings"

	"github.com/fsnotify/fsnotify"
)

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

type Config struct {
	C           <-chan ProxyEvent
	events      chan ProxyEvent
	filename    string
	watcher     *fsnotify.Watcher
	prevEntries []ProxyEntry
}

func NewConfig(filename string) (*Config, error) {
	entries, err := ReadConfigFile(filename)
	if err != nil {
		return nil, err
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := watcher.Add(filename); err != nil {
		watcher.Close()
		return nil, err
	}

	events := make(chan ProxyEvent, len(entries))
	for _, entry := range entries {
		events <- ProxyEvent{
			ProxyEntry: entry,
			Up:         true,
		}
	}

	cfg := &Config{
		C:           events,
		events:      events,
		filename:    filename,
		watcher:     watcher,
		prevEntries: entries,
	}
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				cfg.handleUpdate(event)
				watcher.Add(filename)
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Println("Config file watch error:", err)
			}
		}
	}()
	return cfg, nil
}

func (cfg *Config) Close() error {
	close(cfg.events)
	return cfg.watcher.Close()
}

func (cfg *Config) handleUpdate(event fsnotify.Event) {
	newEntries, err := ReadConfigFile(cfg.filename)
	if err != nil {
		log.Println("Failed to read config file:", err)
		return
	}
	up, down := cfg.getConfigChange(newEntries)
	if len(up) > 0 || len(down) > 0 {
		log.Println("Config updated")
	}
	cfg.prevEntries = newEntries

	go func() {
		for _, upEntry := range up {
			cfg.events <- ProxyEvent{
				ProxyEntry: upEntry,
				Up:         true,
			}
		}
		for _, downEntry := range down {
			cfg.events <- ProxyEvent{
				ProxyEntry: downEntry,
				Up:         false,
			}
		}
	}()
}

func (cfg *Config) getConfigChange(newEntries []ProxyEntry) (up, down []ProxyEntry) {
	for _, newEntry := range newEntries {
		if !proxyEntryList(cfg.prevEntries).contains(newEntry) {
			up = append(up, newEntry)
		}
	}
	for _, prevEntry := range cfg.prevEntries {
		if !proxyEntryList(newEntries).contains(prevEntry) {
			down = append(down, prevEntry)
		}
	}
	return
}

func ReadConfigFile(filename string) ([]ProxyEntry, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return ReadConfig(file)
}

func ReadConfig(config io.Reader) ([]ProxyEntry, error) {
	var entries configEntryList
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
		entry := configEntry{
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

	return entries.toProxyEntries(), scanner.Err()
}

type configEntry struct {
	Hostnames []string
	Targets   []url.URL
}

func (entry configEntry) toProxyEntries() []ProxyEntry {
	proxies := make([]ProxyEntry, 0, len(entry.Hostnames)*len(entry.Targets))
	for _, hostname := range entry.Hostnames {
		for _, target := range entry.Targets {
			proxies = append(proxies, ProxyEntry{
				Hostname: hostname,
				Target:   target,
			})
		}
	}
	return proxies
}

type configEntryList []configEntry

func (entries configEntryList) toProxyEntries() (proxies []ProxyEntry) {
	for _, entry := range entries {
		proxies = append(proxies, entry.toProxyEntries()...)
	}
	return
}

type proxyEntryList []ProxyEntry

func (entries proxyEntryList) contains(proxy ProxyEntry) bool {
	for _, entry := range entries {
		if entry.Hostname == proxy.Hostname && entry.Target == proxy.Target {
			return true
		}
	}
	return false
}

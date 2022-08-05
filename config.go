package razvhost

import (
	"bufio"
	"io"
	"log"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"

	"text/template"

	"github.com/Masterminds/sprig/v3"
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
	modCounter  uint32
	mtx         sync.Mutex
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
			case _, ok := <-watcher.Events:
				if !ok {
					return
				}
				go cfg.handleUpdate()
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

func (cfg *Config) handleUpdate() {
	// aggregate updates and only handle the last one
	modCount := atomic.AddUint32(&cfg.modCounter, 1)
	<-time.After(time.Second)
	if atomic.LoadUint32(&cfg.modCounter) != modCount {
		return
	}

	newEntries, err := ReadConfigFile(cfg.filename)
	if err != nil {
		log.Println("Failed to read config file:", err)
		return
	}

	cfg.mtx.Lock()
	defer cfg.mtx.Unlock()

	up, down := cfg.getConfigChange(newEntries)
	if len(up) > 0 || len(down) > 0 {
		log.Println("Config updated")
	}
	cfg.prevEntries = newEntries

	for _, upEntry := range up {
		go cfg.sendEvent(upEntry, true)
	}
	for _, downEntry := range down {
		go cfg.sendEvent(downEntry, false)
	}
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

func (cfg *Config) sendEvent(entry ProxyEntry, up bool) {
	cfg.events <- ProxyEvent{
		ProxyEntry: entry,
		Up:         up,
	}
}

func ReadConfigFile(filename string) ([]ProxyEntry, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return ReadConfig(file)
}

func ReadConfig(reader io.Reader) ([]ProxyEntry, error) {
	r, err := executeTemplates(reader)
	if err != nil {
		return nil, err
	}

	var entries configEntryList
	scanner := bufio.NewScanner(r)
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

func executeTemplates(reader io.Reader) (io.Reader, error) {
	config := new(strings.Builder)
	_, err := io.Copy(config, reader)
	if err != nil {
		return nil, err
	}

	tpl, err := template.New("").Funcs(sprig.TxtFuncMap()).Parse(config.String())
	if err != nil {
		return nil, err
	}

	config.Reset()
	if err := tpl.Execute(config, nil); err != nil {
		return nil, err
	}

	return strings.NewReader(config.String()), nil
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

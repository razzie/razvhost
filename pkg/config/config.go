package config

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"text/template"
	"time"

	"github.com/Masterminds/sprig/v3"
	"github.com/fsnotify/fsnotify"
)

type ConfigEntry struct {
	Hostname string
	Target   url.URL
}

type ConfigEvent struct {
	ConfigEntry
	Up bool
}

func (e ConfigEvent) String() string {
	str := e.Hostname + " -> " + e.Target.Redacted()
	if e.Up {
		str += " [UP]"
	} else {
		str += " [DOWN]"
	}
	return str
}

type Config struct {
	C           <-chan []ConfigEvent
	events      chan []ConfigEvent
	filename    string
	watcher     *fsnotify.Watcher
	prevEntries []ConfigEntry
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

	events := make(chan []ConfigEvent, 1)
	events <- configEntries(entries).toEvents(true)

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

	go func() {
		cfg.events <- append(up.toEvents(true), down.toEvents(false)...)
	}()
}

func (cfg *Config) getConfigChange(newEntries []ConfigEntry) (up, down configEntries) {
	for _, newEntry := range newEntries {
		if !configEntries(cfg.prevEntries).contains(newEntry) {
			up = append(up, newEntry)
		}
	}
	for _, prevEntry := range cfg.prevEntries {
		if !configEntries(newEntries).contains(prevEntry) {
			down = append(down, prevEntry)
		}
	}
	return
}

func ReadConfigFile(filename string) ([]ConfigEntry, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return ReadConfig(file)
}

func ReadConfig(reader io.Reader) ([]ConfigEntry, error) {
	r, err := executeTemplates(reader)
	if err != nil {
		return nil, err
	}

	var config configLines
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line, err := readConfigLine(scanner.Text())
		if err != nil {
			log.Println(err)
			continue
		}
		config = append(config, line)
	}

	return config.toConfigEntries(), scanner.Err()
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

type configLine struct {
	Hostnames []string
	Targets   []url.URL
}

func readConfigLine(text string) (line configLine, err error) {
	text = strings.TrimSpace(text)
	if len(text) == 0 || strings.HasPrefix(text, "#") {
		return
	}
	items := strings.SplitN(text, "->", 2)
	if len(items) < 2 {
		err = fmt.Errorf("bad config line: %s", text)
		return
	}
	line.Hostnames = strings.Fields(items[0])
	for _, target := range strings.Fields(items[1]) {
		targetURL, urlErr := url.Parse(target)
		if urlErr != nil {
			err = fmt.Errorf("cannot parse target url: %v", urlErr)
			return
		}
		line.Targets = append(line.Targets, *targetURL)
	}
	return
}

func (line configLine) toConfigEntries() []ConfigEntry {
	entries := make([]ConfigEntry, 0, len(line.Hostnames)*len(line.Targets))
	for _, hostname := range line.Hostnames {
		for _, target := range line.Targets {
			entries = append(entries, ConfigEntry{
				Hostname: hostname,
				Target:   target,
			})
		}
	}
	return entries
}

type configLines []configLine

func (lines configLines) toConfigEntries() (entries []ConfigEntry) {
	for _, entry := range lines {
		entries = append(entries, entry.toConfigEntries()...)
	}
	return
}

type configEntries []ConfigEntry

func (entries configEntries) contains(other ConfigEntry) bool {
	for _, entry := range entries {
		if entry.Hostname == other.Hostname && entry.Target.String() == other.Target.String() {
			return true
		}
	}
	return false
}

func (entries configEntries) toEvents(up bool) []ConfigEvent {
	events := make([]ConfigEvent, 0, len(entries))
	for _, entry := range entries {
		events = append(events, ConfigEvent{
			ConfigEntry: entry,
			Up:          up,
		})
	}
	return events
}

package config

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	docker "github.com/fsouza/go-dockerclient"
)

func TestNewConfigInitialEventsAndClose(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "config")
	if _, err := NewConfig(filename); err == nil {
		t.Fatal("expected an error for missing configuration")
	}
	if err := os.WriteFile(filename, []byte("example.test -> http://localhost:8080\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := NewConfig(filename)
	if err != nil {
		t.Fatal(err)
	}
	events := <-cfg.C
	if len(events) != 1 || !events[0].Up || events[0].Hostname != "example.test" || events[0].Target.String() != "http://localhost:8080" {
		t.Errorf("initial events = %+v", events)
	}
	if err := cfg.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok := <-cfg.C; ok {
		t.Fatal("event channel remains open after Close")
	}
}

func TestConfigHandleUpdate(t *testing.T) {
	for _, tt := range []struct {
		name, content string
		wantError     bool
		wantEvents    []ConfigEvent
	}{
		{"replace", "new.test -> http://localhost:8081\n", false, []ConfigEvent{
			{ConfigEntry: ConfigEntry{Hostname: "new.test", Target: url.URL{Scheme: "http", Host: "localhost:8081"}}, Up: true},
			{ConfigEntry: ConfigEntry{Hostname: "old.test", Target: url.URL{Scheme: "http", Host: "localhost:8080"}}, Up: false},
		}},
		{"unchanged", "old.test -> http://localhost:8080\n", false, []ConfigEvent{}},
		{"invalid template", "{{", true, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			filename := filepath.Join(t.TempDir(), "config")
			if err := os.WriteFile(filename, []byte(tt.content), 0600); err != nil {
				t.Fatal(err)
			}
			previous := []ConfigEntry{{Hostname: "old.test", Target: url.URL{Scheme: "http", Host: "localhost:8080"}}}
			cfg := &Config{filename: filename, prevEntries: previous, events: make(chan []ConfigEvent, 1)}
			cfg.handleUpdate()
			if tt.wantError {
				if !reflect.DeepEqual(cfg.prevEntries, previous) || len(cfg.events) != 0 {
					t.Fatal("failed reload changed configuration or emitted events")
				}
				return
			}
			select {
			case got := <-cfg.events:
				if !reflect.DeepEqual(got, tt.wantEvents) {
					t.Fatalf("events = %+v, want %+v", got, tt.wantEvents)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("timed out waiting for reload events")
			}
			entries, err := ReadConfigFile(filename)
			if err != nil || !reflect.DeepEqual(cfg.prevEntries, entries) {
				t.Fatalf("configuration was not updated: %+v, error = %v", cfg.prevEntries, err)
			}
		})
	}
}

func TestConfigUpdateDebounce(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(filename, []byte("example.test -> http://localhost:8080\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{filename: filename, events: make(chan []ConfigEvent, 2)}
	done := make(chan struct{})
	go func() { cfg.handleUpdate(); close(done) }()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(5 * time.Second)
	for atomic.LoadUint32(&cfg.modCounter) == 0 {
		select {
		case <-ticker.C:
		case <-deadline:
			t.Fatal("first update did not start")
		}
	}
	cfg.handleUpdate()
	<-done
	select {
	case events := <-cfg.events:
		if len(events) != 1 || !events[0].Up || events[0].Hostname != "example.test" {
			t.Fatalf("unexpected events: %+v", events)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("debounced update did not emit events")
	}
	if len(cfg.events) != 0 {
		t.Fatal("superseded update emitted an additional batch")
	}
}

func TestNewDockerWatchFromEnvironment(t *testing.T) {
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_API_VERSION", "")
	t.Setenv("DOCKER_HOST", "http://docker.test:2375")
	watch, err := NewDockerWatch()
	if err != nil || watch == nil || watch.client == nil {
		t.Fatalf("NewDockerWatch = (%v, %v)", watch, err)
	}
	t.Setenv("DOCKER_HOST", "://invalid")
	if _, err := NewDockerWatch(); err == nil {
		t.Fatal("expected an error for invalid Docker host")
	}
}

func mockDockerWatch(t *testing.T, failList bool) *DockerWatch {
	t.Helper()
	client, err := docker.NewClient("http://docker.test")
	if err != nil {
		t.Fatal(err)
	}
	client.HTTPClient.Transport = configTestTransport(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		switch r.URL.Path {
		case "/containers/json":
			if failList {
				http.Error(w, "list failed", http.StatusInternalServerError)
				break
			}
			json.NewEncoder(w).Encode([]docker.APIContainers{{ID: "valid"}, {ID: "unlabelled"}, {ID: "missing"}})
		case "/containers/valid/json", "/containers/unlabelled/json":
			labels := map[string]string{}
			if r.URL.Path == "/containers/valid/json" {
				labels["VIRTUAL_HOST"] = "example.test alias.test"
			}
			json.NewEncoder(w).Encode(docker.Container{
				Config: &docker.Config{Labels: labels},
				NetworkSettings: &docker.NetworkSettings{Ports: map[docker.Port][]docker.PortBinding{
					"8080/tcp": {{HostPort: "18080"}},
				}},
			})
		case "/containers/missing/json":
			http.NotFound(w, r)
		default:
			t.Errorf("unexpected Docker request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
		return w.Result(), nil
	})
	return &DockerWatch{client: client}
}

type configTestTransport func(*http.Request) (*http.Response, error)

func (f configTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDockerActiveContainers(t *testing.T) {
	events, err := mockDockerWatch(t, false).GetActiveContainers()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Hostname != "example.test" || events[1].Hostname != "alias.test" {
		t.Fatalf("active events = %+v", events)
	}
	for _, event := range events {
		if !event.Up || event.Target.String() != "http://localhost:18080" {
			t.Fatalf("unexpected event: %+v", event)
		}
	}
	if _, err := mockDockerWatch(t, true).GetActiveContainers(); err == nil {
		t.Fatal("expected list failure to propagate")
	}
}

func TestDockerEventConversion(t *testing.T) {
	in := make(chan *docker.APIEvents, 6)
	for _, e := range []*docker.APIEvents{
		{Type: "network", Action: "start"},
		{Type: "container", Action: "create"},
		{Type: "container", Action: "start", Actor: docker.APIActor{ID: "valid"}},
		{Type: "container", Action: "stop", Actor: docker.APIActor{ID: "valid"}},
		{Type: "container", Action: "start", Actor: docker.APIActor{ID: "missing"}},
	} {
		in <- e
	}
	close(in)
	out := make(chan []ConfigEvent, 4)
	mockDockerWatch(t, false).convertEvents(in, out)
	i := 0
	for batch := range out {
		if len(batch) != 1 {
			t.Fatalf("batch = %+v, want one event", batch)
		}
		wantHost := []string{"example.test", "alias.test"}[i%2]
		if batch[0].Hostname != wantHost || batch[0].Up != (i < 2) {
			t.Fatalf("event %d = %+v", i, batch[0])
		}
		i++
	}
	if i != 4 {
		t.Fatalf("event count = %d, want 4", i)
	}
}

package config

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	docker "github.com/fsouza/go-dockerclient"
)

func TestDockerWatchContainerLabels(t *testing.T) {
	tests := []struct {
		name      string
		labels    map[string]string
		env       []string
		unbound   bool
		wantHosts []string
		wantPort  string
		wantError string
	}{
		{
			name:      "default port",
			labels:    map[string]string{"VIRTUAL_HOST": "example.com"},
			wantHosts: []string{"example.com"},
			wantPort:  "18080",
		},
		{
			name:      "multiple hosts and explicit port",
			labels:    map[string]string{"VIRTUAL_HOST": " example.com\talias.com\n", "VIRTUAL_PORT": "80"},
			wantHosts: []string{"example.com", "alias.com"},
			wantPort:  "18000",
		},
		{
			name:      "empty port uses default",
			labels:    map[string]string{"VIRTUAL_HOST": "example.com", "VIRTUAL_PORT": ""},
			wantHosts: []string{"example.com"},
			wantPort:  "18080",
		},
		{
			name: "environment alone is ignored",
			env:  []string{"VIRTUAL_HOST=legacy.com", "VIRTUAL_PORT=80"},
		},
		{
			name:   "empty host label is ignored",
			labels: map[string]string{"VIRTUAL_HOST": ""},
			env:    []string{"VIRTUAL_HOST=legacy.com"},
		},
		{
			name:      "labels override conflicting environment",
			labels:    map[string]string{"VIRTUAL_HOST": "example.com", "VIRTUAL_PORT": "80"},
			env:       []string{"VIRTUAL_HOST=legacy.com", "VIRTUAL_PORT=8080"},
			wantHosts: []string{"example.com"},
			wantPort:  "18000",
		},
		{
			name:      "environment port is ignored",
			labels:    map[string]string{"VIRTUAL_HOST": "example.com"},
			env:       []string{"VIRTUAL_PORT=80"},
			wantHosts: []string{"example.com"},
			wantPort:  "18080",
		},
		{
			name:      "missing port binding",
			labels:    map[string]string{"VIRTUAL_HOST": "example.com", "VIRTUAL_PORT": "3000"},
			wantError: `no "3000" port bindings`,
		},
		{
			name:      "unpublished port",
			labels:    map[string]string{"VIRTUAL_HOST": "example.com"},
			unbound:   true,
			wantError: `no "8080" port bindings`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ports := map[docker.Port][]docker.PortBinding{
				"8080/tcp": {{HostIP: "127.0.0.1", HostPort: "18080"}},
				"80/tcp":   {{HostIP: "127.0.0.1", HostPort: "18000"}},
			}
			if tt.unbound {
				ports["8080/tcp"] = nil
			}
			container := docker.Container{
				Config:          &docker.Config{Labels: tt.labels, Env: tt.env},
				NetworkSettings: &docker.NetworkSettings{Ports: ports},
			}
			client, err := docker.NewClient("http://docker.test")
			if err != nil {
				t.Fatal(err)
			}
			client.HTTPClient.Transport = dockerWatchTestTransport(func(r *http.Request) (*http.Response, error) {
				w := httptest.NewRecorder()
				if r.Method != http.MethodGet || r.URL.Path != "/containers/test-container/json" {
					t.Errorf("unexpected Docker request: %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
					return w.Result(), nil
				}
				if err := json.NewEncoder(w).Encode(container); err != nil {
					t.Errorf("encode container: %v", err)
				}
				return w.Result(), nil
			})
			watch := &DockerWatch{client: client}

			for _, start := range []bool{true, false} {
				events, err := watch.getContainerEvents("test-container", start)
				if tt.wantError != "" {
					if err == nil || !strings.Contains(err.Error(), tt.wantError) {
						t.Fatalf("start=%t: expected error containing %q, got %v", start, tt.wantError, err)
					}
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				if len(events) != len(tt.wantHosts) {
					t.Fatalf("start=%t: expected %d events, got %d", start, len(tt.wantHosts), len(events))
				}
				for i, event := range events {
					if event.Hostname != tt.wantHosts[i] || event.Target.String() != "http://localhost:"+tt.wantPort || event.Up != start {
						t.Errorf("start=%t: unexpected event: %+v", start, event)
					}
				}
			}
		})
	}
}

type dockerWatchTestTransport func(*http.Request) (*http.Response, error)

func (f dockerWatchTestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

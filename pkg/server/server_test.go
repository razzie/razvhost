package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/razzie/razvhost/pkg/config"
)

func TestNewServer(t *testing.T) {
	for _, enableHTTP2 := range []bool{true, false} {
		s := NewServer(ServerConfig{EnableHTTP2: enableHTTP2, CertsDir: t.TempDir()})
		if s.internalServer.Addr != ":443" || s.internalServer.Handler == nil || s.internalServer.TLSConfig.GetCertificate == nil || s.factory == nil {
			t.Fatalf("incomplete server setup: %+v", s)
		}
		if got := s.internalServer.TLSNextProto == nil; got != enableHTTP2 {
			t.Fatalf("HTTP2 enabled = %t, want %t", got, enableHTTP2)
		}
		if err := s.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestServerConfigErrors(t *testing.T) {
	s := NewServer(ServerConfig{PHPAddr: "http://%zz", ConfigFile: filepath.Join(t.TempDir(), "missing")})
	if s.factory == nil {
		t.Fatal("invalid configuration prevented server initialization")
	}
	if err := s.loadConfig(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("loadConfig error = %v", err)
	}
	s.config.ConfigFile = ""
	if err := s.loadConfig(); err != nil {
		t.Fatalf("empty config path error = %v", err)
	}
	t.Setenv("DOCKER_HOST", "://invalid")
	if err := s.watchDockerEvents(); err == nil {
		t.Fatal("expected an error for invalid Docker address")
	}
}

func TestServerRequestHeaders(t *testing.T) {
	s := NewServer(ServerConfig{
		DiscardHeaders: []string{"X-Secret", "x-discard"},
		ExtraHeaders:   map[string]string{"X-Extra": "configured"},
	})
	s.mux.Add("example.test/app", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Forwarded-Host") != "example.test" || r.Header.Get("X-Razvhost-Remoteaddr") != "192.0.2.1:1234" {
			t.Errorf("missing forwarding headers: %v", r.Header)
		}
		if r.Header.Get("X-Secret") != "" || r.Header.Get("X-Discard") != "" {
			t.Errorf("discarded headers retained: %v", r.Header)
		}
		if got := r.Header.Values("X-Extra"); !reflect.DeepEqual(got, []string{"existing", "configured"}) {
			t.Errorf("extra request header = %v", got)
		}
		w.WriteHeader(http.StatusAccepted)
		io.WriteString(w, "served")
	}), "test")
	r := httptest.NewRequest(http.MethodGet, "http://example.test/app/page", nil)
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("X-Forwarded-Host", "untrusted.test")
	r.Header.Set("X-Secret", "secret")
	r.Header.Set("X-Discard", "discard")
	r.Header.Set("X-Extra", "existing")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusAccepted || w.Body.String() != "served" || w.Header().Get("X-Extra") != "configured" {
		t.Fatalf("unexpected response: %+v", w)
	}
	w = httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://unknown.test/", nil))
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "Cannot serve path: unknown.test/") {
		t.Fatalf("unknown host response = %d %q", w.Code, w.Body.String())
	}
	if err := s.ValidateHost(context.Background(), "unknown.test"); err == nil {
		t.Fatal("unknown certificate host was accepted")
	}
}

func TestServerConfigEvents(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "hello.txt")
	if err := os.WriteFile(filename, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	s := NewServer(ServerConfig{})
	entry := config.ConfigEntry{Hostname: "example.test", Target: url.URL{Scheme: "file", Path: filename}}
	channel := make(chan []config.ConfigEvent, 1)
	channel <- []config.ConfigEvent{
		{ConfigEntry: entry, Up: true},
		{ConfigEntry: config.ConfigEntry{Hostname: "bad.test", Target: url.URL{Scheme: "unsupported"}}, Up: true},
	}
	close(channel)
	s.Listen(channel)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://example.test/hello.txt", nil))
	if w.Code != http.StatusOK || w.Body.String() != "hello" {
		t.Fatalf("configured route response = %d %q", w.Code, w.Body.String())
	}
	if s.mux.Handler("bad.test/") != nil {
		t.Fatal("unsupported target was installed")
	}
	s.ProcessEvent(config.ConfigEvent{ConfigEntry: entry, Up: false})
	w = httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://example.test/hello.txt", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("removed route status = %d", w.Code)
	}
}

func TestServeAfterShutdown(t *testing.T) {
	s := NewServer(ServerConfig{NoCert: true})
	if err := s.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if err := s.Serve(); !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("Serve error = %v, want ErrServerClosed", err)
	}
	if s.internalServer.Addr != ":80" {
		t.Fatalf("HTTP listener address = %q", s.internalServer.Addr)
	}
}

func TestValidateConfiguredHosts(t *testing.T) {
	s := NewServer(ServerConfig{})
	for _, host := range []string{"example.test/app", "*.wild.test/files"} {
		s.mux.Add(host, http.NotFoundHandler(), "target")
	}
	for _, host := range []string{"example.test", "a.wild.test"} {
		if err := s.ValidateHost(context.Background(), host); err != nil {
			t.Errorf("configured host %s rejected: %v", host, err)
		}
	}
	s.mux.Remove("example.test/app", "target")
	if err := s.ValidateHost(context.Background(), "example.test"); err == nil {
		t.Fatal("removed certificate host was accepted")
	}
}

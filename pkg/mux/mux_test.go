package mux

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func routeHandler(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	})
}

func routeResult(m *Mux, path string) string {
	h := m.Handler(path)
	if h == nil {
		return ""
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	return w.Body.String()
}

func TestMuxRouting(t *testing.T) {
	var m Mux
	m.Add("example.test", routeHandler("root"), "root")
	m.Add("example.test/app", routeHandler("app"), "app")
	m.Add("example.test/app/admin", routeHandler("admin"), "admin")
	m.Add("*.test/files", routeHandler("wildcard"), "wildcard")
	for _, tt := range []struct{ path, want string }{
		{"example.test/", "root"},
		{"example.test/app/page", "app"},
		{"example.test/app/admin/users", "admin"},
		{"example.test/files/logo.png", "root"}, // literal routes precede wildcard routes
		{"other.test/files/logo.png", "wildcard"},
		{"other.test/elsewhere", ""},
		{"unknown.invalid/", ""},
	} {
		t.Run(tt.path, func(t *testing.T) {
			if got := routeResult(&m, tt.path); got != tt.want {
				t.Fatalf("handler response = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMuxWildcardMatching(t *testing.T) {
	for _, tt := range []struct {
		pattern, path string
		want          bool
	}{
		{"*.test/*/files", "a.test/public/files/logo.png", true},
		{"*.test/*/files", "a.test/files", false},
		{"*.test/*/files", "a.test/public/other", false},
		{"?.test/files", "a.test/files", true},
		{"?.test/files", "ab.test/files", false},
		{"[ab].test/files", "b.test/files", true},
		{"[ab].test/files", "c.test/files", false},
		{"[.test", "a.test/", false},
	} {
		t.Run(tt.pattern+"/"+tt.path, func(t *testing.T) {
			var m Mux
			m.Add(tt.pattern, routeHandler("matched"), "test")
			if got := m.Handler(tt.path) != nil; got != tt.want {
				t.Fatalf("matched = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestMuxLoadBalancingAndRemoval(t *testing.T) {
	var m Mux
	m.Add("example.test", routeHandler("a"), "a")
	m.Add("example.test", routeHandler("b"), "b")
	m.Add("example.test", routeHandler("c"), "c")
	counts := make(map[string]int)
	for range 12 {
		counts[routeResult(&m, "example.test/")]++
	}
	for _, id := range []string{"a", "b", "c"} {
		if counts[id] != 4 {
			t.Fatalf("distribution = %v, want four requests per handler", counts)
		}
	}
	m.Remove("missing.test", "a")
	m.Remove("example.test", "unknown")
	m.Remove("example.test", "b")
	for range 6 {
		if got := routeResult(&m, "example.test/"); got != "a" && got != "c" {
			t.Fatalf("unexpected handler after removal: %q", got)
		}
	}
	m.Remove("example.test", "a")
	m.Remove("example.test", "c")
	if h := m.Handler("example.test/"); h != nil {
		t.Fatal("expected no handler after removing all targets")
	}
	m.Add("example.test", routeHandler("new"), "new")
	if got := routeResult(&m, "example.test/"); got != "new" {
		t.Fatalf("handler after re-add = %q, want new", got)
	}
}

func TestMuxConcurrentAccess(t *testing.T) {
	var m Mux
	m.Add("example.test", routeHandler("stable"), "stable")
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			id := fmt.Sprintf("worker-%d", i)
			for range 100 {
				m.Add("example.test", routeHandler("temporary"), id)
				if m.Handler("example.test/") == nil {
					t.Error("stable handler disappeared")
				}
				m.Remove("example.test", id)
			}
		})
	}
	wg.Wait()
	if got := routeResult(&m, "example.test/"); got != "stable" {
		t.Fatalf("final handler = %q, want stable", got)
	}
}

func TestMuxContains(t *testing.T) {
	var m Mux
	m.Add("example.test/app", routeHandler("app"), "app")
	m.Add("*.wild.test/files", routeHandler("wild"), "wild")
	for _, tt := range []struct {
		path string
		want bool
	}{
		{"example.test/app/page", true}, {"example.test/other", false},
		{"a.wild.test/files/page", true}, {"a.wild.test/other", false},
		{"unknown.test/", false},
	} {
		if got := m.Contains(tt.path); got != tt.want {
			t.Errorf("Contains(%q) = %t, want %t", tt.path, got, tt.want)
		}
	}
	for _, tt := range []struct {
		host string
		want bool
	}{
		{"example.test", true}, {"a.wild.test", true}, {"unknown.test", false},
	} {
		if got := m.ContainsHost(tt.host); got != tt.want {
			t.Errorf("ContainsHost(%q) = %t, want %t", tt.host, got, tt.want)
		}
	}
	m.Remove("example.test/app", "app")
	m.Remove("*.wild.test/files", "wild")
	if m.Contains("example.test/app") || m.ContainsHost("example.test") || m.Contains("a.wild.test/files") || m.ContainsHost("a.wild.test") {
		t.Fatal("removed routes are still reported as present")
	}
}

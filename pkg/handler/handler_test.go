package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSplitHostnameAndPath(t *testing.T) {
	for _, tt := range []struct{ input, host, path string }{
		{"example.test", "example.test", ""},
		{"example.test/", "example.test", "/"},
		{"*.test/app/nested", "*.test", "/app/nested"},
		{"", "", ""},
	} {
		t.Run(tt.input, func(t *testing.T) {
			host, path := splitHostnameAndPath(tt.input)
			if host != tt.host || path != tt.path {
				t.Fatalf("split = (%q, %q), want (%q, %q)", host, path, tt.host, tt.path)
			}
		})
	}
}

func TestJoinURLPath(t *testing.T) {
	for _, tt := range []struct{ base, request, path, rawPath string }{
		{"/base", "/page", "/base/page", ""},
		{"/base/", "/page", "/base/page", ""},
		{"/base", "page", "/base/page", ""},
		{"/base/", "page", "/base/page", ""},
		{"", "/page", "/page", ""},
		{"/base", "", "/base/", ""},
		{"/base%2Fdir/", "/page%2Fname", "/base/dir/page/name", "/base%2Fdir/page%2Fname"},
		{"/base%2Fdir", "page", "/base/dir/page", "/base%2Fdir/page"},
		{"/base", "/page%2Fname", "/base/page/name", "/base/page%2Fname"},
	} {
		t.Run(tt.base+"+"+tt.request, func(t *testing.T) {
			a, err := url.Parse(tt.base)
			if err != nil {
				t.Fatal(err)
			}
			b, err := url.Parse(tt.request)
			if err != nil {
				t.Fatal(err)
			}
			path, rawPath := joinURLPath(a, b)
			if path != tt.path || rawPath != tt.rawPath {
				t.Fatalf("joined = (%q, %q), want (%q, %q)", path, rawPath, tt.path, tt.rawPath)
			}
		})
	}
}

func TestRedirectHandler(t *testing.T) {
	for _, tt := range []struct{ target, request, want string }{
		{"redirect://destination.test/base", "http://example.test/app/page", "http://destination.test/base/page"},
		{"redirect://destination.test/base?fixed=1", "http://example.test/app/page?dynamic=2", "http://destination.test/base/page?fixed=1&dynamic=2"},
		{"redirect://destination.test/base?fixed=1", "http://example.test/app/", "http://destination.test/base/?fixed=1"},
		{"redirect://destination.test/base", "http://example.test/app/page?q=1", "http://destination.test/base/page?q=1"},
		{"redirect://destination.test/base%2Fdir", "http://example.test/app/page%2Fname", "http://destination.test/base%2Fdir/page%2Fname"},
	} {
		t.Run(tt.target+"+"+tt.request, func(t *testing.T) {
			target, err := url.Parse(tt.target)
			if err != nil {
				t.Fatal(err)
			}
			h, err := NewHandlerFactory(nil).Handler("example.test/app", *target)
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tt.request, nil))
			if w.Code != http.StatusSeeOther || w.Header().Get("Location") != tt.want {
				t.Fatalf("redirect = (%d, %q), want (303, %q)", w.Code, w.Header().Get("Location"), tt.want)
			}
		})
	}
}

func TestHandlePathCombinations(t *testing.T) {
	h := handlePathCombinations(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/page/name" || r.URL.RawPath != "/page%2Fname" || r.URL.RawQuery != "q=1" {
			t.Errorf("unexpected forwarded URL: %s", r.URL)
		}
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, `<a href="/base/next">next</a>`)
	}), "backend.test", "/app", "/base")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://example.test/app/page%2Fname?q=1", nil))
	if got, want := w.Body.String(), `<a href="/app/next">next</a>`; got != want {
		t.Fatalf("response = %q, want %q", got, want)
	}
}

func TestHandlerFactoryUnknownScheme(t *testing.T) {
	h, err := NewHandlerFactory(nil).Handler("example.test", url.URL{Scheme: "unknown"})
	if h != nil || err == nil || !strings.Contains(err.Error(), "unknown target URL scheme: unknown") {
		t.Fatalf("Handler = (%v, %v), want unsupported scheme error", h, err)
	}
}

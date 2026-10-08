package handler

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yookoala/gofast"
)

func TestProxyHandler(t *testing.T) {
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	http.DefaultTransport = handlerTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "http://backend.test/base/page%2Fname?fixed=1&q=2" || r.Method != http.MethodPost {
			t.Errorf("forwarded request = %s %s", r.Method, r.URL)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || string(body) != "payload" || r.Header.Get("X-Test") != "preserved" {
			t.Errorf("forwarded body = %q, error = %v, headers = %v", body, err, r.Header)
		}
		w := httptest.NewRecorder()
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Location", "http://example.test/base/next")
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, `<a href="/base/next">next</a>`)
		return w.Result(), nil
	})
	target, err := url.Parse("http://backend.test/base?fixed=1")
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewHandlerFactory(nil).Handler("example.test/app", *target)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "http://example.test/app/page%2Fname?q=2", strings.NewReader("payload"))
	r.Header.Set("X-Test", "preserved")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusCreated || w.Body.String() != `<a href="/app/next">next</a>` || w.Header().Get("Location") != "/app/next" {
		t.Fatalf("proxy response = %d %q, Location = %q", w.Code, w.Body.String(), w.Header().Get("Location"))
	}
	http.DefaultTransport = handlerTestTransport(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("backend unavailable")
	})
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://example.test/app/", nil))
	if w.Code != http.StatusBadGateway {
		t.Fatalf("unavailable backend status = %d", w.Code)
	}
}

type handlerTestTransport func(*http.Request) (*http.Response, error)

func (f handlerTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFileHandler(t *testing.T) {
	root := t.TempDir()
	filename := filepath.Join(root, "hello.txt")
	if err := os.WriteFile(filename, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ endpoint, request string }{
		{root, "/files/hello.txt"}, {filename, "/files/hello.txt"}, {root, "/files"},
	} {
		h, err := NewHandlerFactory(nil).Handler("example.test/files", url.URL{Scheme: "file", Path: tt.endpoint})
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tt.request, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("file response status = %d", w.Code)
		}
		if tt.request == "/files" {
			if !strings.Contains(w.Body.String(), `href="/files/hello.txt"`) {
				t.Fatalf("listing did not rewrite links: %s", w.Body.String())
			}
		} else if w.Body.String() != "hello" {
			t.Fatalf("file body = %q", w.Body.String())
		}
	}
}

func TestGoWasmHandler(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "build.wasm")
	if err := os.WriteFile(filename, []byte("wasm payload"), 0600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandlerFactory(nil).Handler("example.test/app", url.URL{Scheme: "go-wasm", Path: filename})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		path, contentType, body string
		status                  int
	}{
		{"/app", "", "", http.StatusSeeOther},
		{"/app/", "text/html; charset=utf-8", "<html", http.StatusOK},
		{"/app/go-wasm.js", "text/javascript", "Go", http.StatusOK},
		{"/app/main.wasm", "", "wasm payload", http.StatusOK},
		{"/app/missing", "", "Not found", http.StatusNotFound},
	} {
		t.Run(tt.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tt.path, nil))
			if w.Code != tt.status || (tt.contentType != "" && w.Header().Get("Content-Type") != tt.contentType) || !strings.Contains(w.Body.String(), tt.body) {
				t.Fatalf("response = %d %q, Content-Type = %q", w.Code, w.Body.String(), w.Header().Get("Content-Type"))
			}
			if tt.status == http.StatusSeeOther && w.Header().Get("Location") != "/app/" {
				t.Fatalf("Location = %q", w.Header().Get("Location"))
			}
		})
	}
}

func TestPHPHandler(t *testing.T) {
	root := t.TempDir()
	filename := filepath.Join(root, "index.php")
	if err := os.WriteFile(filename, []byte("<?php echo 'hello';"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := newPHPHandler(nil, "example.test", "", filename); err == nil || err.Error() != "PHP not configured" {
		t.Fatalf("unconfigured PHP error = %v", err)
	}
	factory := func() (gofast.Client, error) { return nil, errors.New("FastCGI unavailable") }
	if _, err := newPHPHandler(factory, "example.test", "", filepath.Join(root, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing PHP endpoint error = %v", err)
	}
	for _, endpoint := range []string{root, filename} {
		h, err := (&HandlerFactory{phpClientFactory: factory}).Handler("example.test/php", url.URL{Scheme: "php", Path: endpoint})
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/php/index.php", nil))
		if w.Code != http.StatusBadGateway {
			t.Fatalf("unavailable PHP backend status = %d", w.Code)
		}
	}
	addr := &url.URL{Scheme: "unix", Path: filepath.Join(root, "missing.sock")}
	hf := NewHandlerFactory(addr)
	if _, err := hf.phpClientFactory(); err == nil {
		t.Fatal("expected missing FastCGI socket error")
	}
	if setupPHP(&url.URL{Scheme: "tcp", Host: "localhost:9000"}) == nil {
		t.Fatal("TCP FastCGI address did not produce a factory")
	}
}

func TestS3HandlerConstruction(t *testing.T) {
	t.Setenv("AWS_SDK_LOAD_CONFIG", "")
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "missing"))
	for _, raw := range []string{"s3://bucket?region=eu-central-1", "s3://key:secret@bucket.s3.test/?region=eu-central-1"} {
		target, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		h, err := NewHandlerFactory(nil).Handler("example.test/s3", *target)
		if err != nil || h == nil {
			t.Fatalf("S3 handler = (%v, %v)", h, err)
		}
	}
	target := url.URL{Scheme: "s3", Host: "bucket", Path: "/invalid-prefix"}
	if _, err := newS3Handler("example.test", "", target); err == nil {
		t.Fatal("expected invalid fs.Sub prefix error")
	}
}

func TestSftpConfigurationAndDirectory(t *testing.T) {
	for _, tt := range []struct {
		target, user, dir string
		auth              int
	}{
		{"sftp://server.test", "anonymous", ".", 0},
		{"sftp://user:secret@server.test:22/public", "user", "/public", 1},
	} {
		target, err := url.Parse(tt.target)
		if err != nil {
			t.Fatal(err)
		}
		fsys := newSftpFS(*target).(*sftpFS)
		if fsys.hostname != target.Host || fsys.config.User != tt.user || fsys.dir != tt.dir || len(fsys.config.Auth) != tt.auth {
			t.Fatalf("SFTP configuration = %+v, SSH configuration = %+v", fsys, fsys.config)
		}
		if h, err := NewHandlerFactory(nil).Handler("example.test/files", *target); err != nil || h == nil {
			t.Fatalf("SFTP handler = (%v, %v)", h, err)
		}
	}
	root := t.TempDir()
	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	d := &sftpDir{fi: info, files: []os.FileInfo{info}}
	if got, err := d.Stat(); err != nil || got != info {
		t.Fatalf("Stat = (%v, %v)", got, err)
	}
	if files, err := d.Readdir(-1); err != nil || len(files) != 1 || files[0] != info {
		t.Fatalf("Readdir = (%v, %v)", files, err)
	}
	if _, err := d.Read(make([]byte, 1)); err == nil {
		t.Fatal("directory Read should be unsupported")
	}
	if _, err := d.Seek(0, io.SeekStart); err == nil {
		t.Fatal("directory Seek should be unsupported")
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := (&sftpFile{}).Readdir(-1); err == nil {
		t.Fatal("file Readdir should be unsupported")
	}
}

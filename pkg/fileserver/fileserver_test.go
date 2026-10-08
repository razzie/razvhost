package fileserver

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestFileServerContent(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hello world"), 0600); err != nil {
		t.Fatal(err)
	}
	h := FileServer(Directory(root))
	for _, tt := range []struct {
		name, method, path, byteRange, body string
		status                              int
	}{
		{"file", http.MethodGet, "/hello.txt", "", "hello world", http.StatusOK},
		{"clean path", http.MethodGet, "/unused/../hello.txt", "", "hello world", http.StatusOK},
		{"HEAD", http.MethodHead, "/hello.txt", "", "", http.StatusOK},
		{"range", http.MethodGet, "/hello.txt", "bytes=6-10", "world", http.StatusPartialContent},
		{"missing", http.MethodGet, "/missing.txt", "", "", http.StatusNotFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(tt.method, tt.path, nil)
			if tt.byteRange != "" {
				r.Header.Set("Range", tt.byteRange)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tt.status {
				t.Fatalf("status = %d, want %d", w.Code, tt.status)
			}
			if tt.status != http.StatusNotFound && w.Body.String() != tt.body {
				t.Fatalf("body = %q, want %q", w.Body.String(), tt.body)
			}
			if tt.byteRange != "" && w.Header().Get("Content-Range") != "bytes 6-10/11" {
				t.Fatalf("Content-Range = %q", w.Header().Get("Content-Range"))
			}
		})
	}
}

func TestFileServerDirectoryListing(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"z-dir", "b-dir", "empty"} {
		if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"z.txt", "a&b.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("content"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	h := FileServer(Directory(root))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("listing status = %d", w.Code)
	}
	body := w.Body.String()
	last := -1
	for _, link := range []string{`href="/b-dir"`, `href="/empty"`, `href="/z-dir"`, `href="/a&amp;b.txt"`, `href="/z.txt"`} {
		index := strings.Index(body, link)
		if index <= last {
			t.Fatalf("missing or out-of-order link %s in %s", link, body)
		}
		last = index
	}
	if !strings.Contains(body, ">a&amp;b.txt</a>") || strings.Contains(body, ">..</a>") {
		t.Fatalf("unexpected root listing: %s", body)
	}
	for _, tt := range []struct{ path, marker string }{
		{"/empty", `href="/">..</a>`},
		{"/empty/..", `href="/b-dir"`},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tt.path, nil))
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), tt.marker) {
			t.Fatalf("listing %s = %d %s", tt.path, w.Code, w.Body.String())
		}
	}
	empty := httptest.NewRecorder()
	FileServer(Directory(t.TempDir())).ServeHTTP(empty, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(empty.Body.String(), ">Empty</td>") {
		t.Fatalf("unexpected empty listing: %s", empty.Body.String())
	}
}

func TestDirectoryResolve(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(root, "file.txt")
	if err := os.WriteFile(filename, []byte("inside"), 0600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(parent, "outside.txt")
	if err := os.WriteFile(out, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ input, want string }{{"/", "."}, {"/file.txt", "file.txt"}, {"file.txt", "file.txt"}} {
		got, err := Directory(root).resolve(tt.input)
		if err != nil || got != tt.want {
			t.Fatalf("resolve(%q) = (%q, %v), want %q", tt.input, got, err, tt.want)
		}
	}
	if _, err := Directory(root).Open("missing"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing file error = %v", err)
	}
	if _, err := Directory(root).Open("../outside.txt"); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("traversal error = %v, want ErrOutsideRoot", err)
	}
	t.Chdir(root)
	file, err := Directory("").Open("file.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil || string(data) != "inside" {
		t.Fatalf("default root read = (%q, %v)", data, err)
	}
}

func TestDirectorySymlinks(t *testing.T) {
	root := t.TempDir()
	filename := filepath.Join(root, "file.txt")
	if err := os.WriteFile(filename, []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, link := range []struct{ name, target string }{
		{"inside", filename}, {"outside", outside}, {"loop", filepath.Join(root, "loop")},
	} {
		if err := os.Symlink(link.target, filepath.Join(root, link.name)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Directory(root).resolve("inside")
	if err != nil || got != "file.txt" {
		t.Fatalf("inside symlink = (%q, %v)", got, err)
	}
	for _, tt := range []struct {
		name string
		err  error
	}{{"outside", ErrOutsideRoot}, {"loop", ErrSymlinkMaxDepth}} {
		if _, err := Directory(root).resolve(tt.name); !errors.Is(err, tt.err) {
			t.Fatalf("resolve(%s) error = %v, want %v", tt.name, err, tt.err)
		}
	}
}

func TestFileServerErrors(t *testing.T) {
	wantErr := errors.New("filesystem failed")
	for _, tt := range []struct {
		name                         string
		openErr, statErr, readDirErr error
		status                       int
	}{
		{"open", wantErr, nil, nil, http.StatusNotFound},
		{"stat", nil, wantErr, nil, http.StatusInternalServerError},
		{"readdir", nil, nil, wantErr, http.StatusInternalServerError},
	} {
		t.Run(tt.name, func(t *testing.T) {
			file := &errorFile{statErr: tt.statErr, readDirErr: tt.readDirErr}
			fsys := testFileSystem(func(name string) (http.File, error) { return file, tt.openErr })
			w := httptest.NewRecorder()
			FileServer(fsys).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
			if w.Code != tt.status || !strings.Contains(w.Body.String(), wantErr.Error()) {
				t.Fatalf("response = %d %q", w.Code, w.Body.String())
			}
			if tt.openErr == nil && !file.closed {
				t.Fatal("file was not closed on error")
			}
		})
	}
}

type testFileSystem func(string) (http.File, error)

func (f testFileSystem) Open(name string) (http.File, error) { return f(name) }

type errorFile struct {
	http.File
	statErr, readDirErr error
	closed              bool
}

func (f *errorFile) Stat() (os.FileInfo, error) {
	if f.statErr != nil {
		return nil, f.statErr
	}
	file, err := fstest.MapFS{"dir": &fstest.MapFile{Mode: os.ModeDir}}.Open("dir")
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return file.Stat()
}
func (f *errorFile) Readdir(int) ([]os.FileInfo, error) { return nil, f.readDirErr }
func (f *errorFile) Close() error                       { f.closed = true; return nil }

func TestNewFsEntry(t *testing.T) {
	fsys := fstest.MapFS{"hello.txt": &fstest.MapFile{Data: make([]byte, 1024)}}
	file, err := fsys.Open("hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	entry := newFsEntry(info, "/public")
	if entry.Name != "hello.txt" || entry.FullName != "/public/hello.txt" || entry.Size != "1.0 KiB" || entry.Created != entry.Modified {
		t.Fatalf("unexpected entry: %+v", entry)
	}
}

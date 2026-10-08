package stream

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestUpdateLocation(t *testing.T) {
	for _, tt := range []struct {
		name, location, hostPath, targetPath, want string
	}{
		{"root path", "/base/image.png?q=1#top", "/app", "/base", "/app/image.png?q=1#top"},
		{"trailing slash", "/base/image.png", "/app/", "/base", "/app/image.png"},
		{"target trailing slash", "/base/image.png", "/app", "/base/", "/app/image.png"},
		{"same host", "https://backend.test/base/image.png?q=1", "/app", "/base", "/app/image.png?q=1"},
		{"protocol relative", "//backend.test/base/image.png", "/app", "/base", "/app/image.png"},
		{"external host", "https://external.test/base/image.png", "/app", "/base", "https://external.test/base/image.png"},
		{"relative path", "image.png", "/app", "", "image.png"},
		{"fragment", "#top", "/app", "", "#top"},
		{"mailto", "mailto:user@example.test", "/app", "", "mailto:user@example.test"},
		{"invalid URL", "http://%zz", "/app", "", "http://%zz"},
		{"no prefix", "/image.png", "", "", "/image.png"},
		{"empty location", "", "/app", "", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.location
			updateLocation(&got, "backend.test", tt.hostPath, tt.targetPath)
			if got != tt.want {
				t.Fatalf("location = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPathPrefixHTMLStreamer(t *testing.T) {
	input := `<a href="/base/page?x=1&amp;y=2">link</a><img src="https://backend.test/base/image.png"/><form action="/base/save"><button formaction="/base/other">save</button></form><a href="https://external.test/page">external</a><img src="relative.png" data-src="/base/lazy.png">`
	want := `<a href="/app/page?x=1&amp;y=2">link</a><img src="/app/image.png"/><form action="/app/save"><button formaction="/app/other">save</button></form><a href="https://external.test/page">external</a><img src="relative.png" data-src="/base/lazy.png">`
	r := NewPathPrefixHTMLStreamer("backend.test", "/app", "/base", io.NopCloser(strings.NewReader(input)))
	defer r.Close()
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("HTML = %s, want %s", got, want)
	}
}

func TestHTMLStreamerSmallReads(t *testing.T) {
	input := `<!DOCTYPE html><!--comment--><p title="a &amp; b">&lt;hello&gt; &amp; world</p><script>if (a < b && c > d) {}</script><style>a > b {}</style>`
	for _, size := range []int{1, 3, 64} {
		r := &HTMLStreamer{R: io.NopCloser(strings.NewReader(input))}
		var out strings.Builder
		buf := make([]byte, size)
		for {
			n, err := r.Read(buf)
			out.Write(buf[:n])
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		r.Close()
		if got := out.String(); got != input {
			t.Fatalf("buffer size %d: HTML = %q, want %q", size, got, input)
		}
	}
}

func TestHTMLStreamerModifyToken(t *testing.T) {
	r := &HTMLStreamer{
		R: io.NopCloser(strings.NewReader(`<p>old &amp; text</p>`)),
		ModifyToken: func(token *html.Token) {
			if token.Type == html.TextToken {
				token.Data = "new <text> & more"
			}
		},
	}
	defer r.Close()
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if want := `<p>new &lt;text&gt; &amp; more</p>`; string(got) != want {
		t.Fatalf("HTML = %q, want %q", got, want)
	}
}

func TestHTMLStreamerErrorsAndClose(t *testing.T) {
	wantErr := errors.New("stream failed")
	source := &failingReadCloser{err: wantErr}
	r := &HTMLStreamer{R: source}
	if _, err := io.ReadAll(r); !errors.Is(err, wantErr) {
		t.Fatalf("read error = %v, want %v", err, wantErr)
	}
	if err := r.Close(); !errors.Is(err, wantErr) || !source.closed {
		t.Fatalf("close error = %v, closed = %t", err, source.closed)
	}
}

type failingReadCloser struct {
	err    error
	closed bool
}

func (r *failingReadCloser) Read([]byte) (int, error) { return 0, r.err }
func (r *failingReadCloser) Close() error {
	r.closed = true
	return r.err
}

func TestPathPrefixHTMLResponseWriter(t *testing.T) {
	for _, tt := range []struct {
		name, contentType, input, want string
		status                         int
		sniff                          bool
	}{
		{"HTML", "text/html; charset=utf-8", `<a href="/base/page">page</a>`, `<a href="/app/page">page</a>`, http.StatusCreated, false},
		{"JSON", "application/json", `{"url":"/base/page"}`, `{"url":"/base/page"}`, http.StatusOK, false},
		{"sniffed HTML", "", `<html><a href="/base/page">page</a></html>`, `<html><a href="/app/page">page</a></html>`, http.StatusOK, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			w := NewPathPrefixHTMLResponseWriter("backend.test", "/app", "/base", recorder)
			t.Cleanup(func() { w.Close() })
			if !tt.sniff {
				w.Header().Set("Content-Type", tt.contentType)
				w.Header().Set("Content-Length", "123")
				w.WriteHeader(tt.status)
			}
			if n, err := io.WriteString(w, tt.input); err != nil || n != len(tt.input) {
				t.Fatalf("Write = (%d, %v)", n, err)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			if recorder.Code != tt.status || recorder.Body.String() != tt.want {
				t.Fatalf("response = %d %q, want %d %q", recorder.Code, recorder.Body.String(), tt.status, tt.want)
			}
			if strings.HasPrefix(recorder.Header().Get("Content-Type"), "text/html") {
				if got := recorder.Header().Get("Content-Length"); got != "" {
					t.Fatalf("rewritten HTML retains Content-Length: %q", got)
				}
			} else if got := recorder.Header().Get("Content-Length"); got != "123" {
				t.Fatalf("non-HTML Content-Length = %q, want 123", got)
			}
		})
	}
}

func TestPathPrefixResponseWriterRedirectAndFlush(t *testing.T) {
	recorder := httptest.NewRecorder()
	w := NewPathPrefixHTMLResponseWriter("backend.test", "/app", "/base", recorder)
	defer w.Close()
	w.Header().Set("Location", "http://backend.test/base/login?q=1")
	w.WriteHeader(http.StatusSeeOther)
	w.(http.Flusher).Flush()
	if got := recorder.Header().Get("Location"); got != "/app/login?q=1" {
		t.Fatalf("Location = %q", got)
	}
	if recorder.Code != http.StatusSeeOther || !recorder.Flushed {
		t.Fatalf("status = %d, flushed = %t", recorder.Code, recorder.Flushed)
	}
}

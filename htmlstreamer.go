package razvhost

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/net/html"
)

// HTMLStreamer ...
type HTMLStreamer struct {
	R           io.ReadCloser
	ModifyToken func(*html.Token)
	buffer      []byte
	z           *html.Tokenizer
}

// Read implements io.Reader
func (h *HTMLStreamer) Read(p []byte) (n int, err error) {
	if h.z == nil {
		h.z = html.NewTokenizer(h.R)
	}
	if len(h.buffer) == 0 {
		tt := h.z.Next()
		if tt == html.ErrorToken {
			return 0, h.z.Err()
		}
		token := h.z.Token()
		if h.ModifyToken != nil {
			h.ModifyToken(&token)
		}
		h.buffer = []byte(tokenToString(token))
	}
	n = copy(p, h.buffer)
	h.buffer = h.buffer[n:]
	return
}

// Close implements io.Closer
func (h *HTMLStreamer) Close() error {
	return h.R.Close()
}

// NewPathPrefixHTMLStreamer ...
func NewPathPrefixHTMLStreamer(hostname, trimPath, addPath string, r io.ReadCloser) io.ReadCloser {
	modifyToken := func(token *html.Token) {
		if token.Type != html.StartTagToken && token.Type != html.SelfClosingTagToken {
			return
		}
		for i, attr := range token.Attr {
			switch attr.Key {
			case "href", "src", "action", "formaction":
				if u, _ := url.Parse(attr.Val); u != nil && u.Host == hostname {
					attr.Val = u.RequestURI()
				}
				if strings.HasPrefix(attr.Val, "/") && !strings.HasPrefix(attr.Val, "//") {
					attr.Val = addPath + strings.TrimPrefix(attr.Val, trimPath)
				}
				token.Attr[i] = attr
			}
		}
	}
	return &HTMLStreamer{
		R:           r,
		ModifyToken: modifyToken,
	}
}

// NewPathPrefixHTMLResponseWriter ...
func NewPathPrefixHTMLResponseWriter(hostname, trimPath, addPath string, w http.ResponseWriter) ResponseWriterCloser {
	var wg sync.WaitGroup
	var reader io.ReadCloser
	var writer io.WriteCloser
	reader, writer = io.Pipe()
	reader = NewPathPrefixHTMLStreamer(hostname, trimPath, addPath, reader)
	wg.Add(1)
	go func() {
		if _, err := io.Copy(w, reader); err != nil {
			log.Println(err)
		}
		wg.Done()
	}()
	return &pathPrefixHTMLResponseWriter{
		w:        w,
		wg:       &wg,
		reader:   reader,
		writer:   writer,
		trimPath: trimPath,
		addPath:  addPath,
		hostname: hostname,
	}
}

// ResponseWriterCloser is a closeable http.ResponseWriter
type ResponseWriterCloser interface {
	http.ResponseWriter
	io.Closer
}

type pathPrefixHTMLResponseWriter struct {
	w        http.ResponseWriter
	wg       *sync.WaitGroup
	reader   io.ReadCloser
	writer   io.WriteCloser
	trimPath string
	addPath  string
	hostname string
	isHTML   bool
}

func (w *pathPrefixHTMLResponseWriter) Header() http.Header {
	return w.w.Header()
}

func (w *pathPrefixHTMLResponseWriter) Write(p []byte) (int, error) {
	if w.isHTML {
		return w.writer.Write(p)
	}
	return w.w.Write(p)
}

func (w *pathPrefixHTMLResponseWriter) WriteHeader(statusCode int) {
	h := w.w.Header()
	if location := h.Get("Location"); len(location) > 0 {
		if u, _ := url.Parse(location); u != nil && u.Host == w.hostname {
			location = u.RequestURI()
		}
		h.Set("Location", w.addPath+strings.TrimPrefix(location, w.trimPath))
	}
	if ctype := h.Get("Content-Type"); strings.HasPrefix(ctype, "text/html") {
		w.isHTML = true
		h.Del("Content-Length")
	}
	w.w.WriteHeader(statusCode)
}

func (w *pathPrefixHTMLResponseWriter) Flush() {
	if flusher, ok := w.w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *pathPrefixHTMLResponseWriter) Close() error {
	w.writer.Close()
	w.wg.Wait()
	w.reader.Close()
	return nil
}

func tokenToString(t html.Token) string {
	tagString := func() string {
		if len(t.Attr) == 0 {
			return t.Data
		}
		buf := bytes.NewBufferString(t.Data)
		for _, a := range t.Attr {
			buf.WriteByte(' ')
			buf.WriteString(a.Key)
			buf.WriteString(`="`)
			buf.WriteString(html.EscapeString(a.Val))
			buf.WriteByte('"')
		}
		return buf.String()
	}
	switch t.Type {
	case html.ErrorToken:
		return ""
	case html.TextToken:
		return t.Data
	case html.StartTagToken:
		return "<" + tagString() + ">"
	case html.EndTagToken:
		return "</" + tagString() + ">"
	case html.SelfClosingTagToken:
		return "<" + tagString() + "/>"
	case html.CommentToken:
		return "<!--" + t.Data + "-->"
	case html.DoctypeToken:
		return "<!DOCTYPE " + t.Data + ">"
	}
	return "Invalid(" + strconv.Itoa(int(t.Type)) + ")"
}

package razvhost

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"strings"

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
func NewPathPrefixHTMLStreamer(path string, r io.ReadCloser) io.ReadCloser {
	modifyToken := func(token *html.Token) {
		if token.Type != html.StartTagToken && token.Type != html.SelfClosingTagToken {
			return
		}
		for i, attr := range token.Attr {
			switch attr.Key {
			case "href", "src", "action", "formaction":
				if strings.HasPrefix(attr.Val, "/") && !strings.HasPrefix(attr.Val, "//") {
					attr.Val = path + attr.Val
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
func NewPathPrefixHTMLResponseWriter(path string, w http.ResponseWriter) http.ResponseWriter {
	reader, writer := io.Pipe()
	go io.Copy(w, NewPathPrefixHTMLStreamer(path, reader))
	return &pathPrefixHTMLResponseWriter{
		w:    w,
		pipe: writer,
		path: path,
	}
}

type pathPrefixHTMLResponseWriter struct {
	w    http.ResponseWriter
	pipe *io.PipeWriter
	path string
}

func (w *pathPrefixHTMLResponseWriter) Header() http.Header {
	return w.w.Header()
}

func (w *pathPrefixHTMLResponseWriter) Write(p []byte) (int, error) {
	return w.pipe.Write(p)
}

func (w *pathPrefixHTMLResponseWriter) WriteHeader(statusCode int) {
	h := w.w.Header()
	if location := h.Get("Location"); len(location) > 0 {
		h.Set("Location", w.path+location)
	}
	w.w.WriteHeader(statusCode)
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

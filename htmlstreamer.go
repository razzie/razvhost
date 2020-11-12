package razvhost

import (
	"bytes"
	"io"
	"strconv"

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

package stream

import (
	"io"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

func NewPathPrefixHTMLStreamer(hostname, hostPath, targetPath string, r io.ReadCloser) io.ReadCloser {
	modifyToken := func(token *html.Token) {
		if token.Type != html.StartTagToken && token.Type != html.SelfClosingTagToken {
			return
		}
		for i := range token.Attr {
			switch token.Attr[i].Key {
			case "href", "src", "action", "formaction":
				updateLocation(&token.Attr[i].Val, hostname, hostPath, targetPath)
			}
		}
	}
	return &HTMLStreamer{
		R:           r,
		ModifyToken: modifyToken,
	}
}

func updateLocation(loc *string, hostname, hostPath, targetPath string) {
	join := func(a, b string) string {
		aslash := strings.HasSuffix(a, "/")
		bslash := strings.HasPrefix(b, "/")
		switch {
		case aslash && bslash:
			return a + b[1:]
		case !aslash && !bslash:
			return a + "/" + b
		}
		return a + b
	}

	u, _ := url.Parse(*loc)
	if u == nil {
		return
	}
	if len(u.Host) > 0 {
		if u.Host != hostname {
			return
		}
		*loc = u.RequestURI()
	}
	if strings.HasPrefix(*loc, "/") {
		*loc = join(hostPath, strings.TrimPrefix(*loc, targetPath))
	}
}

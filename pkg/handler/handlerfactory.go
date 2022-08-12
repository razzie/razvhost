package handler

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/yookoala/gofast"
)

type HandlerFactory struct {
	phpClientFactory gofast.ClientFactory
}

// NewHandlerFactory returns a new HandlerFactory
func NewHandlerFactory(phpaddr *url.URL) *HandlerFactory {
	hf := &HandlerFactory{}
	if phpaddr != nil {
		hf.phpClientFactory = setupPHP(phpaddr)
	}
	return hf
}

func (hf *HandlerFactory) Handler(hostname string, target url.URL) (handler http.Handler, err error) {
	hostname, hostPath := splitHostnameAndPath(hostname)
	switch target.Scheme {
	case "file":
		handler = newFileServer(hostname, hostPath, target.Host+target.Path)
	case "http", "https":
		handler = newProxyHandler(hostname, hostPath, target)
	case "redirect":
		handler = newRedirectHandler(hostname, hostPath, target)
	case "s3":
		handler, err = newS3Handler(hostname, hostPath, target)
	case "sftp":
		handler, err = newSftpHandler(hostname, hostPath, target)
	case "php":
		handler, err = newPHPHandler(hf.phpClientFactory, hostname, hostPath, target.Host+target.Path)
	case "go-wasm":
		handler = newGoWasmHandler(hostname, hostPath, target.Host+target.Path)
	case "tail":
		handler = newTailHandler(hostname, hostPath, target.Host+target.Path, io.SeekStart)
	case "tail-new":
		handler = newTailHandler(hostname, hostPath, target.Host+target.Path, io.SeekEnd)
	default:
		err = fmt.Errorf("unknown target URL scheme: %s", target.Scheme)
	}
	return
}

func splitHostnameAndPath(hostname string) (string, string) {
	i := strings.Index(hostname, "/")
	if i == -1 {
		return hostname, ""
	}
	return hostname[:i], hostname[i:]
}

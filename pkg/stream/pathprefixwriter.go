package stream

import (
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
)

func NewPathPrefixHTMLResponseWriter(hostname, hostPath, targetPath string, w http.ResponseWriter) ResponseWriterCloser {
	var wg sync.WaitGroup
	var reader io.ReadCloser
	var writer io.WriteCloser
	reader, writer = io.Pipe()
	reader = NewPathPrefixHTMLStreamer(hostname, hostPath, targetPath, reader)
	wg.Add(1)
	go func() {
		if _, err := io.Copy(w, reader); err != nil {
			log.Println(err)
		}
		wg.Done()
	}()
	return &pathPrefixHTMLResponseWriter{
		w:          w,
		wg:         &wg,
		reader:     reader,
		writer:     writer,
		hostPath:   hostPath,
		targetPath: targetPath,
		hostname:   hostname,
	}
}

// ResponseWriterCloser is a closeable http.ResponseWriter
type ResponseWriterCloser interface {
	http.ResponseWriter
	io.Closer
}

type pathPrefixHTMLResponseWriter struct {
	w          http.ResponseWriter
	wg         *sync.WaitGroup
	reader     io.ReadCloser
	writer     io.WriteCloser
	hostPath   string
	targetPath string
	hostname   string
	isHTML     bool
	headerSent bool
}

func (w *pathPrefixHTMLResponseWriter) Header() http.Header {
	return w.w.Header()
}

func (w *pathPrefixHTMLResponseWriter) Write(p []byte) (int, error) {
	if !w.headerSent {
		w.Header().Set("Content-Type", http.DetectContentType(p))
		w.WriteHeader(http.StatusOK)
	}
	if w.isHTML {
		return w.writer.Write(p)
	}
	return w.w.Write(p)
}

func (w *pathPrefixHTMLResponseWriter) WriteHeader(statusCode int) {
	w.headerSent = true
	h := w.w.Header()
	if location := h.Get("Location"); len(location) > 0 {
		updateLocation(&location, w.hostname, w.hostPath, w.targetPath)
		h.Set("Location", location)
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

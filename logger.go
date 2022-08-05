package razvhost

import (
	"bufio"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mssola/user_agent"
)

func LoggerMiddleware(handler http.Handler) http.Handler {
	var counter uint32
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/favicon.ico") {
			handler.ServeHTTP(w, r)
			return
		}
		reqId := atomic.AddUint32(&counter, 1)
		ua := user_agent.New(r.UserAgent())
		browser, ver := ua.Browser()
		log.Printf("#%08x BEGIN - %s %s%s - %s (%s %s %s)",
			reqId,
			r.Method, r.Host, r.URL.RequestURI(),
			r.RemoteAddr, ua.OS(), browser, ver)

		rcount := NewReadCloserCounter(r.Body)
		r.Body = rcount
		wcount := NewResponseWriterCounter(w)
		started := time.Now()

		handler.ServeHTTP(wcount, r)

		elapsed := time.Since(started).Seconds()

		log.Printf("#%08x END - request: %s - response: %s - elapsed: %.6f sec",
			reqId,
			byteCountIEC(rcount.Count()),
			byteCountIEC(wcount.Count()),
			elapsed)
	})
}

type ReadCloserCounter struct {
	io.ReadCloser
	count int64
}

func NewReadCloserCounter(r io.ReadCloser) *ReadCloserCounter {
	return &ReadCloserCounter{
		ReadCloser: r,
	}
}

func (counter *ReadCloserCounter) Read(buf []byte) (n int, err error) {
	n, err = counter.ReadCloser.Read(buf)
	atomic.AddInt64(&counter.count, int64(n))
	return
}

func (counter *ReadCloserCounter) Close() error {
	return counter.ReadCloser.Close()
}

func (counter *ReadCloserCounter) Count() int64 {
	return atomic.LoadInt64(&counter.count)
}

type ResponseWriterCounter struct {
	http.ResponseWriter
	count int64
}

func NewResponseWriterCounter(w http.ResponseWriter) *ResponseWriterCounter {
	return &ResponseWriterCounter{
		ResponseWriter: w,
	}
}

func (counter *ResponseWriterCounter) Write(buf []byte) (n int, err error) {
	n, err = counter.ResponseWriter.Write(buf)
	atomic.AddInt64(&counter.count, int64(n))
	return
}

func (counter *ResponseWriterCounter) ReadFrom(r io.Reader) (n int64, err error) {
	if rf, ok := counter.ResponseWriter.(io.ReaderFrom); ok {
		n, err = rf.ReadFrom(r)
	} else {
		n, err = io.Copy(counter.ResponseWriter, r)
	}
	atomic.AddInt64(&counter.count, int64(n))
	return
}

func (counter *ResponseWriterCounter) Header() http.Header {
	return counter.ResponseWriter.Header()
}

func (counter *ResponseWriterCounter) WriteHeader(statusCode int) {
	counter.ResponseWriter.WriteHeader(statusCode)
}

func (counter *ResponseWriterCounter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return counter.ResponseWriter.(http.Hijacker).Hijack()
}

func (counter *ResponseWriterCounter) Flush() {
	if flusher, ok := counter.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (counter *ResponseWriterCounter) Count() int64 {
	return atomic.LoadInt64(&counter.count)
}

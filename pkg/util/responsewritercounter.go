package util

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"sync/atomic"
)

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

package util

import (
	"bufio"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestByteCountIEC(t *testing.T) {
	for _, tt := range []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"}, {1, "1 B"}, {1023, "1023 B"},
		{1024, "1.0 KiB"}, {1536, "1.5 KiB"},
		{1 << 20, "1.0 MiB"}, {1 << 30, "1.0 GiB"},
		{1 << 40, "1.0 TiB"}, {1 << 50, "1.0 PiB"},
		{1 << 60, "1.0 EiB"}, {math.MaxInt64, "8.0 EiB"},
	} {
		t.Run(tt.want, func(t *testing.T) {
			if got := ByteCountIEC(tt.bytes); got != tt.want {
				t.Fatalf("ByteCountIEC(%d) = %q, want %q", tt.bytes, got, tt.want)
			}
		})
	}
}

func TestReadCloserCounter(t *testing.T) {
	counter := NewReadCloserCounter(io.NopCloser(strings.NewReader("hello world")))
	defer counter.Close()
	if got := counter.Count(); got != 0 {
		t.Fatalf("initial count = %d", got)
	}
	buf := make([]byte, 5)
	if n, err := counter.Read(buf); n != 5 || err != nil || string(buf) != "hello" {
		t.Fatalf("first Read = (%d, %v), data = %q", n, err, buf)
	}
	if got := counter.Count(); got != 5 {
		t.Fatalf("partial count = %d, want 5", got)
	}
	remaining, err := io.ReadAll(counter)
	if err != nil || string(remaining) != " world" {
		t.Fatalf("remaining = %q, error = %v", remaining, err)
	}
	if got := counter.Count(); got != 11 {
		t.Fatalf("final count = %d, want 11", got)
	}
}

func TestReadCloserCounterPartialError(t *testing.T) {
	wantErr := errors.New("read failed")
	source := &counterErrorReader{err: wantErr}
	counter := NewReadCloserCounter(source)
	if n, err := counter.Read(make([]byte, 10)); n != 3 || !errors.Is(err, wantErr) {
		t.Fatalf("Read = (%d, %v), want (3, %v)", n, err, wantErr)
	}
	if got := counter.Count(); got != 3 {
		t.Fatalf("count = %d, want 3", got)
	}
	if err := counter.Close(); !errors.Is(err, wantErr) || !source.closed {
		t.Fatalf("Close error = %v, closed = %t", err, source.closed)
	}
}

type counterErrorReader struct {
	err    error
	closed bool
}

func (r *counterErrorReader) Read(p []byte) (int, error) { return copy(p, "abc"), r.err }
func (r *counterErrorReader) Close() error {
	r.closed = true
	return r.err
}

func TestResponseWriterCounter(t *testing.T) {
	recorder := httptest.NewRecorder()
	counter := NewResponseWriterCounter(recorder)
	if got := counter.Count(); got != 0 {
		t.Fatalf("initial count = %d", got)
	}
	counter.Header().Set("X-Test", "value")
	counter.WriteHeader(http.StatusCreated)
	if n, err := counter.Write([]byte("hello")); n != 5 || err != nil {
		t.Fatalf("Write = (%d, %v)", n, err)
	}
	if n, err := counter.ReadFrom(strings.NewReader(" world")); n != 6 || err != nil {
		t.Fatalf("ReadFrom = (%d, %v)", n, err)
	}
	counter.Flush()
	if got := counter.Count(); got != 11 {
		t.Fatalf("count = %d, want 11", got)
	}
	if recorder.Code != http.StatusCreated || recorder.Body.String() != "hello world" || recorder.Header().Get("X-Test") != "value" || !recorder.Flushed {
		t.Fatalf("unexpected response: %+v, body = %q", recorder, recorder.Body.String())
	}
}

func TestResponseWriterCounterPartialWrite(t *testing.T) {
	wantErr := errors.New("write failed")
	counter := NewResponseWriterCounter(&partialResponseWriter{ResponseWriter: httptest.NewRecorder(), err: wantErr})
	if n, err := counter.Write([]byte("abcdef")); n != 3 || !errors.Is(err, wantErr) {
		t.Fatalf("Write = (%d, %v), want (3, %v)", n, err, wantErr)
	}
	if got := counter.Count(); got != 3 {
		t.Fatalf("count = %d, want 3", got)
	}
}

type partialResponseWriter struct {
	http.ResponseWriter
	err error
}

func (w *partialResponseWriter) Write(p []byte) (int, error) {
	n, _ := w.ResponseWriter.Write(p[:min(3, len(p))])
	return n, w.err
}

func TestResponseWriterCounterReadFromDelegation(t *testing.T) {
	for _, wantErr := range []error{nil, errors.New("copy failed")} {
		w := &readerFromResponseWriter{ResponseWriter: httptest.NewRecorder(), err: wantErr}
		counter := NewResponseWriterCounter(w)
		if n, err := counter.ReadFrom(strings.NewReader("payload")); n != 3 || !errors.Is(err, wantErr) {
			t.Fatalf("ReadFrom = (%d, %v), want (3, %v)", n, err, wantErr)
		}
		if !w.called || counter.Count() != 3 {
			t.Fatalf("delegated = %t, count = %d", w.called, counter.Count())
		}
	}
}

type readerFromResponseWriter struct {
	http.ResponseWriter
	err    error
	called bool
}

func (w *readerFromResponseWriter) ReadFrom(r io.Reader) (int64, error) {
	w.called = true
	n, err := io.CopyN(w.ResponseWriter, r, 3)
	if err != nil {
		return n, err
	}
	return n, w.err
}

func TestResponseWriterCounterHijack(t *testing.T) {
	conn, peer := net.Pipe()
	defer conn.Close()
	defer peer.Close()
	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	for _, wantErr := range []error{nil, errors.New("hijack failed")} {
		w := &hijackResponseWriter{ResponseWriter: httptest.NewRecorder(), conn: conn, rw: rw, err: wantErr}
		counter := NewResponseWriterCounter(w)
		gotConn, gotRW, err := counter.Hijack()
		if gotConn != conn || gotRW != rw || !errors.Is(err, wantErr) {
			t.Fatalf("Hijack = (%v, %v, %v), want original connection, buffers and error", gotConn, gotRW, err)
		}
		if counter.Count() != 0 {
			t.Fatal("hijacking changed the byte count")
		}
	}
}

type hijackResponseWriter struct {
	http.ResponseWriter
	conn net.Conn
	rw   *bufio.ReadWriter
	err  error
}

func (w *hijackResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.conn, w.rw, w.err
}

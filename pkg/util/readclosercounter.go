package util

import (
	"io"
	"sync/atomic"
)

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

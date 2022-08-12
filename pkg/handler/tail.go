package handler

import (
	"io"
	"net/http"

	"github.com/hpcloud/tail"
)

func newTailHandler(hostname, hostPath, tailFile string, whence int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg := tail.Config{
			Follow:   true,
			Logger:   tail.DiscardingLogger,
			Location: &tail.SeekInfo{Offset: 0, Whence: whence},
		}
		t, err := tail.TailFile(tailFile, cfg)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		w.Header().Add("content-type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		f, _ := w.(http.Flusher)
		for {
			select {
			case <-r.Context().Done():
				return
			case line, ok := <-t.Lines:
				if !ok {
					return
				}
				io.WriteString(w, line.Text+"\n")
				if line.Err != nil {
					io.WriteString(w, line.Err.Error())
					return
				}
				if f != nil {
					f.Flush()
				}
			}
		}
	})
}

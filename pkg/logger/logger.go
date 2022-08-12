package logger

import (
	"log"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mssola/user_agent"
	"github.com/razzie/razvhost/pkg/util"
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

		rcount := util.NewReadCloserCounter(r.Body)
		r.Body = rcount
		wcount := util.NewResponseWriterCounter(w)
		started := time.Now()

		handler.ServeHTTP(wcount, r)

		elapsed := time.Since(started).Seconds()

		log.Printf("#%08x END - request: %s - response: %s - elapsed: %.6f sec",
			reqId,
			util.ByteCountIEC(rcount.Count()),
			util.ByteCountIEC(wcount.Count()),
			elapsed)
	})
}

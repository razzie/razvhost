package logger

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoggerMiddleware(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })
	h := LoggerMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		w.Header().Set("X-Test", "preserved")
		w.WriteHeader(http.StatusCreated)
		w.Write(data)
	}))
	for _, id := range []string{"#00000001", "#00000002"} {
		output.Reset()
		r := httptest.NewRequest(http.MethodPost, "http://example.test/upload?q=1", strings.NewReader("payload"))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusCreated || w.Body.String() != "payload" || w.Header().Get("X-Test") != "preserved" {
			t.Fatalf("unexpected response: %+v", w)
		}
		for _, want := range []string{id + " BEGIN", "POST example.test/upload?q=1", id + " END", "request: 7 B - response: 7 B - elapsed:"} {
			if !strings.Contains(output.String(), want) {
				t.Fatalf("missing %q in logs: %s", want, output.String())
			}
		}
	}
	output.Reset()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://example.test/favicon.ico", strings.NewReader("icon")))
	if output.Len() != 0 || w.Body.String() != "icon" {
		t.Fatalf("favicon response = %q, logs = %q", w.Body.String(), output.String())
	}
}

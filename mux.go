package razvhost

import (
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
)

// Mux is a http.Handler router similar to http.ServeMux, but with load balancing
type Mux struct {
	mtx      sync.RWMutex
	entries  []*muxEntry
	entryMap map[string]*muxEntry
}

// Add ...
func (m *Mux) Add(path string, handler http.Handler, target url.URL) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	if m.entryMap == nil {
		m.entryMap = make(map[string]*muxEntry)
	}

	if len(path) > 1 {
		handler = &trimPathHandler{path: path, handler: handler}
	}

	entry := m.entryMap[path]
	if entry != nil {
		entry.add(handler, target)
		return
	}

	entry = &muxEntry{path: path}
	entry.add(handler, target)
	m.entryMap[path] = entry

	for i, other := range m.entries {
		if len(path) > len(other.path) {
			m.entries = append(m.entries[:i+1], m.entries[i:]...)
			m.entries[i] = entry
			return
		}
	}

	m.entries = append(m.entries, entry)
}

// Remove ...
func (m *Mux) Remove(path string, target url.URL) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	entry := m.entryMap[path]
	if entry != nil {
		entry.remove(target)
	}
}

// Handler ...
func (m *Mux) Handler(path string) http.Handler {
	m.mtx.RLock()
	defer m.mtx.RUnlock()

	for _, entry := range m.entries {
		if strings.HasPrefix(path, entry.path) {
			if handler := entry.handler(); handler != nil {
				return handler
			}
		}
	}

	return nil
}

type muxEntry struct {
	path     string
	handlers []muxHandler
	next     uint32
}

func (e *muxEntry) add(handler http.Handler, target url.URL) {
	e.handlers = append(e.handlers, muxHandler{handler: handler, target: target})
}

func (e *muxEntry) remove(target url.URL) {
	for i, handler := range e.handlers {
		if handler.target == target {
			e.handlers = append(e.handlers[:i], e.handlers[i+1:]...)
			return
		}
	}
}

func (e *muxEntry) handler() http.Handler {
	handlersCount := uint32(len(e.handlers))
	if handlersCount == 0 {
		return nil
	}
	next := int(atomic.AddUint32(&e.next, 1) % handlersCount)
	return e.handlers[next].handler
}

type muxHandler struct {
	handler http.Handler
	target  url.URL
}

type trimPathHandler struct {
	path    string
	handler http.Handler
}

func (h trimPathHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r.URL.Path = strings.TrimPrefix(r.URL.Path, h.path)
	r.URL.RawPath = strings.TrimPrefix(r.URL.RawPath, h.path)
	if !strings.HasPrefix(r.URL.Path, "/") {
		r.URL.Path = "/" + r.URL.Path
		r.URL.RawPath = "/" + r.URL.RawPath
	}
	h.handler.ServeHTTP(w, r)
}

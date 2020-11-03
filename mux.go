package razvhost

import (
	"net/http"
	"net/url"
	"strings"
	"sync"
)

type mux struct {
	mtx      sync.Mutex
	entries  []*muxEntry
	entryMap map[string]*muxEntry
}

func (m *mux) add(path string, handler http.Handler, target url.URL) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	if m.entryMap == nil {
		m.entryMap = make(map[string]*muxEntry)
	}

	entry := m.entryMap[path]
	if entry != nil {
		entry.add(handler, target)
		return
	}

	entry = &muxEntry{path: path}
	entry.add(handler, target)

	for i, other := range m.entries {
		if len(path) > len(other.path) {
			m.entries = append(m.entries[:i+1], m.entries[i:]...)
			m.entries[i] = entry
			return
		}
	}

	m.entries = append(m.entries, entry)
}

func (m *mux) remove(path string, target url.URL) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	entry := m.entryMap[path]
	if entry != nil {
		entry.remove(target)
	}
}

func (m *mux) handler(path string) http.Handler {
	m.mtx.Lock()
	defer m.mtx.Unlock()

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
	next     int
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
	handlersCount := len(e.handlers)
	if handlersCount == 0 {
		return nil
	}
	e.next = (e.next + 1) % handlersCount
	return e.handlers[e.next].handler
}

type muxHandler struct {
	handler http.Handler
	target  url.URL
}

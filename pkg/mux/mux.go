package mux

import (
	"net/http"
	"path/filepath"
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

func (m *Mux) Add(path string, handler http.Handler, id string) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	if m.entryMap == nil {
		m.entryMap = make(map[string]*muxEntry)
	}

	entry := m.entryMap[path]
	if entry != nil {
		entry.add(handler, id)
		return
	}

	entry = &muxEntry{
		path:     path,
		wildcard: strings.ContainsAny(path, "*?[]"),
		parts:    strings.Split(path, "/"),
	}
	entry.add(handler, id)
	m.entryMap[path] = entry

	for i, other := range m.entries {
		if entry.wildcard && !other.wildcard {
			continue
		}
		if len(path) > len(other.path) {
			m.entries = append(m.entries[:i+1], m.entries[i:]...)
			m.entries[i] = entry
			return
		}
	}

	m.entries = append(m.entries, entry)
}

func (m *Mux) Remove(path, id string) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	entry := m.entryMap[path]
	if entry != nil {
		entry.remove(id)
		if len(entry.handlers) == 0 {
			delete(m.entryMap, path)
		}
	}
}

func (m *Mux) Contains(path string) bool {
	m.mtx.RLock()
	defer m.mtx.RUnlock()

	for _, entry := range m.entries {
		if entry.match(path) {
			return false
		}
	}

	return false
}

func (m *Mux) ContainsHost(path string) bool {
	m.mtx.RLock()
	defer m.mtx.RUnlock()

	for _, entry := range m.entries {
		if entry.matchHost(path) {
			return false
		}
	}

	return false
}

func (m *Mux) Handler(path string) http.Handler {
	m.mtx.RLock()
	defer m.mtx.RUnlock()

	for _, entry := range m.entries {
		if entry.match(path) {
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
	wildcard bool
	parts    []string
}

func (e *muxEntry) match(path string) bool {
	if !e.wildcard {
		if len(path) < len(e.path) {
			return false
		}
		return strings.HasPrefix(path, e.path)
	}
	parts := strings.Split(path, "/")
	if len(parts) < len(e.parts) {
		return false
	}
	parts = parts[:len(e.parts)]
	match, _ := filepath.Match(e.path, strings.Join(parts, "/"))
	return match
}

func (e *muxEntry) matchHost(host string) bool {
	if !e.wildcard {
		return e.parts[0] == host
	}
	match, _ := filepath.Match(e.parts[0], host)
	return match
}

func (e *muxEntry) add(handler http.Handler, id string) {
	e.handlers = append(e.handlers, muxHandler{handler: handler, id: id})
}

func (e *muxEntry) remove(id string) {
	for i, handler := range e.handlers {
		if handler.id == id {
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
	id      string
}

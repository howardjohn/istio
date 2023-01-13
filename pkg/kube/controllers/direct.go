package controllers

import (
	"github.com/sasha-s/go-deadlock"
	"golang.org/x/exp/maps"
)

type direct[O any] struct {
	handlers []func(O)
	objects  map[string]O
	mu       deadlock.RWMutex
}

func (h *direct[O]) Get(k string) *O {
	h.mu.RLock()
	defer h.mu.RUnlock()
	o, f := h.objects[k]
	if !f {
		return nil
	}
	return &o
}

func (h *direct[O]) Register(f func(O)) {
	h.handlers = append(h.handlers, f)
}

func (h *direct[O]) Handle(conv O) {
	for _, hh := range h.handlers {
		hh(conv)
	}
}

func (h *direct[O]) List() []O {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return maps.Values(h.objects)
}

func Direct[I any, O any](input Watcher[I], convert func(i I) O) Watcher[O] {
	h := &direct[O]{
		objects: make(map[string]O),
		mu:      deadlock.RWMutex{},
	}

	ti := *new(I)
	to := *new(O)
	handler := func(i I) *O {
		key := Key(i)
		log.Debugf("Direct[%T, %T]: event for %v", ti, to, key)
		cur := input.Get(key)
		if cur == nil {
			old, f := h.objects[key]
			if f {
				delete(h.objects, key)
				h.Handle(old)
				log.Debugf("Direct[%T, %T]: %v no longer exists (delete)", ti, to, key)
				return &old
			}
			log.Debugf("Direct[%T, %T]: %v no longer exists (delete) NOP", ti, to, key)
			return nil
		}
		conv := convert(*cur)
		exist, f := h.objects[key]
		if false { // conv == nil { TODO: convert return *O so they can skip
			// This is a delete
			if f {
				// Used to exist
				return &conv
			}
			return nil
		}
		updated := !Equal(conv, exist)
		log.Debugf("Direct[%T, %T]: conv %v, updated=%v", ti, to, key, updated)
		h.objects[key] = conv
		if updated {
			return &conv
		}
		return nil
	}
	input.Register(func(i I) {
		h.mu.Lock()
		out := handler(i)
		h.mu.Unlock()
		if out != nil {
			h.Handle(*out)
		}
	})
	return h
}

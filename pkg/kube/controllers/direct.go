package controllers

import (
	"fmt"
	"sync"

	"golang.org/x/exp/maps"
	"istio.io/istio/pkg/kube"
)

type direct[O any] struct {
	parent kube.Registerer
	handlers []func(O)
	objects  map[Key[O]]O
	mu       sync.RWMutex
	name     string
}

func (h *direct[O]) AddDependency(chain []string) {
		chain = append(chain, h.Name())
		h.parent.AddDependency(chain)
}

func (h *direct[O]) Get(k Key[O]) *O {
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
	if !h.mu.TryRLock() {
		panic("handle called with lock!")
	} else {
		h.mu.RUnlock()
	}
	for _, hh := range h.handlers {
		hh(conv)
	}
}

func (h *direct[O]) List() []O {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return maps.Values(h.objects)
}

func (h *direct[O]) Name() string {
	return h.name
}

func Direct[I any, O any](input Watcher[I], convert func(i I) *O) Watcher[O] {
	ti := *new(I)
	to := *new(O)
	h := &direct[O]{
		objects: make(map[Key[O]]O),
		mu:      sync.RWMutex{},
		name: fmt.Sprintf("direct[%T,%T]", ti, to),
		parent: input,
	}
	input.AddDependency([]string{h.name})

	log := log.WithLabels("origin", h.Name())
	handler := func(i I) *O {
		key := GetKey(i)
		conv := convert(i)
		log := log.WithLabels("key", key)
		log.Debugf("event")

		cur := input.Get(key)
		if cur == nil { // It was a delete...
			if conv == nil { // and converted to nil... this shouldn't happen
				log.Errorf("Double deletion")
				return nil
			}
			oKey := GetKey(*conv)
			old, f := h.objects[oKey]
			if f {
				delete(h.objects, oKey)
				log.Debugf("no longer exists (delete)")
				return &old
			}
			log.Debugf("no longer exists (delete) NOP")
			return nil
		}
		if conv == nil {
			log.WithLabels("updated", "nil").Debugf("converted")
			return nil
		}
		oKey := GetKey(*conv)
		exist, f := h.objects[oKey]
		updated := !f || !Equal(*conv, exist)
		log.WithLabels("updated", updated).Debugf("converted")
		h.objects[GetKey(*conv)] = *conv
		if updated {
			return conv
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



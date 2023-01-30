package controllers

import (
	"fmt"

	"go.uber.org/atomic"

	"istio.io/istio/pkg/kube"
)

type singleton[O any] struct {
	parent   kube.Registerer
	name     string
	handlers []func(O)
	item     *atomic.Pointer[O]
}

func (s *singleton[O]) Name() string {
	return s.name
}

func (s *singleton[O]) Handle(conv O) {
	for _, hh := range s.handlers {
		hh(conv)
	}
}

func (s *singleton[O]) Register(f func(O)) {
	s.handlers = append(s.handlers, f)
}

func (s *singleton[O]) List() []O {
	if i := s.item.Load(); i != nil {
		return []O{*i}
	}
	return nil
}

func (s *singleton[O]) Get(k Key[O]) *O {
	return s.item.Load()
}

func (s *singleton[O]) AddDependency(chain []string) {
	chain = append(chain, s.Name())
	s.parent.AddDependency(chain)
}

func Singleton[I any, O any](name string, input Watcher[I], convert func(i I) O) Watcher[O] {
	ti := *new(I)
	to := *new(O)
	if name == "" {
		name = fmt.Sprintf("singleton[%T,%T]", ti, to)
	}
	h := &singleton[O]{
		item:   atomic.NewPointer[O](nil),
		name:   name,
		parent: input,
	}
	input.AddDependency([]string{h.name})
	input.Register(func(i I) {
		cur := input.Get(GetKey(i))
		if cur == nil {
			// Delete
			old := h.item.Swap(nil)
			if old != nil {
				h.Handle(*old)
			}
			return
		}
		conv := convert(*cur)
		prev := h.item.Swap(&conv)
		if prev == nil || !Equal(*prev, conv) {
			h.Handle(conv)
		}
	})
	return h
}

var _ Watcher[any] = &singleton[any]{}

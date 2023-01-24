package cv2

import (
	"sync"

	"go.uber.org/atomic"

	"istio.io/istio/pkg/kube/controllers"
)

// singletonAdapter exposes a singleton as a collection
type singletonAdapter[T any] struct {
	s Singleton[T]
}

func (s singletonAdapter[T]) Register(f func(o Event)) {
	s.s.Register(f)
}

func (s singletonAdapter[T]) GetKey(k Key[T]) *T {
	return s.s.Get()
}

func (s singletonAdapter[T]) List(namespace string) []T {
	res := s.s.Get()
	if res == nil {
		return nil
	}
	return []T{*res}
}

var _ Collection[any] = &singletonAdapter[any]{}

func NewSingleton[T any](hf HandleEmpty[T]) Singleton[T] {
	h := &singleton[T]{
		handle: hf,
		deps: dependencies{
			deps:      map[depKey]dependency{},
			finalized: false,
		},
		state: atomic.NewPointer[T](nil),
	}
	h.execute = func() {
		res := hf(h)
		oldRes := h.state.Swap(res)
		updated := !controllers.Equal(res, oldRes)
		log.Errorf("howardjohn: updated %v", updated)
		if updated {
			for _, handler := range h.handlers {
				event := controllers.EventUpdate
				if oldRes == nil {
					event = controllers.EventAdd
				} else if res == nil {
					event = controllers.EventDelete
				}
				handler(Event{
					Old:   oldRes,
					New:   res,
					Event: event,
				})
			}
		}
	}
	// Run the singleton, but do not persist state. This is just to register dependencies
	// I suppose we could make this also persist state
	// hf(h)
	// TODO: wait for dependencies to be ready
	// Populate initial state. It is a singleton so we don't have any hard dependencies
	h.execute()
	h.deps.finalized = true
	mu := sync.Mutex{}
	for _, dep := range h.deps.deps {
		dep := dep
		log := log.WithLabels("dep", dep.key)
		log.Infof("insert dep, filter: %+v", dep.filter)
		dep.dep.Register(func(o Event) {
			mu.Lock()
			defer mu.Unlock()
			log.Debugf("got event %v", o.Event)
			switch o.Event {
			case controllers.EventAdd:
				if dep.filter.Matches(o.New) {
					log.Debugf("Add match %v", GetName(o.New))
					h.execute()
				} else {
					log.Debugf("Add no match %v", GetName(o.New))
				}
			case controllers.EventDelete:
				if dep.filter.Matches(o.Old) {
					log.Debugf("delete match %v", GetName(o.Old))
					h.execute()
				} else {
					log.Debugf("Add no match %v", GetName(o.Old))
				}
			case controllers.EventUpdate:
				if dep.filter.Matches(o.New) {
					log.Debugf("Update match %v", GetName(o.New))
					h.execute()
				} else if dep.filter.Matches(o.Old) {
					log.Debugf("Update no match, but used to %v", GetName(o.New))
					h.execute()
				} else {
					log.Debugf("Update no change")
				}
			}
		})
	}
	return h
}

type singleton[T any] struct {
	deps     dependencies
	handle   any
	handlers []func(o Event)
	state    *atomic.Pointer[T]
	execute  func()
}

func (h *singleton[T]) _internalHandler() {
}

func (h *singleton[T]) AsCollection() Collection[T] {
	return singletonAdapter[T]{h}
}

func (h *singleton[T]) Register(f func(o Event)) {
	h.handlers = append(h.handlers, f)
}

func (h *singleton[T]) getDeps() dependencies {
	return h.deps
}

func (h *singleton[T]) Get() *T {
	return h.state.Load()
}

func (h *singleton[T]) GetKey(k Key[T]) *T {
	// TODO implement me
	panic("implement me")
}

func (h *singleton[T]) List(namespace string) []T {
	// TODO implement me
	panic("implement me")
}

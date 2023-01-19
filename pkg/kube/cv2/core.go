package cv2

import (
	"fmt"
	"reflect"
	"sync"

	"go.uber.org/atomic"
	"istio.io/istio/pkg/kube/controllers"
	istiolog "istio.io/pkg/log"
)

var log = istiolog.RegisterScope("cv2", "", 0)

type erasedCollection interface {
	// TODO: cannot use Event as it assumes Object
	Register(f func(o controllers.Event))
}

type Collection[T any] interface {
	Get(k Key[T]) *T
	List(namespace string) []T
	Register(f func(o controllers.Event))
}

type Singleton[T any] interface {
	Get() *T
}

// singletonAdapter exposes a singleton as a collection
type singletonAdapter[T any] struct {
	s Singleton[T]
}

func (s singletonAdapter[T]) Register(f func(o controllers.Event)) {
	//TODO implement me
	panic("implement me")
}

func (s singletonAdapter[T]) Get(k Key[T]) *T {
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

// Key is a string, but with a type associated to avoid mixing up keys
type Key[O any] string

func Map[T, U any](data []T, f func(T) U) []U {
	res := make([]U, 0, len(data))
	for _, e := range data {
		res = append(res, f(e))
	}
	return res
}

func Filter[T any](data []T, f func(T) bool) []T {
	fltd := make([]T, 0, len(data))
	for _, e := range data {
		if f(e) {
			fltd = append(fltd, e)
		}
	}
	return fltd
}

type filter struct {
	name      string
	namespace string
}

func GetName(a any) string {
	ak, ok := a.(controllers.Object)
	if ok {
		return ak.GetName()
	}
	panic(fmt.Sprintf("No Name, got %T", a))
	return ""
}

func GetNamespace(a any) string {
	ak, ok := a.(controllers.Object)
	if ok {
		return ak.GetNamespace()
	}
	panic(fmt.Sprintf("No Namespace, got %T", a))
	return ""
}

func (f filter) Matches(object any) bool {
	if f.name != "" && f.name != GetName(object) {
		log.Debugf("no match name: %q vs %q", f.name, GetName(object))
		return false
	} else {
		log.Debugf("matches name: %q vs %q", f.name, GetName(object))
	}
	if f.namespace != "" && f.namespace != GetNamespace(object) {
		log.Debugf("no match namespace: %q vs %q", f.namespace, GetNamespace(object))
		return false
	} else {
		log.Debugf("matches namespace: %q vs %q", f.namespace, GetNamespace(object))
	}
	return true
}

type dependency struct {
	key    depKey
	dep    erasedCollection
	filter filter
}

type depKey struct {
	// Explicit name
	name string
	// Type. If there are multiple with the same type, name is required
	dtype reflect.Type
}

func (d depKey) String() string {
	return fmt.Sprintf("%v/%v", d.dtype.Name(), d.name)
}

type handler[T any] struct {
	deps    map[depKey]dependency
	handle  any
	state   *atomic.Pointer[T]
	execute func()
}

type depper interface {
	getDeps() map[depKey]dependency
}

func (h *handler[T]) getDeps() map[depKey]dependency {
	return h.deps
}

func (h handler[T]) Get() *T {
	return h.state.Load()
}

func DependOnCollection[T any](c Collection[T], opts ...DepOption) Option {
	return func(deps map[depKey]dependency) {
		d := dependency{
			dep: c,
			key: depKey{dtype: getType[T]()},
		}
		for _, o := range opts {
			o(&d)
		}
		if _, f := deps[d.key]; f {
			panic(fmt.Sprintf("Conflicting dependency already registered, %+v", d.key))
		}
		deps[d.key] = d
	}
}

func FilterName(name string) DepOption {
	return func(h *dependency) {
		h.filter.name = name
		h.key.name = name
	}
}

func Named(name string) DepOption {
	return func(h *dependency) {
		h.key.name = name
	}
}

type HandlerContext interface {
	_internalHandler()
}

type hc struct{}

// TODO
func (h hc) _internalHandler() {}

// Todo.. we need a way to get the current context
func FetchOneNamed[T any](ctx HandlerContext, name string) *T {
	key := depKey{
		name:  name,
		dtype: getType[T](),
	}
	h := ctx.(depper)
	d, f := h.getDeps()[key]
	if !f {
		panic(fmt.Sprintf("Dependency for %v not found", key))
	}
	dep := d.dep.(Collection[T])
	// TODO: list it....
	_ = d
	// name is wrong
	var res *T
	for _, c := range dep.List(d.filter.namespace) {
		if d.filter.Matches(c) {
			if res != nil {
				panic("FetchOne got multiple resources")
			}
			res = &c
		}
	}
	return res
}

type DepOption func(*dependency)
type Option func(map[depKey]dependency)

type HandleEmpty[T any] func(ctx HandlerContext) *T

func NewSingleton[T any](hf HandleEmpty[T], opts ...Option) Singleton[T] {
	h := &handler[T]{
		handle: hf,
		deps:   map[depKey]dependency{},
		state:  atomic.NewPointer[T](nil),
	}
	h.execute = func() {
		res := hf(h)
		oldRes := h.state.Swap(res)
		updated := controllers.Equal(res, oldRes)
		log.Errorf("howardjohn: updated %v", updated) // TODO: compare
		// TODO: propogate event
	}
	for _, o := range opts {
		o(h.deps)
	}
	// Populate initial state. It is a singleton so we don't have any hard dependencies
	h.execute()
	mu := sync.Mutex{}
	for _, dep := range h.deps {
		dep := dep
		log := log.WithLabels("dep", dep.key)
		log.Infof("insert dep, filter: %+v", dep.filter)
		dep.dep.Register(func(o controllers.Event) {
			mu.Lock()
			defer mu.Unlock()
			log.Debugf("got event %v", o.Event)
			switch o.Event {
			case controllers.EventAdd:
				if dep.filter.Matches(o.New) {
					log.Debugf("Add match %v", o.New.GetName())
					h.execute()
				} else {
					log.Debugf("Add no match %v", o.New.GetName())
				}
			case controllers.EventDelete:
				if dep.filter.Matches(o.Old) {
					log.Debugf("delete match %v", o.Old.GetName())
					h.execute()
				} else {
					log.Debugf("Add no match %v", o.Old.GetName())
				}
			case controllers.EventUpdate:
				if dep.filter.Matches(o.New) {
					log.Debugf("Update match %v", o.New.GetName())
					h.execute()
				} else if dep.filter.Matches(o.Old) {
					log.Debugf("Update no match, but used to %v", o.New.GetName())
					h.execute()
				} else {
					log.Debugf("Update no change")
				}
			}
		})
	}
	return h
}

func (h *handler[T]) _internalHandler() {

}

func (h *handler[T]) FetchOneNamed() {

}

func getType[T any]() reflect.Type {
	return reflect.TypeOf(*new(T)).Elem()
}

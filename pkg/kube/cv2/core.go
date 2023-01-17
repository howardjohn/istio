package cv2

import (
	"fmt"
	"reflect"
)

type erasedCollection interface {

}

type Collection[T any] interface {
	Get(k Key[T]) *T
	List(namespace string) []T
}

type Singleton[T any] interface {
	Get() *T
}

// singletonAdapter exposes a singleton as a collection
type singletonAdapter[T any] struct {
	s Singleton[T]
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
	name string
	namespace string
}

type dependency struct {
	key depKey
	dep erasedCollection
	filter filter
}

type depKey struct {
	// Explicit name
	name string
	// Type. If there are multiple with the same type, name is required
	dtype string
}

type handler struct {
	deps   map[depKey]dependency
	handle any
}

func (h handler) Get() *T {
	//TODO implement me
	panic("implement me")
}

func DependOnCollection[T any](c Collection[T], opts ...DepOption) Option {
	return func(h *handler) {
		d := dependency{
			dep: c,
			key: depKey{dtype: getType[T]()},
		}
		for _, o := range opts {
			o(&d)
		}
		if _, f := h.deps[d.key]; f {
			panic(fmt.Sprintf("Conflicting dependency already registered, %+v", d.key))
		}
		h.deps[d.key] = d
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

// Todo.. we need a way to get the current context
func FetchOneNamed[T any](ctx HandlerContext, name string) *T {

}

type DepOption func(*dependency)
type Option func(*handler)

type HandleEmpty[T any] func(ctx HandlerContext) *T

func NewSingleton[T any](hf HandleEmpty[T], opts ...Option) Singleton[T] {
	h := &handler{
		handle: hf,
	}
	for _, o := range opts {
		o(h)
	}
	return h
}

func (h *handler) _internalHandler() {

}

func getType[T any]() string {
	if t := reflect.TypeOf(*new(T)); t.Kind() == reflect.Ptr {
		return "*" + t.Elem().Name()
	} else {
		return t.Name()
	}
}
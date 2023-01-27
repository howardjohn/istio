package cv2

import (
	"fmt"
	"reflect"

	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/util/sets"
	istiolog "istio.io/pkg/log"
)

var log = istiolog.RegisterScope("cv2", "", 0)

type Collection[T any] interface {
	GetKey(k Key[T]) *T
	List(namespace string) []T
	Register(f func(o Event[T]))
}

type Singleton[T any] interface {
	Get() *T
	Register(f func(o Event[T]))
	AsCollection() Collection[T]
}

type erasedCollection interface {
	register(f func(o Event[any]))
	hash() string
}

type erasedCollectionImpl struct {
	r func(f func(o Event[any]))
	h string
}

func (e erasedCollectionImpl) hash() string {
	return e.h
}

func (e erasedCollectionImpl) register(f func(o Event[any])) {
	e.r(f)
}

func eraseCollection[T any](c Collection[T]) erasedCollection {
	return erasedCollectionImpl{
		h: fmt.Sprintf("%p", c),
		r: func(f func(o Event[any])) {
			c.Register(func(o Event[T]) {
				f(castEvent[T, any](o))
			})
		},
	}
}

func castEvent[I, O any](o Event[I]) Event[O] {
	e := Event[O]{
		Event: o.Event,
	}
	if o.Old != nil {
		e.Old = Ptr(any(*o.Old).(O))
	}
	if o.New != nil {
		e.New = Ptr(any(*o.New).(O))
	}
	return e
}

// Key is a string, but with a type associated to avoid mixing up keys
type Key[O any] string

type resourceNamer interface {
	ResourceName() string
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

type dependencies struct {
	deps      map[depKey]dependency
	finalized bool
}

type index[T any] struct {
	objects   map[Key[T]]T
	namespace map[string]sets.Set[Key[T]]
}

type registerer interface {
	register(c erasedCollection)
}

type depper interface {
	getDeps() dependencies
}

type Event[T any] struct {
	Old   *T
	New   *T
	Event controllers.EventType
}

func (e Event[T]) Items() []T {
	res := make([]T, 0, 2)
	if e.Old != nil {
		res = append(res, *e.Old)
	}
	if e.New != nil {
		res = append(res, *e.New)
	}
	return res
}

func (e Event[T]) Latest() T {
	if e.New != nil {
		return *e.New
	}
	return *e.Old
}

type HandlerContext interface {
	_internalHandler()
}

type (
	DepOption func(*dependency)
	Option    func(map[depKey]dependency)
)

type (
	HandleEmpty[T any]     func(ctx HandlerContext) *T
	HandleSingle[I, O any] func(ctx HandlerContext, i I) *O
)

func IsNil(v any) bool {
	return v == nil || (reflect.ValueOf(v).Kind() == reflect.Ptr && reflect.ValueOf(v).IsNil())
}

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
	// TODO: generic Event
	Register(f func(o Event))
}

type Singleton[T any] interface {
	Get() *T
	Register(f func(o Event))
	AsCollection() Collection[T]
}

type erasedCollection interface {
	// TODO: cannot use Event as it assumes Object
	Register(f func(o Event))
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

type Event struct {
	Old   any
	New   any
	Event controllers.EventType
}

func (e Event) Latest() any {
	if e.New != nil {
		return e.New
	}
	return e.Old
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

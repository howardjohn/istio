package cv2

import (
	"fmt"
	"reflect"
	"sync"

	"go.uber.org/atomic"
	"istio.io/api/type/v1beta1"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/util/sets"
	istiolog "istio.io/pkg/log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

var log = istiolog.RegisterScope("cv2", "", 0)

type erasedCollection interface {
	// TODO: cannot use Event as it assumes Object
	Register(f func(o controllers.Event))
}

type Collection[T any] interface {
	GetKey(k Key[T]) *T
	List(namespace string) []T
	Register(f func(o controllers.Event))
}

type Singleton[T any] interface {
	Get() *T
	Register(f func(o controllers.Event))
}

// singletonAdapter exposes a singleton as a collection
type singletonAdapter[T any] struct {
	s Singleton[T]
}

func (s singletonAdapter[T]) Register(f func(o controllers.Event)) {
	//TODO implement me
	panic("implement me")
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

// Key is a string, but with a type associated to avoid mixing up keys
type Key[O any] string

func GetKey[O any](a O) Key[O] {
	ao, ok := any(a).(controllers.Object)
	if ok {
		k, _ := cache.MetaNamespaceKeyFunc(ao)
		return Key[O](k)
	}
	panic(fmt.Sprintf("Cannot get Key, got %T", a))
	return ""
}
func Map[T, U any](data []T, f func(T) U) []U {
	res := make([]U, 0, len(data))
	for _, e := range data {
		res = append(res, f(e))
	}
	return res
}

func AppendNonNil[T any](data []T, i *T) []T {
	if i != nil {
		data = append(data, *i)
	}
	return data
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
	selects   map[string]string
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

func GetLabels(a any) map[string]string {
	ak, ok := a.(controllers.Object)
	if ok {
		return ak.GetLabels()
	}
	panic(fmt.Sprintf("No Labels, got %T", a))
	return nil
}

func GetLabelSelector(a any) map[string]string {
	val := reflect.ValueOf(a)

	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	specField := val.FieldByName("Spec")
	if !specField.IsValid() {
		log.Debugf("obj %T has no Spec", a)
		return nil
	}

	labelsField := specField.FieldByName("Selector")
	if !labelsField.IsValid() {
		log.Debugf("obj %T has no Selector", a)
		return nil
	}

	switch s := labelsField.Interface().(type) {
	case *v1beta1.WorkloadSelector:
		return s.MatchLabels
	case map[string]string:
		return s
	default:
		log.Debugf("obj %T has unknown Selector", s)
		return nil
	}
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
	if f.selects != nil && !labels.Instance(f.selects).SubsetOf(GetLabelSelector(object)) {
		log.Debugf("no match selects: %q vs %q", f.selects, GetLabelSelector(object))
		return false
	} else {
		log.Debugf("matches selects: %q vs %q", f.selects, GetLabelSelector(object))
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

type dependencies struct {
	deps      map[depKey]dependency
	finalized bool
}

type handler[T any] struct {
	deps           dependencies
	handle         any
	state          *atomic.Pointer[T]
	collectionState *mutexGuard[index[T]]
	execute        func()
	executeOne        func(i any) // always I but erased
}

type index[T any] struct {
	objects   map[Key[T]]T
	namespace map[string]sets.Set[Key[T]]
}

type mutexGuard[T any] struct {
	mu   sync.Mutex
	data T
}

func newMutex[T any](initial T) *mutexGuard[T] {
	return &mutexGuard[T]{data: initial}
}

func (m *mutexGuard[T]) With(f func(T)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f(m.data)
}

type depper interface {
	getDeps() dependencies
}

func (h *handler[T]) getDeps() dependencies {
	return h.deps
}

func (h handler[T]) Get() *T {
	return h.state.Load()
}

func (h *handler[T]) GetKey(k Key[T]) *T {
	//TODO implement me
	panic("implement me")
}

func (h *handler[T]) List(namespace string) []T {
	//TODO implement me
	panic("implement me")
}

func (h *handler[T]) Register(f func(o controllers.Event)) {
	//TODO implement me
	panic("implement me")
}

func FilterName(name string) DepOption {
	return func(h *dependency) {
		h.filter.name = name
		h.key.name = name
	}
}

func FilterSelects(lbls map[string]string) DepOption {
	return func(h *dependency) {
		h.filter.selects = lbls
	}
}

type HandlerContext interface {
	_internalHandler()
}

func Fetch[T any](ctx HandlerContext, c Collection[T], opts ...DepOption) []T {
	// First, set up the dependency. On first run, this will be new.
	// One subsequent runs, we just validate
	h := ctx.(depper)
	d := dependency{
		dep: c,
		key: depKey{dtype: getType[T]()},
	}
	for _, o := range opts {
		o(&d)
	}
	deps := h.getDeps()
	_, exists := deps.deps[d.key]
	if exists && !deps.finalized {
		panic(fmt.Sprintf("dependency already registered, %+v", d.key))
	}
	if !exists && deps.finalized {
		panic(fmt.Sprintf("dependency registered after initialization, %+v", d.key))
	}
	deps.deps[d.key] = d
	log.Errorf("howardjohn: register %+v", d.key)

	if !deps.finalized {
		return nil
	}

	// Now we can do the real fetching
	var res []T
	for _, c := range c.List(d.filter.namespace) {
		c := c
		if d.filter.Matches(c) {
			res = append(res, c)
		}
	}
	return res
}

func FetchOne[T any](ctx HandlerContext, c Collection[T], opts ...DepOption) *T {
	res := Fetch[T](ctx, c, opts...)
	switch len(res) {
	case 0:
		return nil
	case 1:
		return &res[0]
	default:
		panic(fmt.Sprintf("FetchOne found for more than 1 item"))
	}
}

func nilSafeDeref[T any](i *T) any {
	if i == nil {
		return i
	}
	return *i
}

type DepOption func(*dependency)
type Option func(map[depKey]dependency)

type HandleEmpty[T any] func(ctx HandlerContext) *T
type HandleSingle[I, O any] func(ctx HandlerContext, i I) *O

func NewCollection[I, O any](c Collection[I], hf HandleSingle[I, O], opts ...Option) Collection[O] {
	h := &handler[O]{
		handle: hf,
		deps: dependencies{
			deps:      map[depKey]dependency{},
			finalized: false,
		},
		collectionState: newMutex(index[O]{
			objects:   map[Key[O]]O{},
			namespace: map[string]sets.Set[Key[O]]{},
		}),

	}
	h.executeOne = func(a any) {
		i := a.(I)
		// TODO: pass H with "I" key as context. Dependencies are keyed by I
		res := hf(h, i)
		// TODO: not 'state'
		h.collectionState.With(func(i index[O]) {
			// TODO: we need the old key and new key
			// Otherwise we cannot delete
			if res == nil {
				log.Errorf("howardjohn: TODO!!! delete")
			} else {
				oKey := GetKey(*res)
				oldRes := i.objects[oKey]
				i.objects[oKey] = *res
				updated := controllers.Equal(*res, oldRes)
				log.Errorf("howardjohn: updated %v", updated)
			}
		})
		// TODO: propogate event
	}
	for _, o := range opts {
		o(h.deps.deps)
	}
	// TODO: wait for dependencies to be ready
	for _, i := range c.List(metav1.NamespaceAll) {
		h.executeOne(i)
	}
	h.deps.finalized = true
	// Setup primary handler. On any change, trigger only that one
	c.Register(func(o controllers.Event) {
		log := log.WithLabels("dep", "primary")
		log.Debugf("got event %v", o.Event)
		switch o.Event {
		case controllers.EventAdd:
				h.executeOne(o.New)
		case controllers.EventDelete:
			// TODO: just delete, never need to re-run
		case controllers.EventUpdate:
			h.executeOne(o.New)
		}
	})
	// TODO: handle sub-deps
	//mu := sync.Mutex{}
	//for _, dep := range h.deps.deps {
	//	dep := dep
	//	log := log.WithLabels("dep", dep.key)
	//	log.Infof("insert dep, filter: %+v", dep.filter)
	//	dep.dep.Register(func(o controllers.Event) {
	//		mu.Lock()
	//		defer mu.Unlock()
	//		log.Debugf("got event %v", o.Event)
	//		switch o.Event {
	//		case controllers.EventAdd:
	//			if dep.filter.Matches(o.New) {
	//				log.Debugf("Add match %v", o.New.GetName())
	//				h.executeOne()
	//			} else {
	//				log.Debugf("Add no match %v", o.New.GetName())
	//			}
	//		case controllers.EventDelete:
	//			if dep.filter.Matches(o.Old) {
	//				log.Debugf("delete match %v", o.Old.GetName())
	//				h.executeOne()
	//			} else {
	//				log.Debugf("Add no match %v", o.Old.GetName())
	//			}
	//		case controllers.EventUpdate:
	//			if dep.filter.Matches(o.New) {
	//				log.Debugf("Update match %v", o.New.GetName())
	//				h.executeOne()
	//			} else if dep.filter.Matches(o.Old) {
	//				log.Debugf("Update no match, but used to %v", o.New.GetName())
	//				h.executeOne()
	//			} else {
	//				log.Debugf("Update no change")
	//			}
	//		}
	//	})
	//}
	return h
}

func NewSingleton[T any](hf HandleEmpty[T], opts ...Option) Singleton[T] {
	h := &handler[T]{
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
		updated := controllers.Equal(res, oldRes)
		log.Errorf("howardjohn: updated %v", updated) // TODO: compare
		// TODO: propogate event
	}
	for _, o := range opts {
		o(h.deps.deps)
	}
	// Run the handler, but do not persist state. This is just to register dependencies
	// I suppose we could make this also persist state
	//hf(h)
	// TODO: wait for dependencies to be ready
	// Populate initial state. It is a singleton so we don't have any hard dependencies
	h.execute()
	h.deps.finalized = true
	mu := sync.Mutex{}
	for _, dep := range h.deps.deps {
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

func getType[T any]() reflect.Type {
	return reflect.TypeOf(*new(T)).Elem()
}

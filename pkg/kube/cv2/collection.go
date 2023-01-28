package cv2

import (
	"sync"

	"golang.org/x/exp/maps"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/util/sets"
)

type collection[I, O any] struct {
	collectionState *mutexGuard[index[O]]
	handle          HandleSingle[I, O]
	dependencies    sets.String
	handlers        []func(o []Event[O])
	parent          Collection[I]
	execute         func(i []any)
	deps            map[Key[I]]dependencies
	mu              sync.Mutex
}

func NewCollection[I, O any](c Collection[I], hf HandleSingle[I, O]) Collection[O] {
	// We need a set of handlers
	h := &collection[I, O]{
		handle:       hf,
		parent:       c,
		dependencies: sets.New[string](),
		deps:         map[Key[I]]dependencies{},
		collectionState: newMutex(index[O]{
			objects:   map[Key[O]]O{},
			namespace: map[string]sets.Set[Key[O]]{},
		}),
	}
	h.execute = func(items []any) {
		var events []Event[O]
		for _, a := range items {
			i := a.(I)

			iKey := GetKey(i)
			log := log.WithLabels("key", iKey)

			h.mu.Lock()
			d, f := h.deps[iKey]
			if !f {
				// TODO mutex
				d = dependencies{
					deps:      map[depKey]dependency{},
					finalized: true, // TODO: set this to rtue at some point
				}
				h.deps[iKey] = d
			}
			h.mu.Unlock()
			// Give them a context for this specific input
			ctx := &indexedCollection[I, O]{h, d}
			res := hf(ctx, i)
			h.collectionState.With(func(i index[O]) {
				// TODO: we need the old key and new key
				// Otherwise we cannot delete
				if res == nil {
					log.Errorf("howardjohn: TODO!!! delete")
				} else {
					oKey := GetKey(*res)
					oldRes, oldExists := i.objects[oKey]
					i.objects[oKey] = *res
					updated := !controllers.Equal(*res, oldRes)
					e := Event[O]{}
					if !oldExists {
						e.Event = controllers.EventAdd
						e.New = res
					} else if res == nil {
						e.Event = controllers.EventDelete
						e.Old = &oldRes
					} else {
						e.Event = controllers.EventUpdate
						e.New = res
						e.Old = &oldRes
					}
					events = append(events, e)
					log.WithLabels("updated", updated).Debugf("handled")
				}
			})
		}
		for _, handler := range h.handlers {
			handler(events)
		}
	}
	// TODO: wait for dependencies to be ready
	h.execute(Map(c.List(metav1.NamespaceAll), Cast[I, any]))
	// Setup primary singleton. On any change, trigger only that one
	c.RegisterBatch(func(events []Event[I]) {
		log := log.WithLabels("dep", "primary")
		log.Debugf("got event batch %v", len(events))
		h.execute(Map(MapFilter(events, func(t Event[I]) *I {
			return t.New
		}), Cast[I, any]))
	})
	return h
}

func (h *collection[I, O]) handler() func(events []Event[any]) {
	return func(events []Event[any]) {
		h.mu.Lock()
		// Got an event. Now we need to find out who depends on it..
		ks := sets.Set[Key[I]]{}
		// Check old and new
		for _, ev := range events {
			for i, v := range h.deps {
				for _, item := range ev.Items() {
					named := depKey{
						name:  GetName(item),
						dtype: GetTypeOf(item),
					}
					if d, f := v.deps[named]; f {
						match := d.filter.Matches(item)
						log.WithLabels("match", match).Infof("event for %v", named)
						if match {
							ks.Insert(i)
							break
						}
					}
					unnamed := depKey{
						dtype: GetTypeOf(item),
					}
					if d, f := v.deps[unnamed]; f {
						match := d.filter.Matches(item)
						log.WithLabels("match", match).Infof("event for collection %v", named)
						if match {
							ks.Insert(i)
							break
						}
					}
				}
			}
		}
		h.mu.Unlock()
		log.Infof("collection event size %v, trigger %v dependencies", len(events), len(ks))
		toRun := make([]any, 0, len(ks))
		for i := range ks {
			ii := h.parent.GetKey(i)
			if ii == nil {
				log.Errorf("BUG: Parent missing key!! %v", i)
			} else {
				toRun = append(toRun, *ii)
			}
		}
		h.execute(toRun)
	}
}

func (h *collection[I, O]) _internalHandler() {
}

func (h *collection[I, O]) GetKey(k Key[O]) (res *O) {
	h.collectionState.With(func(i index[O]) {
		rf, f := i.objects[k]
		if f {
			res = &rf
		}
	})
	return
}

func (h *collection[I, O]) List(namespace string) (res []O) {
	h.collectionState.With(func(i index[O]) {
		if namespace == "" {
			res = maps.Values(i.objects)
		} else {
			for key := range i.namespace[namespace] {
				res = append(res, i.objects[key])
			}
		}
	})
	return
}

func (h *collection[I, O]) Register(f func(o Event[O])) {
	batchedRegister[O](h, f)
}

func (h *collection[I, O]) RegisterBatch(f func(o []Event[O])) {
	h.handlers = append(h.handlers, f)
}

type indexedCollection[I, O any] struct {
	h *collection[I, O]
	d dependencies
}

var _ registerer = &indexedCollection[any, any]{}

func (i *indexedCollection[I, O]) getDeps() dependencies {
	return i.d
}

func (i *indexedCollection[I, O]) _internalHandler() {
}

func (i *indexedCollection[I, O]) register(e erasedCollection) {
	if !i.h.dependencies.InsertContains(e.hash()) {
		log.Debugf("register singleton %T", e)
		e.register(i.h.handler())
	}
}

package cv2

import (
	"fmt"
	"sync"

	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/util/sets"
	istiolog "istio.io/pkg/log"
)

type collection[I, O any] struct {
	parent Collection[I]
	log    *istiolog.Scope

	// mu protects all items grouped below
	mu              sync.Mutex
	collectionState index[I, O]
	dependencies    sets.String
	// Stores a map of I -> Things depending on it
	objectRelations map[Key[I]]dependencies

	handlersMu sync.RWMutex
	handlers   []func(o []Event[O])

	handle HandleSingle[I, O]
}

type index[I, O any] struct {
	objects   map[Key[O]]O
	inputs    map[Key[I]]Key[O]
	namespace map[string]sets.Set[Key[O]]
}

// onUpdate takes a list of I's that changed and reruns the onDependencyEvent over them.
func (h *collection[I, O]) onUpdate(items []Event[any]) {
	var events []Event[O]
	for _, a := range items {
		i := a.Latest().(I)

		iKey := GetKey(i)
		log := h.log.WithLabels("key", iKey)

		h.mu.Lock()
		// Find dependencies for this input; insert empty set if we don't have any
		d, f := h.objectRelations[iKey]
		if !f {
			// TODO mutex
			d = dependencies{
				dependencies: map[depKey]dependency{},
				finalized:    true, // TODO: set this to true at some point
			}
			h.objectRelations[iKey] = d
		}
		h.mu.Unlock()
		// Give them a context for this specific input
		ctx := &indexedCollection[I, O]{h, d}
		// Handler shouldn't be called with lock
		res := h.handle(ctx, i)
		h.mu.Lock()
		oldKey, existed := h.collectionState.inputs[iKey]
		oldRes, oldExists := h.collectionState.objects[oldKey]
		if existed {
			// Already have seen this, clear state in case it was removed or changed
			delete(h.collectionState.objects, oldKey)
		}
		if existed && !oldExists {
			panic(fmt.Sprintf("inconsistent state for event %+v, %v/%v\n%v->%v", a, existed, oldExists, iKey, oldKey))
		}
		if res == nil || a.Event == controllers.EventDelete {
			if existed {
				e := Event[O]{
					// TODO: dupe below
					Event: controllers.EventDelete,
					Old:   &oldRes,
				}
				events = append(events, e)
				delete(h.collectionState.inputs, iKey)
			}
			log.WithLabels("deleted", true, "existed", existed).Debugf("handled")
		} else {
			oKey := GetKey(*res)

			h.collectionState.inputs[iKey] = oKey
			h.collectionState.objects[oKey] = *res
			updated := !controllers.Equal(*res, oldRes)
			if updated {
				e := Event[O]{}
				if !oldExists {
					e.Event = controllers.EventAdd
					e.New = res
				} else {
					e.Event = controllers.EventUpdate
					e.New = res
					e.Old = &oldRes
				}
				events = append(events, e)
			}
			log.WithLabels("updated", updated).Debugf("handled")
		}
		h.mu.Unlock()
	}
	if len(events) == 0 {
		return
	}
	h.handlersMu.RLock()
	handlers := slices.Clone(h.handlers)
	h.handlersMu.RUnlock()

	for _, handler := range handlers {
		handler(events)
	}
}

func NewCollection[I, O any](c Collection[I], hf HandleSingle[I, O]) Collection[O] {
	// We need a set of handlers
	h := &collection[I, O]{
		handle:          hf,
		log:             log.WithLabels("owner", fmt.Sprintf("Collection[%T,%T]", *new(I), *new(O))),
		parent:          c,
		dependencies:    sets.New[string](),
		objectRelations: map[Key[I]]dependencies{},
		collectionState: index[I, O]{
			objects:   map[Key[O]]O{},
			inputs:    map[Key[I]]Key[O]{},
			namespace: map[string]sets.Set[Key[O]]{},
		},
	}
	// TODO: wait for dependencies to be ready
	h.onUpdate(Map(c.List(metav1.NamespaceAll), func(t I) Event[any] {
		return Event[any]{
			New:   Ptr(any(t)),
			Event: controllers.EventAdd,
		}
	}))
	// Setup primary singleton. On any change, trigger only that one
	c.RegisterBatch(func(events []Event[I]) {
		log := h.log.WithLabels("dep", "primary")
		log.Debugf("got event batch %v", len(events))
		h.onUpdate(Map(events, castEvent[I, any]))
	})
	return h
}

// Handler is called when a dependency changes. We will take as inputs the item that changed.
// Then we find all of our own values (I) that changed and onUpdate() them
func (h *collection[I, O]) onDependencyEvent(events []Event[any]) {
	h.mu.Lock()
	// Got an event. Now we need to find out who depends on it..
	ks := sets.Set[Key[I]]{}
	// Check old and new
	for _, ev := range events {
		// We have a possibly dependant object changed. For each input object, see if it depends on the object.
		// This can be by name or the entire type.
		for i, v := range h.objectRelations {
			log := log.WithLabels("item", i)
			for _, item := range ev.Items() {
				if HasName(item) {
					named := depKey{
						name:  GetName(item),
						dtype: GetTypeOf(item),
					}
					if d, f := v.dependencies[named]; f {
						match := d.filter.Matches(item)
						log.WithLabels("match", match).Infof("event for %v", named)
						if match {
							ks.Insert(i)
							break
						}
					}
				}
				unnamed := depKey{
					dtype: GetTypeOf(item),
				}
				if d, f := v.dependencies[unnamed]; f {
					match := d.filter.Matches(item)
					log.WithLabels("match", match).Infof("event for collection %v", unnamed.dtype)
					if match {
						ks.Insert(i)
						break
					}
				}
			}
		}
	}
	h.mu.Unlock()
	h.log.Debugf("collection event size %v, trigger %v dependencies", len(events), len(ks))
	toRun := make([]Event[any], 0, len(ks))
	for i := range ks {
		ii := h.parent.GetKey(i)
		if ii == nil {
			h.log.Errorf("BUG: Parent missing key!! %v", i)
		} else {
			toRun = append(toRun, Event[any]{
				Event: controllers.EventUpdate,
				// TODO: is Update without old legal?
				New: Ptr(any(*ii)),
			})
		}
	}
	h.onUpdate(toRun)
}

func (h *collection[I, O]) _internalHandler() {
}

func (h *collection[I, O]) GetKey(k Key[O]) (res *O) {
	h.mu.Lock()
	defer h.mu.Unlock()
	rf, f := h.collectionState.objects[k]
	if f {
		return &rf
	}
	return nil
}

func (h *collection[I, O]) List(namespace string) (res []O) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if namespace == "" {
		res = maps.Values(h.collectionState.objects)
		slices.SortFunc(res, func(a, b O) bool {
			return GetKey(a) < GetKey(b)
		})
	} else {
		panic("! not implemented!")
		for key := range h.collectionState.namespace[namespace] {
			res = append(res, h.collectionState.objects[key])
		}
	}
	return
}

func (h *collection[I, O]) Register(f func(o Event[O])) {
	batchedRegister[O](h, f)
}

func (h *collection[I, O]) RegisterBatch(f func(o []Event[O])) {
	h.handlersMu.Lock()
	defer h.handlersMu.Unlock()
	h.handlers = append(h.handlers, f)
}

type indexedCollection[I, O any] struct {
	h *collection[I, O]
	d dependencies
}

// registerDependency creates a
func (i *indexedCollection[I, O]) registerDependency(d dependency) bool {
	i.h.mu.Lock()
	defer i.h.mu.Unlock()
	_, exists := i.d.dependencies[d.key]
	if exists && !i.d.finalized {
		// TODO: make collection handle this and add it back
		// panic(fmt.Sprintf("dependency already registered, %+v", d.key))
	}
	if !exists && i.d.finalized {
		// TODO: make collection handle this and add it back
		// panic(fmt.Sprintf("dependency registered after initialization, %+v", d.key))
	}
	i.d.dependencies[d.key] = d
	c := d.collection
	if !i.h.dependencies.InsertContains(c.hash()) {
		log.Debugf("register collection %T", c)
		c.register(i.h.onDependencyEvent)
	}

	return i.d.finalized
}

func (i *indexedCollection[I, O]) _internalHandler() {
}

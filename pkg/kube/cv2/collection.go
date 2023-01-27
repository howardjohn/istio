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
	handlers        []func(o Event[O])
	parent          Collection[I]
	executeOne      func(i any)
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
	h.executeOne = func(a any) {
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
				for _, handler := range h.handlers {
					handler(e)
				}
				log.WithLabels("updated", updated).Debugf("handled")
			}
		})
		// TODO: propogate event
	}
	// TODO: wait for dependencies to be ready
	for _, i := range c.List(metav1.NamespaceAll) {
		h.executeOne(i)
	}
	// Setup primary singleton. On any change, trigger only that one
	c.Register(func(o Event[I]) {
		log := log.WithLabels("dep", "primary")
		log.Debugf("got event %v", o.Event)
		switch o.Event {
		case controllers.EventAdd:
			h.executeOne(*o.New)
		case controllers.EventDelete:
			// TODO: just delete, never need to re-run
		case controllers.EventUpdate:
			h.executeOne(*o.New)
		}
	})
	// TODO: handle sub-deps
	//mu := sync.Mutex{}
	//for _, dep := range h.deps.deps {
	//	dep := dep
	//	log := log.WithLabels("dep", dep.key)
	//	log.Infof("insert dep, filter: %+v", dep.filter)
	//	dep.dep.Register(func(o Event) {
	//		mu.Lock()
	//		defer mu.Unlock()
	//		log.Debugf("got event %v", o.Event)
	//		switch o.Event {
	//		case EventAdd:
	//			if dep.filter.Matches(o.New) {
	//				log.Debugf("Add match %v", o.New.GetName())
	//				h.executeOne()
	//			} else {
	//				log.Debugf("Add no match %v", o.New.GetName())
	//			}
	//		case EventDelete:
	//			if dep.filter.Matches(o.Old) {
	//				log.Debugf("delete match %v", o.Old.GetName())
	//				h.executeOne()
	//			} else {
	//				log.Debugf("Add no match %v", o.Old.GetName())
	//			}
	//		case EventUpdate:
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

func (h *collection[I, O]) handler() func(o Event[any]) {
	return func(o Event[any]) {
		item := o.Latest()
		h.mu.Lock()
		// Got an event. Now we need to find out who depends on it..
		ks := sets.Set[Key[I]]{}
		for i, v := range h.deps {
			named := depKey{
				name:  GetName(item),
				dtype: GetTypeOf(item),
			}
			if d, f := v.deps[named]; f {
				match := d.filter.Matches(item)
				log.WithLabels("match", match).Infof("event for %v", named)
				if match {
					ks.Insert(i)
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
				}
			}
		}
		h.mu.Unlock()
		log.WithLabels("key", GetKey(item), "event", o.Event).Debugf("singleton event, trigger %v dependencies", len(ks))
		for i := range ks {
			ii := h.parent.GetKey(i)
			if ii == nil {
				log.Errorf("BUG: Parent missing key!! %v", i)
			} else {
				h.executeOne(*ii)
			}
		}
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

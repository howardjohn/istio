package controllers

import (
	"fmt"
	"sync"

	"golang.org/x/exp/maps"
	"istio.io/istio/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
)

type Equaler[K any] interface {
	Equals(k K) bool
	Key() string
}

type Handle[O any] struct {
	handlers []func(O)
	objects  map[string]O
	mu       sync.RWMutex
}

func (h *Handle[O]) Get(k string) *O {
	o, f := h.objects[k]
	if !f {
		return nil
	}
	return &o
}

func (h *Handle[O]) Register(f func(O)) {
	h.handlers = append(h.handlers, f)
}

func (h *Handle[O]) Handle(conv O) {
	for _, hh := range h.handlers {
		hh(conv)
	}
}

func (h *Handle[O]) List() []O {
	return maps.Values(h.objects)
}

type Watcher[O any] interface {
	// Register a handler. TODO: call it for List() when we register
	Register(f func(O))
	List() []O
	Get(k string) *O
}

type ObjectDependencies[O any] struct {
	o            O
	dependencies sets.String
}

type Joined[A any, B any, O any] struct {
	mu       sync.RWMutex
	objects  map[string]ObjectDependencies[O]
	handlers []func(O)
}

func (j *Joined[A, B, O]) Get(k string) *O {
	o, f := j.objects[k]
	if !f {
		return nil
	}
	return &o.o
}

func (j *Joined[A, B, O]) Handle(conv O) {
	for _, hh := range j.handlers {
		hh(conv)
	}
}
func (j *Joined[A, B, O]) Register(f func(O)) {
	j.handlers = append(j.handlers, f)
}

func (j *Joined[A, B, O]) List() []O {
	return Map(maps.Values(j.objects), func(t ObjectDependencies[O]) O {
		return t.o
	})
}

func Join[A any, B any, O any](
	a Watcher[A],
	b Watcher[B],
	match func(a A, b B) bool,
	conv func(a A, b []B) O,
) Watcher[O] {
	joined := &Joined[A, B, O]{
		objects: make(map[string]ObjectDependencies[O]),
	}
	a.Register(func(ai A) {
		key := Key(ai)
		log.Infof("Join got new A %v", key)
		oOld, f := joined.objects[key]
		bs := b.List()
		matches := []B{}
		matchKeys := sets.New[string]()
		for _, bi := range bs {
			if match(ai, bi) {
				matches = append(matches, bi)
				matchKeys.Insert(Key(bi))
			}
		}
		oNew := conv(ai, matches)
		// Always update, in case dependencies changed
		joined.objects[key] = ObjectDependencies[O]{o: oNew, dependencies: matchKeys}
		if f && Equal(oOld.o, oNew) {
			log.Infof("no changes on A")
			return
		}
		joined.Handle(oNew)
	})
	b.Register(func(bi B) {
		log.Errorf("Join got new B %v", Key(bi))
		for _, ai := range a.List() {
			key := Key(ai)
			oOld, f := joined.objects[key]
			var deps sets.String
			if match(ai, bi) {
				// We know it depends on all old things, and this new key (it may have already depended on this, though)
				deps = oOld.dependencies.Copy().Insert(Key(bi))
				log.Infof("a %v matches b %v", Key(ai), Key(bi))
			} else {
				if !oOld.dependencies.Contains(Key(bi)) {
					log.Infof("entirely skip %v", key)
					continue
				}
				// We know it depends on all old things, and but not this new key
				deps = oOld.dependencies.Copy().Delete(Key(bi))
				log.Infof("a %v does not match b anyhmore %v", Key(ai), Key(bi))
			}

			matches := []B{}
			matchKeys := sets.New[string]()
			for bKey := range deps {
				bip := b.Get(bKey)
				if bip == nil {
					continue
				}
				bi := *bip
				if match(ai, bi) {
					matches = append(matches, bi)
					matchKeys.Insert(Key(bi))
				}
			}
			oNew := conv(ai, matches)

			// Always update, in case dependencies changed
			joined.objects[key] = ObjectDependencies[O]{o: oNew, dependencies: matchKeys}
			if f && Equal(oOld.o, oNew) {
				log.Infof("no changes on B")
				return
			}
			joined.Handle(oNew)
		}
	})
	return joined
}

type InformerWatch[I Object] struct {
	inf cache.SharedInformer
}

func (i InformerWatch[I]) Register(f func(I)) {
	addObj := func(obj any) {
		i := Extract[I](obj)
		log.Debugf("informer watch add %v", obj)
		f(i)
	}
	deleteObj := func(obj any) {
		i := Extract[I](obj)
		log.Debugf("informer watch delete %v", obj)
		f(i)
	}
	handler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			addObj(obj)
		},
		UpdateFunc: func(oldObj, newObj any) {
			addObj(newObj)
		},
		DeleteFunc: func(obj any) {
			deleteObj(obj)
		},
	}
	i.inf.AddEventHandler(handler)
}

func (i InformerWatch[I]) List() []I {
	return Map(i.inf.GetStore().List(), func(t any) I {
		return t.(I)
	})
}

func (i InformerWatch[I]) Get(k string) *I {
	iff, _, _ := i.inf.GetStore().GetByKey(k)
	r := iff.(I)
	return &r
}

func InformerToWatcher[I Object](informer cache.SharedInformer) Watcher[I] {
	return InformerWatch[I]{informer}
}


func Equal[O any](a, b O) bool {
	ak, ok := any(a).(Equaler[O])
	if ok {
		return ak.Equals(b)
	}
	ao, ok := any(a).(Object)
	if ok {
		return ao.GetResourceVersion() == any(b).(Object).GetResourceVersion()
	}
	// todo: proto.Equal?
	panic(fmt.Sprintf("Should be Equaler or Object (probably?), got %T", a))
	return false
}

func Key[O any](a O) string {
	ak, ok := any(a).(Equaler[O])
	if ok {
		return ak.Key()
	}
	ao, ok := any(a).(Object)
	if ok {
		k, _ := cache.MetaNamespaceKeyFunc(ao)
		return k
	}
	panic(fmt.Sprintf("Should be Equaler or Object (probably?), got %T", a))
	return ""
}

func Direct[I any, O any](input Watcher[I], convert func(i I) O) Watcher[O] {
	h := &Handle[O]{
		objects: make(map[string]O),
		mu:      sync.RWMutex{},
	}

	ti := *new(I)
	to := *new(O)
	input.Register(func(i I) {
		key := Key(i)
		log.Debugf("Direct[%T, %T]: event for %v", ti, to, key)
		cur := input.Get(key)
		if cur == nil {
			old, f := h.objects[key]
			if f {
				delete(h.objects, key)
				h.Handle(old)
				log.Debugf("Direct[%T, %T]: %v no longer exists (delete)", ti, to, key)
			} else {
				log.Debugf("Direct[%T, %T]: %v no longer exists (delete) NOP", ti, to, key)
			}
		} else {
			conv := convert(*cur)
			exist, f := h.objects[key]
			if  false { // conv == nil { TODO: convert return *O so they can skip
				// This is a delete
				if f {
					// Used to exist
					h.Handle(conv)
				}
				return
			}
			updated := !Equal(conv, exist)
			log.Debugf("Direct[%T, %T]: conv %v, updated=%v", ti, to, key, updated)
			h.objects[key] = conv
			if updated {
				h.Handle(conv)
			}
		}
	})
	return h
}

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

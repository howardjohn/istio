package controllers

import (
	"github.com/sasha-s/go-deadlock"
	"golang.org/x/exp/maps"
	"istio.io/istio/pkg/util/sets"
)

type join[A any, B any, O any] struct {
	mu       deadlock.RWMutex
	objects  map[string]ObjectDependencies[O]
	handlers []func(O)
}

func (j *join[A, B, O]) Get(k string) *O {
	j.mu.RLock()
	defer j.mu.RUnlock()
	o, f := j.objects[k]
	if !f {
		return nil
	}
	return &o.o
}

func (j *join[A, B, O]) Handle(conv O) {
	for _, hh := range j.handlers {
		hh(conv)
	}
}
func (j *join[A, B, O]) Register(f func(O)) {
	j.handlers = append(j.handlers, f)
}

func (j *join[A, B, O]) List() []O {
	j.mu.RLock()
	defer j.mu.RUnlock()
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
	j := &join[A, B, O]{
		objects: make(map[string]ObjectDependencies[O]),
	}
	a.Register(func(ai A) {
		key := Key(ai)
		j.mu.Lock()
		defer j.mu.Unlock() // TODO: unlock before handle, read lock with upgrade
		log.Debugf("Join got new A %v", key)
		oOld, f := j.objects[key]
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
		j.objects[key] = ObjectDependencies[O]{o: oNew, dependencies: matchKeys}
		if f && Equal(oOld.o, oNew) {
			log.Debugf("no changes on A")
			return
		}
		j.Handle(oNew)
	})
	b.Register(func(bi B) {
		log.Debugf("Join got new B %v", Key(bi))

		j.mu.Lock()
		defer j.mu.Unlock() // TODO: unlock before handle, read lock with upgrade
		for _, ai := range a.List() {
			key := Key(ai)
			oOld, f := j.objects[key]
			var deps sets.String
			if match(ai, bi) {
				// We know it depends on all old things, and this new key (it may have already depended on this, though)
				deps = oOld.dependencies.Copy().Insert(Key(bi))
				log.Debugf("a %v matches b %v", Key(ai), Key(bi))
			} else {
				if !oOld.dependencies.Contains(Key(bi)) {
					log.Debugf("entirely skip %v", key)
					continue
				}
				// We know it depends on all old things, and but not this new key
				deps = oOld.dependencies.Copy().Delete(Key(bi))
				log.Debugf("a %v does not match b anyhmore %v", Key(ai), Key(bi))
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
			j.objects[key] = ObjectDependencies[O]{o: oNew, dependencies: matchKeys}
			if f && Equal(oOld.o, oNew) {
				log.Debugf("no changes on B")
				return
			}
			j.Handle(oNew)
		}
	})
	return j
}
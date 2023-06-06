// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controllers

import (
	"fmt"
	"sync"

	"golang.org/x/exp/maps"
)

type singleToMany[A any, B any, O any] struct {
	mu       sync.RWMutex
	objects  map[Key[O]]ObjectDependencies[O]
	handlers []func(O)

	name string
}

func (j *singleToMany[A, B, O]) Get(k Key[O]) *O {
	j.mu.RLock()
	defer j.mu.RUnlock()
	// Broken
	return nil
	//o, f := j.objects[k]
	//if !f {
	//	return nil
	//}
	//return &o.o
}

func (j *singleToMany[A, B, O]) Handle(conv O) {
	if !j.mu.TryRLock() {
		panic("handle called with lock!")
	} else {
		j.mu.RUnlock()
	}
	for _, hh := range j.handlers {
		hh(conv)
	}
}

func (j *singleToMany[A, B, O]) Name() string {
	return j.name
}

func (j *singleToMany[A, B, O]) Register(f func(O)) {
	j.handlers = append(j.handlers, f)
}

func (j *singleToMany[A, B, O]) List() []O {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return Map(maps.Values(j.objects), func(t ObjectDependencies[O]) O {
		return t.o
	})
}

func SingleToMany[A any, B any, O any](
	name string,
	a Watcher[A],
	b Watcher[B],
	match func(a A, b B) bool,
	conv func(a A, b []B) O,
) Watcher[O] {
	ta := *new(A)
	tb := *new(B)
	to := *new(O)
	if name == "" {
		name = fmt.Sprintf("singleToMany[%T,%T,%T]", ta, tb, to)
	}
	j := &singleToMany[A, B, O]{
		objects: make(map[Key[O]]ObjectDependencies[O]),
		name:    name,
	}

	//ta := *new(A)
	//tb := *new(B)
	//to := *new(O)
	//log := log.WithLabels("origin", fmt.Sprintf("join[%T,%T,%T]", ta, tb, to))
	//a.Register(func(ai A) {
	//	key := GetKey(ai)
	//	cur := a.Get(key)
	//	log := log.WithLabels("key", key, "reason", "a")
	//	j.mu.Lock()
	//	if cur == nil {
	//		log.Debugf("got a delete for A")
	//		oOld, f := j.objects[key]
	//		if !f {
	//			log.Errorf("unexpected double deletion?")
	//		}
	//		// A is gone, so remove it
	//		delete(j.objects, key)
	//		// Call with last known object
	//		j.mu.Unlock()
	//		j.Handle(oOld.o)
	//		return
	//	}
	//	log.Debugf("handling new A")
	//	oOld, f := j.objects[key]
	//	bs := b.List()
	//	matches := []B{}
	//	matchKeys := sets.New[string]()
	//	for _, bi := range bs {
	//		if match(*cur, bi) {
	//			matches = append(matches, bi)
	//			matchKeys.Insert(GetKey(bi))
	//		}
	//	}
	//	log.Debugf("matches %v", matchKeys.UnsortedList())
	//	oNew := conv(*cur, matches)
	//	// Always update, in case dependencies changed
	//	j.objects[key] = ObjectDependencies[O]{o: oNew, dependencies: matchKeys}
	//	j.mu.Unlock()
	//	if f && Equal(oOld.o, oNew) {
	//		log.Debugf("no changes on A")
	//		return
	//	}
	//	j.Handle(oNew)
	//})
	//b.Register(func(bi B) {
	//	key := GetKey(bi)
	//	log := log.WithLabels("key", key, "reason", "b")
	//	log.Debugf("handling new b")
	//	j.mu.Lock()
	//	toHandle := []O{}
	//	for _, ai := range a.List() {
	//		aKey := GetKey(ai)
	//		oOld, f := j.objects[aKey]
	//		var deps sets.String
	//		if match(ai, bi) {
	//			// We know it depends on all old things, and this new key (it may have already depended on this, though)
	//			deps = oOld.dependencies.Copy().Insert(GetKey(bi))
	//			log.Debugf("a %v matches b %v", GetKey(ai), GetKey(bi))
	//		} else {
	//			if !oOld.dependencies.Contains(GetKey(bi)) {
	//				log.Debugf("entirely skip %v", aKey)
	//				continue
	//			}
	//			// We know it depends on all old things, and but not this new key
	//			deps = oOld.dependencies.Copy().Delete(GetKey(bi))
	//			log.Debugf("a %v does not match b anyhmore %v", GetKey(ai), GetKey(bi))
	//		}
	//
	//		matches := []B{}
	//		matchKeys := sets.New[string]()
	//		for bKey := range deps {
	//			bip := b.Get(bKey)
	//			if bip == nil {
	//				continue
	//			}
	//			bi := *bip
	//			if match(ai, bi) {
	//				matches = append(matches, bi)
	//				matchKeys.Insert(GetKey(bi))
	//			}
	//		}
	//		oNew := conv(ai, matches)
	//
	//		// Always update, in case dependencies changed
	//		j.objects[aKey] = ObjectDependencies[O]{o: oNew, dependencies: matchKeys}
	//		if f && Equal(oOld.o, oNew) {
	//			log.Debugf("no changes on B")
	//			continue
	//		}
	//		toHandle = append(toHandle, oNew)
	//	}
	//	j.mu.Unlock()
	//	for _, oNew := range toHandle {
	//		j.Handle(oNew)
	//	}
	//})
	return j
}

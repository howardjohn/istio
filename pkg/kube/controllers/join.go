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

	"istio.io/istio/pkg/util/sets"
)

type joined[A any, B any, O any] struct {
	o    O
	aKey *Key[A]
	bKey *Key[B]
}

type join[A any, B any, O any] struct {
	mu       sync.RWMutex
	objects  map[Key[O]]joined[A, B, O]
	aIndex   map[Key[A]]sets.Set[Key[O]]
	bIndex   map[Key[B]]sets.Set[Key[O]]
	handlers []func(O)
	name     string
}

func (j *join[A, B, O]) Get(k Key[O]) *O {
	j.mu.RLock()
	defer j.mu.RUnlock()
	o, f := j.objects[k]
	if !f {
		return nil
	}
	return &o.o
}

func (j *join[A, B, O]) Handle(conv O) {
	if !j.mu.TryRLock() {
		panic("handle called with lock!")
	} else {
		j.mu.RUnlock()
	}
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
	return Map(maps.Values(j.objects), func(t joined[A, B, O]) O {
		return t.o
	})
}

func (j *join[A, B, O]) Name() string {
	return j.name
}

// Join merges two objects, A and B, into a third O.
// Behavior is weird, we will call with all (a,b) pairs, but (a,nil) and (nil,b) if there are no A's or B's
func Join[A any, B any, O any](
	name string,
	a Watcher[A],
	b Watcher[B],
	conv func(a *A, b *B) *O,
) Watcher[O] {
	ta := *new(A)
	tb := *new(B)
	to := *new(O)
	if name == "" {
		name = fmt.Sprintf("join[%T,%T,%T]", ta, tb, to)
	}
	j := &join[A, B, O]{
		objects: make(map[Key[O]]joined[A, B, O]),
		aIndex:  map[Key[A]]sets.Set[Key[O]]{},
		bIndex:  map[Key[B]]sets.Set[Key[O]]{},
		name:    name,
	}

	log := log.WithLabels("origin", j.Name())

	a.Register(func(ai A) {
		key := GetKey(ai)
		log := log.WithLabels("key", key, "reason", "A")
		log.Debugf("event")
		j.mu.Lock()
		// First, clear out old state. We could be more incremental in the future
		for oKey := range j.aIndex[key] {
			prev, pf := j.objects[oKey]
			delete(j.objects, oKey)
			if pf && prev.bKey != nil {
				sets.DeleteCleanupLast(j.bIndex, *prev.bKey, oKey)
			}
		}
		cur := a.Get(key)
		// Now we have an "A"... find all relevant "B"
		bs := b.List()
		toCall := []O{}
		for _, bi := range bs {
			res := conv(cur, &bi)
			if res == nil {
				// Nothing to do since we already cleaned up earlier
				continue
			}
			oKey := GetKey(*res)
			bKey := GetKey(bi)
			j.objects[oKey] = joined[A, B, O]{o: *res, aKey: &key, bKey: &bKey}
			sets.InsertOrNew(j.aIndex, key, oKey)
			sets.InsertOrNew(j.bIndex, bKey, oKey)
			toCall = append(toCall, *res)
		}
		if cur != nil && len(bs) == 0 {
			// Also try inserting without a B
			res := conv(cur, nil)
			if res != nil {
				oKey := GetKey(*res)
				j.objects[oKey] = joined[A, B, O]{o: *res, bKey: nil, aKey: &key}
				sets.InsertOrNew(j.aIndex, key, oKey)
				toCall = append(toCall, *res)
			}
		}

		j.mu.Unlock()
		for _, c := range toCall {
			j.Handle(c)
		}
	})
	b.Register(func(bi B) {
		key := GetKey(bi)
		log := log.WithLabels("key", key, "reason", "B")
		log.Debugf("event")
		j.mu.Lock()
		// First, clear out old state. We could be more incremental in the future
		for oKey := range j.bIndex[key] {
			prev, pf := j.objects[oKey]
			delete(j.objects, oKey)
			if pf && prev.aKey != nil {
				sets.DeleteCleanupLast(j.aIndex, *prev.aKey, oKey)
			}
		}
		cur := b.Get(key)
		// Now we have an "B"... find all relevant "A"
		as := a.List()
		toCall := []O{}
		for _, ai := range as {
			res := conv(&ai, cur)
			if res == nil {
				// Nothing to do since we already cleaned up earlier
				continue
			}
			oKey := GetKey(*res)
			aKey := GetKey(ai)
			j.objects[oKey] = joined[A, B, O]{o: *res, bKey: &key, aKey: &aKey}
			sets.InsertOrNew(j.aIndex, aKey, oKey)
			sets.InsertOrNew(j.bIndex, key, oKey)
			toCall = append(toCall, *res)
		}
		if cur != nil && len(as) == 0 {
			// Also try inserting without an A
			res := conv(nil, cur)
			if res != nil {
				oKey := GetKey(*res)
				j.objects[oKey] = joined[A, B, O]{o: *res, bKey: &key, aKey: nil}
				sets.InsertOrNew(j.bIndex, key, oKey)
				toCall = append(toCall, *res)
			}
		}

		j.mu.Unlock()
		for _, c := range toCall {
			j.Handle(c)
		}
	})
	return j
}

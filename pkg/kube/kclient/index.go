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

package kclient

import (
	"fmt"
	"sync"

	"go.uber.org/atomic"
	"istio.io/istio/pkg/ptr"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/util/sets"
)

// Index maintains a simple index over an informer
type Index[O controllers.ComparableObject, K comparable] struct {
	mu      sync.RWMutex
	objects map[K]sets.Set[types.NamespacedName]
	client  Reader[O]
}

// Lookup finds all objects matching a given key
func (i *Index[O, K]) Lookup(k K) []O {
	i.mu.RLock()
	defer i.mu.RUnlock()
	res := make([]O, 0)
	for obj := range i.objects[k] {
		item := i.client.Get(obj.Name, obj.Namespace)
		if controllers.IsNil(item) {
			// This should be extremely rare, maybe impossible due to the mutex.
			continue
		}
		res = append(res, item)
	}
	return res
}

// Index maintains a simple index over an informer
type Index2[O controllers.ComparableObject, K fmt.Stringer] struct {
	k string
	client  Reader[O]
}

var indexes = atomic.NewInt32(0)


// Lookup finds all objects matching a given key
func (i *Index2[O, K]) Lookup(k K) []O {
	ut, _ := i.client.Inf().GetIndexer().ByIndex(i.k, k.String())
	res := make([]O, 0, len(ut))
	for _, x := range ut {
		res = append(res, x.(O))
	}
	return res
}

func CreateIndex2[O controllers.ComparableObject, K fmt.Stringer](
	client Reader[O],
	extract func(o O) []K,
) *Index2[O, K] {
	k := fmt.Sprintf("%T/%d", ptr.Empty[O](), indexes.Inc())
	client.Inf().AddIndexers(map[string]cache.IndexFunc{
		k: func(obj interface{}) ([]string, error) {
			res := extract(controllers.Extract[O](obj))
			rs := make([]string, 0, len(res))
			for _, x := range res {
				rs = append(rs, x.String())
			}
			return rs, nil
		},
	})
	return &Index2[O, K]{k, client}
}

// CreateIndex creates a simple index, keyed by key K, over an informer for O. This is similar to
// Informer.AddIndex, but is easier to use and can be added after an informer has already started.
func CreateIndex[O controllers.ComparableObject, K comparable](
	client Reader[O],
	extract func(o O) []K,
) *Index[O, K] {
	idx := Index[O, K]{
		objects: make(map[K]sets.Set[types.NamespacedName]),
		client:  client,
		mu:      sync.RWMutex{},
	}
	addObj := func(obj any) {
		ro := controllers.ExtractObject(obj)
		o := ro.(O)
		objectKey := config.NamespacedName(o)
		for _, indexKey := range extract(o) {
			sets.InsertOrNew(idx.objects, indexKey, objectKey)
		}
	}
	deleteObj := func(obj any) {
		ro := controllers.ExtractObject(obj)
		o := ro.(O)
		objectKey := config.NamespacedName(o)
		for _, indexKey := range extract(o) {
			sets.DeleteCleanupLast(idx.objects, indexKey, objectKey)
		}
	}
	handler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			idx.mu.Lock()
			defer idx.mu.Unlock()
			addObj(obj)
		},
		UpdateFunc: func(oldObj, newObj any) {
			idx.mu.Lock()
			defer idx.mu.Unlock()
			deleteObj(oldObj)
			addObj(newObj)
		},
		DeleteFunc: func(obj any) {
			idx.mu.Lock()
			defer idx.mu.Unlock()
			deleteObj(obj)
		},
	}
	client.AddEventHandler(handler)
	return &idx
}

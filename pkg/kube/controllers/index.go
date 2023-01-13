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
	"sync"

	"golang.org/x/exp/maps"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/util/sets"
)

type StringKeyer[O any] interface {
	Key() Key[O]
}

type Keyer[K comparable] interface {
	Key() K
}

type Overlay[I runtime.Object, K comparable, O Keyer[K]] struct {
	mu             sync.RWMutex
	objects        map[K]O
	namespaceIndex map[string]sets.Set[K]
}

// Lookup finds object for a given key
func (i *Overlay[I, K, O]) Get(k K) O {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.objects[k]
}

func (i *Overlay[I, K, O]) ListNamespace(ns string) []O {
	i.mu.RLock()
	defer i.mu.RUnlock()
	if ns == metav1.NamespaceAll {
		return maps.Values(i.objects)
	}
	inNs := i.namespaceIndex[ns]
	res := make([]O, 0, len(inNs))
	for k := range inNs {
		res = append(res, i.objects[k])
	}
	return res
}

type Namer interface {
	GetNamespace() string
	GetName() string
}

func Name(o Namer) types.NamespacedName {
	return types.NamespacedName{
		Namespace: o.GetNamespace(),
		Name:      o.GetName(),
	}
}

// CreateIndex creates a simple index, keyed by key K, over an informer for O. This is similar to
// Informer.AddIndex, but is easier to use and can be added after an informer has already started.
func CreateOverlay[I Object, K comparable, O Keyer[K]](
	informer HandleInformer,
	convert func(i I) O,
) *Overlay[I, K, O] {
	idx := Overlay[I, K, O]{
		objects:        make(map[K]O),
		namespaceIndex: make(map[string]sets.Set[K]),
		mu:             sync.RWMutex{},
	}
	addObj := func(obj any) {
		i := Extract[I](obj)
		conv := convert(i)
		key := conv.Key()
		idx.objects[key] = conv
		idx.namespaceIndex[i.GetNamespace()] = sets.InsertOrNew(idx.namespaceIndex[i.GetNamespace()], key)
	}
	deleteObj := func(obj any) {
		i := Extract[I](obj)
		conv := convert(i)
		key := conv.Key()
		delete(idx.objects, key)
		idx.namespaceIndex[i.GetNamespace()].Delete(key)
		if len(idx.namespaceIndex[i.GetNamespace()]) == 0 {
			delete(idx.namespaceIndex, i.GetNamespace())
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
	informer.AddEventHandler(handler)
	return &idx
}

type HandleInformer interface {
	AddEventHandler(handler cache.ResourceEventHandler)
}

// Index maintains a simple index over an informer
type Index[O runtime.Object, K comparable] struct {
	mu       sync.RWMutex
	objects  map[K]sets.String
	informer cache.SharedIndexInformer
}

// Lookup finds all objects matching a given key
func (i *Index[O, K]) Lookup(k K) []O {
	i.mu.RLock()
	defer i.mu.RUnlock()
	res := make([]O, 0)
	for obj := range i.objects[k] {
		item, f, err := i.informer.GetStore().GetByKey(obj)
		if !f || err != nil {
			// This should be extremely rare, maybe impossible due to the mutex.
			continue
		}
		res = append(res, item.(O))
	}
	return res
}

// CreateIndex creates a simple index, keyed by key K, over an informer for O. This is similar to
// Informer.AddIndex, but is easier to use and can be added after an informer has already started.
func CreateIndex[O runtime.Object, K comparable](
	informer cache.SharedIndexInformer,
	extract func(o O) []K,
) *Index[O, K] {
	idx := Index[O, K]{
		objects:  make(map[K]sets.String),
		informer: informer,
		mu:       sync.RWMutex{},
	}
	addObj := func(obj any) {
		ro := ExtractObject(obj)
		o := ro.(O)
		objectKey := kube.KeyFunc(ro.GetName(), ro.GetNamespace())
		for _, indexKey := range extract(o) {
			if _, f := idx.objects[indexKey]; !f {
				idx.objects[indexKey] = sets.New[string]()
			}
			idx.objects[indexKey].Insert(objectKey)
		}
	}
	deleteObj := func(obj any) {
		ro := ExtractObject(obj)
		o := ro.(O)
		objectKey := kube.KeyFunc(ro.GetName(), ro.GetNamespace())
		for _, indexKey := range extract(o) {
			idx.objects[indexKey].Delete(objectKey)
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
	informer.AddEventHandler(handler)
	return &idx
}

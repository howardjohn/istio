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
	"reflect"

	"google.golang.org/protobuf/proto"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/util/sets"
)

type Equaler[K any] interface {
	Equals(k K) bool
}

type Watcher[O any] interface {
	Name() string
	// Register a handler. TODO: call it for List() when we register
	Register(f func(O))
	List() []O
	Get(k Key[O]) *O
}

type ObjectDependencies[O any] struct {
	o            O
	dependencies sets.String
}

func PtrEqual[O any](a, b *O) bool {
	if a == nil && b == nil {
		return false
	}
	if b == nil != (a == nil) {
		return true
	}
	return Equal(*a, *b)
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
	ap, ok := any(a).(proto.Message)
	if ok {
		return proto.Equal(ap, any(b).(proto.Message))
	}
	return reflect.DeepEqual(a, b)
	panic(fmt.Sprintf("Should be Equaler or Object (probably?), got %T", a))
	return false
}

func KeyPtr[O any](a O) *Key[O] {
	res := GetKey(a)
	return &res
}

// Key is a string, but with a type associated to avoid mixing up keys
type Key[O any] string

type EqualerString string

func (e EqualerString) Equals(k EqualerString) bool {
	return string(e) == string(k)
}

var _ Equaler[EqualerString] = EqualerString("")

func GetKey[O any](a O) Key[O] {
	ak, ok := any(a).(StringKeyer[O])
	if ok {
		return ak.Key()
	}
	xk, ok := any(a).(StringKeyer[EqualerString])
	if ok {
		return any(xk.Key()).(Key[O])
	}
	ao, ok := any(a).(Object)
	if ok {
		k, _ := cache.MetaNamespaceKeyFunc(ao)
		return Key[O](k)
	}
	panic(fmt.Sprintf("Should be StringKeyer or Object (probably?), got %T", a))
	return ""
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

func Identity[A any](a A) A {
	return a
}

func StripStaticKey[O any](w Watcher[StaticKey[O]]) Watcher[O] {
	return Singleton[StaticKey[O], O]("_StripKey", w, func(sko StaticKey[O]) O {
		return sko.Obj
	})
}

type StaticKey[O any] struct {
	Obj O
	K   Key[O]
}

func (k StaticKey[O]) Key() Key[StaticKey[O]] {
	return Key[StaticKey[O]](k.K)
}

func (k StaticKey[O]) Equals(o StaticKey[O]) bool {
	// Delegate to inside object
	return Equal(k.Obj, o.Obj)
}

func Ptr[T any](t T) *T {
	return &t
}

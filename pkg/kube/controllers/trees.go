package controllers

import (
	"fmt"

	"google.golang.org/protobuf/proto"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/util/sets"
)

type Equaler[K any] interface {
	Equals(k K) bool
}

type Watcher[O any] interface {
	// Register a handler. TODO: call it for List() when we register
	Register(f func(O))
	List() []O
	Get(k Key[O]) *O
}

type ObjectDependencies[O any] struct {
	o            O
	dependencies sets.String
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
	// todo: proto.Equal?
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

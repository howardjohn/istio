package controllers

import (
	"fmt"

	"google.golang.org/protobuf/proto"
	"istio.io/istio/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
)

type Equaler[K any] interface {
	Equals(k K) bool
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

func Key[O any](a O) string {
	ak, ok := any(a).(Keyer[string])
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

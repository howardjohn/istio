package main

import (
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/testing"

	"istio.io/istio/pkg/kube"
)

type fakeAPI[T Resource] struct {
	*testing.Fake
	tracker testing.ObjectTracker
}

func NewFake[T Resource](objects ...T) API[T] {
	f := &testing.Fake{}
	// TODO: no scheme
	o := testing.NewObjectTracker(kube.IstioScheme, scheme.Codecs.UniversalDecoder())
	for _, obj := range objects {
		if err := o.Add(any(&obj).(runtime.Object)); err != nil {
			panic(err)
		}
	}

	cs := &fakeAPI[T]{
		tracker: o,
		Fake:    f,
	}
	cs.AddReactor("*", "*", testing.ObjectReaction(o))
	cs.AddWatchReactor("*", func(action testing.Action) (handled bool, ret watch.Interface, err error) {
		gvr := action.GetResource()
		ns := action.GetNamespace()
		watch, err := o.Watch(gvr, ns)
		if err != nil {
			return false, nil, err
		}
		return true, watch, nil
	})

	return cs
}

func (f fakeAPI[T]) Get(name string, namespace string, options metav1.GetOptions) (*T, error) {
	x := new(T)
	gvr := (*x).ResourceMetadata().WithResource((*x).ResourceName())
	obj, err := f.Fake.
		Invokes(testing.NewGetAction(gvr, namespace, name), any(x).(runtime.Object))

	if obj == nil {
		return nil, err
	}
	return any(obj).(*T), err
}

func typeName(o any) string {
	t := reflect.TypeOf(o)
	if t.Kind() != reflect.Ptr {
		panic("All types must be pointers to structs.")
	}
	t = t.Elem()
	return t.Name()
}

func (f fakeAPI[T]) List(namespace string, options metav1.ListOptions) ([]T, error) {
	x := new(T)
	// I guess we should make ResourceMetadata have resource!
	gvr := (*x).ResourceMetadata().WithResource((*x).ResourceName())
	gvk := (*x).ResourceMetadata().WithKind(typeName(x))
	obj, err := f.Fake.
		Invokes(testing.NewListAction(gvr, gvk, namespace, options), &GenericList[T]{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(options)
	if label == nil {
		label = labels.Everything()
	}
	return reflect.ValueOf(obj).Elem().FieldByName("Items").Interface().([]T), nil
}

func (f fakeAPI[T]) Watch(namespace string, options metav1.ListOptions) (Watcher[T], error) {
	// TODO implement me
	panic("implement me")
}

func (f fakeAPI[T]) Namespace(namespace string) NamespacedAPI[T] {
	// TODO implement me
	panic("implement me")
}

var _ API[Resource] = fakeAPI[Resource]{}

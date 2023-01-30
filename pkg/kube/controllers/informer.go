package controllers

import (
	"fmt"

	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/kube"
)

type informer[I Object] struct {
	r   kube.Registerer
	inf cache.SharedInformer
}

func (i informer[I]) AddDependency(chain []string) {
	chain = append(chain, i.Name())
	i.r.AddDependency(chain)
}

func (i informer[I]) Register(f func(I)) {
	addObj := func(obj any) {
		i := Extract[I](obj)
		log.Debugf("informer watch add %v", GetKey(obj))
		f(i)
	}
	deleteObj := func(obj any) {
		i := Extract[I](obj)
		log.Debugf("informer watch delete %v", GetKey(obj))
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

func (i informer[I]) Name() string {
	return fmt.Sprintf("informer[%T]", *new(I))
}

func (i informer[I]) List() []I {
	return Map(i.inf.GetStore().List(), func(t any) I {
		return t.(I)
	})
}

func (i informer[I]) Get(k Key[I]) *I {
	iff, f, _ := i.inf.GetStore().GetByKey(string(k))
	if !f {
		return nil
	}
	r := iff.(I)
	return &r
}

func InformerToWatcher[I Object](r kube.Registerer, i cache.SharedInformer) Watcher[I] {
	return informer[I]{r, i}
}

func WatcherFor[I Object](c kube.Client) Watcher[I] {
	return InformerToWatcher[I](c.DAG(), kube.InformerFor[I](c))
}

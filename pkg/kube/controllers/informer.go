package controllers

import "k8s.io/client-go/tools/cache"

type informer[I Object] struct {
	inf cache.SharedInformer
}

func (i informer[I]) Register(f func(I)) {
	addObj := func(obj any) {
		i := Extract[I](obj)
		log.Debugf("informer watch add %v", obj)
		f(i)
	}
	deleteObj := func(obj any) {
		i := Extract[I](obj)
		log.Debugf("informer watch delete %v", obj)
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

func (i informer[I]) List() []I {
	return Map(i.inf.GetStore().List(), func(t any) I {
		return t.(I)
	})
}

func (i informer[I]) Get(k string) *I {
	iff, _, _ := i.inf.GetStore().GetByKey(k)
	r := iff.(I)
	return &r
}

func InformerToWatcher[I Object](i cache.SharedInformer) Watcher[I] {
	return informer[I]{i}
}

package cv2

import (
	"fmt"

	"golang.org/x/exp/slices"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
)

type informer[I controllers.Object] struct {
	r   kube.Registerer
	inf cache.SharedIndexInformer
}

var _ Collection[controllers.Object] = &informer[controllers.Object]{}

func (i informer[I]) AddDependency(chain []string) {
	chain = append(chain, i.Name())
	i.r.AddDependency(chain)
}

func (i informer[I]) _internalHandler() {}

func (i informer[I]) Name() string {
	return fmt.Sprintf("informer[%T]", *new(I))
}

func (i informer[I]) List(namespace string) (res []I) {
	cache.ListAllByNamespace(i.inf.GetIndexer(), namespace, klabels.Everything(), func(i interface{}) {
		res = append(res, i.(I))
	})
	slices.SortFunc(res, func(a, b I) bool {
		return GetKey(a) < GetKey(b)
	})
	return
}

func (i informer[I]) GetKey(k Key[I]) *I {
	iff, f, _ := i.inf.GetStore().GetByKey(string(k))
	if !f {
		return nil
	}
	r := iff.(I)
	return &r
}

func (i informer[I]) Register(f func(o Event[I])) {
	batchedRegister[I](i, f)
}

func (i informer[I]) RegisterBatch(f func(o []Event[I])) {
	i.inf.AddEventHandler(EventHandler(func(o Event[any]) {
		f([]Event[I]{castEvent[any, I](o)})
	}))
}

func EventHandler(handler func(o Event[any])) cache.ResourceEventHandler {
	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			handler(Event[any]{
				New:   &obj,
				Event: controllers.EventAdd,
			})
		},
		UpdateFunc: func(oldInterface, newInterface any) {
			handler(Event[any]{
				Old:   &oldInterface,
				New:   &newInterface,
				Event: controllers.EventUpdate,
			})
		},
		DeleteFunc: func(obj any) {
			handler(Event[any]{
				Old:   &obj,
				Event: controllers.EventDelete,
			})
		},
	}
}

func InformerToWatcher[I controllers.Object](r kube.Registerer, i cache.SharedIndexInformer) Collection[I] {
	return informer[I]{r, i}
}

func CollectionFor[I controllers.Object](c kube.Client) Collection[I] {
	return InformerToWatcher[I](c.DAG(), kube.InformerFor[I](c))
}

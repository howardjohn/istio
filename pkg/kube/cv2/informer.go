package cv2

import (
	"fmt"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/pkg/log"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
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

func (i informer[I]) Register(f func(I)) {
	addObj := func(obj any) {
		i := controllers.Extract[I](obj)
		log.Debugf("informer watch add %v", controllers.GetKey(obj))
		f(i)
	}
	deleteObj := func(obj any) {
		i := controllers.Extract[I](obj)
		log.Debugf("informer watch delete %v", controllers.GetKey(obj))
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

func (i informer[I]) List(namespace string) []I {
	var res []I
	cache.ListAllByNamespace(i.inf.GetIndexer(), namespace, klabels.Everything(), func(i interface{}) {
		res = append(res, i.(I))
	})
	return res
}

func (i informer[I]) Get(k Key[I]) *I {
	iff, f, _ := i.inf.GetStore().GetByKey(string(k))
	if !f {
		return nil
	}
	r := iff.(I)
	return &r
}

func InformerToWatcher[I controllers.Object](r kube.Registerer, i cache.SharedIndexInformer) Collection[I] {
	return informer[I]{r, i}
}

func CollectionFor[I controllers.Object](c kube.Client) Collection[I] {
	return InformerToWatcher[I](c.DAG(), kube.InformerFor[I](c))
}

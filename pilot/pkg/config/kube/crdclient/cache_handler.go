package crdclient

import (
	"reflect"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"

	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"  // import GKE cluster authentication plugin
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc" // import OIDC cluster authentication plugin, e.g. for Tectonic

	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/schema/collection"
)

// cacheHandler abstracts the logic of an informer with a set of handlers. Handlers can be added at runtime
// and will be invoked on each informer event.
type cacheHandler struct {
	client   *Client
	informer cache.SharedIndexInformer
	handlers []func(model.Config, model.Config, model.Event)
	schema   collection.Schema
	lister   func (namespace string) cache.GenericNamespaceLister
}

func (h *cacheHandler) onEvent(old interface{}, curr interface{}, event model.Event) error {
	if err := h.client.checkReadyForEvents(curr); err != nil {
		return err
	}

	var currItem, oldItem runtime.Object

	ok := false

	if currItem, ok = curr.(runtime.Object); !ok {
		log.Warnf("New Object can not be converted to runtime Object %v, is type %T", curr, curr)
		return nil
	}
	currConfig := TranslateObject(currItem, h.schema.Resource().GroupVersionKind(), h.client.domainSuffix)

	var oldConfig model.Config
	if old != nil {
		if oldItem, ok = old.(runtime.Object); !ok {
			log.Warnf("Old Object can not be converted to runtime Object %v, is type %T", old, old)
			return nil
		}
		oldConfig = *TranslateObject(oldItem, h.schema.Resource().GroupVersionKind(), h.client.domainSuffix)
	}

	for _, f := range h.handlers {
		f(oldConfig, *currConfig, event)
	}
	return nil
}

func createCacheHandler(cl *Client, schema collection.Schema, i informers.GenericInformer) (*cacheHandler, error) {
	h := &cacheHandler{
		client:        cl,
		schema:        schema,
		informer:      i.Informer(),
	}
	h.lister = func (namespace string) cache.GenericNamespaceLister {
		if schema.Resource().IsClusterScoped() {
			return i.Lister()
		} else {
			return i.Lister().ByNamespace(namespace)
		}
	}
	kind := schema.Resource().Kind()
	i.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			incrementEvent(kind, "add")
			rt, ok := obj.(runtime.Object)
			if !ok {
				log.Errorf("failed to convert %v to a runtime.Object", rt)
				return
			}
			_, err := cl.memory.Create(*TranslateObject(rt, h.schema.Resource().GroupVersionKind(), h.client.domainSuffix))
			if err != nil {
				// TODO better handling
				log.Errorf("failed to create object %v", err)
			}
		},
		UpdateFunc: func(old, cur interface{}) {
			if !reflect.DeepEqual(old, cur) {
				incrementEvent(kind, "update")
				rt, ok := cur.(runtime.Object)
				if !ok {
					log.Errorf("failed to convert %v to a runtime.Object", rt)
					return
				}
				_, err := cl.memory.Update(*TranslateObject(rt, h.schema.Resource().GroupVersionKind(), h.client.domainSuffix))
				if err != nil {
					// TODO better handling
					log.Errorf("failed to create object %v", err)
				}
			} else {
				incrementEvent(kind, "updatesame")
			}
		},
		DeleteFunc: func(obj interface{}) {
			incrementEvent(kind, "delete")
			rt, ok := obj.(runtime.Object)
			if !ok {
				log.Errorf("failed to convert %v to a runtime.Object", rt)
				return
			}
			//_, err := cl.memory.Delete(h.schema.Resource().GroupVersionKind(), rt.)
			//if err != nil {
			//	TODO better handling
				//log.Errorf("failed to create object %v", err)
			//}
		},
	})
	return h, nil
}

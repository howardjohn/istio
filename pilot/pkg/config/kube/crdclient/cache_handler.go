package crdclient

import (
	"reflect"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"

	"istio.io/client-go/pkg/informers/externalversions"

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
	informer externalversions.GenericInformer
	handlers []func(model.Config, model.Config, model.Event)
	schema   collection.Schema
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
		client:   cl,
		schema:   schema,
		informer: i,
	}
	kind := schema.Resource().Kind()
	i.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			incrementEvent(kind, "add")
			cl.tryLedgerPut(obj, kind)
			cl.queue.Push(func() error {
				return h.onEvent(nil, obj, model.EventAdd)
			})
		},
		UpdateFunc: func(old, cur interface{}) {
			cl.tryLedgerPut(cur, kind)
			if !reflect.DeepEqual(old, cur) {
				incrementEvent(kind, "update")
				cl.queue.Push(func() error {
					return h.onEvent(old, cur, model.EventUpdate)
				})
			} else {
				incrementEvent(kind, "updatesame")
			}
		},
		DeleteFunc: func(obj interface{}) {
			incrementEvent(kind, "delete")
			cl.tryLedgerDelete(obj, kind)
			cl.queue.Push(func() error {
				return h.onEvent(nil, obj, model.EventDelete)
			})
		},
	})
	return h, nil
}

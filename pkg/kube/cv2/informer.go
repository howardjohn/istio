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

package cv2

import (
	"context"
	"fmt"
	"istio.io/istio/pkg/tracing"
	"sync"

	"golang.org/x/exp/slices"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kubetypes"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/ptr"
)

type informer[I controllers.ComparableObject] struct {
	inf kclient.Informer[I]
	log *istiolog.Scope

	handlers *handlers[I]
}

type handlers[I controllers.ComparableObject] struct {
	handlersMu sync.Mutex
	handlers   []func(ctx context.Context, o Event[any])
}

func (h *handlers[I]) handler() cache.ResourceEventHandler {
	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			ctx, span := tracing.Start(context.Background(), fmt.Sprintf("incoming event for informer[%v]", ptr.TypeName[I]()))
			defer span.End()
			h.handlersMu.Lock()
			handlers := slices.Clone(h.handlers)
			h.handlersMu.Unlock()
			for _, handler := range handlers {
				handler(ctx, Event[any]{
					New:   &obj,
					Event: controllers.EventAdd,
				})
			}
		},
		UpdateFunc: func(oldInterface, newInterface any) {
			ctx, span := tracing.Start(context.Background(), fmt.Sprintf("incoming event for informer[%v]", ptr.TypeName[I]()))
			defer span.End()
			h.handlersMu.Lock()
			handlers := slices.Clone(h.handlers)
			h.handlersMu.Unlock()
			for _, handler := range handlers {
				handler(ctx, Event[any]{
					New:   &newInterface,
					Old:   &oldInterface,
					Event: controllers.EventUpdate,
				})
			}
		},
		DeleteFunc: func(obj any) {
			ctx, span := tracing.Start(context.Background(), fmt.Sprintf("incoming event for informer[%v]", ptr.TypeName[I]()))
			defer span.End()
			h.handlersMu.Lock()
			handlers := slices.Clone(h.handlers)
			h.handlersMu.Unlock()
			for _, handler := range handlers {
				handler(ctx, Event[any]{
					Old:   &obj,
					Event: controllers.EventDelete,
				})
			}
		},
	}
}

func (h *handlers[I]) add(f func(ctx context.Context, o Event[any])) {
	h.handlersMu.Lock()
	defer h.handlersMu.Unlock()
	h.handlers = append(h.handlers, f)
}

var _ Collection[controllers.Object] = &informer[controllers.Object]{}

func (i informer[I]) _internalHandler() {}

func (i informer[I]) Name() string {
	return fmt.Sprintf("informer[%T]", *new(I))
}

func (i informer[I]) List(namespace string) []I {
	res := i.inf.List(namespace, klabels.Everything())
	slices.SortFunc(res, func(a, b I) bool {
		return GetKey(a) < GetKey(b)
	})
	return res
}

func (i informer[I]) GetKey(k Key[I]) *I {
	ns, n := SplitKeyFunc(string(k))
	if got := i.inf.Get(n, ns); !controllers.IsNil(got) {
		return &got
	}
	return nil
}

func (i informer[I]) Register(f func(ctx context.Context, o Event[I])) {
	batchedRegister[I](i, f)
}

func (i informer[I]) RegisterBatch(f func(ctx context.Context, o []Event[I])) {
	i.handlers.add(func(ctx context.Context, o Event[any]) {
		i.log.WithLabels("key", GetKey(o.Latest()), "type", o.Event).Debugf("handling event")
		f(ctx, []Event[I]{castEvent[any, I](o)})
	})
}

func EventHandler[I any](handler func(ctx context.Context, o Event[any])) cache.ResourceEventHandler {
	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			ctx, span := tracing.Start(context.Background(), fmt.Sprintf("informer[%v]", ptr.TypeName[I]()))
			defer span.End()
			handler(ctx, Event[any]{
				New:   &obj,
				Event: controllers.EventAdd,
			})
		},
		UpdateFunc: func(oldInterface, newInterface any) {
			ctx, span := tracing.Start(context.Background(), fmt.Sprintf("informer[%v]", ptr.TypeName[I]()))
			defer span.End()
			handler(ctx, Event[any]{
				Old:   &oldInterface,
				New:   &newInterface,
				Event: controllers.EventUpdate,
			})
		},
		DeleteFunc: func(obj any) {
			ctx, span := tracing.Start(context.Background(), fmt.Sprintf("informer[%v]", ptr.TypeName[I]()))
			defer span.End()
			handler(ctx, Event[any]{
				Old:   &obj,
				Event: controllers.EventDelete,
			})
		},
	}
}

func WrapClient[I controllers.ComparableObject](c kclient.Informer[I]) Collection[I] {
	h := &handlers[I]{}
	c.AddEventHandler(h.handler())
	return informer[I]{
		inf:      c,
		log:      log.WithLabels("owner", fmt.Sprintf("NewInformer[%v]", ptr.TypeName[I]())),
		handlers: h,
	}
}

func NewInformer[I controllers.ComparableObject](c kube.Client) Collection[I] {
	return NewInformerFiltered[I](c, kubetypes.Filter{})
}

func NewInformerFiltered[I controllers.ComparableObject](c kube.Client, filter kubetypes.Filter) Collection[I] {
	return WrapClient[I](kclient.NewFiltered[I](c, filter))
}

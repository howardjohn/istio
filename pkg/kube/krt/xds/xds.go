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

package xds

import (
	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pilot/pkg/status"
	"istio.io/istio/pkg/adsc"
	"istio.io/istio/pkg/kube/krt"
)

type XdsCollection[T any] struct {
	krt.StaticCollection[T]
	xds *adsc.Client
}

// NewFileSingleton returns a collection that reads and watches a single file
// The `readFile` function is used to readOnce and deserialize the file and will be called each time the file changes.
// This will also be called during the initial construction of the collection; if the initial readFile fails an error is returned.
func New(
	address string,
	cfg *adsc.DeltaADSConfig,
	opts ...krt.CollectionOption,
) XdsCollection[*mcp.Resource] {
	collection := krt.NewStaticCollection[*mcp.Resource](nil, opts...)
	handler := adsc.Register(func(
		ctx adsc.HandlerContext,
		resourceName string,
		resourceVersion string,
		val *mcp.Resource,
		event adsc.Event,
	) {
		switch event {
		case adsc.EventAdd:
			collection.UpdateObject(val)
		case adsc.EventDelete:
			collection.DeleteObject(resourceName)
		}
	})
	// This is unfortunate. A few issues:
	// * Istiod has an invalid mismatch of outer and inner type, and wraps everything in MCP.resource
	// * We cannot dynamically add watches
	handlers := []adsc.Option{
		handler,
		adsc.WatchType("networking.istio.io/v1/VirtualService", "*"),
	}
	// Probably what we should do is add an "istio" mode into ADSC (sotw has this) which watches each type.
	// The handler will translate these into a map[type]collection[controller.Object] or something

	xds := adsc.NewDelta(address, cfg, handlers...)
	ctx := status.NewIstioContext(krt.GetStop(opts...))
	xds.Run(ctx)
	//go func() {
	//	select {
	//	case <-c.xds.Synced():
	//	case <-stop:
	//		return
	//	}
	//	log.Infof("sync complete")
	//}()
	return XdsCollection[*mcp.Resource]{StaticCollection: collection, xds: xds}
}

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

package xds_test

import (
	"context"
	"fmt"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"

	"istio.io/istio/pilot/test/xds"
	"istio.io/istio/pkg/adsc"
	"istio.io/istio/pkg/kube/krt"
	xdskrt "istio.io/istio/pkg/kube/krt/xds"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

func TestXDSCollection(t *testing.T) {
	stop := test.NewStop(t)

	ds := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		ConfigString: `apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: route
spec:
  hosts:
  - example
  http:
  - route:
    - destination:
        host: example
`,
	})
	meta, _ := structpb.NewStruct(map[string]interface{}{"GENERATOR": "api"})
	cfg := &adsc.DeltaADSConfig{
		adsc.Config{
			ClientName:         "",
			Address:            "",
			XDSSAN:             "",
			Namespace:          "",
			Workload:           "",
			Revision:           "",
			Meta:               meta,
			Locality:           nil,
			NodeType:           "",
			IP:                 "",
			CertDir:            "",
			SecretManager:      nil,
			XDSRootCAFile:      "",
			InsecureSkipVerify: false,
			BackoffPolicy:      nil,
			GrpcOpts: []grpc.DialOption{
				grpc.WithBlock(),
				grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
					return ds.BufListener.Dial()
				}),
				grpc.WithTransportCredentials(insecure.NewCredentials()),
			},
		},
	}

	xc := xdskrt.New("bogus", cfg, krt.WithStop(stop))
	xc.WaitUntilSynced(stop)
	xc.List()
}

func TrackerHandler[T any](tracker *assert.Tracker[string]) func(krt.Event[T]) {
	return func(o krt.Event[T]) {
		tracker.Record(fmt.Sprintf("%v/%v", o.Event, krt.GetKey(o.Latest())))
	}
}

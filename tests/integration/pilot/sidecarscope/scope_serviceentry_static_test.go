// Copyright 2019 Istio Authors
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

package sidecarscope

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	xdscore "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/gogo/protobuf/proto"
	v2 "istio.io/istio/pilot/pkg/proxy/envoy/v2"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/scopes"
)

func TestServiceEntryStatic(t *testing.T) {
	framework.Run(t, func(ctx framework.TestContext) {
		config := Config{
			Resolution: "STATIC",
		}
		p, nodeID := setupTest(t, ctx, config)

		// Check to ensure only endpoints for imported namespaces
		req := &xdsapi.DiscoveryRequest{
			Node: &xdscore.Node{
				Id: nodeID.ServiceNode(),
			},
			ResourceNames: []string{"outbound|80||app.com"},
			TypeUrl:       v2.EndpointType,
		}

		if err := p.StartDiscovery(req); err != nil {
			t.Fatal(err)
		}
		if err := p.WatchDiscovery(time.Second*5, checkResultStatic); err != nil {
			t.Error(err)
		}

		// Check to ensure only listeners for own namespace
		ListenerReq := &xdsapi.DiscoveryRequest{
			Node: &xdscore.Node{
				Id: nodeID.ServiceNode(),
			},
			TypeUrl: v2.ListenerType,
		}

		if err := p.StartDiscovery(ListenerReq); err != nil {
			t.Fatal(err)
		}
		if err := p.WatchDiscovery(time.Second*500, checkResultStaticListener); err != nil {
			t.Error(err)
		}
	})
}

func checkResultStatic(resp *xdsapi.DiscoveryResponse) (success bool, e error) {
	expected := map[string]int{
		"1.1.1.1": 1,
		"2.2.2.2": 1,
	}

	found := false
	for _, res := range resp.Resources {
		c := &xdsapi.ClusterLoadAssignment{}
		if err := proto.Unmarshal(res.Value, c); err != nil {
			return false, err
		}
		if c.ClusterName != "outbound|80||app.com" {
			continue
		}
		found = true

		got := map[string]int{}
		for _, ep := range c.Endpoints {
			for _, lb := range ep.LbEndpoints {
				got[lb.GetEndpoint().Address.GetSocketAddress().Address]++
			}
		}
		if !reflect.DeepEqual(expected, got) {
			scopes.Framework.Warnf("Excepted load assignments %+v, got %+v. Continuing in case pilot updates with correct response", expected, got)
			return false, nil
		}
	}
	if !found {
		// We didn't find the expected cluster, keep watching
		return false, nil
	}
	return true, nil
}

func checkResultStaticListener(resp *xdsapi.DiscoveryResponse) (success bool, e error) {
	expected := map[string]struct{}{
		"1.1.1.1_80": {},
		"0.0.0.0_80": {},
		"5.5.5.5_443": {},
		"virtual":    {},
	}

	got := map[string]struct{}{}
	for _, res := range resp.Resources {
		c := &xdsapi.Listener{}
		if err := proto.Unmarshal(res.Value, c); err != nil {
			return false, err
		}
		got[c.Name] = struct{}{}

		debug, _ := json.MarshalIndent(c, "howardjohn", "  ")
		scopes.Framework.Warnf("howardjohn: %s", debug)
	}
	if !reflect.DeepEqual(expected, got) {
		scopes.Framework.Warnf("Excepted listeners %+v, got %+v. Continuing in case pilot updates with correct response", expected, got)
		return false, nil
	}
	return true, nil
}

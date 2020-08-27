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
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/xds"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/adsc"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/tests/util"
)

// The connect and reconnect tests are removed - ADS already has coverage, and the
// StreamEndpoints is not used in 1.0+

const (
	asdcLocality  = "region1/zone1/subzone1"
	asdc2Locality = "region2/zone2/subzone2"

	edsIncSvc = "eds.test.svc.cluster.local"
	edsIncVip = "10.10.1.2"
)

func TestIncrementalPush(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{ConfigString: mustReadFile(t, "tests/testdata/config/destination-rule-all.yaml")})
	ads := s.Connect(nil, nil, watchAll)
	t.Run("Full Push", func(t *testing.T) {
		s.Discovery.Push(&model.PushRequest{Full: true})
		if _, err := ads.Wait(time.Second*5, watchAll...); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("Incremental Push", func(t *testing.T) {
		ads.WaitClear()
		s.Discovery.Push(&model.PushRequest{Full: false})
		if err := ads.WaitSingle(time.Second*5, v3.EndpointType); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("Incremental Push with updated services", func(t *testing.T) {
		ads.WaitClear()
		s.Discovery.Push(&model.PushRequest{
			Full: false,
			ConfigsUpdated: map[model.ConfigKey]struct{}{
				{Name: "destall.default.svc.cluster.local", Namespace: "testns", Kind: gvk.ServiceEntry}: {},
			},
		})
		if err := ads.WaitSingle(time.Second*5, v3.EndpointType); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("Full Push with updated services", func(t *testing.T) {
		ads.WaitClear()
		s.Discovery.Push(&model.PushRequest{
			Full: true,
			ConfigsUpdated: map[model.ConfigKey]struct{}{
				{Name: "foo.bar", Namespace: "default", Kind: gvk.ServiceEntry}:   {},
				{Name: "destall", Namespace: "testns", Kind: gvk.DestinationRule}: {},
			},
		})
		if _, err := ads.Wait(time.Second*5, watchAll...); err != nil {
			t.Fatal(err)
		}
		if len(ads.GetEndpoints()) < 3 {
			t.Fatalf("Expected a full EDS update, but got: %v", ads.GetEndpoints())
		}
	})
	t.Run("Full Push without updated services", func(t *testing.T) {
		ads.WaitClear()
		s.Discovery.Push(&model.PushRequest{
			Full: true,
			ConfigsUpdated: map[model.ConfigKey]struct{}{
				{Name: "destall", Namespace: "testns", Kind: gvk.DestinationRule}: {},
			},
		})
		if _, err := ads.Wait(time.Second*5, v3.ClusterType, v3.EndpointType); err != nil {
			t.Fatal(err)
		}
		if len(ads.GetEndpoints()) < 3 {
			t.Fatalf("Expected a full EDS update, but got: %v", ads.GetEndpoints())
		}
	})
}

func TestEds(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{ConfigString: mustReadFile(t, "tests/testdata/config/destination-rule-locality.yaml")})
	addUdsEndpoint(s)

	// enable locality load balancing and add relevant endpoints in order to test
	addLocalityEndpoints(s, "locality.cluster.local")
	addLocalityEndpoints(s, "locality-no-outlier-detection.cluster.local")

	// Add the test ads clients to list of service instances in order to test the context dependent locality coloring.
	addTestClientEndpoints(s)

	s.Discovery.MemRegistry.AddHTTPService(edsIncSvc, edsIncVip, 8080)
	s.Discovery.MemRegistry.SetEndpoints(edsIncSvc, "",
		newEndpointWithAccount("127.0.0.1", "hello-sa", "v1"))

	adscConn := s.Connect(&model.Proxy{IPAddresses: []string{"10.10.10.10"}}, nil, watchAll)
	adscConn2 := s.Connect(&model.Proxy{IPAddresses: []string{"10.10.10.11"}}, nil, watchAll)

	t.Run("TCPEndpoints", func(t *testing.T) {
		testTCPEndpoints("127.0.0.1", adscConn, t)
	})
	t.Run("edsz", func(t *testing.T) {
		testEdsz(t, s, "test-1.default")
	})
	t.Run("LocalityPrioritizedEndpoints", func(t *testing.T) {
		testLocalityPrioritizedEndpoints(adscConn, adscConn2, t)
	})
	t.Run("UDSEndpoints", func(t *testing.T) {
		testUdsEndpoints(adscConn, t)
	})
	t.Run("PushIncremental", func(t *testing.T) {
		edsUpdateInc(s, adscConn, t)
	})
	t.Run("Push", func(t *testing.T) {
		edsUpdates(s, adscConn, t)
	})
	t.Run("MultipleRequest", func(t *testing.T) {
		multipleRequest(s, false, 20, 5, 5*time.Second, nil, t)
	})
	// 5 pushes for 100 clients, using EDS incremental only.
	t.Run("MultipleRequestIncremental", func(t *testing.T) {
		multipleRequest(s, true, 20, 5, 5*time.Second, nil, t)
	})
	t.Run("CDSSave", func(t *testing.T) {
		// Moved from cds_test, using new client
		clusters := adscConn.GetClusters()
		if len(clusters) == 0 {
			t.Error("No clusters in ADS response")
		}
		strResponse, _ := json.MarshalIndent(clusters, " ", " ")
		_ = ioutil.WriteFile(env.IstioOut+"/cdsv2_sidecar.json", strResponse, 0644)

	})
}

func mustReadFile(t *testing.T, fpaths ...string) string {
	result := ""
	for _, fpath := range fpaths {
		bytes, err := ioutil.ReadFile(filepath.Join(env.IstioSrc, fpath))
		if err != nil {
			t.Fatal(err)
		}
		result += "---\n"
		result += string(bytes)
	}
	return result
}
func mustReadfolder(t *testing.T, folder string) string {
	result := ""
	f, err := ioutil.ReadDir(filepath.Join(env.IstioSrc, folder))
	if err != nil {
		t.Fatal(err)
	}
	for _, fpath := range f {
		bytes, err := ioutil.ReadFile(filepath.Join(env.IstioSrc, folder, fpath.Name()))
		if err != nil {
			t.Fatal(err)
		}
		result += "---\n"
		result += string(bytes)
	}
	return result
}

func TestEdsWeightedServiceEntry(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{ConfigString: mustReadFile(t, "tests/testdata/config/static-weighted-se.yaml")})
	adscConn := s.Connect(nil, nil, watchEds)
	endpoints := adscConn.GetEndpoints()
	lbe, f := endpoints["outbound|80||weighted.static.svc.cluster.local"]
	if !f || len(lbe.Endpoints) == 0 {
		t.Fatalf("No lb endpoints for %v, %v", "outbound|80||weighted.static.svc.cluster.local", adscConn.EndpointsJSON())
	}
	expected := map[string]uint32{
		"a":       9, // sum of 1 and 8
		"b":       3,
		"3.3.3.3": 1, // no weight provided is normalized to 1
		"2.2.2.2": 8,
		"1.1.1.1": 3,
	}
	got := make(map[string]uint32)
	for _, lbe := range lbe.Endpoints {
		got[lbe.Locality.Region] = lbe.LoadBalancingWeight.Value
		for _, e := range lbe.LbEndpoints {
			got[e.GetEndpoint().Address.GetSocketAddress().Address] = e.LoadBalancingWeight.Value
		}
	}
	if !reflect.DeepEqual(expected, got) {
		t.Errorf("Expected LB weights %v got %v", expected, got)
	}
}

var watchEds = []string{v3.ClusterType, v3.EndpointType}
var watchAll = []string{v3.ClusterType, v3.EndpointType, v3.ListenerType, v3.RouteType}

func TestEDSOverlapping(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	addOverlappingEndpoints(s)
	adscon := s.Connect(nil, nil, watchEds)
	testOverlappingPorts(s, adscon, t)
}

// Validates the behavior when Service resolution type is updated after initial EDS push.
// See https://github.com/istio/istio/issues/18355 for more details.
func TestEDSServiceResolutionUpdate(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	addEdsCluster(s, "edsdns.svc.cluster.local", "http", "10.0.0.53", 8080)
	addEdsCluster(s, "other.local", "http", "1.1.1.1", 8080)

	adscConn := s.Connect(nil, nil, watchAll)

	// Validate that endpoints are pushed correctly.
	testEndpoints("10.0.0.53", "outbound|8080||edsdns.svc.cluster.local", adscConn, t)

	// Now update the service resolution to DNSLB with a DNS endpoint.
	updateServiceResolution(s)

	if _, err := adscConn.Wait(5*time.Second, "eds"); err != nil {
		t.Fatal(err)
	}

	// Validate that endpoints are skipped.
	lbe := adscConn.GetEndpoints()["outbound|8080||edsdns.svc.cluster.local"]
	if lbe != nil && len(lbe.Endpoints) > 0 {
		t.Fatalf("endpoints not expected for  %s,  but got %v", "edsdns.svc.cluster.local", adscConn.EndpointsJSON())
	}
}

// Validate that when endpoints of a service flipflop between 1 and 0 does not trigger a full push.
func TestEndpointFlipFlops(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	addEdsCluster(s, "flipflop.com", "http", "10.0.0.53", 8080)

	adscConn := s.Connect(nil, nil, watchAll)

	// Validate that endpoints are pushed correctly.
	testEndpoints("10.0.0.53", "outbound|8080||flipflop.com", adscConn, t)

	// Clear the endpoint and validate it does not trigger a full push.
	s.Discovery.MemRegistry.SetEndpoints("flipflop.com", "", []*model.IstioEndpoint{})

	upd, _ := adscConn.Wait(5*time.Second, "eds")

	if contains(upd, "cds") {
		t.Fatalf("Expecting only EDS update as part of a partial push. But received CDS also %v", upd)
	}

	if len(upd) > 0 && !contains(upd, "eds") {
		t.Fatalf("Expecting EDS push as part of a partial push. But received %v", upd)
	}

	lbe := adscConn.GetEndpoints()["outbound|8080||flipflop.com"]
	if len(lbe.Endpoints) != 0 {
		t.Fatalf("There should be no endpoints for outbound|8080||flipflop.com. Endpoints:\n%v", adscConn.EndpointsJSON())
	}

	// Validate that keys in service still exist in EndpointShardsByService - this prevents full push.
	if len(s.Discovery.EndpointShardsByService["flipflop.com"]) == 0 {
		t.Fatalf("Expected service key %s to be present in EndpointShardsByService. But missing %v", "flipflop.com", s.Discovery.EndpointShardsByService)
	}

	// Set the endpoints again and validate it does not trigger full push.
	s.Discovery.MemRegistry.SetEndpoints("flipflop.com", "",
		[]*model.IstioEndpoint{
			{
				Address:         "10.10.1.1",
				ServicePortName: "http",
				EndpointPort:    8080,
			}})

	upd, _ = adscConn.Wait(5 * time.Second)

	if contains(upd, "cds") {
		t.Fatal("Expecting only EDS update as part of a partial push. But received CDS also +v", upd)
	}

	if len(upd) > 0 && !contains(upd, "eds") {
		t.Fatal("Expecting EDS push as part of a partial push. But did not receive +v", upd)
	}

	testEndpoints("10.10.1.1", "outbound|8080||flipflop.com", adscConn, t)
}

// Validate that deleting a service clears entries from EndpointShardsByService.
func TestDeleteService(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	addEdsCluster(s, "removeservice.com", "http", "10.0.0.53", 8080)

	adscConn := s.Connect(nil, nil, watchEds)

	// Validate that endpoints are pushed correctly.
	testEndpoints("10.0.0.53", "outbound|8080||removeservice.com", adscConn, t)

	s.Discovery.MemRegistry.RemoveService("removeservice.com")

	if len(s.Discovery.EndpointShardsByService["removeservice.com"]) != 0 {
		t.Fatalf("Expected service key %s to be deleted in EndpointShardsByService. But is still there %v",
			"removeservice.com", s.Discovery.EndpointShardsByService)
	}
}

func fullPush(s *xds.FakeDiscoveryServer) {
	s.Discovery.Push(&model.PushRequest{Full: true})
}

func adsConnectAndWait(t *testing.T, ip int) *adsc.ADSC {
	adscConn, err := adsc.Dial(util.MockPilotGrpcAddr, "", &adsc.Config{
		IP: testIP(uint32(ip)),
	})
	if err != nil {
		t.Fatal("Error connecting ", err)
	}
	adscConn.Watch()
	_, err = adscConn.Wait(10*time.Second, "eds", "lds", "cds", "rds")
	if err != nil {
		t.Fatal("Error getting initial config ", err)
	}

	if len(adscConn.GetEndpoints()) == 0 {
		t.Fatal("No endpoints")
	}
	return adscConn
}

func addTestClientEndpoints(server *xds.FakeDiscoveryServer) {
	server.Discovery.MemRegistry.AddService("test-1.default", &model.Service{
		Hostname: "test-1.default",
		Ports: model.PortList{
			{
				Name:     "http",
				Port:     80,
				Protocol: protocol.HTTP,
			},
		},
	})
	server.Discovery.MemRegistry.AddInstance("test-1.default", &model.ServiceInstance{
		Endpoint: &model.IstioEndpoint{
			Address:         "10.10.10.10",
			ServicePortName: "http",
			EndpointPort:    80,
			Locality:        model.Locality{Label: asdcLocality},
		},
		ServicePort: &model.Port{
			Name:     "http",
			Port:     80,
			Protocol: protocol.HTTP,
		},
	})
	server.Discovery.MemRegistry.AddInstance("test-1.default", &model.ServiceInstance{
		Endpoint: &model.IstioEndpoint{
			Address:         "10.10.10.11",
			ServicePortName: "http",
			EndpointPort:    80,
			Locality:        model.Locality{Label: asdc2Locality},
		},
		ServicePort: &model.Port{
			Name:     "http",
			Port:     80,
			Protocol: protocol.HTTP,
		},
	})
}

// Verify server sends the endpoint. This check for a single endpoint with the given
// address.
func testTCPEndpoints(expected string, adsc *adsc.ADSC, t *testing.T) {
	t.Helper()
	testEndpoints(expected, "outbound|8080||eds.test.svc.cluster.local", adsc, t)
}

// Verify server sends the endpoint. This check for a single endpoint with the given
// address.
func testEndpoints(expected string, cluster string, adsc *adsc.ADSC, t *testing.T) {
	t.Helper()
	lbe, f := adsc.GetEndpoints()[cluster]
	if !f || len(lbe.Endpoints) == 0 {
		t.Fatalf("No lb endpoints for %v, %v", cluster, adsc.EndpointsJSON())
	}
	var found []string
	for _, lbe := range lbe.Endpoints {
		for _, e := range lbe.LbEndpoints {
			addr := e.GetEndpoint().Address.GetSocketAddress().Address
			found = append(found, addr)
			if expected == addr {
				return
			}
		}
	}
	t.Fatalf("Expecting %s got %v", expected, found)
}

func testLocalityPrioritizedEndpoints(adsc *adsc.ADSC, adsc2 *adsc.ADSC, t *testing.T) {
	endpoints1 := adsc.GetEndpoints()
	endpoints2 := adsc2.GetEndpoints()

	verifyLocalityPriorities(asdcLocality, endpoints1["outbound|80||locality.cluster.local"].GetEndpoints(), t)
	verifyLocalityPriorities(asdc2Locality, endpoints2["outbound|80||locality.cluster.local"].GetEndpoints(), t)

	// No outlier detection specified for this cluster, so we shouldn't apply priority.
	verifyNoLocalityPriorities(endpoints1["outbound|80||locality-no-outlier-detection.cluster.local"].GetEndpoints(), t)
	verifyNoLocalityPriorities(endpoints2["outbound|80||locality-no-outlier-detection.cluster.local"].GetEndpoints(), t)
}

// Tests that Services with multiple ports sharing the same port number are properly sent endpoints.
// Real world use case for this is kube-dns, which uses port 53 for TCP and UDP.
func testOverlappingPorts(s *xds.FakeDiscoveryServer, adsc *adsc.ADSC, t *testing.T) {
	// Test initial state
	testEndpoints("10.0.0.53", "outbound|53||overlapping.cluster.local", adsc, t)

	s.Discovery.Push(&model.PushRequest{
		Full: true,
		ConfigsUpdated: map[model.ConfigKey]struct{}{{
			Kind: gvk.ServiceEntry,
			Name: "overlapping.cluster.local",
		}: {}}})
	_, _ = adsc.Wait(5 * time.Second)

	// After the incremental push, we should still see the endpoint
	testEndpoints("10.0.0.53", "outbound|53||overlapping.cluster.local", adsc, t)
}

func verifyNoLocalityPriorities(eps []*endpoint.LocalityLbEndpoints, t *testing.T) {
	for _, ep := range eps {
		if ep.GetPriority() != 0 {
			t.Errorf("expected no locality priorities to apply, got priority %v.", ep.GetPriority())
		}
	}
}

func verifyLocalityPriorities(proxyLocality string, eps []*endpoint.LocalityLbEndpoints, t *testing.T) {
	items := strings.SplitN(proxyLocality, "/", 3)
	region, zone, subzone := items[0], items[1], items[2]
	for _, ep := range eps {
		if ep.GetLocality().Region == region {
			if ep.GetLocality().Zone == zone {
				if ep.GetLocality().SubZone == subzone {
					if ep.GetPriority() != 0 {
						t.Errorf("expected endpoint pool from same locality to have priority of 0, got %v", ep.GetPriority())
					}
				} else if ep.GetPriority() != 1 {
					t.Errorf("expected endpoint pool from a different subzone to have priority of 1, got %v", ep.GetPriority())
				}
			} else {
				if ep.GetPriority() != 2 {
					t.Errorf("expected endpoint pool from a different zone to have priority of 2, got %v", ep.GetPriority())
				}
			}
		} else {
			if ep.GetPriority() != 3 {
				t.Errorf("expected endpoint pool from a different region to have priority of 3, got %v", ep.GetPriority())
			}
		}
	}
}

// Verify server sends UDS endpoints
func testUdsEndpoints(adsc *adsc.ADSC, t *testing.T) {
	// Check the UDS endpoint ( used to be separate test - but using old unused GRPC method)
	// The new test also verifies CDS is pusing the UDS cluster, since adsc.eds is
	// populated using CDS response
	lbe, f := adsc.GetEndpoints()["outbound|0||localuds.cluster.local"]
	if !f || len(lbe.Endpoints) == 0 {
		t.Error("No UDS lb endpoints")
	} else {
		ep0 := lbe.Endpoints[0]
		if len(ep0.LbEndpoints) != 1 {
			t.Fatalf("expected 1 LB endpoint but got %d", len(ep0.LbEndpoints))
		}
		lbep := ep0.LbEndpoints[0]
		path := lbep.GetEndpoint().GetAddress().GetPipe().GetPath()
		if path != udsPath {
			t.Fatalf("expected Pipe to %s, got %s", udsPath, path)
		}
	}
}

// Update
func edsUpdates(s *xds.FakeDiscoveryServer, adsc *adsc.ADSC, t *testing.T) {
	// Old style (non-incremental)
	s.Discovery.MemRegistry.SetEndpoints(edsIncSvc, "",
		newEndpointWithAccount("127.0.0.3", "hello-sa", "v1"))

	xds.AdsPushAll(s.Discovery)

	// will trigger recompute and push

	if _, err := adsc.Wait(5*time.Second, "eds"); err != nil {
		t.Fatal("EDS push failed", err)
	}
	testTCPEndpoints("127.0.0.3", adsc, t)
}

// edsFullUpdateCheck checks for updates required in a full push after the CDS update
func edsFullUpdateCheck(adsc *adsc.ADSC, t *testing.T) {
	t.Helper()
	if upd, err := adsc.Wait(15*time.Second, "cds", "eds", "lds", "rds"); err != nil {
		t.Fatal("Expecting CDS, EDS, LDS, and RDS update as part of a full push", err, upd)
	}
}

// This test must be run in isolation, can't be parallelized with any other v2 test.
// It makes different kind of updates, and checks that incremental or full push happens.
// In particular:
// - just endpoint changes -> incremental
// - service account changes -> full ( in future: CDS only )
// - label changes -> full
func edsUpdateInc(s *xds.FakeDiscoveryServer, adsc *adsc.ADSC, t *testing.T) {

	// TODO: set endpoints for a different cluster (new shard)

	// Verify initial state
	testTCPEndpoints("127.0.0.1", adsc, t)

	adsc.WaitClear() // make sure there are no pending pushes.

	// Equivalent with the event generated by K8S watching the Service.
	// Will trigger a push.
	s.Discovery.MemRegistry.SetEndpoints(edsIncSvc, "",
		newEndpointWithAccount("127.0.0.2", "hello-sa", "v1"))

	upd, err := adsc.Wait(5 * time.Second)
	if err != nil {
		t.Fatal("Incremental push failed", err)
	}
	if !reflect.DeepEqual(upd, []string{"eds"}) {
		t.Fatal("Expecting EDS only update, got", upd)
	}

	testTCPEndpoints("127.0.0.2", adsc, t)

	// Update the endpoint with different SA - expect full
	s.Discovery.MemRegistry.SetEndpoints(edsIncSvc, "",
		newEndpointWithAccount("127.0.0.3", "account2", "v1"))

	edsFullUpdateCheck(adsc, t)
	testTCPEndpoints("127.0.0.3", adsc, t)

	// Update the endpoint again, no SA change - expect incremental
	s.Discovery.MemRegistry.SetEndpoints(edsIncSvc, "",
		newEndpointWithAccount("127.0.0.4", "account2", "v1"))

	upd, err = adsc.Wait(5 * time.Second)
	if err != nil {
		t.Fatal("Incremental push failed", err)
	}
	if !reflect.DeepEqual(upd, []string{"eds"}) {
		t.Fatal("Expecting EDS only update, got", upd)
	}
	testTCPEndpoints("127.0.0.4", adsc, t)

	// Update the endpoint to original SA - expect full
	s.Discovery.MemRegistry.SetEndpoints(edsIncSvc, "",
		newEndpointWithAccount("127.0.0.2", "hello-sa", "v1"))
	edsFullUpdateCheck(adsc, t)
	testTCPEndpoints("127.0.0.2", adsc, t)

	// Update the endpoint again, no label change - expect incremental
	s.Discovery.MemRegistry.SetEndpoints(edsIncSvc, "",
		newEndpointWithAccount("127.0.0.5", "hello-sa", "v1"))

	upd, err = adsc.Wait(5 * time.Second)
	if err != nil {
		t.Fatal("Incremental push failed", err)
	}
	if !reflect.DeepEqual(upd, []string{"eds"}) {
		t.Fatal("Expecting EDS only update, got", upd)
	}
	testTCPEndpoints("127.0.0.5", adsc, t)

	// Wipe out all endpoints - expect full
	s.Discovery.MemRegistry.SetEndpoints(edsIncSvc, "", []*model.IstioEndpoint{})

	if upd, err := adsc.Wait(15*time.Second, "eds"); err != nil {
		t.Fatal("Expecting EDS update as part of a partial push", err, upd)
	}

	lbe := adsc.GetEndpoints()["outbound|8080||eds.test.svc.cluster.local"]
	if len(lbe.Endpoints) != 0 {
		t.Fatalf("There should be no endpoints for outbound|8080||eds.test.svc.cluster.local. Endpoints:\n%v", adsc.EndpointsJSON())
	}
}

// Make a direct EDS grpc request to pilot, verify the result is as expected.
// This test includes a 'bad client' regression test, which fails to read on the
// stream.
func multipleRequest(s *xds.FakeDiscoveryServer, inc bool, nclients,
	nPushes int, to time.Duration, _ map[string]string, t *testing.T) {
	wgConnect := &sync.WaitGroup{}
	wg := &sync.WaitGroup{}
	errChan := make(chan error, nclients)

	// Bad client - will not read any response. This triggers Write to block, which should
	// be detected
	// This is not using adsc, which consumes the events automatically.
	ads := s.ConnectADS()
	err := sendCDSReq(sidecarID(testIP(0x0a120001), "app3"), ads)
	if err != nil {
		t.Fatal(err)
	}

	n := nclients
	wg.Add(n)
	wgConnect.Add(n)
	rcvPush := int32(0)
	rcvClients := int32(0)
	for i := 0; i < n; i++ {
		current := i
		go func(id int) {
			defer wg.Done()
			// Connect and get initial response
			adscConn := s.Connect(&model.Proxy{IPAddresses: []string{fmt.Sprintf("1.1.1.%d", id)}}, nil, nil)
			_, err := adscConn.Wait(15*time.Second, "rds")
			if err != nil {
				errChan <- errors.New("failed to get initial rds: " + err.Error())
				wgConnect.Done()
				return
			}

			if len(adscConn.GetEndpoints()) == 0 {
				errChan <- errors.New("no endpoints")
				wgConnect.Done()
				return
			}

			wgConnect.Done()

			// Check we received all pushes
			log.Println("Waiting for pushes ", id)

			// Pushes may be merged so we may not get nPushes pushes
			got, err := adscConn.Wait(15*time.Second, "eds")

			// If in incremental mode, shouldn't receive cds|rds|lds here
			if inc {
				for _, g := range got {
					if g == "cds" || g == "rds" || g == "lds" {
						errChan <- fmt.Errorf("should be eds incremental but received cds. %v %v",
							err, id)
						return
					}
				}
			}

			atomic.AddInt32(&rcvPush, 1)
			if err != nil {
				log.Println("Recv failed", err, id)
				errChan <- fmt.Errorf("failed to receive a response in 15 s %v %v",
					err, id)
				return
			}

			log.Println("Received all pushes ", id)
			atomic.AddInt32(&rcvClients, 1)

			adscConn.Close()
		}(current)
	}
	ok := waitTimeout(wgConnect, to)
	if !ok {
		t.Fatal("Failed to connect")
	}
	log.Println("Done connecting")

	// All clients are connected - this can start pushing changes.
	for j := 0; j < nPushes; j++ {
		if inc {
			// This will be throttled - we want to trigger a single push
			s.Discovery.AdsPushAll(strconv.Itoa(j), &model.PushRequest{
				Full: false,
				ConfigsUpdated: map[model.ConfigKey]struct{}{{
					Kind: gvk.ServiceEntry,
					Name: edsIncSvc,
				}: {}},
				Push: s.Discovery.Env.PushContext,
			})
		} else {
			xds.AdsPushAll(s.Discovery)
		}
		log.Println("Push done ", j)
	}

	ok = waitTimeout(wg, to)
	if !ok {
		t.Errorf("Failed to receive all responses %d %d", rcvClients, rcvPush)
		buf := make([]byte, 1<<16)
		runtime.Stack(buf, true)
		fmt.Printf("%s", buf)
	}

	close(errChan)

	// moved from ads_test, which had a duplicated test.
	for e := range errChan {
		t.Error(e)
	}
}

func waitTimeout(wg *sync.WaitGroup, timeout time.Duration) bool {
	c := make(chan struct{})
	go func() {
		defer close(c)
		wg.Wait()
	}()
	select {
	case <-c:
		return true
	case <-time.After(timeout):
		return false
	}
}

const udsPath = "/var/run/test/socket"

func addUdsEndpoint(s *xds.FakeDiscoveryServer) {
	s.Discovery.MemRegistry.AddService("localuds.cluster.local", &model.Service{
		Hostname: "localuds.cluster.local",
		Ports: model.PortList{
			{
				Name:     "grpc",
				Port:     0,
				Protocol: protocol.GRPC,
			},
		},
		MeshExternal: true,
		Resolution:   model.ClientSideLB,
	})
	s.Discovery.MemRegistry.AddInstance("localuds.cluster.local", &model.ServiceInstance{
		Endpoint: &model.IstioEndpoint{
			Address:         udsPath,
			EndpointPort:    0,
			ServicePortName: "grpc",
			Locality:        model.Locality{Label: "localhost"},
			Labels:          map[string]string{"socket": "unix"},
		},
		ServicePort: &model.Port{
			Name:     "grpc",
			Port:     0,
			Protocol: protocol.GRPC,
		},
	})

	pushReq := &model.PushRequest{
		Full:   true,
		Reason: []model.TriggerReason{model.ConfigUpdate},
	}
	s.Discovery.ConfigUpdate(pushReq)
}

func addLocalityEndpoints(server *xds.FakeDiscoveryServer, hostname host.Name) {
	server.Discovery.MemRegistry.AddService(hostname, &model.Service{
		Hostname: hostname,
		Ports: model.PortList{
			{
				Name:     "http",
				Port:     80,
				Protocol: protocol.HTTP,
			},
		},
	})
	localities := []string{
		"region1/zone1/subzone1",
		"region1/zone1/subzone2",
		"region1/zone2/subzone1",
		"region2/zone1/subzone1",
		"region2/zone1/subzone2",
		"region2/zone2/subzone1",
		"region2/zone2/subzone2",
	}
	for i, locality := range localities {
		server.Discovery.MemRegistry.AddInstance(hostname, &model.ServiceInstance{
			Endpoint: &model.IstioEndpoint{
				Address:         fmt.Sprintf("10.0.0.%v", i),
				EndpointPort:    80,
				ServicePortName: "http",
				Locality:        model.Locality{Label: locality},
			},
			ServicePort: &model.Port{
				Name:     "http",
				Port:     80,
				Protocol: protocol.HTTP,
			},
		})
	}
}

// nolint: unparam
func addEdsCluster(s *xds.FakeDiscoveryServer, hostName string, portName string, address string, port int) {
	s.Discovery.MemRegistry.AddService(host.Name(hostName), &model.Service{
		Hostname: host.Name(hostName),
		Ports: model.PortList{
			{
				Name:     portName,
				Port:     port,
				Protocol: protocol.HTTP,
			},
		},
	})

	s.Discovery.MemRegistry.AddInstance(host.Name(hostName), &model.ServiceInstance{
		Endpoint: &model.IstioEndpoint{
			Address:         address,
			EndpointPort:    uint32(port),
			ServicePortName: portName,
		},
		ServicePort: &model.Port{
			Name:     portName,
			Port:     port,
			Protocol: protocol.HTTP,
		},
	})
	fullPush(s)
}

func updateServiceResolution(s *xds.FakeDiscoveryServer) {
	s.Discovery.MemRegistry.AddService("edsdns.svc.cluster.local", &model.Service{
		Hostname: "edsdns.svc.cluster.local",
		Ports: model.PortList{
			{
				Name:     "http",
				Port:     8080,
				Protocol: protocol.HTTP,
			},
		},
		Resolution: model.DNSLB,
	})

	s.Discovery.MemRegistry.AddInstance("edsdns.svc.cluster.local", &model.ServiceInstance{
		Endpoint: &model.IstioEndpoint{
			Address:         "somevip.com",
			EndpointPort:    8080,
			ServicePortName: "http",
		},
		ServicePort: &model.Port{
			Name:     "http",
			Port:     8080,
			Protocol: protocol.HTTP,
		},
	})

	fullPush(s)
}

func addOverlappingEndpoints(s *xds.FakeDiscoveryServer) {
	s.Discovery.MemRegistry.AddService("overlapping.cluster.local", &model.Service{
		Hostname: "overlapping.cluster.local",
		Ports: model.PortList{
			{
				Name:     "dns",
				Port:     53,
				Protocol: protocol.UDP,
			},
			{
				Name:     "tcp-dns",
				Port:     53,
				Protocol: protocol.TCP,
			},
		},
	})
	s.Discovery.MemRegistry.AddInstance("overlapping.cluster.local", &model.ServiceInstance{
		Endpoint: &model.IstioEndpoint{
			Address:         "10.0.0.53",
			EndpointPort:    53,
			ServicePortName: "tcp-dns",
		},
		ServicePort: &model.Port{
			Name:     "tcp-dns",
			Port:     53,
			Protocol: protocol.TCP,
		},
	})
	fullPush(s)
}

// Verify the endpoint debug interface is installed and returns some string.
// TODO: parse response, check if data captured matches what we expect.
// TODO: use this in integration tests.
// TODO: refine the output
// TODO: dump the ServiceInstances as well
func testEdsz(t *testing.T, s *xds.FakeDiscoveryServer, proxyID string) {
	req, err := http.NewRequest("GET", "/debug/edsz?proxyID="+proxyID, nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	debug := http.HandlerFunc(s.Discovery.Edsz)
	debug.ServeHTTP(rr, req)

	data, err := ioutil.ReadAll(rr.Body)
	if err != nil {
		t.Fatalf("Failed to read /edsz")
	}
	statusStr := string(data)

	if !strings.Contains(statusStr, "\"outbound|8080||eds.test.svc.cluster.local\"") {
		t.Fatal("Mock eds service not found ", statusStr)
	}
}

func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

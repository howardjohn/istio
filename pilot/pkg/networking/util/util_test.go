// Copyright 2018 Istio Authors
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

package util

import (
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/ptypes/duration"
	mccpb "istio.io/istio/pilot/pkg/networking/plugin/mixer/client"

	"reflect"
	"testing"
	"time"

	hcm "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"

	tcp_proxy "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/tcp_proxy/v2"
	mpb "istio.io/istio/pilot/pkg/networking/plugin/mixer/client"

	v2 "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	listener "github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	xdsutil "github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/any"
	structpb "github.com/golang/protobuf/ptypes/struct"
	"github.com/golang/protobuf/ptypes/wrappers"
	proto2 "istio.io/istio/pkg/proto"

	"github.com/golang/protobuf/proto"
	"gopkg.in/d4l3k/messagediff.v1"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
)

func TestConvertAddressToCidr(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want *core.CidrRange
	}{
		{
			"return nil when the address is empty",
			"",
			nil,
		},
		{
			"success case with no PrefixLen",
			"1.2.3.4",
			&core.CidrRange{
				AddressPrefix: "1.2.3.4",
				PrefixLen: &wrappers.UInt32Value{
					Value: 32,
				},
			},
		},
		{
			"success case with PrefixLen",
			"1.2.3.4/16",
			&core.CidrRange{
				AddressPrefix: "1.2.3.4",
				PrefixLen: &wrappers.UInt32Value{
					Value: 16,
				},
			},
		},
		{
			"ipv6",
			"2001:db8::",
			&core.CidrRange{
				AddressPrefix: "2001:db8::",
				PrefixLen: &wrappers.UInt32Value{
					Value: 128,
				},
			},
		},
		{
			"ipv6 with prefix",
			"2001:db8::/64",
			&core.CidrRange{
				AddressPrefix: "2001:db8::",
				PrefixLen: &wrappers.UInt32Value{
					Value: 64,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ConvertAddressToCidr(tt.addr); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ConvertAddressToCidr() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetNetworkEndpointAddress(t *testing.T) {
	neUnix := &model.NetworkEndpoint{
		Family:  model.AddressFamilyUnix,
		Address: "/var/run/test/test.sock",
	}
	aUnix := GetNetworkEndpointAddress(neUnix)
	if aUnix.GetPipe() == nil {
		t.Fatalf("GetAddress() => want Pipe, got %s", aUnix.String())
	}
	if aUnix.GetPipe().GetPath() != neUnix.Address {
		t.Fatalf("GetAddress() => want path %s, got %s", neUnix.Address, aUnix.GetPipe().GetPath())
	}

	neIP := &model.NetworkEndpoint{
		Family:  model.AddressFamilyTCP,
		Address: "192.168.10.45",
		Port:    4558,
	}
	aIP := GetNetworkEndpointAddress(neIP)
	sock := aIP.GetSocketAddress()
	if sock == nil {
		t.Fatalf("GetAddress() => want SocketAddress, got %s", aIP.String())
	}
	if sock.GetAddress() != neIP.Address {
		t.Fatalf("GetAddress() => want %s, got %s", neIP.Address, sock.GetAddress())
	}
	if int(sock.GetPortValue()) != neIP.Port {
		t.Fatalf("GetAddress() => want port %d, got port %d", neIP.Port, sock.GetPortValue())
	}
}

func TestResolveHostsInNetworksConfig(t *testing.T) {
	tests := []struct {
		name     string
		address  string
		modified bool
	}{
		{
			"Gateway with IP address",
			"9.142.3.1",
			false,
		},
		{
			"Gateway with localhost address",
			"localhost",
			true,
		},
		{
			"Gateway with empty address",
			"",
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &meshconfig.MeshNetworks{
				Networks: map[string]*meshconfig.Network{
					"network": {
						Gateways: []*meshconfig.Network_IstioNetworkGateway{
							{
								Gw: &meshconfig.Network_IstioNetworkGateway_Address{
									Address: tt.address,
								},
							},
						},
					},
				},
			}
			ResolveHostsInNetworksConfig(config)
			addrAfter := config.Networks["network"].Gateways[0].GetAddress()
			if addrAfter == tt.address && tt.modified {
				t.Fatalf("Expected network address to be modified but it's the same as before calling the function")
			}
			if addrAfter != tt.address && !tt.modified {
				t.Fatalf("Expected network address not to be modified after calling the function")
			}
		})
	}
}

func TestConvertLocality(t *testing.T) {
	tests := []struct {
		name     string
		locality string
		want     *core.Locality
		reverse  string
	}{
		{
			name:     "nil locality",
			locality: "",
			want:     nil,
		},
		{
			name:     "locality with only region",
			locality: "region",
			want: &core.Locality{
				Region: "region",
			},
		},
		{
			name:     "locality with region and zone",
			locality: "region/zone",
			want: &core.Locality{
				Region: "region",
				Zone:   "zone",
			},
		},
		{
			name:     "locality with region zone and subzone",
			locality: "region/zone/subzone",
			want: &core.Locality{
				Region:  "region",
				Zone:    "zone",
				SubZone: "subzone",
			},
		},
		{
			name:     "locality with region zone subzone and rack",
			locality: "region/zone/subzone/rack",
			want: &core.Locality{
				Region:  "region",
				Zone:    "zone",
				SubZone: "subzone",
			},
			reverse: "region/zone/subzone",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertLocality(tt.locality)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Expected locality %#v, but got %#v", tt.want, got)
			}
			// Verify we can reverse the conversion back to the original input
			reverse := LocalityToString(got)
			if tt.reverse != "" {
				// Special case, reverse lookup is different than original input
				if tt.reverse != reverse {
					t.Errorf("Expected locality string %s, got %v", tt.reverse, reverse)
				}
			} else if tt.locality != reverse {
				t.Errorf("Expected locality string %s, got %v", tt.locality, reverse)
			}
		})
	}
}

func TestLocalityMatch(t *testing.T) {
	tests := []struct {
		name     string
		locality *core.Locality
		rule     string
		match    bool
	}{
		{
			name: "wildcard matching",
			locality: &core.Locality{
				Region:  "region1",
				Zone:    "zone1",
				SubZone: "subzone1",
			},
			rule:  "*",
			match: true,
		},
		{
			name: "wildcard matching",
			locality: &core.Locality{
				Region:  "region1",
				Zone:    "zone1",
				SubZone: "subzone1",
			},
			rule:  "region1/*",
			match: true,
		},
		{
			name: "wildcard matching",
			locality: &core.Locality{
				Region:  "region1",
				Zone:    "zone1",
				SubZone: "subzone1",
			},
			rule:  "region1/zone1/*",
			match: true,
		},
		{
			name: "wildcard not matching",
			locality: &core.Locality{
				Region:  "region1",
				Zone:    "zone1",
				SubZone: "subzone1",
			},
			rule:  "region1/zone2/*",
			match: false,
		},
		{
			name: "region matching",
			locality: &core.Locality{
				Region:  "region1",
				Zone:    "zone1",
				SubZone: "subzone1",
			},
			rule:  "region1",
			match: true,
		},
		{
			name: "region and zone matching",
			locality: &core.Locality{
				Region:  "region1",
				Zone:    "zone1",
				SubZone: "subzone1",
			},
			rule:  "region1/zone1",
			match: true,
		},
		{
			name: "zubzone wildcard matching",
			locality: &core.Locality{
				Region: "region1",
				Zone:   "zone1",
			},
			rule:  "region1/zone1",
			match: true,
		},
		{
			name: "subzone mismatching",
			locality: &core.Locality{
				Region: "region1",
				Zone:   "zone1",
			},
			rule:  "region1/zone1/subzone2",
			match: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match := LocalityMatch(tt.locality, tt.rule)
			if match != tt.match {
				t.Errorf("Expected matching result %v, but got %v", tt.match, match)
			}
		})
	}
}

func TestIsLocalityEmpty(t *testing.T) {
	tests := []struct {
		name     string
		locality *core.Locality
		want     bool
	}{
		{
			"non empty locality",
			&core.Locality{
				Region: "region",
			},
			false,
		},
		{
			"empty locality",
			&core.Locality{
				Region: "",
			},
			true,
		},
		{
			"nil locality",
			nil,
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsLocalityEmpty(tt.locality)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Expected locality empty result %#v, but got %#v", tt.want, got)
			}
		})
	}
}

func TestBuildConfigInfoMetadata(t *testing.T) {
	cases := []struct {
		name string
		in   model.ConfigMeta
		want *core.Metadata
	}{
		{
			"destination-rule",
			model.ConfigMeta{
				Group:     "networking.istio.io",
				Version:   "v1alpha3",
				Name:      "svcA",
				Namespace: "default",
				Domain:    "svc.cluster.local",
				Type:      "destination-rule",
			},
			&core.Metadata{
				FilterMetadata: map[string]*structpb.Struct{
					IstioMetadataKey: {
						Fields: map[string]*structpb.Value{
							"config": {
								Kind: &structpb.Value_StringValue{
									StringValue: "/apis/networking.istio.io/v1alpha3/namespaces/default/destination-rule/svcA",
								},
							},
						},
					},
				},
			},
		},
	}

	for _, v := range cases {
		t.Run(v.name, func(tt *testing.T) {
			got := BuildConfigInfoMetadata(v.in)
			if diff, equal := messagediff.PrettyDiff(got, v.want); !equal {
				tt.Errorf("BuildConfigInfoMetadata(%v) produced incorrect result:\ngot: %v\nwant: %v\nDiff: %s", v.in, got, v.want, diff)
			}
		})
	}
}

func TestCloneCluster(t *testing.T) {
	cluster := buildFakeCluster()
	clone := CloneCluster(cluster)
	cluster.LoadAssignment.Endpoints[0].LoadBalancingWeight.Value = 10
	cluster.LoadAssignment.Endpoints[0].Priority = 8
	cluster.LoadAssignment.Endpoints[0].LbEndpoints = nil

	if clone.LoadAssignment.Endpoints[0].LoadBalancingWeight.GetValue() == 10 {
		t.Errorf("LoadBalancingWeight mutated")
	}
	if clone.LoadAssignment.Endpoints[0].Priority == 8 {
		t.Errorf("Priority mutated")
	}
	if clone.LoadAssignment.Endpoints[0].LbEndpoints == nil {
		t.Errorf("LbEndpoints mutated")
	}
}

func buildFakeCluster() *v2.Cluster {
	return &v2.Cluster{
		Name: "outbound|8080||test.example.org",
		LoadAssignment: &v2.ClusterLoadAssignment{
			ClusterName: "outbound|8080||test.example.org",
			Endpoints: []*endpoint.LocalityLbEndpoints{
				{
					Locality: &core.Locality{
						Region:  "region1",
						Zone:    "zone1",
						SubZone: "subzone1",
					},
					LbEndpoints: []*endpoint.LbEndpoint{},
					LoadBalancingWeight: &wrappers.UInt32Value{
						Value: 1,
					},
					Priority: 0,
				},
				{
					Locality: &core.Locality{
						Region:  "region1",
						Zone:    "zone1",
						SubZone: "subzone2",
					},
					LbEndpoints: []*endpoint.LbEndpoint{},
					LoadBalancingWeight: &wrappers.UInt32Value{
						Value: 1,
					},
					Priority: 0,
				},
			},
		},
	}
}

func TestIsHTTPFilterChain(t *testing.T) {
	httpFilterChain := &listener.FilterChain{
		Filters: []*listener.Filter{
			{
				Name: xdsutil.HTTPConnectionManager,
			},
		},
	}

	tcpFilterChain := &listener.FilterChain{
		Filters: []*listener.Filter{
			{
				Name: xdsutil.TCPProxy,
			},
		},
	}

	if !IsHTTPFilterChain(httpFilterChain) {
		t.Errorf("http Filter chain not detected properly")
	}

	if IsHTTPFilterChain(tcpFilterChain) {
		t.Errorf("tcp filter chain detected as http filter chain")
	}
}

var (
	listener80 = &v2.Listener{Address: BuildAddress("0.0.0.0", 80)}
	listener81 = &v2.Listener{Address: BuildAddress("0.0.0.0", 81)}
	listenerip = &v2.Listener{Address: BuildAddress("1.1.1.1", 80)}
)

func BenchmarkGetByAddress(b *testing.B) {
	for n := 0; n < b.N; n++ {
		GetByAddress([]*v2.Listener{
			listener80,
			listener81,
			listenerip,
		}, *listenerip.Address)
	}
}

func TestGetByAddress(t *testing.T) {
	tests := []struct {
		name      string
		listeners []*v2.Listener
		address   *core.Address
		expected  *v2.Listener
	}{
		{
			"no listeners",
			[]*v2.Listener{},
			BuildAddress("0.0.0.0", 80),
			nil,
		},
		{
			"single listener",
			[]*v2.Listener{
				listener80,
			},
			BuildAddress("0.0.0.0", 80),
			listener80,
		},
		{
			"multiple listeners",
			[]*v2.Listener{
				listener81,
				listenerip,
				listener80,
			},
			BuildAddress("0.0.0.0", 80),
			listener80,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetByAddress(tt.listeners, *tt.address)
			if got != tt.expected {
				t.Errorf("Got %v, expected %v", got, tt.expected)
			}
		})
	}
}

func TestMergeAnyWithStruct(t *testing.T) {
	inHCM := &http_conn.HttpConnectionManager{
		CodecType:  http_conn.HttpConnectionManager_HTTP1,
		StatPrefix: "123",
		HttpFilters: []*http_conn.HttpFilter{
			{
				Name: "filter1",
				ConfigType: &http_conn.HttpFilter_TypedConfig{
					TypedConfig: &any.Any{},
				},
			},
		},
		ServerName:        "scooby",
		XffNumTrustedHops: 2,
	}
	inAny := MessageToAny(inHCM)

	// listener.go sets this to 0
	newTimeout := ptypes.DurationProto(5 * time.Minute)
	userHCM := &http_conn.HttpConnectionManager{
		AddUserAgent:      proto2.BoolTrue,
		IdleTimeout:       newTimeout,
		StreamIdleTimeout: newTimeout,
		UseRemoteAddress:  proto2.BoolTrue,
		XffNumTrustedHops: 5,
		ServerName:        "foobar",
		HttpFilters: []*http_conn.HttpFilter{
			{
				Name: "some filter",
			},
		},
	}

	expectedHCM := proto.Clone(inHCM).(*http_conn.HttpConnectionManager)
	expectedHCM.AddUserAgent = userHCM.AddUserAgent
	expectedHCM.IdleTimeout = userHCM.IdleTimeout
	expectedHCM.StreamIdleTimeout = userHCM.StreamIdleTimeout
	expectedHCM.UseRemoteAddress = userHCM.UseRemoteAddress
	expectedHCM.XffNumTrustedHops = userHCM.XffNumTrustedHops
	expectedHCM.HttpFilters = append(expectedHCM.HttpFilters, userHCM.HttpFilters...)
	expectedHCM.ServerName = userHCM.ServerName

	pbStruct := MessageToStruct(userHCM)

	outAny, err := MergeAnyWithStruct(inAny, pbStruct)
	if err != nil {
		t.Errorf("Failed to merge: %v", err)
	}

	outHCM := http_conn.HttpConnectionManager{}
	if err = ptypes.UnmarshalAny(outAny, &outHCM); err != nil {
		t.Errorf("Failed to unmarshall outAny to outHCM: %v", err)
	}

	if !reflect.DeepEqual(expectedHCM, &outHCM) {
		t.Errorf("Merged HCM does not match the expected output")
	}
}

func TestHandleCrash(t *testing.T) {
	defer func() {
		if x := recover(); x != nil {
			t.Errorf("Expected no panic ")
		}
	}()

	defer HandleCrash()
	panic("test")
}

func TestCustomHandleCrash(t *testing.T) {
	ch := make(chan struct{}, 1)
	defer func() {
		select {
		case <-ch:
			t.Logf("crash handler called")
		case <-time.After(1 * time.Second):
			t.Errorf("Custom handler not called")
		}
	}()

	defer HandleCrash(func() {
		ch <- struct{}{}
	})

	panic("test")
}

func buildSmallCluster() *v2.Cluster {
	return &v2.Cluster{
		Name: "outbound|8080||test.example.org",
		LoadAssignment: &v2.ClusterLoadAssignment{
			ClusterName: "outbound|8080||test.example.org",
			Endpoints: []*endpoint.LocalityLbEndpoints{
				{
					Locality: &core.Locality{
						Region:  "region1",
						Zone:    "zone1",
						SubZone: "subzone2",
					},
				},
				{
					Locality: &core.Locality{
						Region:  "region2",
						Zone:    "zone1",
						SubZone: "subzone2",
					},
				},
				{
					Locality: &core.Locality{
						Region:  "region2",
						Zone:    "zone1",
						SubZone: "subzone2",
					},
				},
			},
		},
	}
}

func TestMessageToAny(t *testing.T) {
	la := &v2.ClusterLoadAssignment{
		ClusterName: "outbound|8080||test.example.org",
		Endpoints: []*endpoint.LocalityLbEndpoints{
			{
				Locality: &core.Locality{
					Region:  "region1",
					Zone:    "zone1",
					SubZone: "subzone1",
				},
			},
		},
	}
	la2 := *la
	la2.Endpoints = append(la2.Endpoints, &endpoint.LocalityLbEndpoints{
		Locality: &core.Locality{
			Region:  "region3",
			Zone:    "",
			SubZone: "",
		},
	})
	tcp := &mccpb.TcpClientConfig{
		Transport: &mccpb.TransportConfig{
			CheckCluster:          "foobar",
			ReportCluster:         "asdf",
			NetworkFailPolicy:     &mccpb.NetworkFailPolicy{Policy: mccpb.NetworkFailPolicy_FAIL_CLOSE},
			ReportBatchMaxEntries: 123,
			ReportBatchMaxTime:    &duration.Duration{Seconds: 5},
		},
		MixerAttributes: &mpb.Attributes{
			Attributes: map[string]*mpb.Attributes_AttributeValue{
				"a":           {Value: &mpb.Attributes_AttributeValue_StringValue{StringValue: "b"}},
				"target.user": {Value: &mpb.Attributes_AttributeValue_StringValue{StringValue: "target-user"}},
				"target.name": {Value: &mpb.Attributes_AttributeValue_StringValue{StringValue: "target-name"}},
			},
		},
	}
	tcpFilter := &listener.Filter{
		Name: "tcp-proxy",
	}
	tcpProxy := &tcp_proxy.TcpProxy{
		StatPrefix:       PassthroughCluster,
		ClusterSpecifier: &tcp_proxy.TcpProxy_Cluster{Cluster: PassthroughCluster},
	}
	tcpFilter.ConfigType = &listener.Filter_TypedConfig{TypedConfig: MessageToAny(tcpProxy)}
	l := &v2.Listener{
		Name: "foobar",
		FilterChains: []*listener.FilterChain{{
			Filters: []*listener.Filter{tcpFilter},
		}},
	}
	for i := 0; i < 1000; i++ {
		_ = MessageToAny(tcp)
		_ = MessageToAny(tcpProxy)
		_ = MessageToAny(tcpFilter)
		_ = MessageToAny(l)
		_ = MessageToAny(la)
		_ = MessageToAny(&la2)
	}
	_ = tcpProxy
	base := MessageToAny(la)
	_ = MessageToAny(buildSmallCluster())
	_ = MessageToAny(buildFakeCluster())
	base2 := MessageToAny(&la2)
	for i := 0; i < 10; i++ {
		_ = MessageToAny(tcpProxy)
		_ = MessageToAny(tcp)
		n := MessageToAny(la)
		n2 := MessageToAny(&la2)
		if !reflect.DeepEqual(base, n) {
			t.Fatalf("failed")
		}
		if !reflect.DeepEqual(base2, n2) {
			t.Fatalf("failed")
		}
	}

	pre := []byte{10, 12, 48, 46, 48, 46, 48, 46, 48, 95, 56, 48, 54, 48, 18, 14, 10, 12, 18, 7, 48, 46, 48, 46, 48, 46, 48, 24, 252, 62, 26, 186, 1, 10, 53, 26, 17, 10, 11, 49, 48, 46, 53, 50, 46, 57, 46, 49, 50, 51, 18, 2, 8, 32, 26, 32, 10, 25, 102, 101, 56, 48, 58, 58, 98, 99, 51, 100, 58, 100, 101, 102, 102, 58, 102, 101, 52, 102, 58, 57, 98, 52, 100, 18, 3, 8, 128, 1, 26, 128, 1, 10, 15, 101, 110, 118, 111, 121, 46, 116, 99, 112, 95, 112, 114, 111, 120, 121, 34, 109, 10, 69, 116, 121, 112, 101, 46, 103, 111, 111, 103, 108, 101, 97, 112, 105, 115, 46, 99, 111, 109, 47, 101, 110, 118, 111, 121, 46, 99, 111, 110, 102, 105, 103, 46, 102, 105, 108, 116, 101, 114, 46, 110, 101, 116, 119, 111, 114, 107, 46, 116, 99, 112, 95, 112, 114, 111, 120, 121, 46, 118, 50, 46, 84, 99, 112, 80, 114, 111, 120, 121, 18, 36, 10, 16, 66, 108, 97, 99, 107, 72, 111, 108, 101, 67, 108, 117, 115, 116, 101, 114, 18, 16, 66, 108, 97, 99, 107, 72, 111, 108, 101, 67, 108, 117, 115, 116, 101, 114, 26, 157, 2, 26, 154, 2, 10, 29, 101, 110, 118, 111, 121, 46, 104, 116, 116, 112, 95, 99, 111, 110, 110, 101, 99, 116, 105, 111, 110, 95, 109, 97, 110, 97, 103, 101, 114, 34, 248, 1, 10, 96, 116, 121, 112, 101, 46, 103, 111, 111, 103, 108, 101, 97, 112, 105, 115, 46, 99, 111, 109, 47, 101, 110, 118, 111, 121, 46, 99, 111, 110, 102, 105, 103, 46, 102, 105, 108, 116, 101, 114, 46, 110, 101, 116, 119, 111, 114, 107, 46, 104, 116, 116, 112, 95, 99, 111, 110, 110, 101, 99, 116, 105, 111, 110, 95, 109, 97, 110, 97, 103, 101, 114, 46, 118, 50, 46, 72, 116, 116, 112, 67, 111, 110, 110, 101, 99, 116, 105, 111, 110, 77, 97, 110, 97, 103, 101, 114, 18, 147, 1, 10, 29, 73, 110, 98, 111, 117, 110, 100, 80, 97, 115, 115, 116, 104, 114, 111, 117, 103, 104, 67, 108, 117, 115, 116, 101, 114, 73, 112, 118, 54, 18, 29, 73, 110, 98, 111, 117, 110, 100, 80, 97, 115, 115, 116, 104, 114, 111, 117, 103, 104, 67, 108, 117, 115, 116, 101, 114, 73, 112, 118, 54, 53, 53, 51, 53, 46, 54, 53, 53, 51, 53, 10, 35, 10, 21, 99, 111, 110, 116, 101, 120, 116, 46, 114, 101, 112, 111, 114, 116, 101, 114, 46, 107, 105, 110, 100, 18, 10, 18, 8, 111, 117, 116, 98, 111, 117, 110, 100, 10, 81, 10, 20, 99, 111, 110, 116, 101, 120, 116, 46, 114, 101, 112, 111, 114, 116, 101, 114, 46, 117, 105, 100, 18, 57, 18, 55, 107, 117, 98, 101, 114, 110, 101, 116, 101, 115, 58, 2, 10, 0, 122, 5, 16, 128, 194, 215, 47, 128, 1, 2, 136, 1, 1, 105, 110, 100, 18, 10, 18, 8, 111, 117, 116, 98, 111, 117, 110, 100, 10, 81, 10, 20, 99, 111, 110, 116, 101, 120, 116, 46, 114, 101, 112, 111, 114, 116, 101, 114, 46, 117, 105, 100, 18, 57, 18, 55, 107, 117, 98, 101, 114, 110, 101, 116, 101, 115, 58, 47, 47, 115, 118, 99, 48, 52, 45, 48, 45, 51, 45, 54, 52, 99, 102, 54, 56, 54, 52, 52, 55, 45, 53, 108, 100, 110, 53, 46, 115, 101, 114, 118, 105, 99, 101, 45, 103, 114, 97, 112, 104, 48, 52, 10, 48, 10, 24, 100, 101, 115, 116, 58, 2, 10, 0, 74, 30, 10, 28, 101, 110, 118, 111, 121, 46, 108, 105, 115, 116, 101, 110, 101, 114, 46, 116, 108, 115, 95, 105, 110, 115, 112, 101, 99, 116, 111, 114, 74, 31, 10, 29, 101, 110, 118, 111, 121, 46, 108, 105, 115, 116, 101, 110, 101, 114, 46, 104, 116, 116, 112, 95, 105, 110, 115, 112, 101, 99, 116, 111, 114, 122, 5, 16, 128, 194, 215, 47, 128, 1, 2, 136, 1, 1, 5, 16, 128, 194, 215, 47, 128, 1, 2, 136, 1, 1, 118, 105, 99, 101, 45, 103, 114, 97, 112, 104, 48, 52, 46, 115, 118, 99, 46, 99, 108, 117, 115, 116, 101, 114, 46, 108, 111, 99, 97, 108, 18, 167, 1, 10, 17, 105, 110, 98, 111, 117, 110, 100, 124, 104, 116, 116, 112, 124, 56, 48, 56, 48, 18, 1, 42, 26, 142, 1, 10, 3, 10, 1, 47, 42, 52, 10, 50, 115, 118, 99, 48, 52, 45, 48, 45, 51, 46, 115, 101, 114, 118, 105, 99, 101, 45, 103, 114, 97, 112, 104, 48, 52, 46, 115, 118, 99, 46, 99, 108, 117, 115, 116, 101, 114, 46, 108, 111, 99, 97, 108, 58, 56, 48, 56, 48, 47, 42, 114, 7, 100, 101, 102, 97, 117, 108, 116, 18, 72, 66, 0, 186, 1, 0, 10, 65, 105, 110, 98, 111, 117, 110, 100, 124, 56, 48, 56, 48, 124, 104, 116, 116, 112, 45, 119, 101, 98, 124, 115, 118, 99, 48, 52, 45, 48, 45, 51, 46, 115, 101, 114, 118, 105, 99, 101, 45, 103, 114, 97, 112, 104, 48, 52, 46, 115, 118, 99, 46, 99, 108, 117, 115, 116, 101, 114, 46, 108, 111, 99, 97, 108, 58, 0, 26, 133, 6, 10, 0, 26, 128, 6, 10, 29, 101, 110, 118, 111, 121, 46, 104, 116, 116, 112, 95, 99, 111, 110, 110, 101, 99, 116, 105, 111, 110, 95, 109, 97, 110, 97, 103, 101, 114, 34, 222, 5, 10, 96, 116, 121, 112, 101, 46, 103, 111, 111, 103, 108, 101, 97, 112, 105, 115, 46, 99, 111, 109, 47, 101, 110, 118, 111, 121, 46, 99, 111, 110, 102, 105, 103, 46, 102, 105, 108, 116, 101, 114, 46, 110, 101, 116, 119, 111, 114, 107, 46, 104, 116, 116, 112, 95, 99, 111, 110, 110, 101, 99, 116, 105, 111, 110, 95, 109, 97, 110, 97, 103, 101, 114, 46, 118, 50, 46, 72, 116, 116, 112, 67, 111, 110, 110, 101, 99, 116, 105, 111, 110, 77, 97, 110, 97, 103, 101, 114, 18, 249, 4, 10, 29, 73, 110, 98, 111, 117, 110, 100, 80, 97, 115, 115, 116, 104, 114, 111, 117, 103, 104, 67, 108, 117, 115, 116, 101, 114, 73, 112, 118, 54, 18, 29, 73, 110, 98, 111, 117, 110, 100, 80, 97, 115, 115, 116, 104, 114, 111, 117, 103, 104, 67, 108, 117, 115, 116, 101, 114, 73, 112, 118, 54, 53, 53, 51, 53, 46, 54, 53, 53, 51, 53, 10, 35, 10, 21, 99, 111, 110, 116, 101, 120, 116, 46, 114, 101, 112, 111, 114, 116, 101, 114, 46, 107, 105, 110, 100, 18, 10, 18, 8, 111, 117, 116, 98, 111, 117, 110, 100, 10, 81, 10, 20, 99, 111, 110, 116, 101, 120, 116, 46, 114, 101, 112, 111, 114, 116, 101, 114, 46, 117, 105, 100, 18, 57, 18, 55, 107, 117, 98, 101, 114, 110, 101, 116, 101, 115, 58, 47, 47, 115, 118, 99, 48, 52, 45, 48, 45, 51, 45, 54, 52, 99, 102, 54, 56, 54, 52, 52, 55, 45, 53, 108, 100, 110, 53, 46, 115, 101, 114, 118, 105, 99, 101, 45, 103, 114, 97, 112, 104, 48, 52, 10, 48, 10, 24, 100, 101, 115, 116, 105, 110, 97, 116, 105, 111, 110, 46, 115, 101, 114, 118, 105, 99, 101, 46, 104, 111, 115, 116, 18, 20, 18, 18, 80, 97, 115, 115, 116, 104, 114, 111, 117, 103, 104, 67, 108, 117, 115, 116, 101, 114, 10, 37, 10, 16, 115, 111, 117, 114, 99, 101, 46, 110, 97, 109, 101, 115, 112, 97, 99, 101, 18, 17, 18, 15, 115, 101, 114, 118, 105, 99, 101, 45, 103, 114, 97, 112, 104, 48, 52, 10, 71, 10, 10, 115, 111, 117, 114, 99, 101, 46, 117, 105, 100, 18, 57, 18, 55, 107, 117, 98, 101, 114, 110, 101, 116, 101, 115, 58, 47, 47, 115, 118, 99, 48, 52, 45, 48, 45, 51, 45, 54, 52, 99, 102, 54, 56, 54, 52, 52, 55, 45, 53, 108, 100, 110, 53, 46, 115, 101, 114, 118, 105, 99, 101, 45, 103, 114, 97, 112, 104, 48, 52, 24, 1, 1, 2, 138, 1, 8, 10, 2, 8, 1, 32, 1, 40, 1, 186, 1, 11, 10, 9, 119, 101, 98, 115, 111, 99, 107, 101, 116, 194, 1, 0, 242, 1, 2, 8, 1, 34, 239, 1, 10, 65, 105, 110, 98, 111, 117, 110, 100, 124, 56, 48, 56, 48, 124, 104, 116, 116, 112, 45, 119, 101, 98, 124, 115, 118, 99, 48, 52, 45, 48, 45, 51, 46, 115, 101, 114, 118, 105, 99, 101, 45, 103, 114, 97, 112, 104, 48, 52, 46, 115, 118, 99, 46, 99, 108, 117, 115, 116, 101, 114, 46, 108, 111, 99, 97, 108, 18, 167, 1, 10, 17, 105, 110, 98, 111, 117, 110, 100, 124, 104, 116, 116, 112, 124, 56, 48, 56, 48, 18, 1, 42, 26, 142, 1, 10, 3, 10, 1, 47, 42, 52, 10, 50, 115, 118, 99, 48, 52, 45, 48, 45, 51, 46, 115, 101, 114, 118, 105, 99, 101, 45, 103, 114, 97, 112, 104, 48, 52, 46, 115, 118, 99, 46, 99, 108, 117, 115, 116, 101, 114, 46, 108, 111, 99, 97, 108, 58, 56, 48, 56, 48, 47, 42, 114, 7, 100, 101, 102, 97, 117, 108, 116, 18, 72, 66, 0, 186, 1, 0, 10, 65, 105, 110, 98, 111, 117, 110, 100, 124, 56, 48, 56, 48, 124, 104, 116, 116, 112, 45, 119, 101, 98, 124, 115, 118, 99, 48, 52, 45, 48, 45, 51, 46, 115, 101, 114, 118, 105, 99, 101, 45, 103, 114, 97, 112, 104, 48, 52, 46, 115, 118, 99, 46, 99, 108, 117, 115, 116, 101, 114, 46, 108, 111, 99, 97, 108, 58, 0, 58, 2, 10, 0, 74, 30, 10, 28, 101, 110, 118, 111, 121, 46, 108, 105, 115, 116, 101, 110, 101, 114, 46, 116, 108, 115, 95, 105, 110, 115, 112, 101, 99, 116, 111, 114, 122, 5, 16, 128, 194, 215, 47, 128, 1, 1, 136, 1, 1}
	buf := proto.NewBuffer(pre)
	buf.Reset()

	hcm := &hcm.HttpConnectionManager{
		CodecType:  hcm.HttpConnectionManager_AUTO,
		StatPrefix: "foobar",
		RouteSpecifier: &hcm.HttpConnectionManager_Rds{
			Rds: &hcm.Rds{RouteConfigName: "foo", ConfigSource: &core.ConfigSource{
				ConfigSourceSpecifier: &core.ConfigSource_Ads{Ads: &core.AggregatedConfigSource{}},
			}},
		},
		HttpFilters: []*hcm.HttpFilter{{Name: "envoy.route"}},
	}
	msg := &v2.Listener{Name: "0.0.0.0_15004", Address: nil, FilterChains: []*listener.FilterChain{{
		Filters: []*listener.Filter{tcpFilter},
	}, {Filters: []*listener.Filter{{
		Name:       "http-con-manager",
		ConfigType: &listener.Filter_TypedConfig{TypedConfig: MessageToAny(hcm)},
	}}}}, ListenerFiltersTimeout: &duration.Duration{Seconds: 5}, ContinueOnListenerFiltersTimeout: true, Transparent: (*wrappers.BoolValue)(nil), Freebind: (*wrappers.BoolValue)(nil), TrafficDirection: 2}
	err := buf.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	resp := &any.Any{
		TypeUrl: "type.googleapis.com/" + proto.MessageName(msg),
		Value:   buf.Bytes(),
	}
	s1, e2 := (&jsonpb.Marshaler{}).MarshalToString(resp)
	t.Log(s1)
	if e2 != nil {
		t.Fatal(e2)
	}
}

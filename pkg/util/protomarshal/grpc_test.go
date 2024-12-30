package protomarshal_test

import (
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/grpc/encoding"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/util/protoconv"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/workloadapi"
	"testing"
)

func TestX(t *testing.T) {

	dr := &discovery.DeltaDiscoveryResponse{
		TypeUrl: v3.AddressType,
		// TODO: send different version for incremental eds
		SystemVersionInfo: "xxx",
		Resources: []*discovery.Resource{
			{Name: "aaaaaaaaa", Resource: &anypb.Any{
				TypeUrl: "bbbbbbbbbbb",
				Value:   []byte{1, 2, 3, 4, 5, 6, 7, 8, 9},
			},
			},
			{Name: "aaaaaaaaa", Resource: &anypb.Any{
				TypeUrl: "bbbbbbbbbbb",
				Value:   []byte{1, 2, 3, 4, 5, 6, 7, 8, 9},
			},
			},
		},
	}
	bb := protoconv.MessageToAny(dr)
	bs, _ := proto.Marshal(bb)
	t.Log(bs)
	protomarshal.EncodeDiscoveryResponse(dr)
	return
}

func BenchmarkEncoding(b *testing.B) {
	wl := &workloadapi.Workload{
		Uid:               "x/networking.istio.io/WorkloadEntry/ns/name1",
		Name:              "name1",
		Namespace:         "1234",
		Node:              "",
		CanonicalName:     "a",
		CanonicalRevision: "latest",
		ServiceAccount:    "sa1",
		WorkloadType:      workloadapi.WorkloadType_POD,
		WorkloadName:      "name1",
		Services: map[string]*workloadapi.PortList{
			"ns1/se.istio.io": {
				Ports: []*workloadapi.Port{
					{
						ServicePort: 80,
						TargetPort:  8080,
					},
				},
			},
		},
	}
	marshalled := protoconv.MessageToAny(wl)
	res := discovery.Resource{
		Name:     wl.Uid,
		Resource: marshalled,
	}
	ress := []*discovery.Resource{}
	for range 100 {
		ress = append(ress, &res)
	}
	dr := &discovery.DeltaDiscoveryResponse{
		TypeUrl: v3.AddressType,
		// TODO: send different version for incremental eds
		SystemVersionInfo: "xxx",
		Resources:         ress,
	}
	c := encoding.GetCodecV2("proto")
	b.Run("vt", func(b *testing.B) {
		for range b.N {
			bs, err := c.Marshal(dr)
			if err != nil {
				b.Fatal(err)
			}
			bs.Free()
		}
	})
	b.Run("legacy", func(b *testing.B) {
		test.SetForTest(b, &features.EnableVtprotobuf, false)
		for range b.N {
			bs, err := c.Marshal(dr)
			if err != nil {
				b.Fatal(err)
			}
			bs.Free()
		}
	})
	b.Run("optimized", func(b *testing.B) {
		test.SetForTest(b, &features.EnableVtprotobuf, false)
		for range b.N {
			_, err := protomarshal.EncodeDiscoveryResponse(dr)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

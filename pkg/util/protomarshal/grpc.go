package protomarshal

import (
	vtanypb "github.com/planetscale/vtprotobuf/types/known/anypb"
	discoveryv3 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/mem"
	"istio.io/istio/pilot/pkg/features"

	// Guarantee that the built-in proto is called registered before this one
	// so that it can be replaced.
	_ "google.golang.org/grpc/encoding/proto" // nolint:revive
)

// Derivied from https://raw.githubusercontent.com/vitessio/vitess/refs/heads/main/go/vt/servenv/grpc_codec.go which
// has the following copyright:
/*
Copyright 2021 The Vitess Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Name is the name registered for the proto compressor.
const Name = "proto"

type vtprotoMessage interface {
	MarshalToSizedBufferVTStrict(data []byte) (int, error)
	SizeVT() int
}

type Codec struct {
	fallback encoding.CodecV2
}

func (Codec) Name() string { return Name }

var defaultBufferPool = mem.DefaultBufferPool()

func (c *Codec) Marshal(v any) (mem.BufferSlice, error) {

	if features.EnableVtprotobuf {
		if dr, ok := v.(*discoveryv3.DeltaDiscoveryResponse); ok {
			return EncodeDiscoveryResponse(dr)
		}
		if m, ok := v.(vtprotoMessage); ok {
			size := m.SizeVT()
			if mem.IsBelowBufferPoolingThreshold(size) {
				buf := make([]byte, size)
				if _, err := m.MarshalToSizedBufferVTStrict(buf[:size]); err != nil {
					return nil, err
				}
				return mem.BufferSlice{mem.SliceBuffer(buf)}, nil
			}
			buf := defaultBufferPool.Get(size)
			if _, err := m.MarshalToSizedBufferVTStrict((*buf)[:size]); err != nil {
				defaultBufferPool.Put(buf)
				return nil, err
			}
			return mem.BufferSlice{mem.NewBuffer(buf, defaultBufferPool)}, nil
		}
	}

	return c.fallback.Marshal(v)
}

func (c *Codec) Unmarshal(data mem.BufferSlice, v any) error {
	// We don't currently implement Unmarshal for any types.
	// The only useful one would be DiscoveryRequest.
	return c.fallback.Unmarshal(data, v)
}

func init() {
	if features.EnableVtprotobuf {
		encoding.RegisterCodecV2(&Codec{
			fallback: encoding.GetCodecV2("proto"),
		})
	}
}

func EncodeDiscoveryResponse(t *discoveryv3.DeltaDiscoveryResponse) (mem.BufferSlice, error) {
	res := t.Resources
	t.Resources = nil
	bs := make(mem.BufferSlice, 0, 2*len(res)+1)
	buf, err := t.MarshalVTStrict()
	if err != nil {
		return nil, err
	}
	bs = append(bs, mem.NewBuffer(&buf, nil))
	for iNdEx := len(res) - 1; iNdEx >= 0; iNdEx-- {
		r := res[iNdEx]
		// TODO: split the name from the value as well
		buf1, buf2, err := encodeResource(r)
		if err != nil {
			return nil, err
		}
		bs = append(bs, mem.NewBuffer(&buf1, nil), mem.NewBuffer(&buf2, nil))
	}
	return bs, nil
}

func encodeResource(r *discoveryv3.Resource) ([]byte, []byte, error) {
	hdr := make([]byte, 0, len(r.Name) + len(r.Resource.TypeUrl) + 2 + 20)
	// Tag for resource in DeltaDiscoveryResponse
	hdr = append(hdr, 0x12)
	hdr = appendVarint(hdr, uint64(r.SizeVT()))

	// Name in DeltaDiscoveryResponse
	hdr = append(hdr, 0x1a)
	hdr = appendVarint(hdr, uint64(len(r.Name)))
	hdr = append(hdr, r.Name...)

	// Now the Resource...
	a := (*vtanypb.Any)(r.Resource)
	hdr = append(hdr, 0x12)
	hdr = appendVarint(hdr, uint64(a.SizeVT()))

	hdr = append(hdr, 0xa)
	hdr = appendVarint(hdr, uint64(len(a.TypeUrl)))
	hdr = append(hdr, a.TypeUrl...)

	hdr = append(hdr, 0x12)
	hdr = appendVarint(hdr, uint64(len(a.Value)))
	return hdr, a.Value, nil
}

func encodeVarint(tag byte, x uint64) []byte {
	var buf [11]byte
	buf[0] = tag
	var n int
	for n = 0; x > 127; n++ {
		buf[n+1] = 0x80 | uint8(x&0x7F)
		x >>= 7
	}
	buf[n+1] = uint8(x)
	n++
	return buf[0 : n+1]
}

func appendVarint(b []byte, x uint64) []byte {
	for x >= 128 {
		b = append(b, byte(x)|0x80)
		x >>= 7
	}
	b = append(b, byte(x))
	return b
}

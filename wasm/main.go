// Copyright 2020-2021 Tetrate
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"github.com/tetratelabs/proxy-wasm-go-sdk/proxywasm"
	"github.com/tetratelabs/proxy-wasm-go-sdk/proxywasm/types"
)

func main() {
	proxywasm.SetVMContext(&vmContext{})
}

type vmContext struct {
	// Embed the default VM context here,
	// so that we don't need to reimplement all the methods.
	types.DefaultVMContext
}

// Override types.DefaultVMContext.
func (*vmContext) NewPluginContext(contextID uint32) types.PluginContext {
	return &pluginContext{}
}

type pluginContext struct {
	// Embed the default plugin context here,
	// so that we don't need to reimplement all the methods.
	types.DefaultPluginContext
}

// Override types.DefaultPluginContext.
func (ctx *pluginContext) NewTcpContext(contextID uint32) types.TcpContext {
	return &networkContext{}
}

type networkContext struct {
	// Embed the default tcp context here,
	// so that we don't need to reimplement all the methods.
	types.DefaultTcpContext
}

// Override types.DefaultTcpContext.
func (ctx *networkContext) OnNewConnection() types.Action {
	proxywasm.LogError("howardjohn: new connection!")
	proxywasm.SetProperty([]string{"howardjohn_meta"}, []byte("fake"))

	headers := [][2]string{
		{":authority", "localhost"},
		{":method", "GET"},
		{":path", "/dummy.json"},
	}
	if _, err := proxywasm.DispatchHttpCall("mds", headers, nil, nil,
		50000, httpCallResponseCallback); err != nil {
		proxywasm.LogCriticalf("dipatch httpcall failed: %v", err)
		return types.ActionContinue
	}
	return types.ActionPause
}

// ConnectionMetadata returns the metadata about a connection
type ConnectionMetadata struct {
	// Identity provides the identity of the peer.
	// Example: spiffe://cluster.local/ns/a/sa/b.
	Identity string `json:"identity"`
}

func httpCallResponseCallback(numHeaders, bodySize, numTrailers int) {
	defer proxywasm.ContinueTcpStream()
	hs, err := proxywasm.GetHttpCallResponseHeaders()
	if err != nil {
		proxywasm.LogCriticalf("failed to get response body: %v", err)
		return
	}

	for _, h := range hs {
		proxywasm.LogInfof("response header from: %s: %s", h[0], h[1])
	}

	b, err := proxywasm.GetHttpCallResponseBody(0, bodySize)
	if err != nil {
		proxywasm.LogCriticalf("failed to get response body: %v", err)
		return
	}
	mr := ConnectionMetadata{}
	// No JSON lol...
	mr.Identity = string(b[17 : len(b)-3])

	proxywasm.LogInfof("got howardjohn_meta: %v", mr.Identity)
	if err := proxywasm.SetProperty([]string{"howardjohn_meta"}, []byte(mr.Identity)); err != nil {
		proxywasm.LogCriticalf("failed to get set property: %v", err)
		return
	}
}

// Override types.DefaultTcpContext.
func (ctx *networkContext) OnStreamDone() {
	proxywasm.LogError("howardjohn: connection complete!")
}

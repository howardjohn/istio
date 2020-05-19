// Copyright 2017 Istio Authors
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

package v2

import (
	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pkg/config/schema/collections"

	"istio.io/istio/pilot/pkg/model"
)

const (
	TypeURLConnections = "istio.io/connections"
	TypeURLDisconnect  = "istio.io/disconnect"

	// TODO: TypeURLReady - readiness events for endpoints, agent can propagate

	// TypeURLNACK will receive messages of type DiscoveryRequest, containing
	// the 'NACK' from envoy on rejected configs. Only ID is set in metadata.
	// This includes all the info that envoy (client) provides.
	TypeURLNACK = "istio.io/nack"
)

// InternalGen is a Generator for XDS status updates: connect, disconnect, nacks, acks
type InternalGen struct {
	Server *DiscoveryServer

	Client model.ConfigStoreCache

	// TODO: track last N Nacks and connection events, with 'version' based on timestamp.
	// On new connect, use version to send recent events since last update.
}

func (sg *InternalGen) Generate(proxy *model.Proxy, push *model.PushContext, w *model.WatchedResource, updates model.XdsUpdates) model.Resources {
	panic("implement me")
}

var _ model.XdsResourceGenerator = &InternalGen{}

func (sg *InternalGen) OnConnect(node *model.Proxy) {
	adsLog.Errorf("howardjohn: on connect")
	sg.registerWorkload(node)
}

func (sg *InternalGen) OnDisconnect(node *model.Proxy) {
	adsLog.Errorf("howardjohn: on disconnect")
	sg.unregisterWorkload(node)
}

func (sg *InternalGen) registerWorkload(node *model.Proxy) {
	we := &networking.WorkloadEntry{
		Address:        node.IPAddresses[0],
		Labels:         node.Metadata.Labels,
		ServiceAccount: node.Metadata.ServiceAccount,
	}
	cfg := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      collections.IstioNetworkingV1Alpha3Workloadentries.Resource().Kind(),
			Group:     collections.IstioNetworkingV1Alpha3Workloadentries.Resource().Group(),
			Version:   collections.IstioNetworkingV1Alpha3Workloadentries.Resource().Version(),
			Name:      node.Metadata.InstanceName,
			Namespace: node.Metadata.ConfigNamespace,
			Labels:    node.Metadata.Labels,
		},
		Spec: we,
	}
	_, err := sg.Client.Create(cfg)
	if err != nil {
		adsLog.Errorf("failed to create workload entry: %v", err)
	}
}

func (sg *InternalGen) unregisterWorkload(node *model.Proxy) {
	err := sg.Client.Delete(
		collections.IstioNetworkingV1Alpha3Workloadentries.Resource().GroupVersionKind(),
		node.Metadata.InstanceName,
		node.Metadata.ConfigNamespace,
	)
	if err != nil {
		adsLog.Errorf("failed to cleanup workload entry: %v", err)
	}
}

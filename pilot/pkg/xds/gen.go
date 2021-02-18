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
	"encoding/json"
	"time"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/golang/protobuf/ptypes"
	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/util/sets"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/pkg/env"
	istioversion "istio.io/pkg/version"
)

// gen2 provides experimental support for extended generation mechanism.

// IstioControlPlaneInstance defines the format Istio uses for when creating Envoy config.core.v3.ControlPlane.identifier
type IstioControlPlaneInstance struct {
	// The Istio component type (e.g. "istiod")
	Component string
	// The ID of the component instance
	ID string
	// The Istio version
	Info istioversion.BuildInfo
}

var controlPlane *corev3.ControlPlane

// ControlPlane identifies the instance and Istio version.
func ControlPlane() *corev3.ControlPlane {
	return controlPlane
}

func init() {
	// The Pod Name (instance identity) is in PilotArgs, but not reachable globally nor from DiscoveryServer
	podName := env.RegisterStringVar("POD_NAME", "", "").Get()
	byVersion, err := json.Marshal(IstioControlPlaneInstance{
		Component: "istiod",
		ID:        podName,
		Info:      istioversion.Info,
	})
	if err != nil {
		adsLog.Warnf("XDS: Could not serialize control plane id: %v", err)
	}
	controlPlane = &corev3.ControlPlane{Identifier: string(byVersion)}
}

var SkipLogTypes = map[string]struct{}{
	v3.EndpointType: {},
	v3.SecretType:   {},
}

func (s *DiscoveryServer) findGenerator(typeURL string, con *Connection) model.XdsResourceGenerator {
	if g, f := s.Generators[con.proxy.Metadata.Generator+"/"+typeURL]; f {
		return g
	}

	if g, f := s.Generators[typeURL]; f {
		return g
	}

	// XdsResourceGenerator is the default generator for this connection. We want to allow
	// some types to use custom generators - for example EDS.
	g := con.proxy.XdsResourceGenerator
	if g == nil {
		// TODO move this to just directly using the resource TypeUrl
		g = s.Generators["api"] // default to "MCP" generators - any type supported by store
	}
	return g
}

// Push an XDS resource for the given connection. Configuration will be generated
// based on the passed in generator. Based on the updates field, generators may
// choose to send partial or even no response if there are no changes.
func (s *DiscoveryServer) pushXds(con *Connection, push *model.PushContext,
	currentVersion string, w *model.WatchedResource, req *model.PushRequest) error {
	if w == nil {
		return nil
	}
	gen := s.findGenerator(w.TypeUrl, con)
	if gen == nil {
		return nil
	}

	t0 := time.Now()

	res, err := gen.Generate(con.proxy, push, w, req)
	if err != nil || res == nil {
		// If we have nothing to send, report that we got an ACK for this version.
		if s.StatusReporter != nil {
			s.StatusReporter.RegisterEvent(con.ConID, w.TypeUrl, push.LedgerVersion)
		}
		return err
	}
	defer func() { recordPushTime(w.TypeUrl, time.Since(t0)) }()

	resp := &discovery.DiscoveryResponse{
		TypeUrl:     w.TypeUrl,
		VersionInfo: currentVersion,
		Nonce:       nonce(push.LedgerVersion),
		Resources:   res,
	}

	if err := con.send(resp); err != nil {
		recordSendError(w.TypeUrl, con.ConID, err)
		return err
	}

	// Some types handle logs inside Generate, skip them here
	if _, f := SkipLogTypes[w.TypeUrl]; !f {
		if adsLog.DebugEnabled() {
			// Add additional information to logs when debug mode enabled
			adsLog.Infof("%s: PUSH for node:%s resources:%d size:%s nonce:%v version:%v",
				v3.GetShortType(w.TypeUrl), con.proxy.ID, len(res), util.ByteCount(ResourceSize(res)), resp.Nonce, resp.VersionInfo)
		} else {
			adsLog.Infof("%s: PUSH for node:%s resources:%d size:%s",
				v3.GetShortType(w.TypeUrl), con.proxy.ID, len(res), util.ByteCount(ResourceSize(res)))
		}
	}
	return nil
}

func extractNames(res []*discovery.Resource) []string {
	names := []string{}
	for _, r := range res {
		names = append(names, r.Name)
	}
	return names
}

// TODO: make generator return discovery.Resource; then we don't need to introspect the name
func convertResponseToDelta(ver string, resources model.Resources) []*discovery.Resource {
	convert := []*discovery.Resource{}
	for _, r := range resources {
		var name string
		switch r.TypeUrl {
		case v3.ClusterType:
			aa := &cluster.Cluster{}
			ptypes.UnmarshalAny(r, aa)
			name = aa.Name
		case v3.ListenerType:
			aa := &listener.Listener{}
			ptypes.UnmarshalAny(r, aa)
			name = aa.Name
		case v3.EndpointType:
			aa := &endpoint.ClusterLoadAssignment{}
			ptypes.UnmarshalAny(r, aa)
			name = aa.ClusterName
		case v3.RouteType:
			aa := &route.RouteConfiguration{}
			ptypes.UnmarshalAny(r, aa)
			name = aa.Name
		}
		c := &discovery.Resource{
			Name:     name,
			Version:  ver,
			Resource: r,
		}
		convert = append(convert, c)
	}
	return convert
}

func init() {
	adsLog.SetOutputLevel(log.DebugLevel)
}

// Push an XDS resource for the given connection. Configuration will be generated
// based on the passed in generator. Based on the updates field, generators may
// choose to send partial or even no response if there are no changes.
func (s *DiscoveryServer) pushXdsDelta(con *Connection, push *model.PushContext,
	currentVersion string, w *model.WatchedResource, req *model.PushRequest) error {
	if w == nil {
		return nil
	}
	gen := s.findGenerator(w.TypeUrl, con)
	if gen == nil {
		return nil
	}

	t0 := time.Now()

	res, err := gen.Generate(con.proxy, push, w, req)
	if err != nil || res == nil {
		// If we have nothing to send, report that we got an ACK for this version.
		if s.StatusReporter != nil {
			s.StatusReporter.RegisterEvent(con.ConID, w.TypeUrl, push.LedgerVersion)
		}
		return err
	}
	defer func() { recordPushTime(w.TypeUrl, time.Since(t0)) }()

	resp := &discovery.DeltaDiscoveryResponse{
		TypeUrl:           w.TypeUrl,
		SystemVersionInfo: currentVersion,
		Nonce:             nonce(push.LedgerVersion),
		// TODO removed
		Resources: convertResponseToDelta(currentVersion, res),
	}
	cur := sets.NewSet(w.ResourceNames...)
	cur.Delete(extractNames(resp.Resources)...)
	resp.RemovedResources = cur.SortedList()
	if len(resp.RemovedResources) > 0 {
		adsLog.Errorf("ADS:%v REMOVE %v", v3.GetShortType(w.TypeUrl), resp.RemovedResources)
	}

	if err := con.sendDelta(resp); err != nil {
		recordSendError(w.TypeUrl, con.ConID, err)
		return err
	}

	// Some types handle logs inside Generate, skip them here
	if _, f := SkipLogTypes[w.TypeUrl]; !f {
		if adsLog.DebugEnabled() {
			// Add additional information to logs when debug mode enabled
			adsLog.Infof("%s: PUSH for node:%s resources:%d size:%s nonce:%v version:%v",
				v3.GetShortType(w.TypeUrl), con.proxy.ID, len(res), util.ByteCount(ResourceSize(res)), resp.Nonce, resp.SystemVersionInfo)
		} else {
			adsLog.Infof("%s: PUSH for node:%s resources:%d size:%s",
				v3.GetShortType(w.TypeUrl), con.proxy.ID, len(res), util.ByteCount(ResourceSize(res)))
		}
	}
	return nil
}

func ResourceSize(r model.Resources) int {
	// Approximate size by looking at the Any marshaled size. This avoids high cost
	// proto.Size, at the expense of slightly under counting.
	size := 0
	for _, r := range r {
		size += len(r.Value)
	}
	return size
}

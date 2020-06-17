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

package v2

import (
	"time"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/grpc/codes"

	"istio.io/istio/pilot/pkg/model"
	v3 "istio.io/istio/pilot/pkg/proxy/envoy/v3"
)

// gen2 provides experimental support for extended generation mechanism.

// handleReqAck checks if the message is an ack/nack and handles it, returning true.
// If false, the request should be processed by calling the generator.
func (s *DiscoveryServer) handleReqAck(con *XdsConnection, discReq *discovery.DiscoveryRequest) (*model.WatchedResource, bool) {

	// All NACKs should have ErrorDetail set !
	// Relying on versionCode != sentVersionCode as nack is less reliable.

	isAck := true

	t := discReq.TypeUrl
	con.mu.Lock()
	w := con.node.Active[t]
	if w == nil {
		w = &model.WatchedResource{
			TypeUrl: t,
		}
		con.node.Active[t] = w
		isAck = false // newly watched resource
	}
	con.mu.Unlock()

	if discReq.ErrorDetail != nil {
		errCode := codes.Code(discReq.ErrorDetail.Code)
		adsLog.Warnf("ADS: ACK ERROR %s %s:%s", con.ConID, errCode.String(), discReq.ErrorDetail.GetMessage())
		if s.InternalGen != nil {
			s.InternalGen.OnNack(con.node, discReq)
		}
		return w, true
	}

	if discReq.ResponseNonce == "" {
		isAck = false // initial request
	}
	// This is an ACK response to a previous message - but it may refer to a response on a previous connection to
	// a different XDS server instance.
	nonceSent := w.NonceSent

	// GRPC doesn't send version info in NACKs for RDS. Technically if nonce matches
	// previous response, it is an ACK/NACK.
	if nonceSent != "" && nonceSent == discReq.ResponseNonce {
		adsLog.Debugf("ADS: ACK %s %s %s %v", con.ConID, discReq.VersionInfo, discReq.ResponseNonce,
			time.Since(w.LastSent))
		w.NonceAcked = discReq.ResponseNonce
	}

	if nonceSent != discReq.ResponseNonce {
		adsLog.Debugf("ADS:RDS: Expired nonce received %s, sent %s, received %s",
			con.ConID, nonceSent, discReq.ResponseNonce)
		rdsExpiredNonce.Increment()
		// This is an ACK for a resource sent on an older stream, or out of sync.
		// Send a response back.
		isAck = false
	}

	// Change in the set of watched resource - regardless of ack, send new data.
	if !listEqualUnordered(w.ResourceNames, discReq.ResourceNames) {
		isAck = false
		w.ResourceNames = discReq.ResourceNames
	}

	return w, isAck
}

func getShortType(typeURL string) string {
	switch typeURL {
	case ClusterType, v3.ClusterType:
		return "CDS"
	case ListenerType, v3.ListenerType:
		return "LDS"
	case RouteType, v3.RouteType:
		return "RDS"
	case EndpointType, v3.EndpointType:
		return "EDS"
	default:
		return typeURL
	}
}

// CDS and LDS are specially in that they are "root" resources, and requests are for all clusters/listeners
// Other types are derived from these root resources, and we serve only requested ones
func shouldWatch(typeURL string) bool {
	switch typeURL {
	case ClusterType, v3.ClusterType, ListenerType, v3.ListenerType:
		return false
	default:
		return true
	}
}

// handleAck returns true if this is an ACK or NACK. Otherwise returns false
func (s *DiscoveryServer) handleAck(con *XdsConnection, discReq *discovery.DiscoveryRequest) bool {
	shortType := getShortType(discReq.TypeUrl)
	if discReq.ErrorDetail != nil {
		errCode := codes.Code(discReq.ErrorDetail.Code)
		adsLog.Warnf("ADS:%s: ACK ERROR %s %s:%s", shortType, con.ConID, errCode.String(), discReq.ErrorDetail.GetMessage())
		if s.InternalGen != nil {
			s.InternalGen.OnNack(con.node, discReq)
		}
		return true
	}

	// Nonce is empty on initial requests
	if discReq.ResponseNonce == "" {
		con.WatchedResources[shortType] = discReq.ResourceNames
		return false
	}

	nonceSent := con.NoncesSent[shortType]

	// This is an ACK response to a previous message - but it may refer to an old message, not our most recent response
	if nonceSent == "" {
		adsLog.Debugf("ADS:%s: got connection for nonce from another connection for %v received %s",
			shortType, con.ConID, discReq.ResponseNonce)
		con.WatchedResources[shortType] = discReq.ResourceNames
		return false
	}

	// This is an ACK for a resource sent on an older stream, or out of sync. Handle the same, but log differently
	if nonceSent != discReq.ResponseNonce {
		adsLog.Debugf("ADS:%s: Expired nonce received %s: sent %s, received %s",
			shortType, con.ConID, nonceSent, discReq.ResponseNonce)
		// TODO don't use "RDS" in metric name
		rdsExpiredNonce.Increment()
		return true
	}

	// Change in the set of watched resource - regardless of ack, send new data.
	// TODO if empty then do something else
	if shouldWatch(discReq.TypeUrl) && len(discReq.ResourceNames) > 0 && !listEqualUnordered(con.WatchedResources[shortType], discReq.ResourceNames) {

		con.WatchedResources[shortType] = discReq.ResourceNames

		return false
	}

	// Nonce matches previously sent; this is an ACK for the most recent response
	adsLog.Debugf("ADS:%s: ACK %s %s %s. Resources:%d", shortType, con.ConID, discReq.VersionInfo, discReq.ResponseNonce, len(discReq.ResourceNames))

	con.NoncesAcked[shortType] = discReq.ResponseNonce
	return true

	// Change in the set of watched resource - regardless of ack, send new data.
	// TODO handle
	//if !listEqualUnordered(w.ResourceNames, discReq.ResourceNames) {
	//	isAck = false
	//	w.ResourceNames = discReq.ResourceNames
	//}
}

// handleCustomGenerator uses model.Generator to generate the response.
func (s *DiscoveryServer) handleCustomGenerator(con *XdsConnection, req *discovery.DiscoveryRequest) error {
	w, isAck := s.handleReqAck(con, req)
	if isAck {
		return nil
	}

	push := s.globalPushContext()
	resp := &discovery.DiscoveryResponse{
		TypeUrl:     w.TypeUrl,
		VersionInfo: push.Version, // TODO: we can now generate per-type version !
		Nonce:       nonce(push.Version),
	}
	if push.Version == "" { // Usually in tests.
		resp.VersionInfo = resp.Nonce
	}

	// XdsResourceGenerator is the default generator for this connection. We want to allow
	// some types to use custom generators - for example EDS.
	g := con.node.XdsResourceGenerator
	if cg, f := s.Generators[con.node.Metadata.Generator+"/"+w.TypeUrl]; f {
		g = cg
	}

	cl := g.Generate(con.node, push, w, nil)
	sz := 0
	for _, rc := range cl {
		resp.Resources = append(resp.Resources, rc)
		sz += len(rc.Value)
	}

	err := con.send(resp)
	if err != nil {
		recordSendError("ADS", con.ConID, apiSendErrPushes, err)
		return err
	}
	apiPushes.Increment()
	w.LastSent = time.Now()
	w.LastSize = sz // just resource size - doesn't include header and types
	w.NonceSent = resp.Nonce
	adsLog.Infof("Pushed %s to %s count=%d size=%d", w.TypeUrl, con.node.ID, len(cl), sz)

	return nil
}

// TODO: verify that ProxyNeedsPush works correctly for Generator - ie. Sidecar visibility
// is respected for arbitrary resource types.

// Called for config updates.
// Will not be called if ProxyNeedsPush returns false - ie. if the update
func (s *DiscoveryServer) pushGeneratorV2(con *XdsConnection, push *model.PushContext,
	currentVersion string, w *model.WatchedResource, updates model.XdsUpdates) error {
	// TODO: generators may send incremental changes if both sides agree on the protocol.
	// This is specific to each generator type.
	cl := con.node.XdsResourceGenerator.Generate(con.node, push, w, updates)
	if cl == nil {
		return nil // No push needed.
	}

	// TODO: add a 'version' to the result of generator. If set, use it to determine if the result
	// changed - in many cases it will not change, so we can skip the push. Also the version will
	// become dependent of the specific resource - for example in case of API it'll be the largest
	// version of the requested type.

	resp := &discovery.DiscoveryResponse{
		TypeUrl:     w.TypeUrl,
		VersionInfo: currentVersion,
		Nonce:       nonce(push.Version),
	}

	sz := 0
	for _, rc := range cl {
		resp.Resources = append(resp.Resources, rc)
		sz += len(rc.Value)
	}

	err := con.send(resp)
	if err != nil {
		recordSendError("ADS", con.ConID, apiSendErrPushes, err)
		return err
	}
	w.LastSent = time.Now()
	w.LastSize = sz // just resource size - doesn't include header and types
	w.NonceSent = resp.Nonce

	adsLog.Infof("XDS: PUSH %s for node:%s resources:%d", w.TypeUrl, con.node.ID, len(cl))
	return nil
}

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

package istio

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	stdatomic "sync/atomic"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/google/uuid"
	"go.uber.org/atomic"
	"golang.org/x/time/rate"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"istio.io/istio/pilot/pkg/autoregistration"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/adsc"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/security"

	"istio.io/istio/pilot/pkg/features"
	istiogrpc "istio.io/istio/pilot/pkg/grpc"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	pilotxds "istio.io/istio/pilot/pkg/xds"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	istiolog "istio.io/istio/pkg/log"
	_ "istio.io/istio/pkg/util/protomarshal" // Ensure we get the more efficient vtproto gRPC encoder
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/xds"
)

var log = istiolog.RegisterScope("krtxds", "delta xds debugging")

type Registration func(map[string]krt.Collection[*discovery.Resource])

func Collection[T proto.Message](collection krt.Collection[T]) Registration {
	return func(m map[string]krt.Collection[*discovery.Resource]) func(stop <-chan struct{}){
		nc := krt.NewCollection(collection, func(ctx krt.HandlerContext, i T) **discovery.Resource {
			return ptr.Of(&discovery.Resource{
				Name:         krt.GetKey(i),
				Version:      "",
				Resource:     protoconv.MessageToAny(i),
				Ttl:          nil,
				CacheControl: nil,
				Metadata:     nil,
			})
		})

		t := adsc.TypeName[T]()
		m[t] = nc
		return func(stop <-chan struct{}) {
			 if !nc.WaitUntilSynced(stop) {
				 return
			 }
			 nc.RegisterBatch(func(o []krt.Event[*discovery.Resource]) {

			 }, true)
		}
	}
}

// NewDiscoveryServer creates DiscoveryServer that sources data from Pilot's internal mesh data structures
func NewDiscoveryServer(debugger *krt.DebugHandler, reg ...Registration) *DiscoveryServer {
	out := &DiscoveryServer{
		concurrentPushLimit: make(chan struct{}, features.PushThrottle),
		RequestRateLimit:    rate.NewLimiter(rate.Limit(features.RequestLimit), 1),
		InboundUpdates:      atomic.NewInt64(0),
		CommittedUpdates:    atomic.NewInt64(0),
		pushChannel:         make(chan *PushRequest, 10),
		pushQueue:           pilotxds.NewPushQueue(),
		debugHandlers:       map[string]string{},
		adsClients:          map[string]*Connection{},
		krtDebugger:         debugger,
		DebounceOptions: pilotxds.DebounceOptions{
			DebounceAfter: features.DebounceAfter,
			DebounceMax:   features.DebounceMax,
		},
		Collections: make(map[string]krt.Collection[*discovery.Resource]),
	}

	out.pushQueue
	for _, r := range reg {
		r(out.Collections)
	}

	return out
}

// DiscoveryServer is Pilot's gRPC implementation for Envoy's xds APIs
type DiscoveryServer struct {
	// Generators allow customizing the generated config, based on the client metadata.
	// Key is the generator type - will match the Generator metadata to set the per-connection
	// default generator, or the combination of Generator metadata and TypeUrl to select a
	// different generator for a type.
	// Normal istio clients use the default generator - will not be impacted by this.
	Collections map[string]krt.Collection[*discovery.Resource]

	// concurrentPushLimit is a semaphore that limits the amount of concurrent XDS pushes.
	concurrentPushLimit chan struct{}
	// RequestRateLimit limits the number of new XDS requests allowed. This helps prevent thundering hurd of incoming requests.
	RequestRateLimit *rate.Limiter

	// InboundUpdates describes the number of configuration updates the discovery server has received
	InboundUpdates *atomic.Int64
	// CommittedUpdates describes the number of configuration updates the discovery server has
	// received, process, and stored in the push context. If this number is less than InboundUpdates,
	// there are updates we have not yet processed.
	// Note: This does not mean that all proxies have received these configurations; it is strictly
	// the push context, which means that the next push to a proxy will receive this configuration.
	CommittedUpdates *atomic.Int64

	// pushChannel is the buffer used for debouncing.
	// after debouncing the pushRequest will be sent to pushQueue
	pushChannel chan *PushRequest

	// pushQueue is the buffer that used after debounce and before the real xds push.
	pushQueue *pilotxds.PushQueue

	// debugHandlers is the list of all the supported debug handlers.
	debugHandlers map[string]string

	// adsClients reflect active gRPC channels, for both ADS and EDS.
	adsClients      map[string]*Connection
	adsClientsMutex sync.RWMutex

	// Authenticators for XDS requests. Should be same/subset of the CA authenticators.
	Authenticators []security.Authenticator

	WorkloadEntryController *autoregistration.Controller

	// serverReady indicates caches have been synced up and server is ready to process requests.
	serverReady atomic.Bool

	DebounceOptions pilotxds.DebounceOptions

	// pushVersion stores the numeric push version. This should be accessed via NextVersion()
	pushVersion atomic.Uint64

	krtDebugger *krt.DebugHandler
	pushOrder   []string
}

type Connection struct {
	xds.Connection

	// Original node metadata, to avoid unmarshal/marshal.
	// This is included in internal events.
	node *core.Node

	// proxy is the client to which this connection is established.
	proxy *model.Proxy

	// deltaStream is used for Delta XDS. Only one of deltaStream or stream will be set
	deltaStream pilotxds.DeltaDiscoveryStream

	deltaReqChan chan *discovery.DeltaDiscoveryRequest

	s   *DiscoveryServer
	ids []string
}

func (s *DiscoveryServer) StreamDeltas(stream pilotxds.DeltaDiscoveryStream) error {
	// Check if server is ready to accept clients and process new requests.
	// Currently ready means caches have been synced and hence can build
	// clusters correctly. Without this check, InitContext() call below would
	// initialize with empty config, leading to reconnected Envoys loosing
	// configuration. This is an additional safety check inaddition to adding
	// cachesSynced logic to readiness probe to handle cases where kube-proxy
	// ip tables update latencies.
	// See https://github.com/istio/istio/issues/25495.
	if !s.IsServerReady() {
		return errors.New("server is not ready to serve discovery information")
	}

	ctx := stream.Context()
	peerAddr := "0.0.0.0"
	if peerInfo, ok := peer.FromContext(ctx); ok {
		peerAddr = peerInfo.Addr.String()
	}

	if err := s.WaitForRequestLimit(stream.Context()); err != nil {
		log.Warnf("ADS: %q exceeded rate limit: %v", peerAddr, err)
		return status.Errorf(codes.ResourceExhausted, "request rate limit exceeded: %v", err)
	}

	ids, err := s.authenticate(ctx)
	if err != nil {
		return status.Error(codes.Unauthenticated, err.Error())
	}
	if ids != nil {
		log.Debugf("Authenticated XDS: %v with identity %v", peerAddr, ids)
	} else {
		log.Debugf("Unauthenticated XDS: %v", peerAddr)
	}

	con := newDeltaConnection(peerAddr, stream)

	// Do not call: defer close(con.pushChannel). The push channel will be garbage collected
	// when the connection is no longer used. Closing the channel can cause subtle race conditions
	// with push. According to the spec: "It's only necessary to close a channel when it is important
	// to tell the receiving goroutines that all data have been sent."

	// Block until either a request is received or a push is triggered.
	// We need 2 go routines because 'read' blocks in Recv().
	go s.receiveDelta(con, ids)

	// Wait for the proxy to be fully initialized before we start serving traffic. Because
	// initialization doesn't have dependencies that will block, there is no need to add any timeout
	// here. Prior to this explicit wait, we were implicitly waiting by receive() not sending to
	// reqChannel and the connection not being enqueued for pushes to pushChannel until the
	// initialization is complete.
	<-con.InitializedCh()

	for {
		// Go select{} statements are not ordered; the same channel can be chosen many times.
		// For requests, these are higher priority (client may be blocked on startup until these are done)
		// and often very cheap to handle (simple ACK), so we check it first.
		select {
		case req, ok := <-con.deltaReqChan:
			if ok {
				if err := s.processDeltaRequest(req, con); err != nil {
					return err
				}
			} else {
				// Remote side closed connection or error processing the request.
				return <-con.ErrorCh()
			}
		case <-con.StopCh():
			return nil
		default:
		}
		// If there wasn't already a request, poll for requests and pushes. Note: if we have a huge
		// amount of incoming requests, we may still send some pushes, as we do not `continue` above;
		// however, requests will be handled ~2x as much as pushes. This ensures a wave of requests
		// cannot completely starve pushes. However, this scenario is unlikely.
		select {
		case req, ok := <-con.deltaReqChan:
			if ok {
				if err := s.processDeltaRequest(req, con); err != nil {
					return err
				}
			} else {
				// Remote side closed connection or error processing the request.
				return <-con.ErrorCh()
			}
		case ev := <-con.PushCh():
			pushEv := ev.(*Event)
			err := s.pushConnectionDelta(con, pushEv)
			pushEv.Done()
			if err != nil {
				return err
			}
		case <-con.StopCh():
			return nil
		}
	}
}

// Compute and send the new configuration for a connection.
func (s *DiscoveryServer) pushConnectionDelta(con *Connection, pushEv *Event) error {
	pushRequest := pushEv.PushRequst

	//if pushRequest.Full {
	// Update Proxy with current information.
	//s.computeProxyState(con.proxy, pushRequest)
	//}

	needsPush := s.ProxyNeedsPush(con.proxy, pushRequest)
	if !needsPush {
		log.Debugf("Skipping push to %v, no updates required", con.ID())
		return nil
	}

	// Send pushes to all generators
	// Each Generator is responsible for determining if the push event requires a push
	wrl := con.watchedResourcesByOrder(s.pushOrder)
	for _, w := range wrl {
		if err := s.pushDeltaXds(con, w, pushRequest, pushEv.PushVersion); err != nil {
			return err
		}
	}

	//proxiesConvergeDelay.Record(time.Since(pushRequest.Start).Seconds())
	return nil
}

func (s *DiscoveryServer) receiveDelta(con *Connection, identities []string) {
	defer func() {
		close(con.deltaReqChan)
		close(con.ErrorCh())
		// Close the initialized channel, if its not already closed, to prevent blocking the stream
		select {
		case <-con.InitializedCh():
		default:
			close(con.InitializedCh())
		}
	}()
	firstRequest := true
	for {
		req, err := con.deltaStream.Recv()
		if err != nil {
			if istiogrpc.GRPCErrorType(err) != istiogrpc.UnexpectedError {
				log.Infof("ADS: %q %s terminated", con.Peer(), con.ID())
				return
			}
			con.ErrorCh() <- err
			log.Errorf("ADS: %q %s terminated with error: %v", con.Peer(), con.ID(), err)
			xds.TotalXDSInternalErrors.Increment()
			return
		}
		// This should be only set for the first request. The node id may not be set - for example malicious clients.
		if firstRequest {
			// probe happens before envoy sends first xDS request
			if req.TypeUrl == v3.HealthInfoType {
				log.Warnf("ADS: %q %s send health check probe before normal xDS request", con.Peer(), con.ID())
				continue
			}
			firstRequest = false
			if req.Node == nil || req.Node.Id == "" {
				con.ErrorCh() <- status.New(codes.InvalidArgument, "missing node information").Err()
				return
			}
			if err := s.initConnection(req.Node, con, identities); err != nil {
				con.ErrorCh() <- err
				return
			}
			defer s.closeConnection(con)
			log.Infof("ADS: new delta connection for node:%s", con.ID())
		}

		select {
		case con.deltaReqChan <- req:
		case <-con.deltaStream.Context().Done():
			log.Infof("ADS: %q %s terminated with stream closed", con.Peer(), con.ID())
			return
		}
	}
}

func (conn *Connection) sendDelta(res *discovery.DeltaDiscoveryResponse) error {
	sendResonse := func() error {
		start := time.Now()
		defer func() { xds.RecordSendTime(time.Since(start)) }()
		return conn.deltaStream.Send(res)
	}
	err := sendResonse()
	if err == nil {
		if !strings.HasPrefix(res.TypeUrl, v3.DebugType) {
			conn.proxy.UpdateWatchedResource(res.TypeUrl, func(wr *model.WatchedResource) *model.WatchedResource {
				if wr == nil {
					wr = &model.WatchedResource{TypeUrl: res.TypeUrl}
				}
				wr.NonceSent = res.Nonce
				wr.LastSendTime = time.Now()
				return wr
			})
		}
	} else if status.Convert(err).Code() == codes.DeadlineExceeded {
		log.Infof("Timeout writing %s: %v", conn.ID(), v3.GetShortType(res.TypeUrl))
		xds.ResponseWriteTimeouts.Increment()
	}
	return err
}

// processDeltaRequest is handling one request. This is currently called from the 'main' thread, which also
// handles 'push' requests and close - the code will eventually call the 'push' code, and it needs more mutex
// protection. Original code avoided the mutexes by doing both 'push' and 'process requests' in same thread.
func (s *DiscoveryServer) processDeltaRequest(req *discovery.DeltaDiscoveryRequest, con *Connection) error {
	stype := v3.GetShortType(req.TypeUrl)
	log.Debugf("ADS:%s: REQ %s resources sub:%d unsub:%d nonce:%s", stype,
		con.ID(), len(req.ResourceNamesSubscribe), len(req.ResourceNamesUnsubscribe), req.ResponseNonce)

	shouldRespond := shouldRespondDelta(con, req)
	if !shouldRespond {
		return nil
	}

	subs, _, _ := deltaWatchedResources(nil, req)
	request := &PushRequest{
		// The usage of LastPushTime (rather than time.Now()), is critical here for correctness; This time
		// is used by the XDS cache to determine if a entry is stale. If we use Now() with an old push context,
		// we may end up overriding active cache entries with stale ones.
		Start: con.proxy.LastPushTime,
		Delta: model.ResourceDelta{
			// Record sub/unsub, but drop synthetic wildcard info
			Subscribed:   subs,
			Unsubscribed: sets.New(req.ResourceNamesUnsubscribe...).Delete("*"),
		},
	}

	err := s.pushDeltaXds(con, con.proxy.GetWatchedResource(req.TypeUrl), request, "")
	if err != nil {
		return err
	}
	return nil
}

// shouldRespondDelta determines whether this request needs to be responded back. It applies the ack/nack rules as per xds protocol
// using WatchedResource for previous state and discovery request for the current state.
func shouldRespondDelta(con *Connection, request *discovery.DeltaDiscoveryRequest) bool {
	stype := v3.GetShortType(request.TypeUrl)

	// If there is an error in request that means previous response is erroneous.
	// We do not have to respond in that case. In this case request's version info
	// will be different from the version sent. But it is fragile to rely on that.
	if request.ErrorDetail != nil {
		errCode := codes.Code(request.ErrorDetail.Code)
		log.Warnf("ADS:%s: ACK ERROR %s %s:%s", stype, con.ID(), errCode.String(), request.ErrorDetail.GetMessage())
		xds.IncrementXDSRejects(request.TypeUrl, con.proxy.ID, errCode.String())
		con.proxy.UpdateWatchedResource(request.TypeUrl, func(wr *model.WatchedResource) *model.WatchedResource {
			wr.LastError = request.ErrorDetail.GetMessage()
			return wr
		})
		return false
	}

	log.Debugf("ADS:%s REQUEST %v: sub:%v unsub:%v initial:%v", stype, con.ID(),
		request.ResourceNamesSubscribe, request.ResourceNamesUnsubscribe, request.InitialResourceVersions)
	previousInfo := con.proxy.GetWatchedResource(request.TypeUrl)

	// This can happen in two cases:
	// 1. Envoy initially send request to Istiod
	// 2. Envoy reconnect to Istiod i.e. Istiod does not have
	// information about this typeUrl, but Envoy sends response nonce - either
	// because Istiod is restarted or Envoy disconnects and reconnects.
	// We should always respond with the current resource names.
	if previousInfo == nil {
		con.proxy.Lock()
		defer con.proxy.Unlock()

		if len(request.InitialResourceVersions) > 0 {
			log.Debugf("ADS:%s: RECONNECT %s %s resources:%v", stype, con.ID(), request.ResponseNonce, len(request.InitialResourceVersions))
		} else {
			log.Debugf("ADS:%s: INIT %s %s", stype, con.ID(), request.ResponseNonce)
		}

		res, wildcard, _ := deltaWatchedResources(nil, request)
		skip := request.TypeUrl == v3.AddressType && wildcard
		if skip {
			// Due to the high resource count in WDS at scale, we do not store ResourceName.
			// See the workload generator for more information on why we don't use this.
			res = nil
		}
		con.proxy.WatchedResources[request.TypeUrl] = &model.WatchedResource{
			TypeUrl:       request.TypeUrl,
			ResourceNames: res,
			Wildcard:      wildcard,
		}
		return true
	}

	// If there is mismatch in the nonce, that is a case of expired/stale nonce.
	// A nonce becomes stale following a newer nonce being sent to Envoy.
	if request.ResponseNonce != "" && request.ResponseNonce != previousInfo.NonceSent {
		log.Debugf("ADS:%s: REQ %s Expired nonce received %s, sent %s", stype,
			con.ID(), request.ResponseNonce, previousInfo.NonceSent)
		//xds.ExpiredNonce.With(typeTag.Value(v3.GetMetricType(request.TypeUrl))).Increment()
		return false
	}

	// Spontaneous DeltaDiscoveryRequests from the client.
	// This can be done to dynamically add or remove elements from the tracked resource_names set.
	// In this case response_nonce is empty.
	spontaneousReq := request.ResponseNonce == ""

	var alwaysRespond bool
	var subChanged bool

	// Update resource names, and record ACK if required.
	con.proxy.UpdateWatchedResource(request.TypeUrl, func(wr *model.WatchedResource) *model.WatchedResource {
		wr.ResourceNames, _, subChanged = deltaWatchedResources(wr.ResourceNames, request)
		if !spontaneousReq {
			// Clear last error, we got an ACK.
			// Otherwise, this is just a change in resource subscription, so leave the last ACK info in place.
			wr.LastError = ""
			wr.NonceAcked = request.ResponseNonce
		}
		alwaysRespond = wr.AlwaysRespond
		wr.AlwaysRespond = false
		return wr
	})

	// It is invalid in the below two cases:
	// 1. no subscribed resources change from spontaneous delta request.
	// 2. subscribed resources changes from ACK.
	if spontaneousReq && !subChanged || !spontaneousReq && subChanged {
		log.Errorf("ADS:%s: Subscribed resources check mismatch: %v vs %v", stype, spontaneousReq, subChanged)
		if features.EnableUnsafeAssertions {
			panic(fmt.Sprintf("ADS:%s: Subscribed resources check mismatch: %v vs %v", stype, spontaneousReq, subChanged))
		}
	}

	// Envoy can send two DiscoveryRequests with same version and nonce
	// when it detects a new resource. We should respond if they change.
	if !subChanged {
		// We should always respond "alwaysRespond" marked requests to let Envoy finish warming
		// even though Nonce match and it looks like an ACK.
		if alwaysRespond {
			log.Infof("ADS:%s: FORCE RESPONSE %s for warming.", stype, con.ID())
			return true
		}

		log.Debugf("ADS:%s: ACK %s %s", stype, con.ID(), request.ResponseNonce)
		return false
	}
	log.Debugf("ADS:%s: RESOURCE CHANGE %s %s", stype, con.ID(), request.ResponseNonce)

	return true
}

// Push a Delta XDS resource for the given connection.
func (s *DiscoveryServer) pushDeltaXds(con *Connection, w *model.WatchedResource, req *PushRequest, pushVersion string) error {
	if w == nil {
		return nil
	}
	gen, f := s.findGenerator(w.TypeUrl)
	if !f {
		return nil
	}
	res, deletedRes, logdata, err := gen.GenerateDeltas(req, w)
	if err != nil || (res == nil && deletedRes == nil) {
		return err
	}
	//defer func() { recordPushTime(w.TypeUrl, time.Since(t0)) }()
	resp := &discovery.DeltaDiscoveryResponse{
		//ControlPlane: ControlPlane(w.TypeUrl),
		TypeUrl:           w.TypeUrl,
		SystemVersionInfo: pushVersion,
		Nonce:             nonce(pushVersion),
		Resources:         res,
		RemovedResources:  deletedRes,
	}
	if len(resp.RemovedResources) > 0 {
		log.Debugf("ADS:%v REMOVE for node:%s %v", v3.GetShortType(w.TypeUrl), con.ID(), resp.RemovedResources)
	}

	configSize := pilotxds.ResourceSize(res)
	//configSizeBytes.With(typeTag.Value(w.TypeUrl)).Record(float64(configSize))

	ptype := "PUSH"
	info := ""
	if logdata.Incremental {
		ptype = "PUSH INC"
	}
	if len(logdata.AdditionalInfo) > 0 {
		info = " " + logdata.AdditionalInfo
	}

	if err := con.sendDelta(resp); err != nil {
		if log.DebugEnabled() {
			log.Debugf("%s: Send failure for node:%s resources:%d size:%s%s: %v",
				v3.GetShortType(w.TypeUrl), con.proxy.ID, len(res), util.ByteCount(configSize), info, err)
		}
		return err
	}

	debug := ""
	if log.DebugEnabled() {
		// Add additional information to logs when debug mode enabled.
		debug = " nonce:" + resp.Nonce + " version:" + resp.SystemVersionInfo
	}
	log.Infof("%s: %s%s for node:%s resources:%d removed:%d size:%v%s%s",
		v3.GetShortType(w.TypeUrl), ptype, req.PushReason(), con.proxy.ID, len(res), len(resp.RemovedResources),
		util.ByteCount(pilotxds.ResourceSize(res)), info, debug)

	return nil
}

func (s *DiscoveryServer) IsServerReady() bool {
	// TODO
	return true
}

func newDeltaConnection(peerAddr string, stream pilotxds.DeltaDiscoveryStream) *Connection {
	return &Connection{
		Connection:   xds.NewConnection(peerAddr, nil),
		deltaStream:  stream,
		deltaReqChan: make(chan *discovery.DeltaDiscoveryRequest, 1),
	}
}

// deltaWatchedResources returns current watched resources of delta xds
func deltaWatchedResources(existing sets.String, request *discovery.DeltaDiscoveryRequest) (sets.String, bool, bool) {
	res := existing
	if res == nil {
		res = sets.New[string]()
	}
	changed := false
	for _, r := range request.ResourceNamesSubscribe {
		if !res.InsertContains(r) {
			changed = true
		}
	}
	// This is set by Envoy on first request on reconnection so that we are aware of what Envoy knows
	// and can continue the xDS session properly.
	for r := range request.InitialResourceVersions {
		if !res.InsertContains(r) {
			changed = true
		}
	}
	for _, r := range request.ResourceNamesUnsubscribe {
		if res.DeleteContains(r) {
			changed = true
		}
	}
	wildcard := false
	// A request is wildcard if they explicitly subscribe to "*" or subscribe to nothing
	if res.Contains("*") {
		wildcard = true
		res.Delete("*")
	}
	// "if the client sends a request but has never explicitly subscribed to any resource names, the
	// server should treat that identically to how it would treat the client having explicitly
	// subscribed to *"
	// NOTE: this means you cannot subscribe to nothing, which is useful for on-demand loading; to workaround this
	// Istio clients will send and initial request both subscribing+unsubscribing to `*`.
	if len(request.ResourceNamesSubscribe) == 0 {
		wildcard = true
	}
	return res, wildcard, changed
}

// Clients returns all currently connected clients. This method can be safely called concurrently,
// but care should be taken with the underlying objects (ie model.Proxy) to ensure proper locking.
// This method returns only fully initialized connections; for all connections, use AllClients
func (s *DiscoveryServer) Clients() []*Connection {
	s.adsClientsMutex.RLock()
	defer s.adsClientsMutex.RUnlock()
	clients := make([]*Connection, 0, len(s.adsClients))
	for _, con := range s.adsClients {
		select {
		case <-con.InitializedCh():
		default:
			// Initialization not complete, skip
			continue
		}
		clients = append(clients, con)
	}
	return clients
}

// SortedClients returns all currently connected clients in an ordered manner.
// Sorting order priority is as follows: ClusterID, Namespace, ID.
func (s *DiscoveryServer) SortedClients() []*Connection {
	clients := s.Clients()
	sort.Slice(clients, func(i, j int) bool {
		if clients[i].proxy.GetClusterID().String() < clients[j].proxy.GetClusterID().String() {
			return true
		}
		if clients[i].proxy.GetNamespace() < clients[j].proxy.GetNamespace() {
			return true
		}
		return clients[i].proxy.GetID() < clients[j].proxy.GetID()
	})
	return clients
}

// AllClients returns all connected clients, per Clients, but additionally includes uninitialized connections
// Warning: callers must take care not to rely on the con.proxy field being set
func (s *DiscoveryServer) AllClients() []*Connection {
	s.adsClientsMutex.RLock()
	defer s.adsClientsMutex.RUnlock()
	return maps.Values(s.adsClients)
}

func (s *DiscoveryServer) WaitForRequestLimit(ctx context.Context) error {
	if s.RequestRateLimit.Limit() == 0 {
		// Allow opt out when rate limiting is set to 0qps
		return nil
	}
	// Give a bit of time for queue to clear out, but if not fail fast. Client will connect to another
	// instance in best case, or retry with backoff.
	wait, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	return s.RequestRateLimit.Wait(wait)
}

func (s *DiscoveryServer) NextVersion() string {
	return time.Now().Format(time.RFC3339) + "/" + strconv.FormatUint(s.pushVersion.Inc(), 10)
}

func (s *DiscoveryServer) authenticate(ctx context.Context) ([]string, error) {
	return nil, nil
}

func (s *DiscoveryServer) ProxyNeedsPush(proxy *model.Proxy, request *PushRequest) bool {
	return true
	// TODO(krt) make pushrequest a {type,name} and then filter if we dont watch any... maybe? Does it help?
}

// watchedResourcesByOrder returns the ordered list of
// watched resources for the proxy, ordered in accordance with known push order.
func (conn *Connection) watchedResourcesByOrder(pushOrder []string) []*model.WatchedResource {
	allWatched := conn.proxy.ShallowCloneWatchedResources()
	ordered := make([]*model.WatchedResource, 0, len(allWatched))
	// first add all known types, in order
	pushOrderSet := sets.New(pushOrder...)
	for _, tp := range pushOrder {
		if allWatched[tp] != nil {
			ordered = append(ordered, allWatched[tp])
		}
	}
	// Then add any undeclared types
	for tp, res := range allWatched {
		if !pushOrderSet.Contains(tp) {
			ordered = append(ordered, res)
		}
	}
	return ordered
}

// update the node associated with the connection, after receiving a packet from envoy, also adds the connection
// to the tracking map.
func (s *DiscoveryServer) initConnection(node *core.Node, con *Connection, identities []string) error {
	// Setup the initial proxy metadata
	proxy, err := s.initProxyMetadata(node)
	if err != nil {
		return err
	}
	// First request so initialize connection id and start tracking it.
	con.SetID(connectionID(proxy.ID))
	con.node = node
	con.proxy = proxy

	// Authorize xds clients
	// TODO(krt)
	//if err := s.authorize(con, identities); err != nil {
	//	return err
	//}

	// Register the connection. this allows pushes to be triggered for the proxy. Note: the timing of
	// this and initializeProxy important. While registering for pushes *after* initialization is complete seems like
	// a better choice, it introduces a race condition; If we complete initialization of a new push
	// context between initializeProxy and addCon, we would not get any pushes triggered for the new
	// push context, leading the proxy to have a stale state until the next full push.
	s.addCon(con.ID(), con)
	// Register that initialization is complete. This triggers to calls that it is safe to access the
	// proxy
	defer con.MarkInitialized()
	proxy.WatchedResources = map[string]*model.WatchedResource{}
	return nil
}

func (s *DiscoveryServer) closeConnection(con *Connection) {
	if con.ID() == "" {
		return
	}
	s.removeCon(con.ID())
}

func (s *DiscoveryServer) addCon(conID string, con *Connection) {
	s.adsClientsMutex.Lock()
	defer s.adsClientsMutex.Unlock()
	s.adsClients[conID] = con
	//recordXDSClients(con.proxy.Metadata.IstioVersion, 1)
}

func (s *DiscoveryServer) removeCon(conID string) {
	s.adsClientsMutex.Lock()
	defer s.adsClientsMutex.Unlock()

	if _, exist := s.adsClients[conID]; !exist {
		log.Errorf("ADS: Removing connection for non-existing node:%v.", conID)
		//xds.TotalXDSInternalErrors.Increment()
	} else {
		delete(s.adsClients, conID)
		//recordXDSClients(con.proxy.Metadata.IstioVersion, -1)
	}
}

// Debouncing and push request happens in a separate thread, it uses locks
// and we want to avoid complications, ConfigUpdate may already hold other locks.
// handleUpdates processes events from pushChannel
// It ensures that at minimum minQuiet time has elapsed since the last event before processing it.
// It also ensures that at most maxDelay is elapsed between receiving an event and processing it.
func (s *DiscoveryServer) handleUpdates(stopCh <-chan struct{}) {
	debounce(s.pushChannel, stopCh, s.DebounceOptions, s.Push, s.CommittedUpdates)
}


// The debounce helper function is implemented to enable mocking
func debounce(ch chan *PushRequest, stopCh <-chan struct{}, opts pilotxds.DebounceOptions, pushFn func(req *PushRequest), updateSent *atomic.Int64) {
	var timeChan <-chan time.Time
	var startDebounce time.Time
	var lastConfigUpdateTime time.Time

	pushCounter := 0
	debouncedEvents := 0

	// Keeps track of the push requests. If updates are debounce they will be merged.
	var req *PushRequest

	free := true
	freeCh := make(chan struct{}, 1)

	push := func(req *PushRequest, debouncedEvents int, startDebounce time.Time) {
		pushFn(req)
		updateSent.Add(int64(debouncedEvents))
		//debounceTime.Record(time.Since(startDebounce).Seconds())
		freeCh <- struct{}{}
	}

	pushWorker := func() {
		eventDelay := time.Since(startDebounce)
		quietTime := time.Since(lastConfigUpdateTime)
		// it has been too long or quiet enough
		if eventDelay >= opts.DebounceMax || quietTime >= opts.DebounceAfter {
			if req != nil {
				pushCounter++
				if req.ConfigsUpdated == nil {
					log.Infof("Push debounce stable[%d] %d: %v since last change, %v since last push",
						pushCounter, debouncedEvents,
						quietTime, eventDelay)
				} else {
					log.Infof("Push debounce stable[%d] %d for config %s: %v since last change, %v since last push",
						pushCounter, debouncedEvents, configsUpdated(req),
						quietTime, eventDelay)
				}
				free = false
				go push(req, debouncedEvents, startDebounce)
				req = nil
				debouncedEvents = 0
			}
		} else {
			timeChan = time.After(opts.DebounceAfter - quietTime)
		}
	}

	for {
		select {
		case <-freeCh:
			free = true
			pushWorker()
		case r := <-ch:
			lastConfigUpdateTime = time.Now()
			if debouncedEvents == 0 {
				timeChan = time.After(opts.DebounceAfter)
				startDebounce = lastConfigUpdateTime
			}
			debouncedEvents++

			req = req.Merge(r)
		case <-timeChan:
			if free {
				pushWorker()
			}
		case <-stopCh:
			return
		}
	}
}

func configsUpdated(req *PushRequest) any {
	// TODO(krt)
	return nil
}


func nonce(noncePrefix string) string {
	return noncePrefix + uuid.New().String()
}

// initProxyMetadata initializes just the basic metadata of a proxy. This is decoupled from
// initProxyState such that we can perform authorization before attempting expensive computations to
// fully initialize the proxy.
func (s *DiscoveryServer) initProxyMetadata(node *core.Node) (*model.Proxy, error) {
	meta, err := model.ParseMetadata(node.Metadata)
	if err != nil {
		return nil, status.New(codes.InvalidArgument, err.Error()).Err()
	}
	proxy, err := model.ParseServiceNodeWithMetadata(node.Id, meta)
	if err != nil {
		return nil, status.New(codes.InvalidArgument, err.Error()).Err()
	}
	// Update the config namespace associated with this proxy
	proxy.ConfigNamespace = model.GetProxyConfigNamespace(proxy)
	proxy.XdsNode = node
	return proxy, nil
}

func (s *DiscoveryServer) findGenerator(url string) (CollectionGenerator, bool) {
	c, f := s.Collections[url]
	if f {
		return CollectionGenerator{Col: c}, f
	}
	return CollectionGenerator{}, false
}

var connectionNumber = int64(0)
func connectionID(node string) string {
	id := stdatomic.AddInt64(&connectionNumber, 1)
	return node + "-" + strconv.FormatInt(id, 10)
}

type CollectionGenerator struct {
	Col krt.Collection[*discovery.Resource]
}

// GenerateDeltas computes Workload resources. This is design to be highly optimized to delta updates,
// and supports *on-demand* client usage. A client can subscribe with a wildcard subscription and get all
// resources (with delta updates), or on-demand and only get responses for specifically subscribed resources.
//
// Incoming requests may be for VIP or Pod IP addresses. However, all responses are Workload resources, which are pod based.
// This means subscribing to a VIP may end up pushing many resources of different name than the request.
// On-demand clients are expected to handle this (for wildcard, this is not applicable, as they don't specify any resources at all).
func (e CollectionGenerator) GenerateDeltas(
	req *PushRequest,
	w *model.WatchedResource,
) (model.Resources, model.DeletedResources, model.XdsLogDetails, error) {
	var res []*discovery.Resource
	var deletes []string
	if req.IsRequest() {
		// Full update, expect everything
		res = e.Col.List()
		toDeleted := w.ResourceNames.Copy()
		for _, r := range res {
			toDeleted.Delete(r.Name)
		}
		deletes = sets.SortedList(toDeleted)
	} else {
		k := req.ConfigsUpdated[TypeUrl(w.TypeUrl)]

		res = make([]*discovery.Resource, 0, len(k))
		for k := range k {
			v := e.Col.GetKey(k)
			if v == nil {
				deletes = append(deletes, k)
			} else {
				res = append(res, *v)
			}
		}
	}

	if len(res) == 0 && len(deletes) == 0 {
		// No changes
		return nil, nil, model.DefaultXdsLogDetails, nil
	}

	return res, deletes, model.DefaultXdsLogDetails, nil
}

type TypeUrl string

// PushRequest defines a request to push to proxies
// It is used to send updates to the config update debouncer and pass to the PushQueue.
type PushRequest struct {
	// ConfigsUpdated keeps track of configs that have changed.
	// This is used as an optimization to avoid unnecessary pushes to proxies that are scoped with a Sidecar.
	// If this is empty, then all proxies will get an update.
	// Otherwise only proxies depend on these configs will get an update.
	// The kind of resources are defined in pkg/config/schemas.
	ConfigsUpdated map[TypeUrl]sets.String

	// Start represents the time a push was started. This represents the time of adding to the PushQueue.
	// Note that this does not include time spent debouncing.
	Start time.Time

	// Delta defines the resources that were added or removed as part of this push request.
	// This is set only on requests from the client which change the set of resources they (un)subscribe from.
	Delta xds.ResourceDelta
}

func (r PushRequest) IsRequest() bool {
	// TODO(krt)
	return false
}
func (pr *PushRequest) PushReason() string {
	if pr.IsRequest() {
		return " request"
	}
	return ""
}

// Merge two update requests together
// Merge behaves similarly to a list append; usage should in the form `a = a.merge(b)`.
// Importantly, Merge may decide to allocate a new PushRequest object or reuse the existing one - both
// inputs should not be used after completion.
func (pr *PushRequest) Merge(other *PushRequest) *PushRequest {
	if pr == nil {
		return other
	}
	if other == nil {
		return pr
	}


	if pr.ConfigsUpdated == nil {
		pr.ConfigsUpdated = other.ConfigsUpdated
	} else {
		for k, v := range other.ConfigsUpdated {
			sets.InsertOrNew(pr.ConfigsUpdated, k, v)
		}
	}

	return pr
}

// Event represents a config or registry event that results in a push.
type Event struct {
	// PushRequst PushRequest to use for the push.
	PushRequst *PushRequest

	PushVersion string
	// function to call once a push is finished. This must be called or future changes may be blocked.
	Done func()
}

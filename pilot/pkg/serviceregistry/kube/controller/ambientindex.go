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

package controller

import (
	"net/netip"
	"strings"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"

	meshapi "istio.io/api/mesh/v1alpha1"
	securityclient "istio.io/client-go/pkg/apis/security/v1beta1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/kind"
	kubeutil "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/cv2"
	kubelabels "istio.io/istio/pkg/kube/labels"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/workloadapi"
	"istio.io/istio/pkg/workloadapi/security"
)

type WorkloadsCollection struct {
	cv2.Collection[model.WorkloadInfo]
	ByAddress        *cv2.Index[model.WorkloadInfo, networkAddress]
	ByServiceVIP     *cv2.Index[model.WorkloadInfo, string]
	ByOwningWaypoint *cv2.Index[model.WorkloadInfo, model.WaypointScope]
}

type WaypointsCollection struct {
	cv2.Collection[Waypoint]
	ByScope *cv2.Index[Waypoint, model.WaypointScope]
}

type ServicesCollection struct {
	cv2.Collection[model.ServiceInfo]
}

// AmbientIndexImpl maintains an index of ambient WorkloadInfo objects by various keys.
// These are intentionally pre-computed based on events such that lookups are efficient.
type AmbientIndexImpl struct {
	services  ServicesCollection
	workloads WorkloadsCollection
	waypoints WaypointsCollection

	authorizationPolicies cv2.Collection[model.WorkloadAuthorization]
}

func workloadToAddressInfo(w *workloadapi.Workload) model.AddressInfo {
	return model.AddressInfo{
		Address: &workloadapi.Address{
			Type: &workloadapi.Address_Workload{
				Workload: w,
			},
		},
	}
}

func modelWorkloadToAddressInfo(w model.WorkloadInfo) model.AddressInfo {
	return workloadToAddressInfo(w.Workload)
}

func serviceToAddressInfo(s *workloadapi.Service) model.AddressInfo {
	return model.AddressInfo{
		Address: &workloadapi.Address{
			Type: &workloadapi.Address_Service{
				Service: s,
			},
		},
	}
}

// name format: <cluster>/<group>/<kind>/<namespace>/<name></section-name>
func (c *Controller) generatePodUID(p *v1.Pod) string {
	return c.clusterID.String() + "//" + "Pod/" + p.Namespace + "/" + p.Name
}

// Lookup finds a given IP address.
func (a *AmbientIndexImpl) Lookup(key string) []model.AddressInfo {
	// 1. Workload UID
	if w := a.workloads.GetKey(cv2.Key[model.WorkloadInfo](key)); w != nil {
		return []model.AddressInfo{workloadToAddressInfo(w.Workload)}
	}

	network, ip, found := strings.Cut(key, "/")
	if !found {
		log.Warnf(`key (%v) did not contain the expected "/" character`, key)
		return nil
	}
	networkAddr := networkAddress{network: network, ip: ip}

	// 2. Workload by IP
	if wls := a.workloads.ByAddress.Lookup(networkAddr); len(wls) > 0 {
		return slices.Map(wls, modelWorkloadToAddressInfo)
	}

	// 3. Service
	if svc := a.lookupService(key); svc != nil {
		vips := sets.New[string]()
		for _, addr := range svc.Service.Addresses {
			vips.Insert(byteIPToString(addr.Address))
		}
		res := []model.AddressInfo{serviceToAddressInfo(svc.Service)}
		// TODO: avoid full scan
		for _, wl := range a.workloads.List(metav1.NamespaceAll) {
			for vip := range wl.Services {
				if vips.Contains(vip) {
					res = append(res, workloadToAddressInfo(wl.Workload))
					break
				}
			}
		}
		return res
	}
	return nil
}

func (a *AmbientIndexImpl) lookupService(key string) *model.ServiceInfo {
	// 1. namespace/hostname format
	s := a.services.GetKey(cv2.Key[model.ServiceInfo](key))
	if s != nil {
		return s
	}

	// 2. network/ip format
	network, ip, _ := strings.Cut(key, "/")
	// Maybe its a hostname..
	// TODO remove full scan
	for _, maybe := range a.services.List(metav1.NamespaceAll) {
		for _, addr := range maybe.Addresses {
			if network == addr.Network && ip == byteIPToString(addr.Address) {
				return &maybe
			}
		}
	}
	return nil
}

// All return all known workloads. Result is un-ordered
func (a *AmbientIndexImpl) All() []model.AddressInfo {
	res := []model.AddressInfo{}
	for _, wl := range a.workloads.List("") {
		res = append(res, workloadToAddressInfo(wl.Workload))
	}
	for _, s := range a.services.List("") {
		res = append(res, serviceToAddressInfo(s.Service))
	}
	return res
}

func (c *Controller) WorkloadsForWaypoint(scope model.WaypointScope) []model.WorkloadInfo {
	// Lookup scope. If its namespace wide, remove entries that are in SA scope
	workloads := c.ambientIndex.workloads.ByOwningWaypoint.Lookup(scope)
	if scope.ServiceAccount == "" {
		// TODO: find a way filter workloads that have a per-SA waypoint
	}
	return workloads
}

// Waypoint finds all waypoint IP addresses for a given scope.  Performs first a Namespace+ServiceAccount
// then falls back to any Namespace wide waypoints
func (c *Controller) Waypoint(scope model.WaypointScope) []netip.Addr {
	a := c.ambientIndex
	res := sets.Set[netip.Addr]{}
	waypoints := a.waypoints.ByScope.Lookup(scope)
	if len(waypoints) == 0 {
		// Now look for namespace-wide
		scope.ServiceAccount = ""
		waypoints = a.waypoints.ByScope.Lookup(scope)
	}
	for _, waypoint := range waypoints {
		res.Insert(waypoint.Addresses[0])
	}
	return res.UnsortedList()
}

func meshConfigMapData(cm *v1.ConfigMap) string {
	if cm == nil {
		return ""
	}

	cfgYaml, exists := cm.Data["mesh"]
	if !exists {
		return ""
	}

	return cfgYaml
}

type Waypoint struct {
	cv2.Named

	ForServiceAccount string
	Addresses         []netip.Addr
}

func (w Waypoint) ResourceName() string {
	return w.GetNamespace() + "/" + w.GetName()
}

func (c *Controller) setupIndex(options Options) *AmbientIndexImpl {
	ConfigMaps := cv2.NewInformer[*v1.ConfigMap](c.client)
	MeshConfig := cv2.NewSingleton[meshapi.MeshConfig](
		func(ctx cv2.HandlerContext) *meshapi.MeshConfig {
			meshCfg := mesh.DefaultMeshConfig()
			cms := []*v1.ConfigMap{}
			cms = cv2.AppendNonNil(cms, cv2.FetchOne(ctx, ConfigMaps, cv2.FilterName("istio-user", c.opts.SystemNamespace)))
			cms = cv2.AppendNonNil(cms, cv2.FetchOne(ctx, ConfigMaps, cv2.FilterName("istio", c.opts.SystemNamespace)))

			for _, c := range cms {
				n, err := mesh.ApplyMeshConfig(meshConfigMapData(c), meshCfg)
				if err != nil {
					log.Error(err)
					continue
				}
				meshCfg = n
			}
			return meshCfg
		},
	)
	AuthzPolicies := cv2.NewInformer[*securityclient.AuthorizationPolicy](c.client)
	PeerAuths := cv2.NewInformer[*securityclient.PeerAuthentication](c.client)
	Services := cv2.WrapClient[*v1.Service](c.services)
	Pods := cv2.WrapClient[*v1.Pod](c.podsClient)
	Waypoints := cv2.NewCollection(Services, func(ctx cv2.HandlerContext, svc *v1.Service) *Waypoint {
		if svc.Labels[constants.ManagedGatewayLabel] != constants.ManagedGatewayMeshControllerLabel {
			// not a waypoint
			return nil
		}
		sa := svc.Annotations[constants.WaypointServiceAccount]
		return &Waypoint{
			Named:             cv2.NewNamed(svc.ObjectMeta),
			ForServiceAccount: sa,
			Addresses:         getVIPAddrs(svc),
		}
	})
	AuthzDerivedPolicies := cv2.NewCollection(AuthzPolicies, func(ctx cv2.HandlerContext, i *securityclient.AuthorizationPolicy) *model.WorkloadAuthorization {
		meshCfg := cv2.FetchOne(ctx, MeshConfig.AsCollection())
		pol := convertAuthorizationPolicy(meshCfg.GetRootNamespace(), i)
		if pol == nil {
			return nil
		}
		return &model.WorkloadAuthorization{Authorization: pol, LabelSelector: model.NewSelector(i.Spec.GetSelector().GetMatchLabels())}
	})
	PeerAuthDerivedPolicies := cv2.NewCollection(PeerAuths, func(ctx cv2.HandlerContext, i *securityclient.PeerAuthentication) *model.WorkloadAuthorization {
		meshCfg := cv2.FetchOne(ctx, MeshConfig.AsCollection())
		pol := convertPeerAuthentication(meshCfg.GetRootNamespace(), i)
		if pol == nil {
			return nil
		}
		return &model.WorkloadAuthorization{
			Authorization: pol,
			LabelSelector: model.NewSelector(i.Spec.GetSelector().GetMatchLabels()),
		}
	})
	DefaultPolicy := cv2.NewSingleton[model.WorkloadAuthorization](func(ctx cv2.HandlerContext) *model.WorkloadAuthorization {
		if len(cv2.Fetch(ctx, PeerAuths)) == 0 {
			return nil
		}
		// If there are any PeerAuthentications in our cache, send our static STRICT policy
		return &model.WorkloadAuthorization{
			LabelSelector: model.LabelSelector{},
			Authorization: &security.Authorization{
				Name:      staticStrictPolicyName,
				Namespace: c.meshWatcher.Mesh().GetRootNamespace(),
				Scope:     security.Scope_WORKLOAD_SELECTOR,
				Action:    security.Action_DENY,
				Groups: []*security.Group{
					{
						Rules: []*security.Rules{
							{
								Matches: []*security.Match{
									{
										NotPrincipals: []*security.StringMatch{
											{
												MatchType: &security.StringMatch_Presence{},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}
	})

	// Policies contains all of the policies we will send down to clients
	Policies := cv2.JoinCollection(AuthzDerivedPolicies, PeerAuthDerivedPolicies, DefaultPolicy.AsCollection())
	Policies.RegisterBatch(func(events []cv2.Event[model.WorkloadAuthorization]) {
		cu := sets.New[model.ConfigKey]()
		for _, e := range events {
			for _, i := range e.Items() {
				cu.Insert(model.ConfigKey{Kind: kind.AuthorizationPolicy, Name: i.Authorization.Name, Namespace: i.Authorization.Namespace})
			}
		}
		log.Errorf("howardjohn: policy events: %+v", cu)
		c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
			Full:           false,
			ConfigsUpdated: cu,
			Reason:         model.NewReasonStats(model.AmbientUpdate),
		})
	})
	Workloads := cv2.NewCollection(Pods, func(ctx cv2.HandlerContext, p *v1.Pod) *model.WorkloadInfo {
		log := log.WithLabels("pod", p.Name)
		if !IsPodRunning(p) || p.Spec.HostNetwork {
			return nil
		}
		meshCfg := cv2.FetchOne(ctx, MeshConfig.AsCollection())
		// We need to filter from the policies that are present, which apply to us.
		// We only want label selector ones, we handle global ones through another mechanism.
		// In general we just take all ofthe policies
		basePolicies := cv2.Fetch(ctx, AuthzDerivedPolicies, cv2.FilterSelects(p.Labels), cv2.FilterGeneric(func(a any) bool {
			return a.(model.WorkloadAuthorization).GetLabelSelector() != nil
		}))
		policies := slices.Map(basePolicies, func(t model.WorkloadAuthorization) string {
			return t.ResourceName()
		})
		// We could do a non-FilterGeneric but cv2 currently blows up if we depend on the same collection twice
		auths := cv2.Fetch(ctx, PeerAuths, cv2.FilterGeneric(func(a any) bool {
			pol := a.(*securityclient.PeerAuthentication)
			if pol.Namespace == meshCfg.GetRootNamespace() && pol.Spec.Selector == nil {
				return true
			}
			if pol.Namespace != p.Namespace {
				return false
			}
			sel := pol.Spec.Selector
			if sel == nil {
				return true // No selector matches everything
			}
			return labels.Instance(sel.MatchLabels).SubsetOf(p.Labels)
		}))
		// auths := cv2.Fetch(ctx, PeerAuths, cv2.FilterNamespace(meshCfg.GetRootNamespace()))
		// auths = append(auths, cv2.Fetch(ctx, PeerAuths, cv2.FilterSelects(p.Labels), cv2.FilterNamespace(p.Namespace))...)
		policies = append(policies, c.convertedSelectorPeerAuthentications(meshCfg.GetRootNamespace(), auths)...)
		log.Errorf("howardjohn: policies %v", policies)
		//isEffectiveStrictPolicy := true
		//if isEffectiveStrictPolicy {
		//	// TODO: do not hardcode bool or istio-system. This will need the convertedSelectorPeerAuthentications logic.
		//	policies = append(policies, fmt.Sprintf("%s/%s", "istio-system", staticStrictPolicyName))
		//}
		log.Errorf("howardjohn: build workload with %v policies", len(policies))
		services := cv2.Fetch(ctx, Services, cv2.FilterSelects(p.GetLabels()))
		var waypoints []Waypoint
		if p.Labels[constants.ManagedGatewayLabel] != constants.ManagedGatewayMeshControllerLabel {
			// Waypoints do not have waypoints, but anything else does
			waypoints = cv2.Fetch(ctx, Waypoints,
				cv2.FilterNamespace(p.Namespace), cv2.FilterGeneric(func(a any) bool {
					w := a.(Waypoint)
					return w.ForServiceAccount == "" || w.ForServiceAccount == p.Spec.ServiceAccountName
				}))
		}
		status := workloadapi.WorkloadStatus_HEALTHY
		if !IsPodReady(p) {
			status = workloadapi.WorkloadStatus_UNHEALTHY
		}
		w := &workloadapi.Workload{
			Uid:                   c.generatePodUID(p),
			Name:                  p.Name,
			Namespace:             p.Namespace,
			Network:               c.network.String(),
			ClusterId:             string(c.Cluster()),
			Addresses:             [][]byte{netip.MustParseAddr(p.Status.PodIP).AsSlice()},
			ServiceAccount:        p.Spec.ServiceAccountName,
			Node:                  p.Spec.NodeName,
			Services:              c.constructServices(p, services),
			AuthorizationPolicies: policies,
			Status:                status,
		}
		if len(waypoints) > 0 {
			wp := waypoints[0]
			w.Waypoint = &workloadapi.GatewayAddress{
				Destination: &workloadapi.GatewayAddress_Address{
					Address: &workloadapi.NetworkAddress{
						Network: c.Network(wp.Addresses[0].String(), nil).String(),
						Address: wp.Addresses[0].AsSlice(),
					},
				},
				// TODO: look up the HBONE port instead of hardcoding it
				Port: 15008,
			}
		}

		if td := spiffe.GetTrustDomain(); td != "cluster.local" {
			w.TrustDomain = td
		}
		w.WorkloadName, w.WorkloadType = workloadNameAndType(p)
		w.CanonicalName, w.CanonicalRevision = kubelabels.CanonicalService(p.Labels, w.WorkloadName)

		if p.Annotations[constants.AmbientRedirection] == constants.AmbientRedirectionEnabled {
			// Configured for override
			w.TunnelProtocol = workloadapi.TunnelProtocol_HBONE
		}
		// Otherwise supports tunnel directly
		if model.SupportsTunnel(p.Labels, model.TunnelHTTP) {
			w.TunnelProtocol = workloadapi.TunnelProtocol_HBONE
			w.NativeTunnel = true
		}
		return &model.WorkloadInfo{Workload: w, Labels: p.Labels}
	})
	Workloads.RegisterBatch(func(events []cv2.Event[model.WorkloadInfo]) {
		cu := sets.New[model.ConfigKey]()
		for _, e := range events {
			for _, i := range e.Items() {
				cu.Insert(model.ConfigKey{Kind: kind.Address, Name: i.ResourceName()})
			}
		}
		c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
			Full:           false,
			ConfigsUpdated: cu,
			Reason:         model.NewReasonStats(model.AmbientUpdate),
		})
	})
	WorkloadServices := cv2.NewCollection(Services, func(ctx cv2.HandlerContext, s *v1.Service) *model.ServiceInfo {
		return &model.ServiceInfo{Service: c.constructService(s)}
	})
	WorkloadServices.RegisterBatch(func(events []cv2.Event[model.ServiceInfo]) {
		cu := sets.New[model.ConfigKey]()
		for _, e := range events {
			for _, i := range e.Items() {
				cu.Insert(model.ConfigKey{Kind: kind.Address, Name: i.ResourceName()})
			}
		}
		c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
			Full:           false,
			ConfigsUpdated: cu,
			Reason:         model.NewReasonStats(model.AmbientUpdate),
		})
	})

	WorkloadAddressIndex := cv2.CreateIndex[model.WorkloadInfo, networkAddress](Workloads, networkAddressFromWorkload)
	WorkloadServiceIndex := cv2.CreateIndex[model.WorkloadInfo, string](Workloads, func(o model.WorkloadInfo) []string {
		return maps.Keys(o.Services)
	})
	WorkloadWaypointIndex := cv2.CreateIndex[model.WorkloadInfo, model.WaypointScope](Workloads, func(w model.WorkloadInfo) []model.WaypointScope {
		// Filter out waypoints.
		if w.Labels[constants.ManagedGatewayLabel] == constants.ManagedGatewayMeshControllerLabel {
			return nil
		}
		// We can be a part of a service account waypoint, or a namespace waypoint
		return []model.WaypointScope{
			{
				Namespace:      w.Namespace,
				ServiceAccount: w.ServiceAccount,
			},
			{
				Namespace: w.Namespace,
			},
		}
	})
	WaypointIndex := cv2.CreateIndex[Waypoint, model.WaypointScope](Waypoints, func(w Waypoint) []model.WaypointScope {
		// We can be a part of a service account waypoint, or a namespace waypoint
		return []model.WaypointScope{{Namespace: w.Namespace, ServiceAccount: w.ForServiceAccount}}
	})
	return &AmbientIndexImpl{
		workloads: WorkloadsCollection{
			Collection:       Workloads,
			ByAddress:        WorkloadAddressIndex,
			ByServiceVIP:     WorkloadServiceIndex,
			ByOwningWaypoint: WorkloadWaypointIndex,
		},
		services: ServicesCollection{Collection: WorkloadServices},
		waypoints: WaypointsCollection{
			Collection: Waypoints,
			ByScope:    WaypointIndex,
		},
		authorizationPolicies: Policies,
	}
}

func (c *Controller) getPodsInService(svc *v1.Service) []*v1.Pod {
	if svc.Spec.Selector == nil {
		// services with nil selectors match nothing, not everything.
		return nil
	}
	return c.podsClient.List(svc.Namespace, klabels.ValidatedSetSelector(svc.Spec.Selector))
}

// AddressInformation returns all AddressInfo's in the cluster.
// This may be scoped to specific subsets by specifying a non-empty addresses field
func (c *Controller) AddressInformation(addresses sets.String) ([]model.AddressInfo, []string) {
	if len(addresses) == 0 {
		// Full update
		return c.ambientIndex.All(), nil
	}
	var res []model.AddressInfo
	var removed []string
	for wname := range addresses {
		wl := c.ambientIndex.Lookup(wname)
		if len(wl) == 0 {
			removed = append(removed, wname)
		} else {
			res = append(res, wl...)
		}
	}
	return res, removed
}

func (c *Controller) hostname(svc *v1.Service) string {
	return string(kube.ServiceHostname(svc.Name, svc.Namespace, c.opts.DomainSuffix))
}

func (c *Controller) namespacedHostname(svc *v1.Service) string {
	return namespacedHostname(svc.Namespace, c.hostname(svc))
}

func namespacedHostname(namespace, hostname string) string {
	return namespace + "/" + hostname
}

func (c *Controller) constructServices(p *v1.Pod, services []*v1.Service) map[string]*workloadapi.PortList {
	res := map[string]*workloadapi.PortList{}
	for _, svc := range services {
		n := c.namespacedHostname(svc)
		if res[n] == nil {
			res[n] = &workloadapi.PortList{}
		}
		for _, port := range svc.Spec.Ports {
			if port.Protocol != v1.ProtocolTCP {
				continue
			}
			targetPort, err := FindPort(p, &port)
			if err != nil {
				continue
			}
			res[n].Ports = append(res[n].Ports, &workloadapi.Port{
				ServicePort: uint32(port.Port),
				TargetPort:  uint32(targetPort),
			})
		}
	}
	return res
}

func networkAddressFromWorkload(wl model.WorkloadInfo) []networkAddress {
	networkAddrs := make([]networkAddress, 0, len(wl.Addresses))
	for _, addr := range wl.Addresses {
		ip, _ := netip.AddrFromSlice(addr)
		networkAddrs = append(networkAddrs, networkAddress{network: wl.Network, ip: ip.String()})
	}
	return networkAddrs
}

// internal object used for indexing in ambientindex maps
type networkAddress struct {
	network string
	ip      string
}

func (n *networkAddress) String() string {
	return n.network + "/" + n.ip
}

func getVIPs(svc *v1.Service) []string {
	res := []string{}
	if svc.Spec.ClusterIP != "" && svc.Spec.ClusterIP != v1.ClusterIPNone {
		res = append(res, svc.Spec.ClusterIP)
	}
	for _, ing := range svc.Status.LoadBalancer.Ingress {
		res = append(res, ing.IP)
	}
	return res
}

func getVIPAddrs(svc *v1.Service) []netip.Addr {
	return slices.Map(getVIPs(svc), func(e string) netip.Addr {
		return netip.MustParseAddr(e)
	})
}

func (c *Controller) AdditionalPodSubscriptions(
	proxy *model.Proxy,
	allAddresses sets.String,
	currentSubs sets.String,
) sets.String {
	shouldSubscribe := sets.New[string]()

	// First, we want to handle VIP subscriptions. Example:
	// Client subscribes to VIP1. Pod1, part of VIP1, is sent.
	// The client wouldn't be explicitly subscribed to Pod1, so it would normally ignore it.
	// Since it is a part of VIP1 which we are subscribe to, add it to the subscriptions
	for addr := range allAddresses {
		for _, wl := range model.ExtractWorkloadsFromAddresses(c.ambientIndex.Lookup(addr)) {
			// We may have gotten an update for Pod, but are subscribed to a Service.
			// We need to force a subscription on the Pod as well
			for namespacedHostname := range wl.Services {
				if currentSubs.Contains(namespacedHostname) {
					shouldSubscribe.Insert(wl.ResourceName())
					break
				}
			}
		}
	}

	// Next, as an optimization, we will send all node-local endpoints
	if nodeName := proxy.Metadata.NodeName; nodeName != "" {
		for _, wl := range model.ExtractWorkloadsFromAddresses(c.ambientIndex.All()) {
			if wl.Node == nodeName {
				n := wl.ResourceName()
				if currentSubs.Contains(n) {
					continue
				}
				shouldSubscribe.Insert(n)
			}
		}
	}

	return shouldSubscribe
}

func workloadNameAndType(pod *v1.Pod) (string, workloadapi.WorkloadType) {
	objMeta, typeMeta := kubeutil.GetDeployMetaFromPod(pod)
	switch typeMeta.Kind {
	case "Deployment":
		return objMeta.Name, workloadapi.WorkloadType_DEPLOYMENT
	case "Job":
		return objMeta.Name, workloadapi.WorkloadType_JOB
	case "CronJob":
		return objMeta.Name, workloadapi.WorkloadType_CRONJOB
	default:
		return pod.Name, workloadapi.WorkloadType_POD
	}
}

func byteIPToString(b []byte) string {
	ip, _ := netip.AddrFromSlice(b)
	return ip.String()
}

func (c *Controller) constructService(svc *v1.Service) *workloadapi.Service {
	ports := make([]*workloadapi.Port, 0, len(svc.Spec.Ports))
	for _, p := range svc.Spec.Ports {
		ports = append(ports, &workloadapi.Port{
			ServicePort: uint32(p.Port),
			TargetPort:  uint32(p.TargetPort.IntVal),
		})
	}

	// TODO this is only checking one controller - we may be missing service vips for instances in another cluster
	vips := getVIPs(svc)
	addrs := make([]*workloadapi.NetworkAddress, 0, len(vips))
	for _, vip := range vips {
		addrs = append(addrs, &workloadapi.NetworkAddress{
			Network: c.Network(vip, make(labels.Instance, 0)).String(),
			Address: netip.MustParseAddr(vip).AsSlice(),
		})
	}
	return &workloadapi.Service{
		Name:      svc.Name,
		Namespace: svc.Namespace,
		Hostname:  string(kube.ServiceHostname(svc.Name, svc.Namespace, c.opts.DomainSuffix)),
		Addresses: addrs,
		Ports:     ports,
	}
}

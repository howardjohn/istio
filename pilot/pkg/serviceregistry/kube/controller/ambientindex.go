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
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/workloadapi"
)

// AmbientIndex maintains an index of ambient WorkloadInfo objects by various keys.
// These are intentionally pre-computed based on events such that lookups are efficient.
type AmbientIndex struct {
	services              cv2.Collection[model.ServiceInfo]
	workloads             cv2.Collection[model.WorkloadInfo]
	workloadServicesIndex *cv2.Index[model.WorkloadInfo, string]
	workloadWaypointIndex *cv2.Index[model.WorkloadInfo, model.WaypointScope]
	waypointIndex         *cv2.Index[model.WorkloadInfo, model.WaypointScope]

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
	return c.clusterID.String() + "//" + "v1/pod/" + p.Namespace + "/" + p.Name
}

// Lookup finds a given IP address.
func (a *AmbientIndex) Lookup(key string) []model.AddressInfo {
	if w := a.workloads.GetKey(cv2.Key[model.WorkloadInfo](key)); w != nil {
		return []model.AddressInfo{workloadToAddressInfo(w.Workload)}
	}
	if svc := a.lookupService(key); svc != nil {
		vips := sets.New[string]()
		for _, addr := range svc.Service.Addresses {
			vips.Insert(byteIPToString(addr.Address))
		}
		res := []model.AddressInfo{serviceToAddressInfo(svc.Service)}
		// TODO: avoid full scan
		for _, wl := range a.workloads.List(metav1.NamespaceAll) {
			for vip := range wl.VirtualIps {
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

func (a *AmbientIndex) lookupService(key string) *model.ServiceInfo {
	_, name, _ := strings.Cut(key, "/")
	s := a.services.GetKey(cv2.Key[model.ServiceInfo](key))
	if s != nil {
		return s
	}

	// Maybe its a hostname..
	// TODO remove full scan
	for _, maybe := range a.services.List(metav1.NamespaceAll) {
		if name == maybe.Hostname {
			return &maybe
		}
	}
	return nil
}

// All return all known workloads. Result is un-ordered
func (a *AmbientIndex) All() []model.AddressInfo {
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
	workloads := c.ambientIndex.workloadWaypointIndex.Lookup(scope)
	if scope.ServiceAccount == "" {
		// TODO: find a way filter workloads that have a per-SA waypoint
	}
	return workloads
}

// Waypoint finds all waypoint IP addresses for a given scope.  Performs first a Namespace+ServiceAccount
// then falls back to any Namespace wide waypoints
func (c *Controller) Waypoint(scope model.WaypointScope) []netip.Addr {
	//a := c.ambientIndex
	//res := sets.Set[netip.Addr]{}
	//waypoints := a.waypointIndex.Lookup(scope)
	//if len(waypoints) == 0 {
	//	// Now look for namespace-wide
	//	scope.ServiceAccount = ""
	//	waypoints = a.waypointIndex.Lookup(scope)
	//}
	//for _, waypoint := range waypoints {
	//	addr, _ := netip.AddrFromSlice(waypoint.Address)
	//	res.Insert(addr)
	//}
	return nil
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
	Address           []byte
}

func (w Waypoint) ResourceName() string {
	return w.GetNamespace() + "/" + w.GetName()
}

func (c *Controller) setupIndex() *AmbientIndex {
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
	Services := cv2.NewInformer[*v1.Service](c.client)
	Pods := cv2.NewInformer[*v1.Pod](c.client)
	Waypoints := cv2.NewCollection(Pods, func(ctx cv2.HandlerContext, p *v1.Pod) *Waypoint {
		if !IsPodReady(p) {
			return nil
		}
		if p.Labels[constants.ManagedGatewayLabel] != constants.ManagedGatewayMeshControllerLabel {
			// not a waypoint
			return nil
		}
		addr, err := netip.ParseAddr(p.Status.PodIP)
		if err != nil {
			return nil
		}
		sa := p.Annotations[constants.WaypointServiceAccount]
		return &Waypoint{
			ForServiceAccount: sa,
			Named:             cv2.NewNamed(p.ObjectMeta),
			Address:           addr.AsSlice(),
		}
	})
	Policies := cv2.NewCollection(AuthzPolicies, func(ctx cv2.HandlerContext, i *securityclient.AuthorizationPolicy) *model.WorkloadAuthorization {
		meshCfg := cv2.FetchOne(ctx, MeshConfig.AsCollection())
		pol := convertAuthorizationPolicy(meshCfg.GetRootNamespace(), i)
		if pol == nil {
			return nil
		}
		return &model.WorkloadAuthorization{Authorization: pol, LabelSelector: model.NewSelector(i.Spec.GetSelector().GetMatchLabels())}
	})
	Workloads := cv2.NewCollection(Pods, func(ctx cv2.HandlerContext, p *v1.Pod) *model.WorkloadInfo {
		// TODO:
		// * Test updating a pod attributes used as filter
		// * Create passthrough Filter type, on the fly filtering for the discovery thing
		// * Race tests
		// * inconsistent state for event - we must not be cleaning up properly (or racy)
		if !IsPodReady(p) {
			return nil
		}
		policies := cv2.Fetch(ctx, Policies, cv2.FilterSelects(p.Labels), cv2.FilterGeneric(func(a any) bool {
			// We only want label selector ones, we handle global ones through another mechanism
			return a.(model.WorkloadAuthorization).GetLabelSelector() != nil
		}))
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
		// TODO: if there are any SA waypoints, drop SA namespaces
		w := &workloadapi.Workload{
			Uid:            c.generatePodUID(p),
			Name:           p.Name,
			Namespace:      p.Namespace,
			Network:        c.network.String(),
			ClusterId:      string(c.Cluster()),
			Addresses:      [][]byte{netip.MustParseAddr(p.Status.PodIP).AsSlice()},
			ServiceAccount: p.Spec.ServiceAccountName,
			Node:           p.Spec.NodeName,
			VirtualIps:     constructVIPs(p, services),
			AuthorizationPolicies: cv2.Map(policies, func(t model.WorkloadAuthorization) string {
				return t.ResourceName()
			}),
		}
		if len(waypoints) > 0 {
			wp := waypoints[0].Address
			w.Waypoint = &workloadapi.GatewayAddress{
				Destination: &workloadapi.GatewayAddress_Address{
					Address: &workloadapi.NetworkAddress{
						Network: c.Network(byteIPToString(wp), nil).String(),
						Address: wp,
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
		return &model.WorkloadInfo{Workload: w, Annotations: p.Annotations, Labels: p.Labels}
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
			Reason:         []model.TriggerReason{model.AmbientUpdate},
		})
	})
	WorkloadServices := cv2.NewCollection(Services, func(ctx cv2.HandlerContext, s *v1.Service) *model.ServiceInfo {
		return &model.ServiceInfo{Service: c.constructService(s)}
	})

	WorkloadServiceIndex := cv2.CreateIndex[model.WorkloadInfo, string](Workloads, func(o model.WorkloadInfo) []string {
		return maps.Keys(o.VirtualIps)
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
	WaypointIndex := cv2.CreateIndex[model.WorkloadInfo, model.WaypointScope](Workloads, func(w model.WorkloadInfo) []model.WaypointScope {
		// Only include waypoints
		if w.Labels[constants.ManagedGatewayLabel] != constants.ManagedGatewayMeshControllerLabel {
			return nil
		}
		// We can be a part of a service account waypoint, or a namespace waypoint
		return []model.WaypointScope{{Namespace: w.Namespace, ServiceAccount: w.Annotations[constants.WaypointServiceAccount]}}
	})
	return &AmbientIndex{
		workloads:             Workloads,
		services:              WorkloadServices,
		workloadServicesIndex: WorkloadServiceIndex,
		workloadWaypointIndex: WorkloadWaypointIndex,
		waypointIndex:         WaypointIndex,
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

func constructVIPs(p *v1.Pod, services []*v1.Service) map[string]*workloadapi.PortList {
	vips := map[string]*workloadapi.PortList{}
	for _, svc := range services {
		for _, vip := range getVIPs(svc) {
			if vips[vip] == nil {
				vips[vip] = &workloadapi.PortList{}
			}
			for _, port := range svc.Spec.Ports {
				if port.Protocol != v1.ProtocolTCP {
					continue
				}
				targetPort, err := FindPort(p, &port)
				if err != nil {
					log.Debug(err)
					continue
				}
				vips[vip].Ports = append(vips[vip].Ports, &workloadapi.Port{
					ServicePort: uint32(port.Port),
					TargetPort:  uint32(targetPort),
				})
			}
		}
	}
	return vips
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
			// We may have gotten an update for Pod, but are subscribe to a Service.
			// We need to force a subscription on the Pod as well
			for vip := range wl.VirtualIps {
				if currentSubs.Contains(vip) {
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

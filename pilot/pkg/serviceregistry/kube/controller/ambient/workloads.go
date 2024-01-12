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

// nolint: gocritic
package ambient

import (
	"net/netip"

	v1 "k8s.io/api/core/v1"

	networkingv1alpha3 "istio.io/api/networking/v1alpha3"
	networkingclient "istio.io/client-go/pkg/apis/networking/v1alpha3"
	securityclient "istio.io/client-go/pkg/apis/security/v1beta1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/kind"
	kubeutil "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/krt"
	kubelabels "istio.io/istio/pkg/kube/labels"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/workloadapi"
)

func (a *index) WorkloadsCollection(
	Pods krt.Collection[*v1.Pod],
	MeshConfig krt.Singleton[MeshConfig],
	AuthorizationPolicies krt.Collection[model.WorkloadAuthorization],
	PeerAuths krt.Collection[*securityclient.PeerAuthentication],
	Waypoints krt.Collection[Waypoint],
	WorkloadServices krt.Collection[model.ServiceInfo],
	WorkloadEntries krt.Collection[*networkingclient.WorkloadEntry],
	ServiceEntries krt.Collection[*networkingclient.ServiceEntry],
	AllPolicies krt.Collection[model.WorkloadAuthorization],
) krt.Collection[model.WorkloadInfo] {
	PodWorkloads := krt.NewCollection(Pods, func(ctx krt.HandlerContext, p *v1.Pod) *model.WorkloadInfo {
		if !IsPodRunning(p) || p.Spec.HostNetwork {
			return nil
		}
		meshCfg := krt.FetchOne(ctx, MeshConfig.AsCollection())
		// We need to filter from the policies that are present, which apply to us.
		// We only want label selector ones, we handle global ones through another mechanism.
		// In general we just take all ofthe policies
		basePolicies := krt.Fetch(ctx, AuthorizationPolicies, krt.FilterSelects(p.Labels), krt.FilterGeneric(func(a any) bool {
			return a.(model.WorkloadAuthorization).GetLabelSelector() != nil
		}))
		policies := slices.Sort(slices.Map(basePolicies, func(t model.WorkloadAuthorization) string {
			return t.ResourceName()
		}))
		// We could do a non-FilterGeneric but krt currently blows up if we depend on the same collection twice
		auths := krt.Fetch(ctx, PeerAuths, krt.FilterGeneric(func(a any) bool {
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
		policies = append(policies, a.convertedSelectorPeerAuthentications(meshCfg.GetRootNamespace(), auths)...)
		var waypoints []Waypoint
		if p.Labels[constants.ManagedGatewayLabel] != constants.ManagedGatewayMeshControllerLabel {
			// Waypoints do not have waypoints, but anything else does
			waypoints = krt.Fetch(ctx, Waypoints,
				krt.FilterNamespace(p.Namespace), krt.FilterGeneric(func(a any) bool {
					w := a.(Waypoint)
					return w.ForServiceAccount == "" || w.ForServiceAccount == p.Spec.ServiceAccountName
				}))
		}
		fo := []krt.FetchOption{krt.FilterNamespace(p.Namespace), krt.FilterSelectsNonEmpty(p.GetLabels())}
		if !features.EnableServiceEntrySelectPods {
			fo = append(fo, krt.FilterGeneric(func(a any) bool {
				return a.(model.ServiceInfo).Source == kind.Service
			}))
		}
		services := krt.Fetch(ctx, WorkloadServices, fo...)
		status := workloadapi.WorkloadStatus_HEALTHY
		if !IsPodReady(p) {
			status = workloadapi.WorkloadStatus_UNHEALTHY
		}
		a.networkUpdateTrigger.MarkDependant(ctx) // Mark we depend on out of band a.Network
		network := a.Network(p.Status.PodIP, p.Labels).String()
		w := &workloadapi.Workload{
			Uid:                   a.generatePodUID(p),
			Name:                  p.Name,
			Namespace:             p.Namespace,
			Network:               network,
			ClusterId:             string(a.ClusterID),
			Addresses:             [][]byte{netip.MustParseAddr(p.Status.PodIP).AsSlice()},
			ServiceAccount:        p.Spec.ServiceAccountName,
			Node:                  p.Spec.NodeName,
			Services:              a.constructServices(p, services),
			AuthorizationPolicies: policies,
			Status:                status,
		}
		if len(waypoints) > 0 {
			// Pick the best waypoint. One scoped to a SA is higher precedence
			wp := ptr.OrDefault(
				slices.FindFunc(waypoints, func(waypoint Waypoint) bool {
					return waypoint.ForServiceAccount != ""
				}),
				waypoints[0],
			)
			w.Waypoint = &workloadapi.GatewayAddress{
				Destination: &workloadapi.GatewayAddress_Address{
					Address: a.toNetworkAddress(wp.Addresses[0].String()),
				},
				// TODO: look up the HBONE port instead of hardcoding it
				HboneMtlsPort: 15008,
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
		return &model.WorkloadInfo{Workload: w, Labels: p.Labels, Source: kind.Pod}
	}, krt.WithName("PodWorkloads"))
	WorkloadEntryWorkloads := krt.NewCollection(WorkloadEntries, func(ctx krt.HandlerContext, p *networkingclient.WorkloadEntry) *model.WorkloadInfo {
		meshCfg := krt.FetchOne(ctx, MeshConfig.AsCollection())
		// We need to filter from the policies that are present, which apply to us.
		// We only want label selector ones, we handle global ones through another mechanism.
		// In general we just take all ofthe policies
		basePolicies := krt.Fetch(ctx, AuthorizationPolicies, krt.FilterSelects(p.Labels), krt.FilterGeneric(func(a any) bool {
			return a.(model.WorkloadAuthorization).GetLabelSelector() != nil
		}))
		policies := slices.Sort(slices.Map(basePolicies, func(t model.WorkloadAuthorization) string {
			return t.ResourceName()
		}))
		// We could do a non-FilterGeneric but krt currently blows up if we depend on the same collection twice
		auths := krt.Fetch(ctx, PeerAuths, krt.FilterGeneric(func(a any) bool {
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
		policies = append(policies, a.convertedSelectorPeerAuthentications(meshCfg.GetRootNamespace(), auths)...)
		var waypoints []Waypoint
		if p.Labels[constants.ManagedGatewayLabel] != constants.ManagedGatewayMeshControllerLabel {
			// Waypoints do not have waypoints, but anything else does
			waypoints = krt.Fetch(ctx, Waypoints,
				krt.FilterNamespace(p.Namespace), krt.FilterGeneric(func(a any) bool {
					w := a.(Waypoint)
					return w.ForServiceAccount == "" || w.ForServiceAccount == p.Spec.ServiceAccount
				}))
		}
		fo := []krt.FetchOption{krt.FilterNamespace(p.Namespace), krt.FilterSelectsNonEmpty(p.GetLabels())}
		if !features.EnableK8SServiceSelectWorkloadEntries {
			fo = append(fo, krt.FilterGeneric(func(a any) bool {
				return a.(model.ServiceInfo).Source == kind.ServiceEntry
			}))
		}
		services := krt.Fetch(ctx, WorkloadServices, fo...)
		a.networkUpdateTrigger.MarkDependant(ctx) // Mark we depend on out of band a.Network
		network := a.Network(p.Spec.Address, p.Labels).String()
		if p.Spec.Network != "" {
			network = p.Spec.Network
		}
		w := &workloadapi.Workload{
			Uid:                   a.generateWorkloadEntryUID(p.Namespace, p.Name),
			Name:                  p.Name,
			Namespace:             p.Namespace,
			Network:               network,
			ClusterId:             string(a.ClusterID),
			ServiceAccount:        p.Spec.ServiceAccount,
			Services:              a.constructServicesFromWorkloadEntry(&p.Spec, services),
			AuthorizationPolicies: policies,
			Status:                workloadapi.WorkloadStatus_HEALTHY, // TODO: WE can be unhealthy
		}

		if addr, err := netip.ParseAddr(p.Spec.Address); err == nil {
			w.Addresses = [][]byte{addr.AsSlice()}
		} else {
			log.Warnf("skipping workload entry %s/%s; DNS Address resolution is not yet implemented", p.Namespace, p.Name)
		}
		if len(waypoints) > 0 {
			wp := waypoints[0]
			w.Waypoint = &workloadapi.GatewayAddress{
				Destination: &workloadapi.GatewayAddress_Address{
					Address: a.toNetworkAddress(wp.Addresses[0].String()),
				},
				// TODO: look up the HBONE port instead of hardcoding it
				HboneMtlsPort: 15008,
			}
		}

		if td := spiffe.GetTrustDomain(); td != "cluster.local" {
			w.TrustDomain = td
		}
		w.WorkloadName, w.WorkloadType = p.Name, workloadapi.WorkloadType_POD // XXX(shashankram): HACK to impersonate pod
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
		return &model.WorkloadInfo{Workload: w, Labels: p.Labels, Source: kind.WorkloadEntry}
	}, krt.WithName("WorkloadEntryWorkloads"))
	ServiceEntryWorkloads := krt.NewManyCollection(ServiceEntries, func(ctx krt.HandlerContext, se *networkingclient.ServiceEntry) []model.WorkloadInfo {
		if len(se.Spec.Endpoints) == 0 {
			return nil
		}
		res := make([]model.WorkloadInfo, 0, len(se.Spec.Endpoints))

		svc := slices.First(a.serviceEntriesInfo(se))
		if svc == nil {
			// Not ready yet
			return nil
		}
		for _, p := range se.Spec.Endpoints {
			meshCfg := krt.FetchOne(ctx, MeshConfig.AsCollection())
			// We need to filter from the policies that are present, which apply to us.
			// We only want label selector ones, we handle global ones through another mechanism.
			// In general we just take all ofthe policies
			basePolicies := krt.Fetch(ctx, AllPolicies, krt.FilterSelects(se.Labels), krt.FilterGeneric(func(a any) bool {
				return a.(model.WorkloadAuthorization).GetLabelSelector() != nil
			}))
			policies := slices.Sort(slices.Map(basePolicies, func(t model.WorkloadAuthorization) string {
				return t.ResourceName()
			}))
			// We could do a non-FilterGeneric but krt currently blows up if we depend on the same collection twice
			auths := krt.Fetch(ctx, PeerAuths, krt.FilterGeneric(func(a any) bool {
				pol := a.(*securityclient.PeerAuthentication)
				if pol.Namespace == meshCfg.GetRootNamespace() && pol.Spec.Selector == nil {
					return true
				}
				if pol.Namespace != se.Namespace {
					return false
				}
				sel := pol.Spec.Selector
				if sel == nil {
					return true // No selector matches everything
				}
				return labels.Instance(sel.MatchLabels).SubsetOf(p.Labels)
			}))
			policies = append(policies, a.convertedSelectorPeerAuthentications(meshCfg.GetRootNamespace(), auths)...)
			var waypoints []Waypoint
			if p.Labels[constants.ManagedGatewayLabel] != constants.ManagedGatewayMeshControllerLabel {
				// Waypoints do not have waypoints, but anything else does
				waypoints = krt.Fetch(ctx, Waypoints,
					krt.FilterNamespace(se.Namespace), krt.FilterGeneric(func(a any) bool {
						w := a.(Waypoint)
						return w.ForServiceAccount == "" || w.ForServiceAccount == p.ServiceAccount
					}))
			}
			a.networkUpdateTrigger.MarkDependant(ctx) // Mark we depend on out of band a.Network
			network := a.Network(p.Address, p.Labels).String()
			if p.Network != "" {
				network = p.Network
			}
			w := &workloadapi.Workload{
				Uid:                   a.generateServiceEntryUID(se.Namespace, se.Name, p.Address),
				Name:                  se.Name,
				Namespace:             se.Namespace,
				Network:               network,
				ClusterId:             string(a.ClusterID),
				ServiceAccount:        p.ServiceAccount,
				Services:              a.constructServicesFromWorkloadEntry(p, []model.ServiceInfo{*svc}),
				AuthorizationPolicies: policies,
				Status:                workloadapi.WorkloadStatus_HEALTHY, // TODO: WE can be unhealthy
			}

			if addr, err := netip.ParseAddr(p.Address); err == nil {
				w.Addresses = [][]byte{addr.AsSlice()}
			} else {
				log.Warnf("skipping workload entry %s/%s; DNS Address resolution is not yet implemented", se.Namespace, se.Name)
			}
			if len(waypoints) > 0 {
				wp := waypoints[0]
				w.Waypoint = &workloadapi.GatewayAddress{
					Destination: &workloadapi.GatewayAddress_Address{
						Address: a.toNetworkAddress(wp.Addresses[0].String()),
					},
					// TODO: look up the HBONE port instead of hardcoding it
					HboneMtlsPort: 15008,
				}
			}

			if td := spiffe.GetTrustDomain(); td != "cluster.local" {
				w.TrustDomain = td
			}
			w.WorkloadName, w.WorkloadType = se.Name, workloadapi.WorkloadType_POD // XXX(shashankram): HACK to impersonate pod
			w.CanonicalName, w.CanonicalRevision = kubelabels.CanonicalService(se.Labels, w.WorkloadName)

			if se.Annotations[constants.AmbientRedirection] == constants.AmbientRedirectionEnabled {
				// Configured for override
				w.TunnelProtocol = workloadapi.TunnelProtocol_HBONE
			}
			// Otherwise supports tunnel directly
			if model.SupportsTunnel(se.Labels, model.TunnelHTTP) {
				w.TunnelProtocol = workloadapi.TunnelProtocol_HBONE
				w.NativeTunnel = true
			}
			res = append(res, model.WorkloadInfo{Workload: w, Labels: se.Labels, Source: kind.WorkloadEntry})
		}
		return res
	}, krt.WithName("ServiceEntryWorkloads"))
	Workloads := krt.JoinCollection([]krt.Collection[model.WorkloadInfo]{PodWorkloads, WorkloadEntryWorkloads, ServiceEntryWorkloads}, krt.WithName("Workloads"))
	return Workloads
}

func (a *index) constructServicesFromWorkloadEntry(p *networkingv1alpha3.WorkloadEntry, services []model.ServiceInfo) map[string]*workloadapi.PortList {
	res := map[string]*workloadapi.PortList{}
	for _, svc := range services {
		n := namespacedHostname(svc.Namespace, svc.Hostname)
		pl := &workloadapi.PortList{}
		res[n] = pl
		for _, port := range svc.Ports {
			targetPort := port.TargetPort
			if named, f := svc.PortNames[int32(port.ServicePort)]; f {
				// get port name or target port
				nv, nf := p.Ports[named.PortName]
				tv, tf := p.Ports[named.TargetPortName]
				// TODO: is this logic/order correct?
				if tf {
					targetPort = tv
				} else if nf {
					targetPort = nv
				} else if named.TargetPortName != "" {
					// We needed an explicit port, but didn't find one - skip this port
					continue
				}
			}
			pl.Ports = append(pl.Ports, &workloadapi.Port{
				ServicePort: port.ServicePort,
				TargetPort:  targetPort,
			})
		}
	}
	return res
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

func (a *index) constructServices(p *v1.Pod, services []model.ServiceInfo) map[string]*workloadapi.PortList {
	res := map[string]*workloadapi.PortList{}
	for _, svc := range services {
		n := namespacedHostname(svc.Namespace, svc.Hostname)
		pl := &workloadapi.PortList{}
		res[n] = pl
		for _, port := range svc.Ports {
			targetPort := port.TargetPort
			if named, f := svc.PortNames[int32(port.ServicePort)]; f && named.TargetPortName != "" {
				// Pods only match on TargetPort names
				tp, ok := FindPortName(p, named.TargetPortName)
				if !ok {
					// Port not present for this workload
					continue
				}
				targetPort = uint32(tp)
			}

			pl.Ports = append(pl.Ports, &workloadapi.Port{
				ServicePort: port.ServicePort,
				TargetPort:  targetPort,
			})
		}
	}
	return res
}

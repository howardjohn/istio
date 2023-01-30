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
	"reflect"
	"strconv"
	"strings"

	"github.com/hashicorp/go-multierror"
	"golang.org/x/exp/maps"
	meshapi "istio.io/api/mesh/v1alpha1"
	"istio.io/api/security/v1beta1"
	securityclient "istio.io/client-go/pkg/apis/security/v1beta1"
	"istio.io/istio/pilot/pkg/ambient/ambientpod"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube/cv2"
	kubelabels "istio.io/istio/pkg/kube/labels"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/workloadapi"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	listerv1 "k8s.io/client-go/listers/core/v1"
)

// AmbientIndex maintains an index of ambient WorkloadInfo objects by various keys.
// These are intentionally pre-computed based on events such that lookups are efficient.
type AmbientIndex struct {
	workloads             cv2.Collection[model.WorkloadInfo]
	workloadServicesIndex *cv2.Index[model.WorkloadInfo, string]
}

// Lookup finds a given IP address.
func (a *AmbientIndex) Lookup(ip string) []*model.WorkloadInfo {
	res := a.workloads.GetKey(cv2.Key[model.WorkloadInfo](ip))
	if res != nil {
		return []*model.WorkloadInfo{res}
	}
	return cv2.Map(a.workloadServicesIndex.Lookup(ip), cv2.Ptr[model.WorkloadInfo])

}

// All return all known workloads. Result is un-ordered
func (a *AmbientIndex) All() []*model.WorkloadInfo {
	return cv2.Map(a.workloads.List(metav1.NamespaceAll), cv2.Ptr[model.WorkloadInfo])
}

func (c *Controller) Policies(requested sets.Set[model.ConfigKey]) []*workloadapi.Authorization {
	cfgs, err := c.configController.List(gvk.AuthorizationPolicy, metav1.NamespaceAll)
	if err != nil {
		log.Warnf("failed to list policies")
		return nil
	}
	l := len(cfgs)
	if len(requested) > 0 {
		l = len(requested)
	}
	res := make([]*workloadapi.Authorization, 0, l)
	for _, cfg := range cfgs {
		k := model.ConfigKey{
			Kind:      kind.AuthorizationPolicy,
			Name:      cfg.Name,
			Namespace: cfg.Namespace,
		}
		if len(requested) > 0 && !requested.Contains(k) {
			continue
		}
		pol := convertAuthorizationPolicy(c.meshWatcher.Mesh().GetRootNamespace(), cfg)
		if pol == nil {
			continue
		}
		res = append(res, pol)
	}
	return res
}

func isNil(v interface{}) bool {
	return v == nil || (reflect.ValueOf(v).Kind() == reflect.Ptr && reflect.ValueOf(v).IsNil())
}

func (c *Controller) selectorAuthorizationPolicies(ns string, lbls map[string]string) []string {
	// Since this is an interface a normal nil check doesn't work (...)
	if isNil(c.configController) {
		return nil
	}
	global, err := c.configController.List(gvk.AuthorizationPolicy, c.meshWatcher.Mesh().GetRootNamespace())
	if err != nil {
		return nil
	}
	local, err := c.configController.List(gvk.AuthorizationPolicy, ns)
	if err != nil {
		return nil
	}
	res := sets.New[string]()
	matches := func(c config.Config) bool {
		sel := c.Spec.(*v1beta1.AuthorizationPolicy).Selector
		if sel == nil {
			return false
		}
		return labels.Instance(sel.MatchLabels).SubsetOf(lbls)
	}

	for _, pl := range [][]config.Config{global, local} {
		for _, p := range pl {
			if matches(p) {
				res.Insert(p.Namespace + "/" + p.Name)
			}
		}
	}
	return sets.SortedList(res)
}

func (c *Controller) getPodsInPolicy(ns string, sel map[string]string) []*v1.Pod {
	if ns == c.meshWatcher.Mesh().GetRootNamespace() {
		ns = metav1.NamespaceAll
	}
	allPods, err := c.podLister.Pods(ns).List(klabels.Everything())
	if err != nil {
		return nil
	}
	var pods []*v1.Pod
	for _, pod := range allPods {
		if labels.Instance(sel).SubsetOf(pod.Labels) {
			pods = append(pods, pod)
		}
	}

	return pods
}

func convertAuthorizationPolicy(rootns string, obj config.Config) *workloadapi.Authorization {
	pol := obj.Spec.(*v1beta1.AuthorizationPolicy)

	scope := workloadapi.Scope_WORKLOAD_SELECTOR
	if pol.Selector == nil {
		scope = workloadapi.Scope_NAMESPACE
		// TODO: TDA
		if rootns == obj.Namespace {
			scope = workloadapi.Scope_GLOBAL // TODO: global workload?
		}
	}
	action := workloadapi.Action_ALLOW
	switch pol.Action {
	case v1beta1.AuthorizationPolicy_ALLOW:
	case v1beta1.AuthorizationPolicy_DENY:
		action = workloadapi.Action_DENY
	default:
		return nil
	}
	opol := &workloadapi.Authorization{
		Name:      obj.Name,
		Namespace: obj.Namespace,
		Scope:     scope,
		Action:    action,
		Groups:    nil,
	}

	for _, rule := range pol.Rules {
		rules := handleRule(action, rule)
		if rules != nil {
			rg := &workloadapi.Group{
				Rules: rules,
			}
			opol.Groups = append(opol.Groups, rg)
		}
	}

	return opol
}

func anyNonEmpty[T any](arr ...[]T) bool {
	for _, a := range arr {
		if len(a) > 0 {
			return true
		}
	}
	return false
}

func handleRule(action workloadapi.Action, rule *v1beta1.Rule) []*workloadapi.Rules {
	toMatches := []*workloadapi.Match{}
	for _, to := range rule.To {
		op := to.Operation
		if action == workloadapi.Action_ALLOW && anyNonEmpty(op.Hosts, op.NotHosts, op.Methods, op.NotMethods, op.Paths, op.NotPaths) {
			// L7 policies never match for ALLOW
			// For DENY they will always match, so it is more restrictive
			return nil
		}
		match := &workloadapi.Match{
			DestinationPorts:    stringToPort(op.Ports),
			NotDestinationPorts: stringToPort(op.NotPorts),
		}
		// if !emptyRuleMatch(match) {
		toMatches = append(toMatches, match)
		//}
	}
	fromMatches := []*workloadapi.Match{}
	for _, from := range rule.From {
		op := from.Source
		if action == workloadapi.Action_ALLOW && anyNonEmpty(op.RemoteIpBlocks, op.NotRemoteIpBlocks, op.RequestPrincipals, op.NotRequestPrincipals) {
			// L7 policies never match for ALLOW
			// For DENY they will always match, so it is more restrictive
			return nil
		}
		match := &workloadapi.Match{
			SourceIps:     stringToIP(op.IpBlocks),
			NotSourceIps:  stringToIP(op.NotIpBlocks),
			Namespaces:    stringToMatch(op.Namespaces),
			NotNamespaces: stringToMatch(op.NotNamespaces),
			Principals:    stringToMatch(op.Principals),
			NotPrincipals: stringToMatch(op.NotPrincipals),
		}
		// if !emptyRuleMatch(match) {
		fromMatches = append(fromMatches, match)
		//}
	}

	rules := []*workloadapi.Rules{}
	if len(toMatches) > 0 {
		rules = append(rules, &workloadapi.Rules{Matches: toMatches})
	}
	if len(fromMatches) > 0 {
		rules = append(rules, &workloadapi.Rules{Matches: fromMatches})
	}
	for _, when := range rule.When {
		l7 := l4WhenAttributes.Contains(when.Key)
		if action == workloadapi.Action_ALLOW && !l7 {
			// L7 policies never match for ALLOW
			// For DENY they will always match, so it is more restrictive
			return nil
		}
		positiveMatch := &workloadapi.Match{
			Namespaces:       whenMatch("source.namespace", when, false, stringToMatch),
			Principals:       whenMatch("source.principal", when, false, stringToMatch),
			SourceIps:        whenMatch("source.ip", when, false, stringToIP),
			DestinationPorts: whenMatch("destination.port", when, false, stringToPort),
			DestinationIps:   whenMatch("destination.ip", when, false, stringToIP),

			NotNamespaces:       whenMatch("source.namespace", when, true, stringToMatch),
			NotPrincipals:       whenMatch("source.principal", when, true, stringToMatch),
			NotSourceIps:        whenMatch("source.ip", when, true, stringToIP),
			NotDestinationPorts: whenMatch("destination.port", when, true, stringToPort),
			NotDestinationIps:   whenMatch("destination.ip", when, true, stringToIP),
		}
		rules = append(rules, &workloadapi.Rules{Matches: []*workloadapi.Match{positiveMatch}})
	}
	return rules
}

var l4WhenAttributes = sets.New(
	"source.ip",
	"source.namespace",
	"source.principal",
	"destination.ip",
	"destination.port",
)

func whenMatch[T any](s string, when *v1beta1.Condition, invert bool, f func(v []string) []T) []T {
	if when.Key != s {
		return nil
	}
	if invert {
		return f(when.NotValues)
	}
	return f(when.Values)
}

func stringToMatch(rules []string) []*workloadapi.StringMatch {
	res := make([]*workloadapi.StringMatch, 0, len(rules))
	for _, v := range rules {
		var sm *workloadapi.StringMatch
		switch {
		case v == "*":
			sm = &workloadapi.StringMatch{MatchType: &workloadapi.StringMatch_Presence{}}
		case strings.HasPrefix(v, "*"):
			sm = &workloadapi.StringMatch{MatchType: &workloadapi.StringMatch_Suffix{
				Suffix: strings.TrimPrefix(v, "*"),
			}}
		case strings.HasSuffix(v, "*"):
			sm = &workloadapi.StringMatch{MatchType: &workloadapi.StringMatch_Prefix{
				Prefix: strings.TrimSuffix(v, "*"),
			}}
		default:
			sm = &workloadapi.StringMatch{MatchType: &workloadapi.StringMatch_Exact{
				Exact: v,
			}}
		}
		res = append(res, sm)
	}
	return res
}

func stringToPort(rules []string) []uint32 {
	res := make([]uint32, 0, len(rules))
	for _, m := range rules {
		p, err := strconv.ParseUint(m, 10, 32)
		if err != nil || p > 65535 {
			continue
		}
		res = append(res, uint32(p))
	}
	return res
}

func stringToIP(rules []string) []*workloadapi.Address {
	res := make([]*workloadapi.Address, 0, len(rules))
	for _, m := range rules {
		if len(m) == 0 {
			continue
		}

		var (
			ipAddr        netip.Addr
			maxCidrPrefix uint32
		)

		if strings.Contains(m, "/") {
			ipp, err := netip.ParsePrefix(m)
			if err != nil {
				continue
			}
			ipAddr = ipp.Addr()
			maxCidrPrefix = uint32(ipp.Bits())
		} else {
			ipa, err := netip.ParseAddr(m)
			if err != nil {
				continue
			}

			ipAddr = ipa
			maxCidrPrefix = uint32(ipAddr.BitLen())
		}

		res = append(res, &workloadapi.Address{
			Address: ipAddr.AsSlice(),
			Length:  maxCidrPrefix,
		})
	}
	return res
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
	ServiceAccount string
	Name           string
	Namespace      string
	Address        []byte
}

func (w Waypoint) ResourceName() string {
	return w.Namespace + "/" + w.Name
}

func (c *Controller) setupIndex() *AmbientIndex {
	ConfigMaps := cv2.CollectionFor[*v1.ConfigMap](c.client)
	MeshConfig := cv2.NewSingleton[meshapi.MeshConfig](
		func(ctx cv2.HandlerContext) *meshapi.MeshConfig {
			log.Errorf("howardjohn: Computing mesh config")
			meshCfg := mesh.DefaultMeshConfig()
			cms := []*v1.ConfigMap{}
			cms = cv2.AppendNonNil(cms, cv2.FetchOne(ctx, ConfigMaps, cv2.FilterName("istio-user")))
			cms = cv2.AppendNonNil(cms, cv2.FetchOne(ctx, ConfigMaps, cv2.FilterName("istio")))

			for _, c := range cms {
				n, err := mesh.ApplyMeshConfig(meshConfigMapData(c), meshCfg)
				if err != nil {
					log.Error(err)
					continue
				}
				meshCfg = n
			}
			log.Errorf("howardjohn: computed mesh config to %v", meshCfg.GetIngressClass())
			return meshCfg
		},
	)
	AuthzPolicies := cv2.CollectionFor[*securityclient.AuthorizationPolicy](c.client)
	Services := cv2.CollectionFor[*v1.Service](c.client)
	Pods := cv2.CollectionFor[*v1.Pod](c.client)
	Namespaces := cv2.CollectionFor[*v1.Namespace](c.client)
	Waypoints := cv2.NewCollection(Pods, func(ctx cv2.HandlerContext, p *v1.Pod) *Waypoint {
		if !IsPodReady(p) {
			return nil
		}
		if p.Labels[constants.ManagedGatewayLabel] != constants.ManagedGatewayMeshController {
			// not a waypoint
			return nil
		}
		addr, err := netip.ParseAddr(p.Status.PodIP)
		if err != nil {
			return nil
		}
		return &Waypoint{
			ServiceAccount: p.Spec.ServiceAccountName,
			Namespace:      p.Namespace,
			Name:           p.Name,
			Address:        addr.AsSlice(),
		}
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
		if p.Labels[constants.ManagedGatewayLabel] == constants.ManagedGatewayMeshController {
			// We don't include waypoints
			return nil
		}
		policies := cv2.Fetch(ctx, AuthzPolicies, cv2.FilterSelects(p.Labels), cv2.FilterGeneric(func(a any) bool {
			// We only want label selector ones, we handle global ones through another mechanism
			return a.(*securityclient.AuthorizationPolicy).Spec.GetSelector().GetMatchLabels() != nil
		}))
		meshCfg := cv2.FetchOne(ctx, MeshConfig.AsCollection())
		namespace := cv2.Flatten(cv2.FetchOne(ctx, Namespaces, cv2.FilterName(p.Namespace)))
		services := cv2.Fetch(ctx, Services, cv2.FilterSelects(p.GetLabels()))
		waypoints := cv2.Fetch(ctx, Waypoints, cv2.FilterGeneric(func(a any) bool {
			return a.(Waypoint).ServiceAccount == p.Spec.ServiceAccountName
		}))
		w := &workloadapi.Workload{
			Name:           p.Name,
			Namespace:      p.Namespace,
			Address:        netip.MustParseAddr(p.Status.PodIP).AsSlice(),
			ServiceAccount: p.Spec.ServiceAccountName,
			WaypointAddresses: cv2.Map(waypoints, func(w Waypoint) []byte {
				return w.Address
			}),
			Node:       p.Spec.NodeName,
			VirtualIps: constructVIPs(p, services),
			AuthorizationPolicies: cv2.Map(policies, func(t *securityclient.AuthorizationPolicy) string {
				return t.Namespace + "/" + t.Name
			}),
		}

		if td := spiffe.GetTrustDomain(); td != "cluster.local" {
			w.TrustDomain = td
		}
		w.WorkloadName, w.WorkloadType = workloadNameAndType(p)
		w.CanonicalName, w.CanonicalRevision = kubelabels.CanonicalService(p.Labels, w.WorkloadName)

		if ambientpod.ShouldPodBeInIpset(namespace, p, meshCfg.GetAmbientMesh().GetMode().String(), true) {
			w.Protocol = workloadapi.Protocol_HTTP
		}
		// Otherwise supports tunnel directly
		if model.SupportsTunnel(p.Labels, model.TunnelHTTP) {
			w.Protocol = workloadapi.Protocol_HTTP
			w.NativeHbone = true
		}
		log.Errorf("howardjohn: made workload: %v", w)
		return &model.WorkloadInfo{Workload: w}
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

	WorkloadServiceIndex := cv2.CreateIndex[model.WorkloadInfo, string](Workloads, func(o model.WorkloadInfo) []string {
		return maps.Keys(o.VirtualIps)
	})
	return &AmbientIndex{
		workloads:             Workloads,
		workloadServicesIndex: WorkloadServiceIndex,
	}
}

func (c *Controller) updateEndpointsOnWaypointChange(name, namespace string) {
	var errs *multierror.Error
	esLabelSelector := endpointSliceSelectorForService(name)
	switch endpointController := c.endpoints.(type) {
	case *endpointsController:
		endpoints, err := listerv1.NewEndpointsLister(c.endpoints.getInformer().GetIndexer()).Endpoints(namespace).List(esLabelSelector)
		if err != nil {
			log.Errorf("error listing endpoints associated with waypoint (%v): %v", name, err)
		}
		for _, ep := range endpoints {
			errs = multierror.Append(errs, c.endpoints.onEvent(ep, model.EventAdd))
		}
	case *endpointSliceController:
		endpointSlices, err := endpointController.listSlices(namespace, esLabelSelector)
		if err != nil {
			log.Errorf("error listing endpoints associated with waypoint (%v): %v", name, err)
		}
		for _, ep := range endpointSlices {
			errs = multierror.Append(errs, c.endpoints.onEvent(ep, model.EventAdd))
		}
	}
	if err := multierror.Flatten(errs.ErrorOrNil()); err != nil {
		log.Errorf("one or more errors while pushing endpoint updates for waypoint %q in namespace %s: %v", name, namespace, err)
	}
}

// PodInformation returns all WorkloadInfo's in the cluster.
// This may be scoped to specific subsets by specifying a non-empty addresses field
func (c *Controller) PodInformation(addresses sets.Set[types.NamespacedName]) ([]*model.WorkloadInfo, []string) {
	if len(addresses) == 0 {
		// Full update
		return c.ambientIndex.All(), nil
	}
	var wls []*model.WorkloadInfo
	var removed []string
	for p := range addresses {
		wl := c.ambientIndex.Lookup(p.Name)
		if len(wl) == 0 {
			removed = append(removed, p.Name)
		} else {
			wls = append(wls, wl...)
		}
	}
	return wls, removed
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
	if svc.Spec.ClusterIP != "" && svc.Spec.ClusterIP != "None" {
		res = append(res, svc.Spec.ClusterIP)
	}
	for _, ing := range svc.Status.LoadBalancer.Ingress {
		res = append(res, ing.IP)
	}
	return res
}

func (c *Controller) AdditionalPodSubscriptions(
	proxy *model.Proxy,
	allAddresses sets.Set[types.NamespacedName],
	currentSubs sets.Set[types.NamespacedName],
) sets.Set[types.NamespacedName] {
	shouldSubscribe := sets.New[types.NamespacedName]()

	// First, we want to handle VIP subscriptions. Example:
	// Client subscribes to VIP1. Pod1, part of VIP1, is sent.
	// The client wouldn't be explicitly subscribed to Pod1, so it would normally ignore it.
	// Since it is a part of VIP1 which we are subscribe to, add it to the subscriptions
	for s := range allAddresses {
		for _, wl := range c.ambientIndex.Lookup(s.Name) {
			// We may have gotten an update for Pod, but are subscribe to a Service.
			// We need to force a subscription on the Pod as well
			for addr := range wl.VirtualIps {
				t := types.NamespacedName{Name: addr}
				if currentSubs.Contains(t) {
					shouldSubscribe.Insert(types.NamespacedName{Name: wl.ResourceName()})
					break
				}
			}
		}
	}

	// Next, as an optimization, we will send all node-local endpoints
	if nodeName := proxy.Metadata.NodeName; nodeName != "" {
		for _, wl := range c.ambientIndex.All() {
			if wl.Node == nodeName {
				n := types.NamespacedName{Name: wl.ResourceName()}
				if currentSubs.Contains(n) {
					continue
				}
				shouldSubscribe.Insert(n)
			}
		}
	}

	return shouldSubscribe
}

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

package model

import (
	"sort"
	"strings"

	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"github.com/gogo/protobuf/types"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"

	meshconfig "istio.io/api/mesh/v1alpha1"
	tpb "istio.io/api/telemetry/v1alpha1"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/collections"
	istiolog "istio.io/pkg/log"
)

var telemetryLog = istiolog.RegisterScope("telemetry", "Istio Telemetry", 0)

// Telemetry holds configuration for Telemetry API resources.
type Telemetry struct {
	Name      string         `json:"name"`
	Namespace string         `json:"namespace"`
	Spec      *tpb.Telemetry `json:"spec"`
}

// Telemetries organizes Telemetry configuration by namespace.
type Telemetries struct {
	// Maps from namespace to the Telemetry configs.
	NamespaceToTelemetries map[string][]Telemetry `json:"namespace_to_telemetries"`

	// The name of the root namespace.
	RootNamespace string `json:"root_namespace"`

	// Computed MeshConfig
	MeshConfig *meshconfig.MeshConfig
}

// GetTelemetries returns the Telemetry configurations for the given environment.
func GetTelemetries(env *Environment) (*Telemetries, error) {
	telemetries := &Telemetries{
		NamespaceToTelemetries: map[string][]Telemetry{},
		RootNamespace:          env.Mesh().GetRootNamespace(),
		MeshConfig:             env.Mesh(),
	}

	fromEnv, err := env.List(collections.IstioTelemetryV1Alpha1Telemetries.Resource().GroupVersionKind(), NamespaceAll)
	if err != nil {
		return nil, err
	}
	sortConfigByCreationTime(fromEnv)
	for _, config := range fromEnv {
		telemetry := Telemetry{
			Name:      config.Name,
			Namespace: config.Namespace,
			Spec:      config.Spec.(*tpb.Telemetry),
		}
		telemetries.NamespaceToTelemetries[config.Namespace] =
			append(telemetries.NamespaceToTelemetries[config.Namespace], telemetry)
	}

	return telemetries, nil
}

// AnyTelemetryExists determines if there are any Telemetries present in the entire mesh.
func (t *Telemetries) AnyTelemetryExists() bool {
	return len(t.NamespaceToTelemetries) > 0
}

func (t *Telemetries) EffectiveTelemetry(proxy *Proxy) *tpb.Telemetry {
	if t == nil {
		return nil
	}

	namespace := proxy.ConfigNamespace
	workload := labels.Collection{proxy.Metadata.Labels}

	var effectiveSpec *tpb.Telemetry
	if t.RootNamespace != "" {
		effectiveSpec = t.namespaceWideTelemetry(t.RootNamespace)
	}

	if namespace != t.RootNamespace {
		nsSpec := t.namespaceWideTelemetry(namespace)
		effectiveSpec = shallowMerge(effectiveSpec, nsSpec)
	}

	for _, telemetry := range t.NamespaceToTelemetries[namespace] {
		spec := telemetry.Spec
		if len(spec.GetSelector().GetMatchLabels()) == 0 {
			continue
		}
		selector := labels.Instance(spec.GetSelector().GetMatchLabels())
		if workload.IsSupersetOf(selector) {
			effectiveSpec = shallowMerge(effectiveSpec, spec)
			break
		}
	}

	return effectiveSpec
}

type telemetryProvider string

type TelemetryMetricsMode struct {
	Provider *meshconfig.MeshConfig_ExtensionProvider
	Client   []MetricsOverride
	Server   []MetricsOverride
}

func (t TelemetryMetricsMode) ForClass(c networking.ListenerClass) []MetricsOverride {
	switch c {
	case networking.ListenerClassGateway:
		return t.Client
	case networking.ListenerClassSidecarInbound:
		return t.Server
	case networking.ListenerClassSidecarOutbound:
		return t.Client
	default:
		return t.Client
	}
}

type MetricsOverride struct {
	Name     string
	Disabled bool
	Tags     []TagOverride
}

type TagOverride struct {
	Name   string
	Remove bool
	Value  string
}

type TelemetryKey struct {
	Root      ConfigKey
	Namespace ConfigKey
	Workload  ConfigKey
}

func (t TelemetryKey) Key() string {
	return t.Root.String() + "~" + t.Namespace.String() + "~" + t.Workload.String()
}

func (t TelemetryKey) DependentTypes() []config.GroupVersionKind {
	return nil
}

func (t TelemetryKey) DependentConfigs() []ConfigKey {
	return []ConfigKey{t.Root, t.Namespace, t.Workload}
}

func (t TelemetryKey) Cacheable() bool {
	return true
}

var telemetryCache = NewXdsCache()

type MetricsFilters struct {
	HTTPClient []*hcm.HttpFilter
	HTTPServer []*hcm.HttpFilter
	TCPClient []*listener.Filter
	TCPServer []*listener.Filter
}

func (t *Telemetries) EffectiveMetrics(proxy *Proxy) []TelemetryMetricsMode {
	if t == nil {
		return nil
	}

	key := TelemetryKey{}
	namespace := proxy.ConfigNamespace
	workload := labels.Collection{proxy.Metadata.Labels}
	// Order here matters. The latter elements will override the first elements
	ms := []*tpb.Metrics{}
	if t.RootNamespace != "" {
		telemetry := t.namespaceWideTelemetryConfig(t.RootNamespace)
		if telemetry != nil {
			key.Root = ConfigKey{Kind: gvk.Telemetry, Name: telemetry.Name, Namespace: telemetry.Namespace}
			ms = append(ms, telemetry.Spec.GetMetrics()...)
		}
	}

	if namespace != t.RootNamespace {
		telemetry := t.namespaceWideTelemetryConfig(namespace)
		if telemetry != nil {
			key.Root = ConfigKey{Kind: gvk.Telemetry, Name: telemetry.Name, Namespace: telemetry.Namespace}
			ms = append(ms, telemetry.Spec.GetMetrics()...)
		}
	}

	for _, telemetry := range t.NamespaceToTelemetries[namespace] {
		spec := telemetry.Spec
		if len(spec.GetSelector().GetMatchLabels()) == 0 {
			continue
		}
		selector := labels.Instance(spec.GetSelector().GetMatchLabels())
		if workload.IsSupersetOf(selector) {
			key.Root = ConfigKey{Kind: gvk.Telemetry, Name: telemetry.Name, Namespace: telemetry.Namespace}
			ms = append(ms, spec.GetMetrics()...)
			break
		}
	}

	// TODO:
	//  inline the filters code here.
	precomputed, f := telemetryCache.Get(key)
	if f {
		 _ = precomputed
		return nil
		//return precomputed.(*MetricsFilters)
	}

	tmm := mergeMetrics(ms, t.MeshConfig)
	fetchProvider := func(m string) *meshconfig.MeshConfig_ExtensionProvider {
		for _, p := range t.MeshConfig.ExtensionProviders {
			if strings.EqualFold(m, p.Name) {
				return p
			}
		}
		return nil
	}
	res := []TelemetryMetricsMode{}
	for k, v := range tmm {
		p := fetchProvider(string(k))
		if p == nil {
			continue
		}
		v.Provider = p
		res = append(res, v)
	}
	sort.Slice(res, func(i, j int) bool {
		return res[i].Provider.GetName() < res[i].Provider.GetName()
	})


	return res
}

func (t *Telemetries) namespaceWideTelemetry(namespace string) *tpb.Telemetry {
	for _, tel := range t.NamespaceToTelemetries[namespace] {
		spec := tel.Spec
		if len(spec.GetSelector().GetMatchLabels()) == 0 {
			return spec
		}
	}
	return nil
}

func (t *Telemetries) namespaceWideTelemetryConfig(namespace string) *Telemetry {
	for _, tel := range t.NamespaceToTelemetries[namespace] {
		if len(tel.Spec.GetSelector().GetMatchLabels()) == 0 {
			return &tel
		}
	}
	return nil
}

func shallowMerge(parent, child *tpb.Telemetry) *tpb.Telemetry {
	if parent == nil {
		return child
	}
	if child == nil {
		return parent
	}
	merged := parent.DeepCopy()
	shallowMergeTracing(merged, child)
	shallowMergeAccessLogs(merged, child)
	return merged
}

var allMetrics = func() []string {
	r := []string{}
	for k := range tpb.MetricSelector_IstioMetric_value {
		if k != tpb.MetricSelector_IstioMetric_name[int32(tpb.MetricSelector_ALL_METRICS)] {
			r = append(r, k)
		}
	}
	sort.Strings(r)
	return r
}()

type MetricOverride struct {
	Disabled     *types.BoolValue
	TagOverrides map[string]*tpb.MetricsOverrides_TagOverride
}

func mergeMetrics(metrics []*tpb.Metrics, mesh *meshconfig.MeshConfig) map[telemetryProvider]TelemetryMetricsMode {
	// provider -> mode -> metric -> overrides
	providers := map[telemetryProvider]map[string]map[string]MetricOverride{}

	for _, dp := range mesh.GetDefaultProviders().GetMetrics() {
		providers[telemetryProvider(dp)] = map[string]map[string]MetricOverride{}
	}
	for _, m := range metrics {
		for _, provider := range m.Providers {
			p := telemetryProvider(provider.GetName())
			if _, f := providers[p]; !f {
				providers[p] = map[string]map[string]MetricOverride{
					"client": {},
					"server": {},
				}
			}
			mp := providers[p]
			for _, o := range m.Overrides {
				for _, mode := range getModes(o.GetMatch().Mode) {
					for _, metricName := range getMatches(o.Match) {
						override := mp[mode][metricName]
						if o.Disabled != nil {
							override.Disabled = o.Disabled
						}
						for k, v := range o.TagOverrides {
							if override.TagOverrides == nil {
								override.TagOverrides = map[string]*tpb.MetricsOverrides_TagOverride{}
							}
							override.TagOverrides[k] = v
						}
						mp[mode][metricName] = override
					}
				}
			}
		}
	}

	processed := map[telemetryProvider]TelemetryMetricsMode{}
	for provider, modeMap := range providers {
		for mode, metricMap := range modeMap {
			for metric, override := range metricMap {
				tags := []TagOverride{}
				for k, v := range override.TagOverrides {
					o := TagOverride{Name: k}
					switch v.Operation {
					case tpb.MetricsOverrides_TagOverride_REMOVE:
						o.Remove = true
					case tpb.MetricsOverrides_TagOverride_UPSERT:
						o.Value = v.GetValue()
					}
					tags = append(tags, o)
				}
				// Keep order deterministic
				sort.Slice(tags, func(i, j int) bool {
					return tags[i].Name < tags[j].Name
				})
				mo := MetricsOverride{
					Name:     metric,
					Disabled: override.Disabled.GetValue(),
					Tags:     tags,
				}
				tmm := processed[provider]
				switch mode {
				case "client":
					tmm.Client = append(tmm.Client, mo)
				default:
					tmm.Server = append(tmm.Server, mo)
				}
				processed[provider] = tmm
			}
		}

		// Keep order deterministic
		tmm := processed[provider]
		sort.Slice(tmm.Server, func(i, j int) bool {
			return tmm.Server[i].Name < tmm.Server[j].Name
		})
		sort.Slice(tmm.Client, func(i, j int) bool {
			return tmm.Client[i].Name < tmm.Client[j].Name
		})
		processed[provider] = tmm
	}
	return processed
}

func getModes(mode tpb.WorkloadMode) []string {
	switch mode {
	case tpb.WorkloadMode_CLIENT:
		return []string{"client"}
	case tpb.WorkloadMode_SERVER:
		return []string{"server"}
	default:
		return []string{"client", "server"}
	}
}

func getMatches(match *tpb.MetricSelector) []string {
	switch m := match.GetMetricMatch().(type) {
	case *tpb.MetricSelector_CustomMetric:
		return []string{m.CustomMetric}
	case *tpb.MetricSelector_Metric:
		if m.Metric == tpb.MetricSelector_ALL_METRICS {
			return allMetrics
		}
		return []string{m.Metric.String()}
	}
	return nil
}

func shallowMergeTracing(parent, child *tpb.Telemetry) {
	if len(parent.GetTracing()) == 0 {
		parent.Tracing = child.Tracing
		return
	}
	if len(child.GetTracing()) == 0 {
		return
	}

	// only use the first Tracing for now (all that is supported)
	childTracing := child.Tracing[0]
	mergedTracing := parent.Tracing[0]
	if len(childTracing.Providers) != 0 {
		mergedTracing.Providers = childTracing.Providers
	}

	if childTracing.GetCustomTags() != nil {
		mergedTracing.CustomTags = childTracing.CustomTags
	}

	if childTracing.GetDisableSpanReporting() != nil {
		mergedTracing.DisableSpanReporting = childTracing.DisableSpanReporting
	}

	if childTracing.GetRandomSamplingPercentage() != nil {
		mergedTracing.RandomSamplingPercentage = childTracing.RandomSamplingPercentage
	}
}

func shallowMergeAccessLogs(parent *tpb.Telemetry, child *tpb.Telemetry) {
	if len(parent.GetAccessLogging()) == 0 {
		parent.AccessLogging = child.AccessLogging
		return
	}
	if len(child.GetAccessLogging()) == 0 {
		return
	}

	// Only use the first AccessLogging for now (all that is supported)
	childLogging := child.AccessLogging[0]
	mergedLogging := parent.AccessLogging[0]
	if len(childLogging.Providers) != 0 {
		mergedLogging.Providers = childLogging.Providers
	}

	if childLogging.GetDisabled() != nil {
		mergedLogging.Disabled = childLogging.Disabled
	}
}

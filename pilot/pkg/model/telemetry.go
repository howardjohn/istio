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

	"github.com/gogo/protobuf/types"

	meshconfig "istio.io/api/mesh/v1alpha1"
	tpb "istio.io/api/telemetry/v1alpha1"
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
}

// GetTelemetries returns the Telemetry configurations for the given environment.
func GetTelemetries(env *Environment) (*Telemetries, error) {
	telemetries := &Telemetries{
		NamespaceToTelemetries: map[string][]Telemetry{},
		RootNamespace:          env.Mesh().GetRootNamespace(),
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

type (
	TelemetryMetrics          map[string]map[string]MetricOverride
	TelemetryMetricsProviders map[*meshconfig.MeshConfig_ExtensionProvider]map[string]MetricOverride
)

// TODO include class
func (t *Telemetries) EffectiveMetrics(proxy *Proxy) TelemetryMetrics {
	if t == nil {
		return nil
	}

	namespace := proxy.ConfigNamespace
	workload := labels.Collection{proxy.Metadata.Labels}
	// Order here matters. The latter elements will override the first elements
	ms := []*tpb.Metrics{}
	if t.RootNamespace != "" {
		ms = append(ms, t.namespaceWideTelemetry(t.RootNamespace).GetMetrics()...)
	}

	if namespace != t.RootNamespace {
		ms = append(ms, t.namespaceWideTelemetry(namespace).GetMetrics()...)
	}

	for _, telemetry := range t.NamespaceToTelemetries[namespace] {
		spec := telemetry.Spec
		if len(spec.GetSelector().GetMatchLabels()) == 0 {
			continue
		}
		selector := labels.Instance(spec.GetSelector().GetMatchLabels())
		if workload.IsSupersetOf(selector) {
			ms = append(ms, spec.GetMetrics()...)
			break
		}
	}

	return mergeMetrics(ms)
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

func mergeMetrics(metrics []*tpb.Metrics) TelemetryMetrics {
	providers := map[string]map[string]MetricOverride{}

	// TODO this doesn't take into account that if providers is empty we should use the default
	for _, m := range metrics {
		for _, p := range m.Providers {
			if _, f := providers[p.GetName()]; !f {
				providers[p.GetName()] = map[string]MetricOverride{}
			}
			mp := providers[p.GetName()]
			for _, o := range m.Overrides {
				for _, metricName := range getMatches(o.Match) {
					override := mp[metricName]
					if o.Disabled != nil {
						override.Disabled = o.Disabled
					}
					for k, v := range o.TagOverrides {
						if override.TagOverrides == nil {
							override.TagOverrides = map[string]*tpb.MetricsOverrides_TagOverride{}
						}
						override.TagOverrides[k] = v
					}
					mp[metricName] = override
				}
			}
		}
	}
	return providers
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

// PrometheusEnabled determines if any of the extension providers are for prometheus
func PrometheusEnabled(providers []*meshconfig.MeshConfig_ExtensionProvider) bool {
	for _, prov := range providers {
		if _, ok := prov.Provider.(*meshconfig.MeshConfig_ExtensionProvider_Prometheus); ok {
			return true
		}
	}
	return false
}

// MetricsEnabled determines if there is any `metrics` configuration for the provided mesh configuration and Telemetry.
// Note that this can return `true` even when `spec` is `nil`, in the case there is a default provider.
func MetricsProviders(metrics TelemetryMetrics, mesh *meshconfig.MeshConfig) TelemetryMetricsProviders {
	fetchProvider := func(m string) *meshconfig.MeshConfig_ExtensionProvider {
		for _, p := range mesh.ExtensionProviders {
			if strings.EqualFold(m, p.Name) {
				return p
			}
		}
		return nil
	}
	res := TelemetryMetricsProviders{}
	for k, v := range metrics {
		p := fetchProvider(k)
		if p == nil {
			continue
		}
		res[p] = v
	}
	for _, dp := range mesh.GetDefaultProviders().GetMetrics() {
		if _, f := metrics[dp]; !f {
			p := fetchProvider(dp)
			if p == nil {
				continue
			}
			// Insert default config, no overrides
			res[p] = map[string]MetricOverride{}
		}
	}
	return res
}

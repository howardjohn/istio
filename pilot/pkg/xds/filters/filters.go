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

package filters

import (
	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	cors "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/cors/v3"
	fault "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/fault/v3"
	grpcstats "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/grpc_stats/v3"
	grpcweb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/grpc_web/v3"
	router "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	httpwasm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/wasm/v3"
	httpinspector "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/http_inspector/v3"
	originaldst "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/original_dst/v3"
	originalsrc "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/original_src/v3"
	tlsinspector "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/tls_inspector/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	wasmfilter "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/wasm/v3"
	wasm "github.com/envoyproxy/go-control-plane/envoy/extensions/wasm/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	protobuf "github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/ptypes/any"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/wrapperspb"

	alpn "istio.io/api/envoy/config/filter/http/alpn/v2alpha1"
	"istio.io/api/envoy/config/filter/network/metadata_exchange"
	"istio.io/api/envoy/config/filter/network/metadata_exchange"
	"istio.io/api/envoy/extensions/stats"
	meshconfig "istio.io/api/mesh/v1alpha1"
	tpb "istio.io/api/telemetry/v1alpha1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/networking/util"
)

const (
	OriginalSrcFilterName = "envoy.filters.listener.original_src"
	// Alpn HTTP filter name which will override the ALPN for upstream TLS connection.
	AlpnFilterName = "istio.alpn"

	TLSTransportProtocol       = "tls"
	RawBufferTransportProtocol = "raw_buffer"

	MxFilterName    = "istio.metadata_exchange"
	StatsFilterName = "istio.stats"
)

// Define static filters to be reused across the codebase. This avoids duplicate marshaling/unmarshaling
// This should not be used for filters that will be mutated
var (
	Cors = &hcm.HttpFilter{
		Name: wellknown.CORS,
		ConfigType: &hcm.HttpFilter_TypedConfig{
			TypedConfig: util.MessageToAny(&cors.Cors{}),
		},
	}
	Fault = &hcm.HttpFilter{
		Name: wellknown.Fault,
		ConfigType: &hcm.HttpFilter_TypedConfig{
			TypedConfig: util.MessageToAny(&fault.HTTPFault{}),
		},
	}
	Router = &hcm.HttpFilter{
		Name: wellknown.Router,
		ConfigType: &hcm.HttpFilter_TypedConfig{
			TypedConfig: util.MessageToAny(&router.Router{}),
		},
	}
	GrpcWeb = &hcm.HttpFilter{
		Name: wellknown.GRPCWeb,
		ConfigType: &hcm.HttpFilter_TypedConfig{
			TypedConfig: util.MessageToAny(&grpcweb.GrpcWeb{}),
		},
	}
	GrpcStats = &hcm.HttpFilter{
		Name: wellknown.HTTPGRPCStats,
		ConfigType: &hcm.HttpFilter_TypedConfig{
			TypedConfig: util.MessageToAny(&grpcstats.FilterConfig{
				EmitFilterState: true,
				PerMethodStatSpecifier: &grpcstats.FilterConfig_StatsForAllMethods{
					StatsForAllMethods: &wrapperspb.BoolValue{Value: false},
				},
			}),
		},
	}
	TLSInspector = &listener.ListenerFilter{
		Name: wellknown.TlsInspector,
		ConfigType: &listener.ListenerFilter_TypedConfig{
			TypedConfig: util.MessageToAny(&tlsinspector.TlsInspector{}),
		},
	}
	HTTPInspector = &listener.ListenerFilter{
		Name: wellknown.HttpInspector,
		ConfigType: &listener.ListenerFilter_TypedConfig{
			TypedConfig: util.MessageToAny(&httpinspector.HttpInspector{}),
		},
	}
	OriginalDestination = &listener.ListenerFilter{
		Name: wellknown.OriginalDestination,
		ConfigType: &listener.ListenerFilter_TypedConfig{
			TypedConfig: util.MessageToAny(&originaldst.OriginalDst{}),
		},
	}
	OriginalSrc = &listener.ListenerFilter{
		Name: OriginalSrcFilterName,
		ConfigType: &listener.ListenerFilter_TypedConfig{
			TypedConfig: util.MessageToAny(&originalsrc.OriginalSrc{
				Mark: 1337,
			}),
		},
	}
	Alpn = &hcm.HttpFilter{
		Name: AlpnFilterName,
		ConfigType: &hcm.HttpFilter_TypedConfig{
			TypedConfig: util.MessageToAny(&alpn.FilterConfig{
				AlpnOverride: []*alpn.FilterConfig_AlpnOverride{
					{
						UpstreamProtocol: alpn.FilterConfig_HTTP10,
						AlpnOverride:     mtlsHTTP10ALPN,
					},
					{
						UpstreamProtocol: alpn.FilterConfig_HTTP11,
						AlpnOverride:     mtlsHTTP11ALPN,
					},
					{
						UpstreamProtocol: alpn.FilterConfig_HTTP2,
						AlpnOverride:     mtlsHTTP2ALPN,
					},
				},
			}),
		},
	}

	tcpMx = util.MessageToAny(&metadata_exchange.MetadataExchange{Protocol: "istio-peer-exchange"})

	TCPListenerMx = &listener.Filter{
		Name:       MxFilterName,
		ConfigType: &listener.Filter_TypedConfig{TypedConfig: tcpMx},
	}

	TCPClusterMx = &cluster.Filter{
		Name:        MxFilterName,
		TypedConfig: tcpMx,
	}

	HTTPMx = buildHTTPMxFilter()
)

func BuildRouterFilter(ctx *RouterFilterContext) *hcm.HttpFilter {
	if ctx == nil {
		return Router
	}

	return &hcm.HttpFilter{
		Name: wellknown.Router,
		ConfigType: &hcm.HttpFilter_TypedConfig{
			TypedConfig: util.MessageToAny(&router.Router{
				StartChildSpan: ctx.StartChildSpan,
			}),
		},
	}
}

var (
	// These ALPNs are injected in the client side by the ALPN filter.
	// "istio" is added for each upstream protocol in order to make it
	// backward compatible. e.g., 1.4 proxy -> 1.3 proxy.
	// Non istio-* variants are added to ensure that traffic sent out of the mesh has a valid ALPN;
	// ideally this would not be added, but because the override filter is in the HCM, rather than cluster,
	// we do not yet know the upstream so we cannot determine if its in or out of the mesh
	mtlsHTTP10ALPN = []string{"istio-http/1.0", "istio", "http/1.0"}
	mtlsHTTP11ALPN = []string{"istio-http/1.1", "istio", "http/1.1"}
	mtlsHTTP2ALPN  = []string{"istio-h2", "istio", "h2"}
)

func buildHTTPMxFilter() *hcm.HttpFilter {
	httpMxConfigProto := &httpwasm.Wasm{
		Config: &wasm.PluginConfig{
			Vm:            constructVMConfig("/etc/istio/extensions/metadata-exchange-filter.compiled.wasm", "envoy.wasm.metadata_exchange"),
			Configuration: util.MessageToAny(&metadata_exchange.MetadataExchange{}),
		},
	}
	return &hcm.HttpFilter{
		Name:       MxFilterName,
		ConfigType: &hcm.HttpFilter_TypedConfig{TypedConfig: util.MessageToAny(httpMxConfigProto)},
	}
}

func BuildTCPStatsFilter(class networking.ListenerClass, metrics model.TelemetryMetricsProviders) []*listener.Filter {
	res := []*listener.Filter{}
	// TODO map is not ordered!
	for provider, metricsCfg := range metrics {
		switch provider.GetProvider().(type) {
		case *meshconfig.MeshConfig_ExtensionProvider_Prometheus:
			cfg := generateStatsConfig(class, metricsCfg)
			vmConfig := constructVMConfig("/etc/istio/extensions/stats-filter.compiled.wasm", "envoy.wasm.stats")
			root := rootIDForClass(class)
			vmConfig.VmConfig.VmId = "tcp_" + root

			wasmConfig := &wasmfilter.Wasm{
				Config: &wasm.PluginConfig{
					RootId:        root,
					Vm:            vmConfig,
					Configuration: cfg,
				},
			}

			f := &listener.Filter{
				Name:       StatsFilterName,
				ConfigType: &listener.Filter_TypedConfig{TypedConfig: util.MessageToAny(wasmConfig)},
			}
			res = append(res, f)
		default:
			// Only prometheus supported currently
			continue
		}
	}
	return res
}

func BuildHTTPStatsFilter(class networking.ListenerClass, metrics model.TelemetryMetricsProviders) []*hcm.HttpFilter {
	res := []*hcm.HttpFilter{}
	// TODO map is not ordered!
	for provider, metricsCfg := range metrics {
		switch provider.GetProvider().(type) {
		case *meshconfig.MeshConfig_ExtensionProvider_Prometheus:
			cfg := generateStatsConfig(class, metricsCfg)
			vmConfig := constructVMConfig("/etc/istio/extensions/stats-filter.compiled.wasm", "envoy.wasm.stats")
			root := rootIDForClass(class)
			vmConfig.VmConfig.VmId = root

			wasmConfig := &httpwasm.Wasm{
				Config: &wasm.PluginConfig{
					RootId:        root,
					Vm:            vmConfig,
					Configuration: cfg,
				},
			}

			f := &hcm.HttpFilter{
				Name:       StatsFilterName,
				ConfigType: &hcm.HttpFilter_TypedConfig{TypedConfig: util.MessageToAny(wasmConfig)},
			}
			res = append(res, f)
		default:
			// Only prometheus supported currently
			continue
		}
	}
	return res
}

var metricToPrometheusMetric = map[string]string{
	"REQUEST_COUNT":          "requests_total",
	"REQUEST_DURATION":       "request_duration_milliseconds",
	"REQUEST_SIZE":           "request_bytes",
	"RESPONSE_SIZE":          "response_bytes",
	"TCP_OPENED_CONNECTIONS": "tcp_connections_opened_total",
	"TCP_CLOSED_CONNECTIONS": "tcp_connections_closed_total",
	"TCP_SENT_BYTES":         "tcp_sent_bytes_total",
	"TCP_RECEIVED_BYTES":     "tcp_received_bytes_total",
	"GRPC_REQUEST_MESSAGES":  "request_messages_total",
	"GRPC_RESPONSE_MESSAGES": "response_messages_total",
}

func generateStatsConfig(class networking.ListenerClass, metricsCfg map[string]model.MetricOverride) *any.Any {
	cfg := stats.PluginConfig{}
	cfg.DisableHostHeaderFallback = class == networking.ListenerClassSidecarInbound || class == networking.ListenerClassGateway
	for metricNameEnum, override := range metricsCfg {
		metricName, f := metricToPrometheusMetric[metricNameEnum]
		if !f {
			// Not a predefined metric, must be a custom one
			metricName = metricNameEnum
		}
		mc := &stats.MetricConfig{
			Dimensions: map[string]string{},
			Name:       metricName,
			Drop:       override.Disabled.GetValue(),
		}
		for k, v := range override.TagOverrides {
			switch v.Operation {
			case tpb.MetricsOverrides_TagOverride_UPSERT:
				mc.Dimensions[k] = v.GetValue()
			case tpb.MetricsOverrides_TagOverride_REMOVE:
				mc.TagsToRemove = append(mc.TagsToRemove, k)
			}
		}
		cfg.Metrics = append(cfg.Metrics, mc)
	}
	cfgJson, _ := protojson.MarshalOptions{UseProtoNames: true}.Marshal(&cfg)
	return util.MessageToAny(&protobuf.StringValue{Value: string(cfgJson)})
}

func rootIDForClass(class networking.ListenerClass) string {
	switch class {
	case networking.ListenerClassSidecarInbound:
		return "stats_inbound"
	default:
		return "stats_outbound"
	}
}

func statsConfigForClass(class networking.ListenerClass) statsConfig {
	switch class {
	case networking.ListenerClassSidecarInbound:
		return statsConfig{
			Metrics: []*metricsConfig{{
				Dimensions: map[string]string{
					"destination_cluster": "node.metadata['CLUSTER_ID']",
					"source_cluster":      "downstream_peer.cluster_id",
				},
			}},
			DisableHostHeaderFallback: true,
		}
	case networking.ListenerClassGateway:
		return statsConfig{
			DisableHostHeaderFallback: true,
		}
	default:
		return statsConfig{}
	}
}

// TODO: import https://github.com/istio/proxy/blob/master/extensions/stats/config.proto
// and use directly.
type statsConfig struct {
	DisableHostHeaderFallback bool             `json:"disable_host_header_fallback,omitempty"`
	Metrics                   []*metricsConfig `json:"metrics,omitempty"`
}

type metricsConfig struct {
	Dimensions map[string]string `json:"dimensions,omitempty"`
}

// constructVMConfig constructs a VM config. If WASM is enabled, the wasm plugin at filename will be used.
// If not, the builtin (null vm) extension, name, will be used.
func constructVMConfig(filename, name string) *wasm.PluginConfig_VmConfig {
	var vmConfig *wasm.PluginConfig_VmConfig
	if features.EnableWasmTelemetry {
		vmConfig = &wasm.PluginConfig_VmConfig{
			VmConfig: &wasm.VmConfig{
				Runtime:          "envoy.wasm.runtime.v8",
				AllowPrecompiled: true,
				Code: &core.AsyncDataSource{Specifier: &core.AsyncDataSource_Local{
					Local: &core.DataSource{
						Specifier: &core.DataSource_Filename{
							Filename: filename,
						},
					},
				}},
			},
		}
	} else {
		vmConfig = &wasm.PluginConfig_VmConfig{
			VmConfig: &wasm.VmConfig{
				Runtime: "envoy.wasm.runtime.null",
				Code: &core.AsyncDataSource{Specifier: &core.AsyncDataSource_Local{
					Local: &core.DataSource{
						Specifier: &core.DataSource_InlineString{
							InlineString: name,
						},
					},
				}},
			},
		}
	}
	return vmConfig
}

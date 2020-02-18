package main

import (
	"github.com/gogo/protobuf/types"
	"io/ioutil"
	"istio.io/api/annotation"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/proxy"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pkg/bootstrap"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/validation"
	"istio.io/pkg/log"
	"strings"
)

func constructProxyConfig() (meshconfig.ProxyConfig, error) {
	meshConfig, err := getMeshConfig()
	if err != nil {
		return meshconfig.ProxyConfig{}, err
	}
	proxyConfig := mesh.DefaultProxyConfig()
	if meshConfig.DefaultConfig != nil {
		proxyConfig = *meshConfig.DefaultConfig
	}

	proxyConfig.CustomConfigFile = customConfigFile
	proxyConfig.ProxyBootstrapTemplatePath = templateFile
	proxyConfig.ConfigPath = configPath
	proxyConfig.BinaryPath = binaryPath
	proxyConfig.ServiceCluster = serviceCluster
	proxyConfig.DrainDuration = types.DurationProto(drainDuration)
	proxyConfig.ParentShutdownDuration = types.DurationProto(parentShutdownDuration)
	proxyConfig.DiscoveryAddress = discoveryAddress
	proxyConfig.ConnectTimeout = types.DurationProto(connectTimeout)
	proxyConfig.StatsdUdpAddress = statsdUDPAddress

	if envoyMetricsService != "" {
		if ms := fromJSON(envoyMetricsService); ms != nil {
			proxyConfig.EnvoyMetricsService = ms
			appendTLSCerts(ms)
		}
	}
	if envoyAccessLogService != "" {
		if rs := fromJSON(envoyAccessLogService); rs != nil {
			proxyConfig.EnvoyAccessLogService = rs
			appendTLSCerts(rs)
		}
	}
	proxyConfig.ProxyAdminPort = int32(proxyAdminPort)
	proxyConfig.Concurrency = int32(concurrency)

	// resolve statsd address
	if proxyConfig.StatsdUdpAddress != "" {
		addr, err := proxy.ResolveAddr(proxyConfig.StatsdUdpAddress)
		if err != nil {
			// If istio-mixer.istio-system can't be resolved, skip generating the statsd config.
			// (instead of crashing). Mixer is optional.
			log.Warnf("resolve StatsdUdpAddress failed: %v", err)
			proxyConfig.StatsdUdpAddress = ""
		} else {
			proxyConfig.StatsdUdpAddress = addr
		}
	}

	// set tracing config
	if lightstepAddress != "" {
		proxyConfig.Tracing = &meshconfig.Tracing{
			Tracer: &meshconfig.Tracing_Lightstep_{
				Lightstep: &meshconfig.Tracing_Lightstep{
					Address:     lightstepAddress,
					AccessToken: lightstepAccessToken,
					Secure:      lightstepSecure,
					CacertPath:  lightstepCacertPath,
				},
			},
		}
	} else if zipkinAddress != "" {
		proxyConfig.Tracing = &meshconfig.Tracing{
			Tracer: &meshconfig.Tracing_Zipkin_{
				Zipkin: &meshconfig.Tracing_Zipkin{
					Address: zipkinAddress,
				},
			},
		}
	} else if datadogAgentAddress != "" {
		proxyConfig.Tracing = &meshconfig.Tracing{
			Tracer: &meshconfig.Tracing_Datadog_{
				Datadog: &meshconfig.Tracing_Datadog{
					Address: datadogAgentAddress,
				},
			},
		}
	} else if stackdriverTracingEnabled.Get() {
		proxyConfig.Tracing = &meshconfig.Tracing{
			Tracer: &meshconfig.Tracing_Stackdriver_{
				Stackdriver: &meshconfig.Tracing_Stackdriver{
					Debug: stackdriverTracingDebug.Get(),
					MaxNumberOfAnnotations: &types.Int64Value{
						Value: int64(stackdriverTracingMaxNumberOfAnnotations.Get()),
					},
					MaxNumberOfAttributes: &types.Int64Value{
						Value: int64(stackdriverTracingMaxNumberOfAttributes.Get()),
					},
					MaxNumberOfMessageEvents: &types.Int64Value{
						Value: int64(stackdriverTracingMaxNumberOfMessageEvents.Get()),
					},
				},
			},
		}
	}

	if err := validation.ValidateProxyConfig(&proxyConfig); err != nil {
		return meshconfig.ProxyConfig{}, err
	}
	annotations, err := readPodAnnotations()
	if err != nil {
		return meshconfig.ProxyConfig{}, err
	}
	return applyAnnotations(proxyConfig, annotations), nil
}

func readPodAnnotations() (map[string]string, error) {
	b, err := ioutil.ReadFile(constants.PodInfoAnnotationsPath)
	if err != nil {
		return nil, err
	}
	return bootstrap.ParseDownwardAPI(string(b))
}

// Apply any overrides to proxy config from annotations
func applyAnnotations(config meshconfig.ProxyConfig, annos map[string]string) meshconfig.ProxyConfig {
	if v, f := annos[annotation.SidecarDiscoveryAddress.Name]; f {
		config.DiscoveryAddress = v
	}
	return config
}

func getControlPlaneNamespace(podNamespace string) string {
	ns := ""
	if registryID == serviceregistry.Kubernetes {
		partDiscoveryAddress := strings.Split(discoveryAddress, ":")
		discoveryHostname := partDiscoveryAddress[0]
		parts := strings.Split(discoveryHostname, ".")
		if len(parts) == 1 {
			// namespace of pilot is not part of discovery address use
			// pod namespace e.g. istio-pilot:15005
			ns = podNamespace
		} else if len(parts) == 2 {
			// namespace is found in the discovery address
			// e.g. istio-pilot.istio-system:15005
			ns = parts[1]
		} else {
			// discovery address is a remote address. For remote clusters
			// only support the default config, or env variable
			ns = istioNamespaceVar.Get()
			if ns == "" {
				ns = constants.IstioSystemNamespace
			}
		}
	}
	return ns
}

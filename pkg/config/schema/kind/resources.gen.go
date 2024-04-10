// Code generated by pkg/config/schema/codegen/tools/collections.main.go. DO NOT EDIT.

package kind

import (
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
)

const (
	Address Kind = iota
	AuthorizationPolicy
	DNSName
	DestinationRule
	Endpoints
	EnvoyFilter
	GRPCRoute
	Gateway
	GatewayClass
	HTTPRoute
	KubernetesGateway
	PeerAuthentication
	ProxyConfig
	ReferenceGrant
	RequestAuthentication
	Secret
	Service
	Sidecar
	TCPRoute
	TLSRoute
	Telemetry
	UDPRoute
	VirtualService
	WasmPlugin
	WorkloadEntry
	WorkloadGroup
)

func (k Kind) String() string {
	switch k {
	case Address:
		return "Address"
	case AuthorizationPolicy:
		return "AuthorizationPolicy"
	case DNSName:
		return "DNSName"
	case DestinationRule:
		return "DestinationRule"
	case Endpoints:
		return "Endpoints"
	case EnvoyFilter:
		return "EnvoyFilter"
	case GRPCRoute:
		return "GRPCRoute"
	case Gateway:
		return "Gateway"
	case GatewayClass:
		return "GatewayClass"
	case HTTPRoute:
		return "HTTPRoute"
	case KubernetesGateway:
		return "Gateway"
	case PeerAuthentication:
		return "PeerAuthentication"
	case ProxyConfig:
		return "ProxyConfig"
	case ReferenceGrant:
		return "ReferenceGrant"
	case RequestAuthentication:
		return "RequestAuthentication"
	case Secret:
		return "Secret"
	case Service:
		return "Service"
	case Sidecar:
		return "Sidecar"
	case TCPRoute:
		return "TCPRoute"
	case TLSRoute:
		return "TLSRoute"
	case Telemetry:
		return "Telemetry"
	case UDPRoute:
		return "UDPRoute"
	case VirtualService:
		return "VirtualService"
	case WasmPlugin:
		return "WasmPlugin"
	case WorkloadEntry:
		return "WorkloadEntry"
	case WorkloadGroup:
		return "WorkloadGroup"
	default:
		return "Unknown"
	}
}

func MustFromGVK(g config.GroupVersionKind) Kind {
	switch g {
	case gvk.AuthorizationPolicy:
		return AuthorizationPolicy
	case gvk.DestinationRule:
		return DestinationRule
	case gvk.Endpoints:
		return Endpoints
	case gvk.EnvoyFilter:
		return EnvoyFilter
	case gvk.GRPCRoute:
		return GRPCRoute
	case gvk.Gateway:
		return Gateway
	case gvk.GatewayClass:
		return GatewayClass
	case gvk.HTTPRoute:
		return HTTPRoute
	case gvk.KubernetesGateway:
		return KubernetesGateway
	case gvk.PeerAuthentication:
		return PeerAuthentication
	case gvk.ProxyConfig:
		return ProxyConfig
	case gvk.ReferenceGrant:
		return ReferenceGrant
	case gvk.RequestAuthentication:
		return RequestAuthentication
	case gvk.Secret:
		return Secret
	case gvk.Service:
		return Service
	case gvk.Sidecar:
		return Sidecar
	case gvk.TCPRoute:
		return TCPRoute
	case gvk.TLSRoute:
		return TLSRoute
	case gvk.Telemetry:
		return Telemetry
	case gvk.UDPRoute:
		return UDPRoute
	case gvk.VirtualService:
		return VirtualService
	case gvk.WasmPlugin:
		return WasmPlugin
	case gvk.WorkloadEntry:
		return WorkloadEntry
	case gvk.WorkloadGroup:
		return WorkloadGroup
	}

	panic("unknown kind: " + g.String())
}

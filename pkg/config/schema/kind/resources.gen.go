// Code generated by pkg/config/schema/codegen/tools/collections.main.go. DO NOT EDIT.

package kind

import (
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
)

const (
	Unknown Kind = iota
	Address
	AuthorizationPolicy
	BackendTLSPolicy
	CertificateSigningRequest
	ConfigMap
	CustomResourceDefinition
	DNSName
	DaemonSet
	Deployment
	DestinationRule
	EndpointSlice
	Endpoints
	EnvoyFilter
	GRPCRoute
	Gateway
	GatewayClass
	HTTPRoute
	HorizontalPodAutoscaler
	Ingress
	IngressClass
	KubernetesGateway
	Lease
	MeshConfig
	MeshNetworks
	MutatingWebhookConfiguration
	Namespace
	Node
	PeerAuthentication
	Pod
	PodDisruptionBudget
	ProxyConfig
	ReferenceGrant
	RequestAuthentication
	Secret
	Service
	ServiceAccount
	ServiceEntry
	Sidecar
	StatefulSet
	TCPRoute
	TLSRoute
	Telemetry
	UDPRoute
	ValidatingWebhookConfiguration
	VirtualService
	WasmPlugin
	WorkloadEntry
	WorkloadGroup
	XBackendTrafficPolicy
)

func (k Kind) String() string {
	switch k {
	case Address:
		return "Address"
	case AuthorizationPolicy:
		return "AuthorizationPolicy"
	case BackendTLSPolicy:
		return "BackendTLSPolicy"
	case CertificateSigningRequest:
		return "CertificateSigningRequest"
	case ConfigMap:
		return "ConfigMap"
	case CustomResourceDefinition:
		return "CustomResourceDefinition"
	case DNSName:
		return "DNSName"
	case DaemonSet:
		return "DaemonSet"
	case Deployment:
		return "Deployment"
	case DestinationRule:
		return "DestinationRule"
	case EndpointSlice:
		return "EndpointSlice"
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
	case HorizontalPodAutoscaler:
		return "HorizontalPodAutoscaler"
	case Ingress:
		return "Ingress"
	case IngressClass:
		return "IngressClass"
	case KubernetesGateway:
		return "KubernetesGateway"
	case Lease:
		return "Lease"
	case MeshConfig:
		return "MeshConfig"
	case MeshNetworks:
		return "MeshNetworks"
	case MutatingWebhookConfiguration:
		return "MutatingWebhookConfiguration"
	case Namespace:
		return "Namespace"
	case Node:
		return "Node"
	case PeerAuthentication:
		return "PeerAuthentication"
	case Pod:
		return "Pod"
	case PodDisruptionBudget:
		return "PodDisruptionBudget"
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
	case ServiceAccount:
		return "ServiceAccount"
	case ServiceEntry:
		return "ServiceEntry"
	case Sidecar:
		return "Sidecar"
	case StatefulSet:
		return "StatefulSet"
	case TCPRoute:
		return "TCPRoute"
	case TLSRoute:
		return "TLSRoute"
	case Telemetry:
		return "Telemetry"
	case UDPRoute:
		return "UDPRoute"
	case ValidatingWebhookConfiguration:
		return "ValidatingWebhookConfiguration"
	case VirtualService:
		return "VirtualService"
	case WasmPlugin:
		return "WasmPlugin"
	case WorkloadEntry:
		return "WorkloadEntry"
	case WorkloadGroup:
		return "WorkloadGroup"
	case XBackendTrafficPolicy:
		return "XBackendTrafficPolicy"
	default:
		return "Unknown"
	}
}

func FromString(s string) Kind {
	switch s {
	case "Address":
		return Address
	case "AuthorizationPolicy":
		return AuthorizationPolicy
	case "BackendTLSPolicy":
		return BackendTLSPolicy
	case "CertificateSigningRequest":
		return CertificateSigningRequest
	case "ConfigMap":
		return ConfigMap
	case "CustomResourceDefinition":
		return CustomResourceDefinition
	case "DNSName":
		return DNSName
	case "DaemonSet":
		return DaemonSet
	case "Deployment":
		return Deployment
	case "DestinationRule":
		return DestinationRule
	case "EndpointSlice":
		return EndpointSlice
	case "Endpoints":
		return Endpoints
	case "EnvoyFilter":
		return EnvoyFilter
	case "GRPCRoute":
		return GRPCRoute
	case "Gateway":
		return Gateway
	case "GatewayClass":
		return GatewayClass
	case "HTTPRoute":
		return HTTPRoute
	case "HorizontalPodAutoscaler":
		return HorizontalPodAutoscaler
	case "Ingress":
		return Ingress
	case "IngressClass":
		return IngressClass
	case "KubernetesGateway":
		return KubernetesGateway
	case "Lease":
		return Lease
	case "MeshConfig":
		return MeshConfig
	case "MeshNetworks":
		return MeshNetworks
	case "MutatingWebhookConfiguration":
		return MutatingWebhookConfiguration
	case "Namespace":
		return Namespace
	case "Node":
		return Node
	case "PeerAuthentication":
		return PeerAuthentication
	case "Pod":
		return Pod
	case "PodDisruptionBudget":
		return PodDisruptionBudget
	case "ProxyConfig":
		return ProxyConfig
	case "ReferenceGrant":
		return ReferenceGrant
	case "RequestAuthentication":
		return RequestAuthentication
	case "Secret":
		return Secret
	case "Service":
		return Service
	case "ServiceAccount":
		return ServiceAccount
	case "ServiceEntry":
		return ServiceEntry
	case "Sidecar":
		return Sidecar
	case "StatefulSet":
		return StatefulSet
	case "TCPRoute":
		return TCPRoute
	case "TLSRoute":
		return TLSRoute
	case "Telemetry":
		return Telemetry
	case "UDPRoute":
		return UDPRoute
	case "ValidatingWebhookConfiguration":
		return ValidatingWebhookConfiguration
	case "VirtualService":
		return VirtualService
	case "WasmPlugin":
		return WasmPlugin
	case "WorkloadEntry":
		return WorkloadEntry
	case "WorkloadGroup":
		return WorkloadGroup
	case "XBackendTrafficPolicy":
		return XBackendTrafficPolicy
	default:
		return Unknown
	}
}

func MustFromGVK(g config.GroupVersionKind) Kind {
	switch g {
	case gvk.AuthorizationPolicy:
		return AuthorizationPolicy
	case gvk.BackendTLSPolicy:
		return BackendTLSPolicy
	case gvk.CertificateSigningRequest:
		return CertificateSigningRequest
	case gvk.ConfigMap:
		return ConfigMap
	case gvk.CustomResourceDefinition:
		return CustomResourceDefinition
	case gvk.DaemonSet:
		return DaemonSet
	case gvk.Deployment:
		return Deployment
	case gvk.DestinationRule:
		return DestinationRule
	case gvk.EndpointSlice:
		return EndpointSlice
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
	case gvk.HorizontalPodAutoscaler:
		return HorizontalPodAutoscaler
	case gvk.Ingress:
		return Ingress
	case gvk.IngressClass:
		return IngressClass
	case gvk.KubernetesGateway:
		return KubernetesGateway
	case gvk.Lease:
		return Lease
	case gvk.MeshConfig:
		return MeshConfig
	case gvk.MeshNetworks:
		return MeshNetworks
	case gvk.MutatingWebhookConfiguration:
		return MutatingWebhookConfiguration
	case gvk.Namespace:
		return Namespace
	case gvk.Node:
		return Node
	case gvk.PeerAuthentication:
		return PeerAuthentication
	case gvk.Pod:
		return Pod
	case gvk.PodDisruptionBudget:
		return PodDisruptionBudget
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
	case gvk.ServiceAccount:
		return ServiceAccount
	case gvk.ServiceEntry:
		return ServiceEntry
	case gvk.Sidecar:
		return Sidecar
	case gvk.StatefulSet:
		return StatefulSet
	case gvk.TCPRoute:
		return TCPRoute
	case gvk.TLSRoute:
		return TLSRoute
	case gvk.Telemetry:
		return Telemetry
	case gvk.UDPRoute:
		return UDPRoute
	case gvk.ValidatingWebhookConfiguration:
		return ValidatingWebhookConfiguration
	case gvk.VirtualService:
		return VirtualService
	case gvk.WasmPlugin:
		return WasmPlugin
	case gvk.WorkloadEntry:
		return WorkloadEntry
	case gvk.WorkloadGroup:
		return WorkloadGroup
	case gvk.XBackendTrafficPolicy:
		return XBackendTrafficPolicy
	}

	panic("unknown kind: " + g.String())
}

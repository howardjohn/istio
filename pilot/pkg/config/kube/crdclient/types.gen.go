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

// Code generated by pilot/pkg/config/kube/crd/codegen/types.go. DO NOT EDIT!

package crdclient

// This file contains Go definitions for Custom Resource Definition kinds
// to adhere to the idiomatic use of k8s API machinery.
// These definitions are synthesized from Istio configuration type descriptors
// as declared in the Istio config model.

import (
	"context"
	"fmt"

	versionedclient "istio.io/client-go/pkg/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	serviceapisclient "sigs.k8s.io/service-apis/pkg/client/clientset/versioned"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/resource"

	mixerclientv1 "istio.io/api/mixer/v1/config/client"
	networkingv1alpha3 "istio.io/api/networking/v1alpha3"
	securityv1beta1 "istio.io/api/security/v1beta1"
	clientconfigv1alpha3 "istio.io/client-go/pkg/apis/config/v1alpha2"
	clientnetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	clientsecurityv1beta1 "istio.io/client-go/pkg/apis/security/v1beta1"

	servicev1alpha1 "sigs.k8s.io/service-apis/apis/v1alpha1"
)

func create(ic versionedclient.Interface, sc serviceapisclient.Interface, config model.Config, objMeta metav1.ObjectMeta) (metav1.Object, error) {
	switch config.GroupVersionKind() {
	case collections.IstioConfigV1Alpha2Httpapispecbindings.Resource().GroupVersionKind():
		return ic.ConfigV1alpha2().HTTPAPISpecBindings(config.Namespace).Create(context.TODO(), &clientconfigv1alpha3.HTTPAPISpecBinding{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*mixerclientv1.HTTPAPISpecBinding)),
		}, metav1.CreateOptions{})
	case collections.IstioConfigV1Alpha2Httpapispecs.Resource().GroupVersionKind():
		return ic.ConfigV1alpha2().HTTPAPISpecs(config.Namespace).Create(context.TODO(), &clientconfigv1alpha3.HTTPAPISpec{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*mixerclientv1.HTTPAPISpec)),
		}, metav1.CreateOptions{})
	case collections.IstioMixerV1ConfigClientQuotaspecbindings.Resource().GroupVersionKind():
		return ic.ConfigV1alpha2().QuotaSpecBindings(config.Namespace).Create(context.TODO(), &clientconfigv1alpha3.QuotaSpecBinding{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*mixerclientv1.QuotaSpecBinding)),
		}, metav1.CreateOptions{})
	case collections.IstioMixerV1ConfigClientQuotaspecs.Resource().GroupVersionKind():
		return ic.ConfigV1alpha2().QuotaSpecs(config.Namespace).Create(context.TODO(), &clientconfigv1alpha3.QuotaSpec{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*mixerclientv1.QuotaSpec)),
		}, metav1.CreateOptions{})
	case collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind():
		return ic.NetworkingV1alpha3().DestinationRules(config.Namespace).Create(context.TODO(), &clientnetworkingv1alpha3.DestinationRule{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*networkingv1alpha3.DestinationRule)),
		}, metav1.CreateOptions{})
	case collections.IstioNetworkingV1Alpha3Envoyfilters.Resource().GroupVersionKind():
		return ic.NetworkingV1alpha3().EnvoyFilters(config.Namespace).Create(context.TODO(), &clientnetworkingv1alpha3.EnvoyFilter{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*networkingv1alpha3.EnvoyFilter)),
		}, metav1.CreateOptions{})
	case collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind():
		return ic.NetworkingV1alpha3().Gateways(config.Namespace).Create(context.TODO(), &clientnetworkingv1alpha3.Gateway{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*networkingv1alpha3.Gateway)),
		}, metav1.CreateOptions{})
	case collections.IstioNetworkingV1Alpha3Serviceentries.Resource().GroupVersionKind():
		return ic.NetworkingV1alpha3().ServiceEntries(config.Namespace).Create(context.TODO(), &clientnetworkingv1alpha3.ServiceEntry{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*networkingv1alpha3.ServiceEntry)),
		}, metav1.CreateOptions{})
	case collections.IstioNetworkingV1Alpha3Sidecars.Resource().GroupVersionKind():
		return ic.NetworkingV1alpha3().Sidecars(config.Namespace).Create(context.TODO(), &clientnetworkingv1alpha3.Sidecar{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*networkingv1alpha3.Sidecar)),
		}, metav1.CreateOptions{})
	case collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind():
		return ic.NetworkingV1alpha3().VirtualServices(config.Namespace).Create(context.TODO(), &clientnetworkingv1alpha3.VirtualService{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*networkingv1alpha3.VirtualService)),
		}, metav1.CreateOptions{})
	case collections.IstioNetworkingV1Alpha3Workloadentries.Resource().GroupVersionKind():
		return ic.NetworkingV1alpha3().WorkloadEntries(config.Namespace).Create(context.TODO(), &clientnetworkingv1alpha3.WorkloadEntry{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*networkingv1alpha3.WorkloadEntry)),
		}, metav1.CreateOptions{})
	case collections.IstioSecurityV1Beta1Authorizationpolicies.Resource().GroupVersionKind():
		return ic.SecurityV1beta1().AuthorizationPolicies(config.Namespace).Create(context.TODO(), &clientsecurityv1beta1.AuthorizationPolicy{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*securityv1beta1.AuthorizationPolicy)),
		}, metav1.CreateOptions{})
	case collections.IstioSecurityV1Beta1Peerauthentications.Resource().GroupVersionKind():
		return ic.SecurityV1beta1().PeerAuthentications(config.Namespace).Create(context.TODO(), &clientsecurityv1beta1.PeerAuthentication{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*securityv1beta1.PeerAuthentication)),
		}, metav1.CreateOptions{})
	case collections.IstioSecurityV1Beta1Requestauthentications.Resource().GroupVersionKind():
		return ic.SecurityV1beta1().RequestAuthentications(config.Namespace).Create(context.TODO(), &clientsecurityv1beta1.RequestAuthentication{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*securityv1beta1.RequestAuthentication)),
		}, metav1.CreateOptions{})
	case collections.K8SServiceApisV1Alpha1Gatewayclasses.Resource().GroupVersionKind():
		return sc.NetworkingV1alpha1().GatewayClasses().Create(context.TODO(), &servicev1alpha1.GatewayClass{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*servicev1alpha1.GatewayClassSpec)),
		}, metav1.CreateOptions{})
	case collections.K8SServiceApisV1Alpha1Gateways.Resource().GroupVersionKind():
		return sc.NetworkingV1alpha1().Gateways(config.Namespace).Create(context.TODO(), &servicev1alpha1.Gateway{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*servicev1alpha1.GatewaySpec)),
		}, metav1.CreateOptions{})
	case collections.K8SServiceApisV1Alpha1Httproutes.Resource().GroupVersionKind():
		return sc.NetworkingV1alpha1().HTTPRoutes(config.Namespace).Create(context.TODO(), &servicev1alpha1.HTTPRoute{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*servicev1alpha1.HTTPRouteSpec)),
		}, metav1.CreateOptions{})
	case collections.K8SServiceApisV1Alpha1Tcproutes.Resource().GroupVersionKind():
		return sc.NetworkingV1alpha1().TcpRoutes(config.Namespace).Create(context.TODO(), &servicev1alpha1.TcpRoute{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*servicev1alpha1.TcpRouteSpec)),
		}, metav1.CreateOptions{})
	case collections.K8SServiceApisV1Alpha1Trafficsplits.Resource().GroupVersionKind():
		return sc.NetworkingV1alpha1().TrafficSplits(config.Namespace).Create(context.TODO(), &servicev1alpha1.TrafficSplit{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*servicev1alpha1.TrafficSplitSpec)),
		}, metav1.CreateOptions{})
	default:
		return nil, fmt.Errorf("unsupported type: %v", config.GroupVersionKind())
	}
}

func update(ic versionedclient.Interface, sc serviceapisclient.Interface, config model.Config, objMeta metav1.ObjectMeta) (metav1.Object, error) {
	switch config.GroupVersionKind() {
	case collections.IstioConfigV1Alpha2Httpapispecbindings.Resource().GroupVersionKind():
		return ic.ConfigV1alpha2().HTTPAPISpecBindings(config.Namespace).Update(context.TODO(), &clientconfigv1alpha3.HTTPAPISpecBinding{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*mixerclientv1.HTTPAPISpecBinding)),
		}, metav1.UpdateOptions{})
	case collections.IstioConfigV1Alpha2Httpapispecs.Resource().GroupVersionKind():
		return ic.ConfigV1alpha2().HTTPAPISpecs(config.Namespace).Update(context.TODO(), &clientconfigv1alpha3.HTTPAPISpec{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*mixerclientv1.HTTPAPISpec)),
		}, metav1.UpdateOptions{})
	case collections.IstioMixerV1ConfigClientQuotaspecbindings.Resource().GroupVersionKind():
		return ic.ConfigV1alpha2().QuotaSpecBindings(config.Namespace).Update(context.TODO(), &clientconfigv1alpha3.QuotaSpecBinding{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*mixerclientv1.QuotaSpecBinding)),
		}, metav1.UpdateOptions{})
	case collections.IstioMixerV1ConfigClientQuotaspecs.Resource().GroupVersionKind():
		return ic.ConfigV1alpha2().QuotaSpecs(config.Namespace).Update(context.TODO(), &clientconfigv1alpha3.QuotaSpec{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*mixerclientv1.QuotaSpec)),
		}, metav1.UpdateOptions{})
	case collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind():
		return ic.NetworkingV1alpha3().DestinationRules(config.Namespace).Update(context.TODO(), &clientnetworkingv1alpha3.DestinationRule{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*networkingv1alpha3.DestinationRule)),
		}, metav1.UpdateOptions{})
	case collections.IstioNetworkingV1Alpha3Envoyfilters.Resource().GroupVersionKind():
		return ic.NetworkingV1alpha3().EnvoyFilters(config.Namespace).Update(context.TODO(), &clientnetworkingv1alpha3.EnvoyFilter{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*networkingv1alpha3.EnvoyFilter)),
		}, metav1.UpdateOptions{})
	case collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind():
		return ic.NetworkingV1alpha3().Gateways(config.Namespace).Update(context.TODO(), &clientnetworkingv1alpha3.Gateway{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*networkingv1alpha3.Gateway)),
		}, metav1.UpdateOptions{})
	case collections.IstioNetworkingV1Alpha3Serviceentries.Resource().GroupVersionKind():
		return ic.NetworkingV1alpha3().ServiceEntries(config.Namespace).Update(context.TODO(), &clientnetworkingv1alpha3.ServiceEntry{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*networkingv1alpha3.ServiceEntry)),
		}, metav1.UpdateOptions{})
	case collections.IstioNetworkingV1Alpha3Sidecars.Resource().GroupVersionKind():
		return ic.NetworkingV1alpha3().Sidecars(config.Namespace).Update(context.TODO(), &clientnetworkingv1alpha3.Sidecar{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*networkingv1alpha3.Sidecar)),
		}, metav1.UpdateOptions{})
	case collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind():
		return ic.NetworkingV1alpha3().VirtualServices(config.Namespace).Update(context.TODO(), &clientnetworkingv1alpha3.VirtualService{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*networkingv1alpha3.VirtualService)),
		}, metav1.UpdateOptions{})
	case collections.IstioNetworkingV1Alpha3Workloadentries.Resource().GroupVersionKind():
		return ic.NetworkingV1alpha3().WorkloadEntries(config.Namespace).Update(context.TODO(), &clientnetworkingv1alpha3.WorkloadEntry{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*networkingv1alpha3.WorkloadEntry)),
		}, metav1.UpdateOptions{})
	case collections.IstioSecurityV1Beta1Authorizationpolicies.Resource().GroupVersionKind():
		return ic.SecurityV1beta1().AuthorizationPolicies(config.Namespace).Update(context.TODO(), &clientsecurityv1beta1.AuthorizationPolicy{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*securityv1beta1.AuthorizationPolicy)),
		}, metav1.UpdateOptions{})
	case collections.IstioSecurityV1Beta1Peerauthentications.Resource().GroupVersionKind():
		return ic.SecurityV1beta1().PeerAuthentications(config.Namespace).Update(context.TODO(), &clientsecurityv1beta1.PeerAuthentication{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*securityv1beta1.PeerAuthentication)),
		}, metav1.UpdateOptions{})
	case collections.IstioSecurityV1Beta1Requestauthentications.Resource().GroupVersionKind():
		return ic.SecurityV1beta1().RequestAuthentications(config.Namespace).Update(context.TODO(), &clientsecurityv1beta1.RequestAuthentication{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*securityv1beta1.RequestAuthentication)),
		}, metav1.UpdateOptions{})
	case collections.K8SServiceApisV1Alpha1Gatewayclasses.Resource().GroupVersionKind():
		return sc.NetworkingV1alpha1().GatewayClasses().Update(context.TODO(), &servicev1alpha1.GatewayClass{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*servicev1alpha1.GatewayClassSpec)),
		}, metav1.UpdateOptions{})
	case collections.K8SServiceApisV1Alpha1Gateways.Resource().GroupVersionKind():
		return sc.NetworkingV1alpha1().Gateways(config.Namespace).Update(context.TODO(), &servicev1alpha1.Gateway{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*servicev1alpha1.GatewaySpec)),
		}, metav1.UpdateOptions{})
	case collections.K8SServiceApisV1Alpha1Httproutes.Resource().GroupVersionKind():
		return sc.NetworkingV1alpha1().HTTPRoutes(config.Namespace).Update(context.TODO(), &servicev1alpha1.HTTPRoute{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*servicev1alpha1.HTTPRouteSpec)),
		}, metav1.UpdateOptions{})
	case collections.K8SServiceApisV1Alpha1Tcproutes.Resource().GroupVersionKind():
		return sc.NetworkingV1alpha1().TcpRoutes(config.Namespace).Update(context.TODO(), &servicev1alpha1.TcpRoute{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*servicev1alpha1.TcpRouteSpec)),
		}, metav1.UpdateOptions{})
	case collections.K8SServiceApisV1Alpha1Trafficsplits.Resource().GroupVersionKind():
		return sc.NetworkingV1alpha1().TrafficSplits(config.Namespace).Update(context.TODO(), &servicev1alpha1.TrafficSplit{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*servicev1alpha1.TrafficSplitSpec)),
		}, metav1.UpdateOptions{})
	default:
		return nil, fmt.Errorf("unsupported type: %v", config.GroupVersionKind())
	}
}

func delete(ic versionedclient.Interface, sc serviceapisclient.Interface, typ resource.GroupVersionKind, name, namespace string) error {
	switch typ {
	case collections.IstioConfigV1Alpha2Httpapispecbindings.Resource().GroupVersionKind():
		return ic.ConfigV1alpha2().HTTPAPISpecBindings(namespace).Delete(context.TODO(), name, metav1.DeleteOptions{})
	case collections.IstioConfigV1Alpha2Httpapispecs.Resource().GroupVersionKind():
		return ic.ConfigV1alpha2().HTTPAPISpecs(namespace).Delete(context.TODO(), name, metav1.DeleteOptions{})
	case collections.IstioMixerV1ConfigClientQuotaspecbindings.Resource().GroupVersionKind():
		return ic.ConfigV1alpha2().QuotaSpecBindings(namespace).Delete(context.TODO(), name, metav1.DeleteOptions{})
	case collections.IstioMixerV1ConfigClientQuotaspecs.Resource().GroupVersionKind():
		return ic.ConfigV1alpha2().QuotaSpecs(namespace).Delete(context.TODO(), name, metav1.DeleteOptions{})
	case collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind():
		return ic.NetworkingV1alpha3().DestinationRules(namespace).Delete(context.TODO(), name, metav1.DeleteOptions{})
	case collections.IstioNetworkingV1Alpha3Envoyfilters.Resource().GroupVersionKind():
		return ic.NetworkingV1alpha3().EnvoyFilters(namespace).Delete(context.TODO(), name, metav1.DeleteOptions{})
	case collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind():
		return ic.NetworkingV1alpha3().Gateways(namespace).Delete(context.TODO(), name, metav1.DeleteOptions{})
	case collections.IstioNetworkingV1Alpha3Serviceentries.Resource().GroupVersionKind():
		return ic.NetworkingV1alpha3().ServiceEntries(namespace).Delete(context.TODO(), name, metav1.DeleteOptions{})
	case collections.IstioNetworkingV1Alpha3Sidecars.Resource().GroupVersionKind():
		return ic.NetworkingV1alpha3().Sidecars(namespace).Delete(context.TODO(), name, metav1.DeleteOptions{})
	case collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind():
		return ic.NetworkingV1alpha3().VirtualServices(namespace).Delete(context.TODO(), name, metav1.DeleteOptions{})
	case collections.IstioNetworkingV1Alpha3Workloadentries.Resource().GroupVersionKind():
		return ic.NetworkingV1alpha3().WorkloadEntries(namespace).Delete(context.TODO(), name, metav1.DeleteOptions{})
	case collections.IstioSecurityV1Beta1Authorizationpolicies.Resource().GroupVersionKind():
		return ic.SecurityV1beta1().AuthorizationPolicies(namespace).Delete(context.TODO(), name, metav1.DeleteOptions{})
	case collections.IstioSecurityV1Beta1Peerauthentications.Resource().GroupVersionKind():
		return ic.SecurityV1beta1().PeerAuthentications(namespace).Delete(context.TODO(), name, metav1.DeleteOptions{})
	case collections.IstioSecurityV1Beta1Requestauthentications.Resource().GroupVersionKind():
		return ic.SecurityV1beta1().RequestAuthentications(namespace).Delete(context.TODO(), name, metav1.DeleteOptions{})
	case collections.K8SServiceApisV1Alpha1Gatewayclasses.Resource().GroupVersionKind():
		return sc.NetworkingV1alpha1().GatewayClasses().Delete(context.TODO(), name, metav1.DeleteOptions{})
	case collections.K8SServiceApisV1Alpha1Gateways.Resource().GroupVersionKind():
		return sc.NetworkingV1alpha1().Gateways(namespace).Delete(context.TODO(), name, metav1.DeleteOptions{})
	case collections.K8SServiceApisV1Alpha1Httproutes.Resource().GroupVersionKind():
		return sc.NetworkingV1alpha1().HTTPRoutes(namespace).Delete(context.TODO(), name, metav1.DeleteOptions{})
	case collections.K8SServiceApisV1Alpha1Tcproutes.Resource().GroupVersionKind():
		return sc.NetworkingV1alpha1().TcpRoutes(namespace).Delete(context.TODO(), name, metav1.DeleteOptions{})
	case collections.K8SServiceApisV1Alpha1Trafficsplits.Resource().GroupVersionKind():
		return sc.NetworkingV1alpha1().TrafficSplits(namespace).Delete(context.TODO(), name, metav1.DeleteOptions{})
	default:
		return fmt.Errorf("unsupported type: %v", typ)
	}
}

var translationMap = map[resource.GroupVersionKind]func(r runtime.Object) *model.Config{
	collections.IstioConfigV1Alpha2Httpapispecbindings.Resource().GroupVersionKind(): func(r runtime.Object) *model.Config {
		obj := r.(*clientconfigv1alpha3.HTTPAPISpecBinding)
		return &model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:              collections.IstioConfigV1Alpha2Httpapispecbindings.Resource().Kind(),
				Group:             collections.IstioConfigV1Alpha2Httpapispecbindings.Resource().Group(),
				Version:           collections.IstioConfigV1Alpha2Httpapispecbindings.Resource().Version(),
				Name:              obj.Name,
				Namespace:         obj.Namespace,
				Labels:            obj.Labels,
				Annotations:       obj.Annotations,
				ResourceVersion:   obj.ResourceVersion,
				CreationTimestamp: obj.CreationTimestamp.Time,
			},
			Spec: &obj.Spec,
		}
	},
	collections.IstioConfigV1Alpha2Httpapispecs.Resource().GroupVersionKind(): func(r runtime.Object) *model.Config {
		obj := r.(*clientconfigv1alpha3.HTTPAPISpec)
		return &model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:              collections.IstioConfigV1Alpha2Httpapispecs.Resource().Kind(),
				Group:             collections.IstioConfigV1Alpha2Httpapispecs.Resource().Group(),
				Version:           collections.IstioConfigV1Alpha2Httpapispecs.Resource().Version(),
				Name:              obj.Name,
				Namespace:         obj.Namespace,
				Labels:            obj.Labels,
				Annotations:       obj.Annotations,
				ResourceVersion:   obj.ResourceVersion,
				CreationTimestamp: obj.CreationTimestamp.Time,
			},
			Spec: &obj.Spec,
		}
	},
	collections.IstioMixerV1ConfigClientQuotaspecbindings.Resource().GroupVersionKind(): func(r runtime.Object) *model.Config {
		obj := r.(*clientconfigv1alpha3.QuotaSpecBinding)
		return &model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:              collections.IstioMixerV1ConfigClientQuotaspecbindings.Resource().Kind(),
				Group:             collections.IstioMixerV1ConfigClientQuotaspecbindings.Resource().Group(),
				Version:           collections.IstioMixerV1ConfigClientQuotaspecbindings.Resource().Version(),
				Name:              obj.Name,
				Namespace:         obj.Namespace,
				Labels:            obj.Labels,
				Annotations:       obj.Annotations,
				ResourceVersion:   obj.ResourceVersion,
				CreationTimestamp: obj.CreationTimestamp.Time,
			},
			Spec: &obj.Spec,
		}
	},
	collections.IstioMixerV1ConfigClientQuotaspecs.Resource().GroupVersionKind(): func(r runtime.Object) *model.Config {
		obj := r.(*clientconfigv1alpha3.QuotaSpec)
		return &model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:              collections.IstioMixerV1ConfigClientQuotaspecs.Resource().Kind(),
				Group:             collections.IstioMixerV1ConfigClientQuotaspecs.Resource().Group(),
				Version:           collections.IstioMixerV1ConfigClientQuotaspecs.Resource().Version(),
				Name:              obj.Name,
				Namespace:         obj.Namespace,
				Labels:            obj.Labels,
				Annotations:       obj.Annotations,
				ResourceVersion:   obj.ResourceVersion,
				CreationTimestamp: obj.CreationTimestamp.Time,
			},
			Spec: &obj.Spec,
		}
	},
	collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind(): func(r runtime.Object) *model.Config {
		obj := r.(*clientnetworkingv1alpha3.DestinationRule)
		return &model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:              collections.IstioNetworkingV1Alpha3Destinationrules.Resource().Kind(),
				Group:             collections.IstioNetworkingV1Alpha3Destinationrules.Resource().Group(),
				Version:           collections.IstioNetworkingV1Alpha3Destinationrules.Resource().Version(),
				Name:              obj.Name,
				Namespace:         obj.Namespace,
				Labels:            obj.Labels,
				Annotations:       obj.Annotations,
				ResourceVersion:   obj.ResourceVersion,
				CreationTimestamp: obj.CreationTimestamp.Time,
			},
			Spec: &obj.Spec,
		}
	},
	collections.IstioNetworkingV1Alpha3Envoyfilters.Resource().GroupVersionKind(): func(r runtime.Object) *model.Config {
		obj := r.(*clientnetworkingv1alpha3.EnvoyFilter)
		return &model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:              collections.IstioNetworkingV1Alpha3Envoyfilters.Resource().Kind(),
				Group:             collections.IstioNetworkingV1Alpha3Envoyfilters.Resource().Group(),
				Version:           collections.IstioNetworkingV1Alpha3Envoyfilters.Resource().Version(),
				Name:              obj.Name,
				Namespace:         obj.Namespace,
				Labels:            obj.Labels,
				Annotations:       obj.Annotations,
				ResourceVersion:   obj.ResourceVersion,
				CreationTimestamp: obj.CreationTimestamp.Time,
			},
			Spec: &obj.Spec,
		}
	},
	collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind(): func(r runtime.Object) *model.Config {
		obj := r.(*clientnetworkingv1alpha3.Gateway)
		return &model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:              collections.IstioNetworkingV1Alpha3Gateways.Resource().Kind(),
				Group:             collections.IstioNetworkingV1Alpha3Gateways.Resource().Group(),
				Version:           collections.IstioNetworkingV1Alpha3Gateways.Resource().Version(),
				Name:              obj.Name,
				Namespace:         obj.Namespace,
				Labels:            obj.Labels,
				Annotations:       obj.Annotations,
				ResourceVersion:   obj.ResourceVersion,
				CreationTimestamp: obj.CreationTimestamp.Time,
			},
			Spec: &obj.Spec,
		}
	},
	collections.IstioNetworkingV1Alpha3Serviceentries.Resource().GroupVersionKind(): func(r runtime.Object) *model.Config {
		obj := r.(*clientnetworkingv1alpha3.ServiceEntry)
		return &model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:              collections.IstioNetworkingV1Alpha3Serviceentries.Resource().Kind(),
				Group:             collections.IstioNetworkingV1Alpha3Serviceentries.Resource().Group(),
				Version:           collections.IstioNetworkingV1Alpha3Serviceentries.Resource().Version(),
				Name:              obj.Name,
				Namespace:         obj.Namespace,
				Labels:            obj.Labels,
				Annotations:       obj.Annotations,
				ResourceVersion:   obj.ResourceVersion,
				CreationTimestamp: obj.CreationTimestamp.Time,
			},
			Spec: &obj.Spec,
		}
	},
	collections.IstioNetworkingV1Alpha3Sidecars.Resource().GroupVersionKind(): func(r runtime.Object) *model.Config {
		obj := r.(*clientnetworkingv1alpha3.Sidecar)
		return &model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:              collections.IstioNetworkingV1Alpha3Sidecars.Resource().Kind(),
				Group:             collections.IstioNetworkingV1Alpha3Sidecars.Resource().Group(),
				Version:           collections.IstioNetworkingV1Alpha3Sidecars.Resource().Version(),
				Name:              obj.Name,
				Namespace:         obj.Namespace,
				Labels:            obj.Labels,
				Annotations:       obj.Annotations,
				ResourceVersion:   obj.ResourceVersion,
				CreationTimestamp: obj.CreationTimestamp.Time,
			},
			Spec: &obj.Spec,
		}
	},
	collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(): func(r runtime.Object) *model.Config {
		obj := r.(*clientnetworkingv1alpha3.VirtualService)
		return &model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:              collections.IstioNetworkingV1Alpha3Virtualservices.Resource().Kind(),
				Group:             collections.IstioNetworkingV1Alpha3Virtualservices.Resource().Group(),
				Version:           collections.IstioNetworkingV1Alpha3Virtualservices.Resource().Version(),
				Name:              obj.Name,
				Namespace:         obj.Namespace,
				Labels:            obj.Labels,
				Annotations:       obj.Annotations,
				ResourceVersion:   obj.ResourceVersion,
				CreationTimestamp: obj.CreationTimestamp.Time,
			},
			Spec: &obj.Spec,
		}
	},
	collections.IstioNetworkingV1Alpha3Workloadentries.Resource().GroupVersionKind(): func(r runtime.Object) *model.Config {
		obj := r.(*clientnetworkingv1alpha3.WorkloadEntry)
		return &model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:              collections.IstioNetworkingV1Alpha3Workloadentries.Resource().Kind(),
				Group:             collections.IstioNetworkingV1Alpha3Workloadentries.Resource().Group(),
				Version:           collections.IstioNetworkingV1Alpha3Workloadentries.Resource().Version(),
				Name:              obj.Name,
				Namespace:         obj.Namespace,
				Labels:            obj.Labels,
				Annotations:       obj.Annotations,
				ResourceVersion:   obj.ResourceVersion,
				CreationTimestamp: obj.CreationTimestamp.Time,
			},
			Spec: &obj.Spec,
		}
	},
	collections.IstioSecurityV1Beta1Authorizationpolicies.Resource().GroupVersionKind(): func(r runtime.Object) *model.Config {
		obj := r.(*clientsecurityv1beta1.AuthorizationPolicy)
		return &model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:              collections.IstioSecurityV1Beta1Authorizationpolicies.Resource().Kind(),
				Group:             collections.IstioSecurityV1Beta1Authorizationpolicies.Resource().Group(),
				Version:           collections.IstioSecurityV1Beta1Authorizationpolicies.Resource().Version(),
				Name:              obj.Name,
				Namespace:         obj.Namespace,
				Labels:            obj.Labels,
				Annotations:       obj.Annotations,
				ResourceVersion:   obj.ResourceVersion,
				CreationTimestamp: obj.CreationTimestamp.Time,
			},
			Spec: &obj.Spec,
		}
	},
	collections.IstioSecurityV1Beta1Peerauthentications.Resource().GroupVersionKind(): func(r runtime.Object) *model.Config {
		obj := r.(*clientsecurityv1beta1.PeerAuthentication)
		return &model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:              collections.IstioSecurityV1Beta1Peerauthentications.Resource().Kind(),
				Group:             collections.IstioSecurityV1Beta1Peerauthentications.Resource().Group(),
				Version:           collections.IstioSecurityV1Beta1Peerauthentications.Resource().Version(),
				Name:              obj.Name,
				Namespace:         obj.Namespace,
				Labels:            obj.Labels,
				Annotations:       obj.Annotations,
				ResourceVersion:   obj.ResourceVersion,
				CreationTimestamp: obj.CreationTimestamp.Time,
			},
			Spec: &obj.Spec,
		}
	},
	collections.IstioSecurityV1Beta1Requestauthentications.Resource().GroupVersionKind(): func(r runtime.Object) *model.Config {
		obj := r.(*clientsecurityv1beta1.RequestAuthentication)
		return &model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:              collections.IstioSecurityV1Beta1Requestauthentications.Resource().Kind(),
				Group:             collections.IstioSecurityV1Beta1Requestauthentications.Resource().Group(),
				Version:           collections.IstioSecurityV1Beta1Requestauthentications.Resource().Version(),
				Name:              obj.Name,
				Namespace:         obj.Namespace,
				Labels:            obj.Labels,
				Annotations:       obj.Annotations,
				ResourceVersion:   obj.ResourceVersion,
				CreationTimestamp: obj.CreationTimestamp.Time,
			},
			Spec: &obj.Spec,
		}
	},
	collections.K8SServiceApisV1Alpha1Gatewayclasses.Resource().GroupVersionKind(): func(r runtime.Object) *model.Config {
		obj := r.(*servicev1alpha1.GatewayClass)
		return &model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:              collections.K8SServiceApisV1Alpha1Gatewayclasses.Resource().Kind(),
				Group:             collections.K8SServiceApisV1Alpha1Gatewayclasses.Resource().Group(),
				Version:           collections.K8SServiceApisV1Alpha1Gatewayclasses.Resource().Version(),
				Name:              obj.Name,
				Namespace:         obj.Namespace,
				Labels:            obj.Labels,
				Annotations:       obj.Annotations,
				ResourceVersion:   obj.ResourceVersion,
				CreationTimestamp: obj.CreationTimestamp.Time,
			},
			Spec: &obj.Spec,
		}
	},
	collections.K8SServiceApisV1Alpha1Gateways.Resource().GroupVersionKind(): func(r runtime.Object) *model.Config {
		obj := r.(*servicev1alpha1.Gateway)
		return &model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:              collections.K8SServiceApisV1Alpha1Gateways.Resource().Kind(),
				Group:             collections.K8SServiceApisV1Alpha1Gateways.Resource().Group(),
				Version:           collections.K8SServiceApisV1Alpha1Gateways.Resource().Version(),
				Name:              obj.Name,
				Namespace:         obj.Namespace,
				Labels:            obj.Labels,
				Annotations:       obj.Annotations,
				ResourceVersion:   obj.ResourceVersion,
				CreationTimestamp: obj.CreationTimestamp.Time,
			},
			Spec: &obj.Spec,
		}
	},
	collections.K8SServiceApisV1Alpha1Httproutes.Resource().GroupVersionKind(): func(r runtime.Object) *model.Config {
		obj := r.(*servicev1alpha1.HTTPRoute)
		return &model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:              collections.K8SServiceApisV1Alpha1Httproutes.Resource().Kind(),
				Group:             collections.K8SServiceApisV1Alpha1Httproutes.Resource().Group(),
				Version:           collections.K8SServiceApisV1Alpha1Httproutes.Resource().Version(),
				Name:              obj.Name,
				Namespace:         obj.Namespace,
				Labels:            obj.Labels,
				Annotations:       obj.Annotations,
				ResourceVersion:   obj.ResourceVersion,
				CreationTimestamp: obj.CreationTimestamp.Time,
			},
			Spec: &obj.Spec,
		}
	},
	collections.K8SServiceApisV1Alpha1Tcproutes.Resource().GroupVersionKind(): func(r runtime.Object) *model.Config {
		obj := r.(*servicev1alpha1.TcpRoute)
		return &model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:              collections.K8SServiceApisV1Alpha1Tcproutes.Resource().Kind(),
				Group:             collections.K8SServiceApisV1Alpha1Tcproutes.Resource().Group(),
				Version:           collections.K8SServiceApisV1Alpha1Tcproutes.Resource().Version(),
				Name:              obj.Name,
				Namespace:         obj.Namespace,
				Labels:            obj.Labels,
				Annotations:       obj.Annotations,
				ResourceVersion:   obj.ResourceVersion,
				CreationTimestamp: obj.CreationTimestamp.Time,
			},
			Spec: &obj.Spec,
		}
	},
	collections.K8SServiceApisV1Alpha1Trafficsplits.Resource().GroupVersionKind(): func(r runtime.Object) *model.Config {
		obj := r.(*servicev1alpha1.TrafficSplit)
		return &model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:              collections.K8SServiceApisV1Alpha1Trafficsplits.Resource().Kind(),
				Group:             collections.K8SServiceApisV1Alpha1Trafficsplits.Resource().Group(),
				Version:           collections.K8SServiceApisV1Alpha1Trafficsplits.Resource().Version(),
				Name:              obj.Name,
				Namespace:         obj.Namespace,
				Labels:            obj.Labels,
				Annotations:       obj.Annotations,
				ResourceVersion:   obj.ResourceVersion,
				CreationTimestamp: obj.CreationTimestamp.Time,
			},
			Spec: &obj.Spec,
		}
	},
}

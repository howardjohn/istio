package crdclient

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeSchema "k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	networking "istio.io/api/networking/v1alpha3"
	istiofake "istio.io/client-go/pkg/clientset/versioned/fake"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config/schema/collections"
)

func TestClient(t *testing.T) {
	fakeClient := istiofake.NewSimpleClientset()
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	schemas := collections.Pilot
	config, err := New(fakeClient, dynamicClient, schemas, nil, "", controller.Options{})
	if err != nil {
		t.Fatal(err)
	}
	rev, err := config.Create(model.Config{
		ConfigMeta: model.ConfigMeta{
			Name:      "foo",
			Namespace: "default",
			Type:      collections.IstioNetworkingV1Alpha3Gateways.Resource().Kind(),
			Version:   collections.IstioNetworkingV1Alpha3Gateways.Resource().Version(),
			Group:     collections.IstioNetworkingV1Alpha3Gateways.Resource().Group(),
		},
		Spec: &networking.Gateway{
			Servers: []*networking.Server{
				{
					Port: &networking.Port{
						Number:   80,
						Protocol: "HTTP",
						Name:     "http",
					},
					Hosts: []string{"*.example.com"},
				},
			},
		},
	})
	t.Log(rev, err)
	cfg := config.Get(collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(), "foo", "default")
	t.Log(cfg)
	cfgs, err := config.List(collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(), "default")
	t.Log(cfgs, err)
	vs, err := fakeClient.NetworkingV1alpha3().VirtualServices("default").List(context.TODO(), metav1.ListOptions{})
	t.Log(vs.Items, err)
	vs2, err := dynamicClient.Resource(kubeSchema.GroupVersionResource{
		Group:    collections.IstioNetworkingV1Alpha3Gateways.Resource().Group(),
		Version:  collections.IstioNetworkingV1Alpha3Gateways.Resource().Version(),
		Resource: collections.IstioNetworkingV1Alpha3Gateways.Resource().Plural(),
	}).Namespace("default").List(context.TODO(), metav1.ListOptions{})
	t.Log(vs2.Items, err)
}

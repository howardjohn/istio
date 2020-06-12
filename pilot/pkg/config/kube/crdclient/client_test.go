package crdclient

import (
	"testing"

	networking "istio.io/api/networking/v1alpha3"
	istiofake "istio.io/client-go/pkg/clientset/versioned/fake"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pilot/test/mock"
	"istio.io/istio/pkg/config/schema/collections"
)

func makeClient(t *testing.T) model.ConfigStoreCache {
	fakeClient := istiofake.NewSimpleClientset()
	stop := make(chan struct{})
	schemas := collections.Pilot
	config, err := New(fakeClient, schemas, &model.DisabledLedger{}, "", controller.Options{})
	if err != nil {
		t.Fatal(err)
	}
	go config.Run(stop)
	cache.WaitForCacheSync(stop, config.HasSynced)
	t.Cleanup(func() {
		close(stop)
	})
	return config
}

func TestClient(t *testing.T) {
	t.Run("invariant", func(t *testing.T) {
		client := makeClient(t)
		mock.CheckIstioConfigTypes(client, "some-namespace", t)
	})
	t.Run("crud", func(t *testing.T) {
		client := makeClient(t)
		_, err := client.Create(model.Config{
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
		cfg := client.Get(collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind(), "foo", "default")
		if cfg.Name != "foo" || cfg.Spec == nil {
			t.Fatalf("couldn't read created gateway, got %v", cfg)
		}
		cfgs, err := client.List(collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind(), "default")
		if err != nil {
			t.Fatal(err)
		}
		if len(cfgs) != 1 {
			t.Fatalf("expected 1 item in list, got %v: %v", len(cfgs), cfgs)
		}
	})
}

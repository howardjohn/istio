package crdclient_test

import (
	"fmt"
	"testing"
	"time"

	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/config/kube/crdclient"
	"istio.io/istio/pilot/pkg/model"
	controller2 "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config/schema/collections"
)

var nextWrite = 0

func buildNewClient(b *testing.B) model.ConfigStoreCache {
	restConfig, err := clientcmd.BuildConfigFromFlags("", "/home/howardjohn/.kube/config")
	restConfig.QPS = 100000
	restConfig.Burst = 100000
	if err != nil {
		b.Fatalf("Failed to create k8s rest client: %s", err)
	}
	config, err := crdclient.NewForConfig(restConfig, &model.DisabledLedger{}, "", controller2.Options{})
	if err != nil {
		b.Fatal(err)
	}
	stop := make(chan struct{})
	go config.Run(stop)
	cache.WaitForCacheSync(stop, config.HasSynced)
	b.Cleanup(func() {
		close(stop)
	})

	return config
}

func BenchmarkCRD(b *testing.B) {
	config := buildNewClient(b)
	for n := 0; n < 200; n++ {
		nextWrite++
		name := fmt.Sprintf("test-gw-%d", nextWrite)
		namespace := "test-ns"
		log.Infof("writing %v", name)
		_, err := config.Create(model.Config{
			ConfigMeta: model.ConfigMeta{
				Name:      name,
				Namespace: namespace,
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
		if err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()

	name := "test-gw-1"
	namespace := "test-ns"
	for {
		log.Infof("checking sync")
		l, err := config.List(collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind(), namespace)
		log.Infof("got %d items with error %v", len(l), err)
		if len(l) == 3 {
			break
		} else {
			time.Sleep(time.Millisecond * 10)

		}
	}

	b.Run("list", func(b *testing.B) {

		for n := 0; n < b.N; n++ {
			_ = config.Get(collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind(), name, namespace)
		}
	})
	b.Run("get", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			_, err := config.List(collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind(), namespace)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

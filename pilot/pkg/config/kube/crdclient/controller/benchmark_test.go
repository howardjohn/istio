package controller_test

import (
	"fmt"
	"testing"
	"time"

	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/pkg/log"

	legacy "istio.io/istio/pilot/pkg/config/kube/crd/controller"
	"istio.io/istio/pilot/pkg/config/kube/crdclient/controller"
	"istio.io/istio/pilot/pkg/model"
	controller2 "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config/schema/collections"
)

var nextWrite = 0

func buildNewClient(b *testing.B) model.ConfigStoreCache {
	schemas := collections.Pilot
	kubeconfig := "/home/howardjohn/.kube/alt/config"

	if len(kubeconfig) == 0 {
		b.Fatalf("Environment variables KUBECONFIG and NAMESPACE need to be set")
	}

	restConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	restConfig.QPS = 100000
	restConfig.Burst = 100000
	if err != nil {
		b.Fatalf("Failed to create k8s rest client: %s", err)
	}
	config, err := controller.NewForConfig(restConfig, schemas, nil, "", controller2.Options{})
	if err != nil {
		b.Fatal(err)
	}

	return config
}

func buildOldClient(b *testing.B) model.ConfigStoreCache {
	schemas := collections.Pilot
	kubeconfig := "/home/howardjohn/.kube/alt/config"

	if len(kubeconfig) == 0 {
		b.Fatalf("Environment variables KUBECONFIG and NAMESPACE need to be set")
	}

	restConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	restConfig.QPS = 100000
	restConfig.Burst = 100000
	if err != nil {
		b.Fatalf("Failed to create k8s rest client: %s", err)
	}
	config, err := legacy.NewForConfig(restConfig, schemas, "cluster.local", &model.DisabledLedger{}, "")
	if err != nil {
		b.Fatal(err)
	}

	controller := legacy.NewController(config, controller2.Options{})
	var stop chan struct{}
	go controller.Run(stop)
	cache.WaitForCacheSync(stop, controller.HasSynced)
	return controller
}

// Run with -benchtime=100x or results are inconsistent
func BenchmarkCRD(b *testing.B) {
	//config := buildNewClient(b)
	config := buildOldClient(b)
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
		l, err := config.List(collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind(), namespace)
		log.Infof("got %d items with error %v", len(l), err)
		if len(l) == 200 {
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

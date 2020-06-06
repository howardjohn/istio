package controller

import (
	"testing"

	"k8s.io/client-go/tools/clientcmd"

	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config/schema/collections"
)

func TestClient(t *testing.T) {
	schemas := collections.Pilot
	kubeconfig := "/home/howardjohn/.kube/alt/config"

	if len(kubeconfig) == 0 {
		t.Fatalf("Environment variables KUBECONFIG and NAMESPACE need to be set")
	}

	restConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		t.Fatalf("Failed to create k8s rest client: %s", err)
	}
	config, err := NewForConfig(restConfig, schemas, nil, "", controller.Options{})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Get(collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(), "foo", "default")
	t.Log(cfg)
}

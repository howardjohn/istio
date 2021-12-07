package main

import (
	"context"
	"fmt"
	"istio.io/istio/pkg/kube"
	"istio.io/pkg/log"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
)

type Client struct {
	scheme *runtime.Scheme
	client rest.Interface
}

func Get[T any](c *Client, name, namespace string, options metav1.GetOptions) (*T, error) {
	result := new(T)
	x := (interface{})(result).(runtime.Object)
	err := c.client.Get().
		Namespace(namespace).
		Resource("deployments").
		Name(name).
		VersionedParams(&options, runtime.NewParameterCodec(c.scheme)).
		Do(context.Background()).
		Into(x)
	return result, err
}

func main() {
	restConfig, err := kube.DefaultRestConfig("", "")
	if err != nil {
		log.Fatal(err)
	}
	k, err := kube.NewClient(kube.NewClientConfigForRestConfig(restConfig))
	if err != nil {
		log.Fatal(err)
	}
	cfg := k.RESTConfig()
	cfg.APIPath = "/apis/apps"
	rc, err := rest.RESTClientFor(cfg)
	if err != nil {
		log.Fatal(err)
	}
	c := Client{
		scheme: kube.IstioScheme,
		client: rc,
	}
	_ = c
	r, err := Get[appsv1.Deployment](&c, "coredns", "kube-system", metav1.GetOptions{})
	fmt.Println(r, err)
}

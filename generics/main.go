package main

import (
	"context"
	"flag"
	"fmt"
	"istio.io/istio/pkg/kube"
	"istio.io/pkg/log"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

type intOrString interface {
	int | string
}

func test[T intOrString](x T) T {
	return x
}

type Client struct {
	scheme *runtime.Scheme
	client rest.Interface
}

func Get[T runtime.Object](c *Client, name, namespace string, options metav1.GetOptions) (*T, error) {
	result := new(T)
	rx := *result
	err := c.client.Get().
		Namespace(namespace).
		Resource("deployments").
		Name(name).
		VersionedParams(&options, runtime.NewParameterCodec(c.scheme)).
		Do(context.Background()).
		Into(rx)
	return result, err
}

func main() {
	fs := &flag.FlagSet{}
	fs.Set("v", "6")
	klog.InitFlags(fs)
	// --v= flag of klog.
	restConfig, err := kube.DefaultRestConfig("", "")
	if err != nil {
		log.Fatal(err)
	}
	k, err := kube.NewExtendedClient(kube.NewClientConfigForRestConfig(restConfig), "")
	if err != nil {
		log.Fatal(err)
	}
	rc, err := k.UtilFactory().RESTClient()
	if err != nil {
		log.Fatal(err)
	}
	c := Client{
		scheme: kube.IstioScheme,
		client: rc,
	}
	_ = c
	r, err := Get[wrapper](&c, "coredns", "kube-system", metav1.GetOptions{})
	fmt.Println(r, err)
	fmt.Println(test("a"))
	fmt.Println(test(2))
}

type wrapper struct {
	*appsv1.Deployment
}

package main

import (
	"context"
	"fmt"

	"istio.io/istio/pkg/kube"
	"istio.io/pkg/log"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/rest"
)

type Client struct {
	scheme *runtime.Scheme
	client rest.Interface
}

func MustGet[T any](c *Client, name, namespace string) *T {
	res, err := Get[T](c, name, namespace, metav1.GetOptions{})
	if err != nil {
		panic(err.Error())
	}
	return res
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

func MustList[T any](c *Client, namespace string) []T {
	res, err := List[T](c, namespace, metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}
	return res
}

func List[T any](c *Client, namespace string, options metav1.ListOptions) ([]T, error) {
	x := ObjectList[T]{}
	err := c.client.Get().
		Namespace(namespace).
		Resource("deployments").
		VersionedParams(&options, runtime.NewParameterCodec(c.scheme)).
		Do(context.Background()).
		Into(&x)
	return x.Items, err
}

func Watch[T any](c *Client, namespace string, options metav1.ListOptions) (<-chan T, error) {
	x := ObjectList[T]{}
	wi, err := c.client.Get().
		Namespace(namespace).
		Resource("deployments").
		VersionedParams(&options, runtime.NewParameterCodec(c.scheme)).
		Watch(context.Background())
	c := make(<- chan T)
	go func() {
		for {
			select {
				case res := <-wi.ResultChan():
					c<-res
					case <-c
			}
			res, ok := <-wi.ResultChan()
			if !ok {
				wi.
			}
		}
	}()
	return t, err
	return x.Items, err
}

type ObjectList[T any] struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []T `json:"items"`
}

var _ runtime.Object = ObjectList[any]{}

func (o ObjectList[T]) GetObjectKind() schema.ObjectKind {
	return o.TypeMeta.GetObjectKind()
}
func (o ObjectList[T]) DeepCopyObject() runtime.Object {
	n := ObjectList[T]{
		TypeMeta: o.TypeMeta,
		ListMeta: o.ListMeta,
	}
	for _, i := range o.Items {
		x := (interface{})(i).(runtime.Object)
		n.Items = append(n.Items, x.DeepCopyObject().(T))
	}
	return n
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
	c := &Client{
		scheme: kube.IstioScheme,
		client: rc,
	}
	r, err := Get[appsv1.Deployment](c, "coredns", "kube-system", metav1.GetOptions{})
	fmt.Println(r.Name, err)
	for _, dep := range MustList[appsv1.Deployment](c, "istio-system") {
		fmt.Println(dep.Name)
	}
}

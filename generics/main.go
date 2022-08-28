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
	"k8s.io/client-go/rest"
)

type Client struct {
	client rest.Interface
}

func MustGet[T Resource](c *Client, name, namespace string) *T {
	res, err := Get[T](c, name, namespace, metav1.GetOptions{})
	if err != nil {
		panic(err.Error())
	}
	return res
}

type ResourceMetadata struct {
	gv schema.GroupVersion
}

type Resource interface {
	ResourceMetadata() schema.GroupVersion
	ResourceName() string
}

func Get[T Resource](c *Client, name, namespace string, options metav1.GetOptions) (*T, error) {
	result := new(T)
	gv := (*result).ResourceMetadata()
	x := (interface{})(result).(runtime.Object)
	err := c.client.Get().
		Namespace(namespace).
		Resource((*result).ResourceName()).
		Name(name).
		//VersionedParams(&options, runtime.NewParameterCodec(c.scheme)).
		AbsPath(rest.DefaultVersionedAPIPath("apis", gv)).
		Do(context.Background()).
		Into(x)
	return result, err
}

func MustList[T Resource](c *Client, namespace string) []T {
	res, err := List[T](c, namespace, metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}
	return res
}

func List[T Resource](c *Client, namespace string, options metav1.ListOptions) ([]T, error) {
	x := ObjectList[T]{}
	result := new(T)
	gv := (*result).ResourceMetadata()
	err := c.client.Get().
		Namespace(namespace).
		Resource((*result).ResourceName()).
		//VersionedParams(&options, runtime.NewParameterCodec(c.scheme)).
		AbsPath(rest.DefaultVersionedAPIPath("apis", gv)).
		Do(context.Background()).
		Into(&x)
	return x.Items, err
}

func Watch[T Resource](c *Client, namespace string, options metav1.ListOptions) (<-chan T, error) {
	result := new(T)
	gv := (*result).ResourceMetadata()
	wi, err := c.client.Get().
		Namespace(namespace).
		Resource((*result).ResourceName()).
		//VersionedParams(&options, runtime.NewParameterCodec(c.scheme)).
		AbsPath(rest.DefaultVersionedAPIPath("apis", gv)).
		Watch(context.Background())
	if err != nil {
		return nil, err
	}
	cc := make(chan T)
	go func() {
		for {
			select {
			case res, ok := <-wi.ResultChan():
				log.Errorf("howardjohn: %+v %v", res, ok)
				cc <- res.Object.(T)
			}
		}
	}()
	return cc, err
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
	log.EnableKlogWithVerbosity(6)
	restConfig, err := kube.DefaultRestConfig("", "")
	if err != nil {
		log.Fatal(err)
	}
	k, err := kube.NewClient(kube.NewClientConfigForRestConfig(restConfig))
	if err != nil {
		log.Fatal(err)
	}
	cfg := k.RESTConfig()
	cfg.APIPath = "/apis"
	rc, err := rest.RESTClientFor(cfg)
	if err != nil {
		log.Fatal(err)
	}
	c := &Client{
		client: rc,
	}
	r, err := Get[appsv1.Deployment](c, "kube-dns", "kube-system", metav1.GetOptions{})
	fmt.Println(r.Name, err)
	for _, dep := range MustList[appsv1.Deployment](c, "istio-system") {
		fmt.Println(dep.Name)
	}
	cc, err := Watch[appsv1.Deployment](c, "istio-system", metav1.ListOptions{})
	for res := range cc {
		fmt.Println("watch", res.Name)
	}
}

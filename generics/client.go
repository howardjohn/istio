package main

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/rest"
	"k8s.io/kubectl/pkg/scheme"
)

type ObjectList[T any] struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []T `json:"items"`
}

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

var _ runtime.Object = ObjectList[any]{}

type Resource interface {
	ResourceMetadata() schema.GroupVersion
	ResourceName() string
}

type Client struct {
	client rest.Interface
}

func Get[T Resource](c *Client, name, namespace string, options metav1.GetOptions) (*T, error) {
	result := new(T)
	gv := (*result).ResourceMetadata()
	x := (interface{})(result).(runtime.Object)
	err := c.client.Get().
		Namespace(namespace).
		Resource((*result).ResourceName()).
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		AbsPath(defaultPath(gv)).
		Do(context.Background()).
		Into(x)
	return result, err
}

func MustGet[T Resource](c *Client, name, namespace string) *T {
	res, err := Get[T](c, name, namespace, metav1.GetOptions{})
	if err != nil {
		panic(err.Error())
	}
	return res
}

func List[T Resource](c *Client, namespace string, opts metav1.ListOptions) ([]T, error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	x := ObjectList[T]{}
	result := new(T)
	gv := (*result).ResourceMetadata()
	err := c.client.Get().
		Namespace(namespace).
		Resource((*result).ResourceName()).
		Timeout(timeout).
		VersionedParams(&opts, scheme.ParameterCodec).
		AbsPath(defaultPath(gv)).
		Do(context.Background()).
		Into(&x)
	return x.Items, err
}

func MustList[T Resource](c *Client, namespace string) []T {
	res, err := List[T](c, namespace, metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}
	return res
}

func Watch[T Resource](c *Client, namespace string, options metav1.ListOptions) (Watcher[T], error) {
	result := new(T)
	gv := (*result).ResourceMetadata()
	options.Watch = true
	wi, err := c.client.Get().
		Namespace(namespace).
		Resource((*result).ResourceName()).
		VersionedParams(&options, scheme.ParameterCodec).
		AbsPath(defaultPath(gv)).
		Watch(context.Background())
	if err != nil {
		return Watcher[T]{}, err
	}
	return newWatcher[T](wi), nil
}

type Watcher[T Resource] struct {
	inner watch.Interface
	ch    chan T
}

func newWatcher[T Resource](wi watch.Interface) Watcher[T] {
	cc := make(chan T)
	go func() {
		for {
			select {
			case res, ok := <-wi.ResultChan():
				if !ok {
					return
				}
				tt, ok := any(res.Object).(*T)
				if !ok {
					return
				}
				cc <- *tt
			}
		}
	}()
	return Watcher[T]{wi, cc}
}

func (w Watcher[T]) Stop() {
	w.inner.Stop()
	close(w.ch)
}

func (w Watcher[T]) Results() <-chan T {
	return w.ch
}

func defaultPath(gv schema.GroupVersion) string {
	apiPath := "apis"
	if gv.Group == corev1.GroupName {
		apiPath = "api"
	}
	return rest.DefaultVersionedAPIPath(apiPath, gv)
}

package main

import (
	"context"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	client "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
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

type API[T Resource] interface {
	Get(name string, namespace string, options metav1.GetOptions) (*T, error)
	List(namespace string, options metav1.ListOptions) ([]T, error)
	Watch(namespace string, options metav1.ListOptions) (Watcher[T], error)
	Namespace(namespace string) NamespacedAPI[T]
}

type NamespacedAPI[T Resource] interface {
	Get(name string, options metav1.GetOptions) (*T, error)
	List(options metav1.ListOptions) ([]T, error)
	Watch(options metav1.ListOptions) (Watcher[T], error)
}

type api[T Resource] struct {
	c *Client
}

type namespaceApi[T Resource] struct {
	api[T]
	namespace string
}

func (a api[T]) Get(name, namespace string, options metav1.GetOptions) (*T, error) {
	return Get[T](a.c, name, namespace, options)
}

func (a api[T]) List(namespace string, options metav1.ListOptions) ([]T, error) {
	return List[T](a.c, namespace, options)
}

func (a api[T]) Watch(namespace string, options metav1.ListOptions) (Watcher[T], error) {
	return Watch[T](a.c, namespace, options)
}

func (a api[T]) Namespace(namespace string) NamespacedAPI[T] {
	return namespaceApi[T]{a, namespace}
}

func (a namespaceApi[T]) Get(name string, options metav1.GetOptions) (*T, error) {
	return Get[T](a.c, name, a.namespace, options)
}

func (a namespaceApi[T]) List(options metav1.ListOptions) ([]T, error) {
	return List[T](a.c, a.namespace, options)
}

func (a namespaceApi[T]) Watch(options metav1.ListOptions) (Watcher[T], error) {
	return Watch[T](a.c, a.namespace, options)
}

var _ API[Resource] = api[Resource]{}

func NewAPI[T Resource](c *Client) API[T] {
	return api[T]{c: c}
}

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

type Lister[T Resource] interface {
	// List will return all objects across namespaces
	List(selector labels.Selector) (ret []T, err error)
	// Get will attempt to retrieve assuming that name==key
	Get(name string) (T, error)
	// ByNamespace will give you a GenericNamespaceLister for one namespace
	ByNamespace(namespace string) NamespaceLister[T]
}


// GenericNamespaceLister is a lister skin on a generic Indexer
type NamespaceLister[T Resource] interface {
	// List will return all objects in this namespace
	List(selector labels.Selector) (ret []T, err error)
	// Get will attempt to retrieve by namespace and name
	Get(name string) (*T, error)
}


type genericInformer struct {
	informer cache.SharedIndexInformer
	resource schema.GroupResource
}


func Informer[T Resource](c *Client, namespace string) Lister[T] {
	// TODO: make it a factory
	informer := cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				res, err := List[T](c, namespace, options)
				if err != nil {
					return nil, err
				}
				// TODO: convert? Or make our own ListWatch
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.AdmissionregistrationV1().MutatingWebhookConfigurations().Watch(context.TODO(), options)
			},
		},
		&admissionregistrationv1.MutatingWebhookConfiguration{},
		resyncPeriod,
		indexers,
	)
	return nil
}

package main

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type NamespacedAPI[T Resource] interface {
	Create(t T, options metav1.CreateOptions) (*T, error)
	Update(t T, options metav1.UpdateOptions) (*T, error)
	Get(name string, options metav1.GetOptions) (*T, error)
	List(options metav1.ListOptions) ([]T, error)
	Watch(options metav1.ListOptions) (Watcher[T], error)
}

type namespaceApi[T Resource] struct {
	API[T]
	namespace string
}

func Namespaced[T Resource](api API[T], namespace string) NamespacedAPI[T] {
	return &namespaceApi[T]{API: api, namespace: namespace}
}

func (a namespaceApi[T]) Get(name string, options metav1.GetOptions) (*T, error) {
	return a.API.Get(name, a.namespace, options)
}

func (a namespaceApi[T]) List(options metav1.ListOptions) ([]T, error) {
	return a.API.List(a.namespace, options)
}

func (a namespaceApi[T]) Watch(options metav1.ListOptions) (Watcher[T], error) {
	return a.API.Watch(a.namespace, options)
}

func (a namespaceApi[T]) Create(t T, options metav1.CreateOptions) (*T, error) {
	// TODO: this should enforce the namespace
	return a.API.Create(t, options)
}

func (a namespaceApi[T]) Update(t T, options metav1.UpdateOptions) (*T, error) {
	// TODO: this should enforce the namespace
	return a.API.Update(t, options)
}

type OptionlessNamespacedAPI[T Resource] interface {
	Create(t T) (*T, error)
	Update(t T) (*T, error)
	Get(name string) (*T, error)
	List() ([]T, error)
	Watch() (Watcher[T], error)
}

type namespacedOptionlessApi[T Resource] struct {
	NamespacedAPI[T]
}

func OptionlessNamespaced[T Resource](api NamespacedAPI[T]) OptionlessNamespacedAPI[T] {
	return namespacedOptionlessApi[T]{NamespacedAPI: api}
}

func (a namespacedOptionlessApi[T]) Create(t T) (*T, error) {
	return a.NamespacedAPI.Create(t, metav1.CreateOptions{})
}

func (a namespacedOptionlessApi[T]) Update(t T) (*T, error) {
	return a.NamespacedAPI.Update(t, metav1.UpdateOptions{})
}
func (a namespacedOptionlessApi[T]) Get(name string) (*T, error) {
	return a.NamespacedAPI.Get(name, metav1.GetOptions{})
}

func (a namespacedOptionlessApi[T]) List() ([]T, error) {
	return a.NamespacedAPI.List(metav1.ListOptions{})
}

func (a namespacedOptionlessApi[T]) Watch() (Watcher[T], error) {
	return a.NamespacedAPI.Watch(metav1.ListOptions{})
}

type InfallibleOptionlessNamespacedAPI[T Resource] interface {
	Create(t T) *T
	Update(t T) *T
	Get(name string) *T
	List() []T
	Watch() Watcher[T]
}

type infallibleOptionlessNamespacedAPI[T Resource] struct {
	OptionlessNamespacedAPI[T]
}

func InfallibleOptionlessNamespaced[T Resource](api OptionlessNamespacedAPI[T]) InfallibleOptionlessNamespacedAPI[T] {
	return infallibleOptionlessNamespacedAPI[T]{OptionlessNamespacedAPI: api}
}

func (a infallibleOptionlessNamespacedAPI[T]) Create(t T) *T {
	res, err := a.OptionlessNamespacedAPI.Create(t)
	if err != nil {
		panic(err.Error()) // Taking a testing.T is probably better
	}
	return res
}

func (a infallibleOptionlessNamespacedAPI[T]) Update(t T) *T {
	res, err := a.OptionlessNamespacedAPI.Update(t)
	if err != nil {
		panic(err.Error()) // Taking a testing.T is probably better
	}
	return res
}
func (a infallibleOptionlessNamespacedAPI[T]) Get(name string) *T {
	res, err := a.OptionlessNamespacedAPI.Get(name)
	if err != nil {
		panic(err.Error()) // Taking a testing.T is probably better
	}
	return res
}

func (a infallibleOptionlessNamespacedAPI[T]) List() []T {
	res, err := a.OptionlessNamespacedAPI.List()
	if err != nil {
		panic(err.Error()) // Taking a testing.T is probably better
	}
	return res
}

func (a infallibleOptionlessNamespacedAPI[T]) Watch() Watcher[T] {
	res, err := a.OptionlessNamespacedAPI.Watch()
	if err != nil {
		panic(err.Error()) // Taking a testing.T is probably better
	}
	return res
}

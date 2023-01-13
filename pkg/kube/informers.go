package kube

import (
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
)

func InformerFor[I runtime.Object](c Client) cache.SharedIndexInformer {
	i := *new(I)
	eq := func(o any) bool {
		return reflect.TypeOf(o) == reflect.TypeOf(i)
	}
	if eq(&corev1.Pod{}) { return c.KubeInformer().Core().V1().Pods().Informer() }
	if eq(&corev1.ConfigMap{}) { return c.KubeInformer().Core().V1().ConfigMaps().Informer() }
	if eq(&corev1.Endpoints{}) { return c.KubeInformer().Core().V1().Endpoints().Informer() }
	if eq(&corev1.Namespace{}) { return c.KubeInformer().Core().V1().Namespaces().Informer() }
	if eq(&corev1.Node{}) { return c.KubeInformer().Core().V1().Nodes().Informer() }
	if eq(&corev1.Pod{}) { return c.KubeInformer().Core().V1().Pods().Informer() }
	if eq(&corev1.Secret{}) { return c.KubeInformer().Core().V1().Secrets().Informer() }
	if eq(&corev1.Service{}) { return c.KubeInformer().Core().V1().Services().Informer() }
	if eq(&corev1.ServiceAccount{}) { return c.KubeInformer().Core().V1().ServiceAccounts().Informer() }
	panic(fmt.Sprintf("Unknown type %T", i))
}

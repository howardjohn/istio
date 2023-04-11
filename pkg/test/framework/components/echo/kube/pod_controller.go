// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kube

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	informersv1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/test/framework/components/echo"
)

var _ cache.Controller = &podController{}

type podHandler func(pod *corev1.Pod) error

type podHandlers struct {
	added   podHandler
	updated podHandler
	deleted podHandler
}

type podController struct {
	queue    controllers.Queue
	pods     kclient.Informer[*corev1.Pod]
	informer cache.SharedIndexInformer
}

func newPodController(cfg echo.Config, handler func(kclient.Informer[*corev1.Pod], types.NamespacedName) error) *podController {
	s := newPodSelector(cfg)
	informer := informersv1.NewFilteredPodInformer(cfg.Cluster.Kube(), cfg.Namespace.Name(), 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, func(options *metav1.ListOptions) {
		if len(options.LabelSelector) > 0 {
			options.LabelSelector += ","
		}
		options.LabelSelector += s.String()
	})
	pods := kclient.NewUntyped(cfg.Cluster, informer, kclient.Filter{}).(kclient.Informer[*corev1.Pod])
	queue := controllers.NewQueue("echo pod",
		controllers.WithReconciler(func(key types.NamespacedName) error {
			handler(pods, key)
			return nil
		}),
		controllers.WithMaxAttempts(5),
	)

	return &podController{
		queue:    queue,
		pods:     pods,
		informer: informer,
	}
}

func (c *podController) Run(stop <-chan struct{}) {
	go c.informer.Run(stop)
	kube.WaitForCacheSync(stop, c.pods.HasSynced)
	c.queue.Run(stop)
	c.pods.ShutdownHandlers()
}

func (c *podController) HasSynced() bool {
	return c.queue.HasSynced()
}

func (c *podController) WaitForSync(stopCh <-chan struct{}) bool {
	return kube.WaitForCacheSync(stopCh, c.HasSynced)
}

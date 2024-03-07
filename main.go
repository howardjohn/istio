package main

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"

	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kubetypes"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/slices"
)

func main() {
	log.EnableKlogWithVerbosity(6)
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	raw, _ := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		&clientcmd.ConfigOverrides{}).RawConfig()
	kind, _ := kube.NewClient(clientcmd.NewNonInteractiveClientConfig(raw, "kind-kind", nil, loadingRules), "kind")
	gke, _ := kube.NewClient(clientcmd.NewNonInteractiveClientConfig(raw, "gke", nil, loadingRules), "gke")
	stop := make(chan struct{})
	// knodes := kclient.New[*corev1.Namespace](kind)
	// gnodes := kclient.New[*corev1.Namespace](gke)
	// tracker := assert.NewTracker[string](nil)
	// knodes.AddEventHandler(clienttest.TrackerHandler(tracker))
	// gnodes.AddEventHandler(clienttest.TrackerHandler(tracker))
	kind.RunAndWait(stop)
	gke.RunAndWait(stop)

	current := atomic.NewPointer[kube.Client](&kind)
	go func() {
		time.Sleep(time.Second)
		for {
			current.Store(&gke)
			time.Sleep(time.Second * 5)
			current.Store(&kind)
			time.Sleep(time.Second * 5)
		}
	}()

	swapped := false
	inf := kind.Informers().InformerFor(gvr.Namespace, kubetypes.InformerOptions{}, func() cache.SharedIndexInformer {
		listClient := ""
		l := func(options metav1.ListOptions) (runtime.Object, error) {
			options.AllowWatchBookmarks = false
			options.ResourceVersion = ""
			l := *current.Load()
			listClient = fmt.Sprintf("%p", l)
			log.Infof("building list with %p, %v", l, options.ResourceVersion)
			return l.Kube().CoreV1().Namespaces().List(context.Background(), options)
		}
		w := func(options metav1.ListOptions) (watch.Interface, error) {
			options.AllowWatchBookmarks = false
			c := *current.Load()
			log.Infof("building watch with %p, %v", c, options.ResourceVersion)
			if listClient != fmt.Sprintf("%p", c) {
				log.Infof("clearing RV %v", options.ResourceVersion)
			}
			w, e := c.Kube().CoreV1().Namespaces().Watch(context.Background(), options)
			if !swapped {
				swapped = true
				go func() {
					time.Sleep(time.Second * 3)
					w.Stop()
				}()
			}
			return w, e
		}
		inf := cache.NewSharedIndexInformer(
			&cache.ListWatch{
				ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
					return l(options)
				},
				WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
					return w(options)
				},
			},
			&corev1.Namespace{},
			0,
			cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
		)
		return inf
	})
	inf.Informer.AddEventHandler(controllers.EventHandler[controllers.Object]{
		AddFunc: func(obj controllers.Object) {
			log.Infof("add/" + obj.GetName())
		},
		UpdateFunc: func(oldObj, newObj controllers.Object) {
			log.Infof("update/" + newObj.GetName())
		},
		DeleteFunc: func(obj controllers.Object) {
			log.Infof("delete/" + obj.GetName())
		},
	})
	inf.Start(stop)
	kube.WaitForCacheSync("test", stop, inf.Informer.HasSynced)

	for {
		items := slices.Sort(slices.Map(inf.Informer.GetStore().List(), func(e any) string {
			return e.(*corev1.Namespace).Name
		}))
		fmt.Println(items)
		time.Sleep(time.Second)
	}
}

package main

import (
	"fmt"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/test/util/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	raw, _ := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		&clientcmd.ConfigOverrides{}).RawConfig()
	kind, _ := kube.NewClient(clientcmd.NewNonInteractiveClientConfig(raw, "kind-kind", nil, loadingRules), "kind")
	gke, _ := kube.NewClient(clientcmd.NewNonInteractiveClientConfig(raw, "kind-kind", nil, loadingRules), "gke")
	stop := make(chan struct{})
	knodes := kclient.New[*corev1.Node](kind)
	gnodes := kclient.New[*corev1.Node](gke)
	tracker := assert.NewTracker[string](nil)
	knodes.AddEventHandler(clienttest.TrackerHandler(tracker))
	gnodes.AddEventHandler(clienttest.TrackerHandler(tracker))
	kind.RunAndWait(stop)
	gke.RunAndWait(stop)

	fmt.Println(tracker.Events())
}
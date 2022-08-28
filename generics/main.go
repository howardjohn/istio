package main

import (
	"flag"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"

	"istio.io/istio/pkg/kube"
	"istio.io/pkg/log"
)

func main() {
	log.EnableKlogWithGoFlag()
	flag.Parse()
	restConfig, err := kube.DefaultRestConfig("", "")
	if err != nil {
		log.Fatal(err)
	}
	rc, err := rest.RESTClientFor(restConfig)
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
	watcher, err := Watch[appsv1.Deployment](c, "istio-system", metav1.ListOptions{})
	go func() {
		time.Sleep(time.Second)
		watcher.Stop()
	}()
	for res := range watcher.Results() {
		fmt.Println("watch", res.Name)
	}

	pod := MustList[corev1.Pod](c, "istio-system")[0].Name
	logs, err := GetLogs[corev1.Pod](c, pod, "istio-system", corev1.PodLogOptions{})
	log.Infof("%v, %v", logs[:100], err)
}

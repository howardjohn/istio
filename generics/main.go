package main

import (
	"flag"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
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
	log.Info(r.Name, err)
	for _, dep := range MustList[appsv1.Deployment](c, "istio-system") {
		log.Info(dep.Name)
	}
	watcher, err := Watch[appsv1.Deployment](c, "istio-system", metav1.ListOptions{})
	go func() {
		time.Sleep(time.Second)
		watcher.Stop()
	}()
	for res := range watcher.Results() {
		log.Info("watch", res.Name)
	}

	pod := MustList[corev1.Pod](c, "istio-system")[0].Name
	logs, err := GetLogs[corev1.Pod](c, pod, "istio-system", corev1.PodLogOptions{})
	log.Infof("%v, %v", logs[:100], err)

	pods := NewAPI[corev1.Pod](c)
	res, _ := pods.List("kube-system", metav1.ListOptions{})
	for _, p := range res {
		log.Info(p.Name)
	}

	res, _ = pods.Namespace("default").List(metav1.ListOptions{})
	for _, p := range res {
		log.Info(p.Name)
	}

	// Example how its easy to make simpler wrapper apis, especially for tests, embedding defaults
	simple := pods.Namespace("default").Optionless()
	simple.List()

	informer := NewInformer[corev1.Pod](c, "kube-system")
	for _, l := range informer.List(klabels.Everything()) {
		log.Infof("informer list: %v", l.Name)
	}

	// Fake
	f := NewFake[corev1.Pod](corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "fake", Namespace: "fake"},
	})
	g, e := f.Get("fake", "fake", metav1.GetOptions{})
	log.Errorf("howardjohn: %v %v", g, e)
	l, e := f.List("fake", metav1.ListOptions{})
	log.Infof("fake list: %v %v", l, e)
}

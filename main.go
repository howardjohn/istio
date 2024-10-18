package main

import (
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/log"
	"iter"
	corev1 "k8s.io/api/core/v1"
	"time"
)

func main() {

	config, err := kube.DefaultRestConfig("", "")
	fatal(err)
	kc, err := kube.NewClient(kube.NewClientConfigForRestConfig(config), "")
	fatal(err)
	pods := kclient.New[*corev1.Pod](kc)
	kc.RunAndWait(make(chan struct{}))
	for event := range Events(pods) {
		log.Errorf("howardjohn: %v", event.Name)
		time.Sleep(time.Millisecond)
	}
}

func Events[T controllers.ComparableObject](pods kclient.Client[T]) iter.Seq[T] {
	return func(yield func(T) bool) {
		running := true
		done := make(chan struct{})
		log.Errorf("howardjohn: setup")
		pods.AddEventHandler(controllers.ObjectHandler(func(o controllers.Object) {
			if !running {
				log.Errorf("howardjohn: not running..")
				// TODO: unsub handler
				return
			}

			if !yield(o.(T)) {
				log.Errorf("howardjohn: yield done")
				running = false
				close(done)
				return
			}
		}))
		<-done
		log.Errorf("howardjohn: done")
	}
	
}

func fatal(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
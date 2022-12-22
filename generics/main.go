package main

import (
	"context"
	"flag"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

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

	defaultPods := Namespaced(pods, "default")

	res, _ = defaultPods.List(metav1.ListOptions{})
	for _, p := range res {
		log.Info(p.Name)
	}

	// Example how its easy to make simpler wrapper apis, especially for tests, embedding defaults
	simple := OptionlessNamespaced(defaultPods)
	simple.List()

	informer := NewInformer(pods, "kube-system")
	for _, l := range informer.List(klabels.Everything()) {
		log.Infof("informer list: %v", l.Name)
	}

	// Fake
	 f := NewFake(corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "fake", Namespace: "fake"},
	})

	g, e := f.Get("fake", "fake", metav1.GetOptions{})
	log.Errorf("fake get: %v %v", g.Name, e)
	l, e := f.List("fake", metav1.ListOptions{})
	log.Infof("fake list: %v %v", len(l), e)

	fakeInformer := NewInformer[corev1.Pod](f, "fake")
	log.Infof("informer list: %v", len(fakeInformer.List(klabels.Everything())))
	log.Infof("creating pod...")
	f.Create(corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "fake-added", Namespace: "fake"},
	}, metav1.CreateOptions{})
	time.Sleep(time.Millisecond * 100)
	log.Infof("informer list: %v", len(fakeInformer.List(klabels.Everything())))

	fcs := f.ToClientSet()
	fcsList, _ := fcs.CoreV1().Pods(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{})

	log.Infof("fcs list: %v", len(fcsList.Items))
	fakeLegacyInformer := informers.NewSharedInformerFactory(fcs, 0)
	legacyPods := fakeLegacyInformer.Core().V1().Pods()
	legacyPods.Informer() // load it
	fakeLegacyInformer.Start(make(chan struct{}))
	cache.WaitForCacheSync(make(chan struct{}), legacyPods.Informer().HasSynced)
	lpil, _ := legacyPods.Lister().List(klabels.Everything())
	log.Infof("fake legacy informer list: %v", len(lpil))

	f.Update(corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "fake", Namespace: "fake", Labels: map[string]string{"a": "b"}},
	}, metav1.UpdateOptions{})
	nf, _ := f.Get("fake", "fake", metav1.GetOptions{})
	log.Infof("after update, label is %v", nf.Labels)

	CreateOrUpdate[corev1.Pod](f, corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "fake2", Namespace: "fake", Labels: map[string]string{"a": "b"}},
	})
	nf, _ = f.Get("fake2", "fake", metav1.GetOptions{})
	log.Infof("create or update, label is %v", nf.Labels)

	CreateOrUpdate[corev1.Pod](f, corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "fake2", Namespace: "fake", Labels: map[string]string{"a": "modified"}},
	})
	nf, _ = f.Get("fake2", "fake", metav1.GetOptions{})
	log.Infof("create or update, label is %v", nf.Labels)

	// Example of using wrappers to provide alternative APIs...
	// The constructors could, of course, use some work
	simpleFake := InfallibleOptionlessNamespaced(OptionlessNamespaced[corev1.Pod](Namespaced[corev1.Pod](f, "fake")))
	simpleFake.Create(corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "fake3", Namespace: "fake"}})
	log.Infof("got %v", simpleFake.Get("fake3").Name)
}

func CreateOrUpdate[T Resource](a API[T], t T) (*T, error) {
	r, err := a.Create(t, metav1.CreateOptions{})
	if err != nil {
		if kerrors.IsAlreadyExists(err) {
			return a.Update(t, metav1.UpdateOptions{})
		}
		return nil, err
	}
	return r, nil
}
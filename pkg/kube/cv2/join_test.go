package cv2_test

import (
	"testing"

	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	istio "istio.io/api/networking/v1alpha3"
	istioclient "istio.io/client-go/pkg/apis/networking/v1alpha3"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/cv2"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

func TestJoinCollection(t *testing.T) {
	c1 := cv2.NewStatic[Named](nil)
	c2 := cv2.NewStatic[Named](nil)
	c3 := cv2.NewStatic[Named](nil)
	j := cv2.JoinCollection(c1.AsCollection(), c2.AsCollection(), c3.AsCollection())
	last := atomic.NewString("")
	j.Register(func(o cv2.Event[Named]) {
		last.Store(o.Latest().ResourceName())
	})
	assert.EventuallyEqual(t, last.Load, "")
	c1.Set(&Named{"c1", "a"})
	assert.EventuallyEqual(t, last.Load, "c1/a")

	c2.Set(&Named{"c2", "a"})
	assert.EventuallyEqual(t, last.Load, "c2/a")

	c3.Set(&Named{"c3", "a"})
	assert.EventuallyEqual(t, last.Load, "c3/a")

	c1.Set(&Named{"c1", "b"})
	assert.EventuallyEqual(t, last.Load, "c1/b")
	// ordered by c1, c2, c3
	sortf := func(a, b Named) bool {
		return a.ResourceName() < b.ResourceName()
	}
	assert.Equal(
		t,
		slices.SortFunc(j.List(""), sortf),
		slices.SortFunc([]Named{
			{"c1", "b"},
			{"c2", "a"},
			{"c3", "a"},
		}, sortf),
	)
}

func TestCollectionJoin(t *testing.T) {
	c := kube.NewFakeClient()
	pods := cv2.NewInformer[*corev1.Pod](c)
	services := cv2.NewInformer[*corev1.Service](c)
	serviceEntries := cv2.NewInformer[*istioclient.ServiceEntry](c)
	c.RunAndWait(test.NewStop(t))
	pc := clienttest.Wrap(t, kclient.New[*corev1.Pod](c))
	sc := clienttest.Wrap(t, kclient.New[*corev1.Service](c))
	sec := clienttest.Wrap(t, kclient.New[*istioclient.ServiceEntry](c))
	SimplePods := SimplePodCollection(pods)
	SimpleServices := SimpleServiceCollection(services)
	SimpleServiceEntries := SimpleServiceCollectionFromEntries(serviceEntries)
	Joined := cv2.JoinCollection(SimpleServices, SimpleServiceEntries)
	SimpleEndpoints := SimpleEndpointsCollection(SimplePods, Joined)

	fetch := func() []SimpleEndpoint {
		return SimpleEndpoints.List("")
	}

	assert.Equal(t, fetch(), nil)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: "namespace",
			Labels:    map[string]string{"app": "foo"},
		},
		Status: corev1.PodStatus{PodIP: "1.2.3.4"},
	}
	pc.Create(pod)
	assert.Equal(t, fetch(), nil)

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc",
			Namespace: "namespace",
		},
		Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "foo"}},
	}
	sc.Create(svc)
	assert.Equal(t, fetch(), nil)

	pc.UpdateStatus(pod)
	assert.EventuallyEqual(t, fetch, []SimpleEndpoint{{pod.Name, svc.Name, pod.Namespace, "1.2.3.4"}})

	pod.Status.PodIP = "1.2.3.5"
	pc.UpdateStatus(pod)
	assert.EventuallyEqual(t, fetch, []SimpleEndpoint{{pod.Name, svc.Name, pod.Namespace, "1.2.3.5"}})

	pc.Delete(pod.Name, pod.Namespace)
	assert.EventuallyEqual(t, fetch, nil)

	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name2",
			Namespace: "namespace",
			Labels:    map[string]string{"app": "foo"},
		},
		Status: corev1.PodStatus{PodIP: "2.3.4.5"},
	}
	pc.CreateOrUpdateStatus(pod)
	pc.CreateOrUpdateStatus(pod2)
	assert.EventuallyEqual(t, fetch, []SimpleEndpoint{
		{pod.Name, svc.Name, pod.Namespace, pod.Status.PodIP},
		{pod2.Name, svc.Name, pod2.Namespace, pod2.Status.PodIP},
	})

	se := &istioclient.ServiceEntry{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-entry",
			Namespace: "namespace",
		},
		Spec: istio.ServiceEntry{WorkloadSelector: &istio.WorkloadSelector{Labels: map[string]string{"app": "foo"}}},
	}
	sec.Create(se)
	assert.EventuallyEqual(t, fetch, []SimpleEndpoint{
		{pod.Name, se.Name, pod.Namespace, pod.Status.PodIP},
		{pod2.Name, se.Name, pod2.Namespace, pod2.Status.PodIP},
		{pod.Name, svc.Name, pod.Namespace, pod.Status.PodIP},
		{pod2.Name, svc.Name, pod2.Namespace, pod2.Status.PodIP},
	})
}

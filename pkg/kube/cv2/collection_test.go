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

package cv2_test

import (
	"testing"

	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	istio "istio.io/api/networking/v1alpha3"
	istioclient "istio.io/client-go/pkg/apis/networking/v1alpha3"
	"istio.io/istio/pkg/config"
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
	c1.Set(&Named{types.NamespacedName{"c1", "a"}})
	assert.EventuallyEqual(t, last.Load, "c1/a")

	c2.Set(&Named{types.NamespacedName{"c2", "a"}})
	assert.EventuallyEqual(t, last.Load, "c2/a")

	c3.Set(&Named{types.NamespacedName{"c3", "a"}})
	assert.EventuallyEqual(t, last.Load, "c3/a")

	c1.Set(&Named{types.NamespacedName{"c1", "b"}})
	assert.EventuallyEqual(t, last.Load, "c1/b")
	// ordered by c1, c2, c3
	sortf := func(a, b Named) bool {
		return a.ResourceName() < b.ResourceName()
	}
	assert.Equal(
		t,
		slices.SortFunc(j.List(""), sortf),
		slices.SortFunc([]Named{
			{types.NamespacedName{"c1", "b"}},
			{types.NamespacedName{"c2", "a"}},
			{types.NamespacedName{"c3", "a"}},
		}, sortf),
	)
}

type SimplePod struct {
	Named
	Labeled
	IP string
}

func SimplePodCollection(pods cv2.Collection[*corev1.Pod]) cv2.Collection[SimplePod] {
	return cv2.NewCollection(pods, func(ctx cv2.HandlerContext, i *corev1.Pod) *SimplePod {
		if i.Status.PodIP == "" {
			return nil
		}
		return &SimplePod{
			Named:   NewNamed(i),
			Labeled: NewLabeled(i.Labels),
			IP:      i.Status.PodIP,
		}
	})
}

func NewNamed(n config.Namer) Named {
	return Named{config.NamespacedName(n)}
}

type Named struct {
	types.NamespacedName
}

func (s Named) ResourceName() string {
	return s.Namespace + "/" + s.Name
}

func NewLabeled(n map[string]string) Labeled {
	return Labeled{n}
}

type Labeled struct {
	Labels map[string]string
}

func (l Labeled) GetLabels() map[string]string {
	return l.Labels
}

type SimpleService struct {
	Named
	Selector map[string]string
}

func SimpleServiceCollection(services cv2.Collection[*corev1.Service]) cv2.Collection[SimpleService] {
	return cv2.NewCollection(services, func(ctx cv2.HandlerContext, i *corev1.Service) *SimpleService {
		return &SimpleService{
			Named:    NewNamed(i),
			Selector: i.Spec.Selector,
		}
	})
}

func SimpleServiceCollectionFromEntries(entries cv2.Collection[*istioclient.ServiceEntry]) cv2.Collection[SimpleService] {
	return cv2.NewCollection(entries, func(ctx cv2.HandlerContext, i *istioclient.ServiceEntry) *SimpleService {
		l := i.Spec.WorkloadSelector.GetLabels()
		if l == nil {
			return nil
		}
		return &SimpleService{
			Named:    NewNamed(i),
			Selector: l,
		}
	})
}

type SimpleEndpoint struct {
	Pod       string
	Service   string
	Namespace string
	IP        string
}

func (s SimpleEndpoint) ResourceName() string {
	return slices.Join("/", s.Namespace+"/"+s.Service+"/"+s.Pod)
}

func SimpleEndpointsCollection(pods cv2.Collection[SimplePod], services cv2.Collection[SimpleService]) cv2.Collection[SimpleEndpoint] {
	return cv2.NewManyCollection[SimpleService, SimpleEndpoint](services, func(ctx cv2.HandlerContext, svc SimpleService) []SimpleEndpoint {
		pods := cv2.Fetch(ctx, pods, cv2.FilterLabel(svc.Selector))
		return slices.Map(pods, func(pod SimplePod) SimpleEndpoint {
			return SimpleEndpoint{
				Pod:       pod.Name,
				Service:   svc.Name,
				Namespace: svc.Namespace,
				IP:        pod.IP,
			}
		})
	})
}

func TestCollectionSimple(t *testing.T) {
	c := kube.NewFakeClient()
	kpc := kclient.New[*corev1.Pod](c)
	pc := clienttest.Wrap(t, kpc)
	pods := cv2.WrapClient[*corev1.Pod](kpc)
	c.RunAndWait(test.NewStop(t))
	SimplePods := SimplePodCollection(pods)

	fetch := func() []SimplePod {
		return SimplePods.List("")
	}

	assert.Equal(t, fetch(), nil)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: "namespace",
		},
		Status: corev1.PodStatus{PodIP: "1.2.3.4"},
	}
	pc.Create(pod)
	assert.Equal(t, fetch(), nil)

	pc.UpdateStatus(pod)
	assert.EventuallyEqual(t, fetch, []SimplePod{{NewNamed(pod), Labeled{}, "1.2.3.4"}})

	pod.Status.PodIP = "1.2.3.5"
	pc.UpdateStatus(pod)
	assert.EventuallyEqual(t, fetch, []SimplePod{{NewNamed(pod), Labeled{}, "1.2.3.5"}})

	pc.Delete(pod.Name, pod.Namespace)
	assert.EventuallyEqual(t, fetch, nil)
}

func TestCollectionMerged(t *testing.T) {
	c := kube.NewFakeClient()
	pods := cv2.NewInformer[*corev1.Pod](c)
	services := cv2.NewInformer[*corev1.Service](c)
	c.RunAndWait(test.NewStop(t))
	pc := clienttest.Wrap(t, kclient.New[*corev1.Pod](c))
	sc := clienttest.Wrap(t, kclient.New[*corev1.Service](c))
	SimplePods := SimplePodCollection(pods)
	SimpleServices := SimpleServiceCollection(services)
	SimpleEndpoints := SimpleEndpointsCollection(SimplePods, SimpleServices)

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

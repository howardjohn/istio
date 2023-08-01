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
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	istioclient "istio.io/client-go/pkg/apis/networking/v1alpha3"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/cv2"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
)

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

type SizedPod struct {
	Named
	Size string
}

func SizedPodCollection(pods cv2.Collection[*corev1.Pod]) cv2.Collection[SizedPod] {
	return cv2.NewCollection(pods, func(ctx cv2.HandlerContext, i *corev1.Pod) *SizedPod {
		s, f := i.Labels["size"]
		if !f {
			return nil
		}
		return &SizedPod{
			Named: NewNamed(i),
			Size:  s,
		}
	})
}

func NewNamed(n config.Namer) Named {
	return Named{
		Namespace: n.GetNamespace(),
		Name:      n.GetName(),
	}
}

type Named struct {
	Namespace string
	Name      string
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

type PodSizeCount struct {
	Named
	MatchingSizes int
}

func TestCollectionCycle(t *testing.T) {
	c := kube.NewFakeClient()
	pods := cv2.NewInformer[*corev1.Pod](c)
	c.RunAndWait(test.NewStop(t))
	pc := clienttest.Wrap(t, kclient.New[*corev1.Pod](c))
	SimplePods := SimplePodCollection(pods)
	SizedPods := SizedPodCollection(pods)
	Thingys := cv2.NewCollection[SimplePod, PodSizeCount](SimplePods, func(ctx cv2.HandlerContext, pd SimplePod) *PodSizeCount {
		if _, f := pd.Labels["want-size"]; !f {
			return nil
		}
		matches := cv2.Fetch(ctx, SizedPods, cv2.FilterGeneric(func(a any) bool {
			return a.(SizedPod).Size == pd.Labels["want-size"]
		}))
		return &PodSizeCount{
			Named:         pd.Named,
			MatchingSizes: len(matches),
		}
	})
	tt := assert.NewTracker[string](t)
	Thingys.RegisterBatch(BatchedTrackerHandler[PodSizeCount](tt))

	fetch := func() []PodSizeCount {
		return Thingys.List("")
	}

	assert.Equal(t, fetch(), nil)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: "namespace",
			Labels:    map[string]string{"want-size": "large"},
		},
		Status: corev1.PodStatus{PodIP: "1.2.3.4"},
	}
	pc.CreateOrUpdateStatus(pod)
	tt.WaitOrdered("add/namespace/name")
	assert.Equal(t, fetch(), []PodSizeCount{{
		Named:         NewNamed(pod),
		MatchingSizes: 0,
	}})

	largePod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name-large",
			Namespace: "namespace",
			Labels:    map[string]string{"size": "large"},
		},
		Status: corev1.PodStatus{PodIP: "1.2.3.5"},
	}
	pc.CreateOrUpdateStatus(largePod)
	tt.WaitOrdered("update/namespace/name")
	assert.Equal(t, fetch(), []PodSizeCount{{
		Named:         NewNamed(pod),
		MatchingSizes: 1,
	}})

	smallPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name-small",
			Namespace: "namespace",
			Labels:    map[string]string{"size": "small"},
		},
		Status: corev1.PodStatus{PodIP: "1.2.3.6"},
	}
	pc.CreateOrUpdateStatus(smallPod)
	pc.CreateOrUpdateStatus(largePod)
	assert.Equal(t, fetch(), []PodSizeCount{{
		Named:         NewNamed(pod),
		MatchingSizes: 1,
	}})
	tt.Empty()

	largePod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name-large2",
			Namespace: "namespace",
			Labels:    map[string]string{"size": "large"},
		},
		Status: corev1.PodStatus{PodIP: "1.2.3.7"},
	}
	pc.CreateOrUpdateStatus(largePod2)
	tt.WaitOrdered("update/namespace/name")
	assert.Equal(t, fetch(), []PodSizeCount{{
		Named:         NewNamed(pod),
		MatchingSizes: 2,
	}})

	dual := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name-dual",
			Namespace: "namespace",
			Labels:    map[string]string{"size": "large", "want-size": "small"},
		},
		Status: corev1.PodStatus{PodIP: "1.2.3.8"},
	}
	pc.CreateOrUpdateStatus(dual)
	tt.WaitUnordered("update/namespace/name", "add/namespace/name-dual")
	assert.Equal(t, fetch(), []PodSizeCount{
		{
			Named:         NewNamed(pod),
			MatchingSizes: 3,
		},
		{
			Named:         NewNamed(dual),
			MatchingSizes: 1,
		},
	})

	largePod2.Labels["size"] = "small"
	pc.CreateOrUpdateStatus(largePod2)
	tt.WaitCompare(CompareUnordered("update/namespace/name-dual", "update/namespace/name"))
	assert.Equal(t, fetch(), []PodSizeCount{
		{
			Named:         NewNamed(pod),
			MatchingSizes: 2,
		},
		{
			Named:         NewNamed(dual),
			MatchingSizes: 2,
		},
	})

	pc.Delete(dual.Name, dual.Namespace)
	tt.WaitUnordered("update/namespace/name", "delete/namespace/name-dual")
	assert.Equal(t, fetch(), []PodSizeCount{{
		Named:         NewNamed(pod),
		MatchingSizes: 1,
	}})

	pc.Delete(largePod.Name, largePod.Namespace)
	tt.WaitOrdered("update/namespace/name")
	assert.Equal(t, fetch(), []PodSizeCount{{
		Named:         NewNamed(pod),
		MatchingSizes: 0,
	}})

	pc.Delete(pod.Name, pod.Namespace)
	tt.WaitOrdered("delete/namespace/name")
	assert.Equal(t, fetch(), []PodSizeCount{})
}

func CompareUnordered(wants ...string) func(s string) bool {
	want := sets.New(wants...)
	return func(s string) bool {
		got := sets.New(strings.Split(s, ",")...)
		return want.Equals(got)
	}
}

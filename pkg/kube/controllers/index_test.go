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

package controllers

import (
	"context"
	"golang.org/x/exp/slices"
	"istio.io/istio/pkg/config/labels"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/exp/maps"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
)

type SaNode struct {
	ServiceAccount types.NamespacedName
	Node           string
}

func TestIndex(t *testing.T) {
	c := kube.NewFakeClient()
	informer := c.KubeInformer().Core().V1().Pods().Informer()
	c.RunAndWait(test.NewStop(t))
	index := CreateIndex[*corev1.Pod, SaNode](informer, func(pod *corev1.Pod) []SaNode {
		if len(pod.Spec.NodeName) == 0 {
			return nil
		}
		if len(pod.Spec.ServiceAccountName) == 0 {
			return nil
		}
		return []SaNode{{
			ServiceAccount: types.NamespacedName{
				Namespace: pod.Namespace,
				Name:      pod.Spec.ServiceAccountName,
			},
			Node: pod.Spec.NodeName,
		}}
	})
	k1 := SaNode{
		ServiceAccount: types.NamespacedName{
			Namespace: "ns",
			Name:      "sa",
		},
		Node: "node",
	}
	k2 := SaNode{
		ServiceAccount: types.NamespacedName{
			Namespace: "ns",
			Name:      "sa2",
		},
		Node: "node",
	}
	assert.Equal(t, index.Lookup(k1), nil)
	assert.Equal(t, index.Lookup(k2), nil)
	pod1 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod",
			Namespace: "ns",
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: "sa",
			NodeName:           "node",
		},
	}
	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod2",
			Namespace: "ns",
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: "sa2",
			NodeName:           "node",
		},
	}
	pod3 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod3",
			Namespace: "ns",
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: "sa",
			NodeName:           "node",
		},
	}

	assertIndex := func(k SaNode, pods ...*corev1.Pod) {
		t.Helper()
		assert.EventuallyEqual(t, func() []*corev1.Pod { return index.Lookup(k) }, pods, retry.Timeout(time.Second*5))
	}

	// When we create a pod, we should (eventually) see it in the index
	c.Kube().CoreV1().Pods("ns").Create(context.Background(), pod1, metav1.CreateOptions{})
	assertIndex(k1, pod1)
	assertIndex(k2)

	// Create another pod; we ought to find it as well now.
	c.Kube().CoreV1().Pods("ns").Create(context.Background(), pod2, metav1.CreateOptions{})
	assertIndex(k1, pod1) // Original one must still persist
	assertIndex(k2, pod2) // New one should be there, eventually

	// Create another pod with the same SA; we ought to find multiple now.
	c.Kube().CoreV1().Pods("ns").Create(context.Background(), pod3, metav1.CreateOptions{})
	assertIndex(k1, pod1, pod3) // Original one must still persist
	assertIndex(k2, pod2)       // New one should be there, eventually

	pod1Alt := pod1.DeepCopy()
	// This can't happen in practice with Pod, but Index supports arbitrary types
	pod1Alt.Spec.ServiceAccountName = "new-sa"

	keyNew := SaNode{
		ServiceAccount: types.NamespacedName{
			Namespace: "ns",
			Name:      "new-sa",
		},
		Node: "node",
	}
	c.Kube().CoreV1().Pods("ns").Update(context.Background(), pod1Alt, metav1.UpdateOptions{})
	assertIndex(k1, pod3)        // Pod should be dropped from the index
	assertIndex(keyNew, pod1Alt) // And added under the new key

	c.Kube().CoreV1().Pods("ns").Delete(context.Background(), pod1Alt.Name, metav1.DeleteOptions{})
	assertIndex(k1, pod3) // Shouldn't impact others
	assertIndex(keyNew)   // but should be removed
}

type PodInfo struct {
	Name   string
	Labels map[string]string
}

func (p PodInfo) Equals(k PodInfo) bool {
	return maps.Equal(p.Labels, k.Labels) && p.Name == k.Name
}

func (p PodInfo) Key() string {
	return p.Name
}

type ServiceInfo struct {
	Name     string
	Selector map[string]string
}

func (p ServiceInfo) Equals(k ServiceInfo) bool {
	return maps.Equal(p.Selector, k.Selector) && p.Name == k.Name
}

func (p ServiceInfo) Key() string {
	return p.Name
}

type PodSvc struct {
	PodName      string
	ServiceNames []string
}

func (p PodSvc) Equals(k PodSvc) bool {
	return p.PodName == k.PodName && slices.Equal(p.ServiceNames, k.ServiceNames)
}

func (p PodSvc) Key() string {
	return p.PodName + "/" + strings.Join(p.ServiceNames, ",")
}

func TestDependency(t *testing.T) {
	c := kube.NewFakeClient()
	podInf := c.KubeInformer().Core().V1().Pods().Informer()
	serviceInf := c.KubeInformer().Core().V1().Services().Informer()
	// I now have a stream for PodInfo
	podInfo := Subscribe[*corev1.Pod, PodInfo](
		podInf,
		func(i *corev1.Pod) PodInfo {
			k, _ := cache.MetaNamespaceKeyFunc(i)
			return PodInfo{Name: k, Labels: i.Labels}
		})
	// I now have a stream for ServiceInfo
	svcInfo := Subscribe[*corev1.Service, ServiceInfo](
		serviceInf,
		func(i *corev1.Service) ServiceInfo {
			k, _ := cache.MetaNamespaceKeyFunc(i)
			return ServiceInfo{Name: k, Selector: i.Spec.Selector}
		})

	c.RunAndWait(test.NewStop(t))
	match := func(a PodInfo, b ServiceInfo) bool {
		log.Infof("match %+v < %+v = %v", b.Selector, a.Labels, labels.Instance(b.Selector).SubsetOf(a.Labels))
		return labels.Instance(b.Selector).SubsetOf(a.Labels)
	}
	convert := func(a PodInfo, b []ServiceInfo) PodSvc {
		svcs := []string{}
		for _, k := range b {
			svcs = append(svcs, k.Name)
		}
		return PodSvc{
			PodName:      a.Name,
			ServiceNames: svcs,
		}
	}
	podSvc := Join[PodInfo, ServiceInfo, PodSvc](podInfo, svcInfo, match, convert)
	podSvc.Register(func(ps PodSvc) {
		log.Infof("computed new PodSvc: %v", ps.Key())
	})
	time.Sleep(time.Millisecond * 50)

	t.Log("svc create")
	c.Kube().CoreV1().Services("default").Create(context.Background(), &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "svc1"},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "bar"}},
	}, metav1.CreateOptions{})
	time.Sleep(time.Millisecond * 50)

	t.Log("pod create")
	c.Kube().CoreV1().Pods("default").Create(context.Background(), &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod1", Labels: map[string]string{"app": "bar"}},
	}, metav1.CreateOptions{})
	time.Sleep(time.Millisecond * 50)

	t.Log("svc update")
	c.Kube().CoreV1().Services("default").Update(context.Background(), &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "svc1"},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "bar2"}},
	}, metav1.UpdateOptions{})
	time.Sleep(time.Millisecond * 50)

	t.Log("svc update NOP")
	c.Kube().CoreV1().Services("default").Update(context.Background(), &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "svc1", Labels: map[string]string{"ignore": "me"}},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "bar2"}},
	}, metav1.UpdateOptions{})
	time.Sleep(time.Millisecond * 50)

	t.Log("svc update back")
	c.Kube().CoreV1().Services("default").Update(context.Background(), &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "svc1"},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "bar"}},
	}, metav1.UpdateOptions{})
	time.Sleep(time.Millisecond * 50)

	t.Log("svc create new")
	c.Kube().CoreV1().Services("default").Create(context.Background(), &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "svc2"},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "bar"}},
	}, metav1.CreateOptions{})
	time.Sleep(time.Second)
	t.Log("final", podSvc.List())
}

type Equaler[K any] interface {
	Equals(k K) bool
	Key() string
}

type Handle[O Equaler[O]] struct {
	handlers []func(O)
	objects  map[string]O
	mu       sync.RWMutex
}

func (h *Handle[O]) Get(k string) *O {
	o, f := h.objects[k]
	if !f {
		return nil
	}
	return &o
}

func (h *Handle[O]) Register(f func(O)) {
	h.handlers = append(h.handlers, f)
}

func (h *Handle[O]) Handle(conv O) {
	for _, hh := range h.handlers {
		hh(conv)
	}
}

func (h *Handle[O]) List() []O {
	return maps.Values(h.objects)
}

type Watcher[O Equaler[O]] interface {
	// Register a handler. TODO: call it for List() when we register
	Register(f func(O))
	List() []O
	Get(k string) *O
}

type Joined[A Equaler[A], B Equaler[B], O Equaler[O]] struct {
	mu       sync.RWMutex
	objects  map[string]O
	handlers []func(O)
}

func (j *Joined[A, B, O]) Get(k string) *O {
	o, f := j.objects[k]
	if !f {
		return nil
	}
	return &o
}

func (j *Joined[A, B, O]) Handle(conv O) {
	for _, hh := range j.handlers {
		hh(conv)
	}
}
func (j *Joined[A, B, O]) Register(f func(O)) {
	j.handlers = append(j.handlers, f)
}

func (j *Joined[A, B, O]) List() []O {
	return maps.Values(j.objects)
}

func Join[A Equaler[A], B Equaler[B], O Equaler[O]](
	a Watcher[A],
	b Watcher[B],
	match func(a A, b B) bool,
	conv func(a A, b []B) O,
) Watcher[O] {
	joined := &Joined[A, B, O]{
		objects: make(map[string]O),
	}
	a.Register(func(ai A) {
		key := ai.Key()
		log.Infof("Join got new A %v", key)
		o, f := joined.objects[key]
		bs := b.List()
		matches := []B{}
		for _, bi := range bs {
			if match(ai, bi) {
				matches = append(matches, bi)
			}
		}
		oNew := conv(ai, matches)
		if f && o.Equals(oNew) {
			log.Infof("no changes on A")
			return
		}
		joined.objects[key] = oNew
		joined.Handle(oNew)
	})
	b.Register(func(bi B) {
		log.Errorf("Join got new B %v", bi.Key())
		for _, ai := range a.List() {
			if match(ai, bi) {
				log.Infof("a %v matches b %v", ai.Key(), bi.Key())
				bs := b.List()
				log.Infof("have %v B's", len(bs))
				matches := []B{}
				for _, bi := range bs {
					if match(ai, bi) {
						matches = append(matches, bi)
					}
				}
				key := ai.Key()
				oOld := joined.objects[key]
				oNew := conv(ai, matches)
				if oOld.Equals(oNew) {
					log.Infof("a unchanged")
					continue
				}
				joined.objects[key] = oNew
				joined.Handle(oNew)
			}
		}
	})
	return joined
}

func Subscribe[I Object, O Equaler[O]](informer HandleInformer, convert func(i I) O) Watcher[O] {
	h := &Handle[O]{
		objects: make(map[string]O),
		mu:      sync.RWMutex{},
	}

	addObj := func(obj any) {
		i := Extract[I](obj)
		key, _ := cache.MetaNamespaceKeyFunc(obj)
		conv := convert(i)
		updated := !conv.Equals(h.objects[key])
		h.objects[key] = conv
		if updated {
			h.Handle(conv)
		}
	}
	deleteObj := func(obj any) {
		key, _ := cache.MetaNamespaceKeyFunc(obj)
		old := h.objects[key]
		delete(h.objects, key)
		h.Handle(old)
	}
	handler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			h.mu.Lock()
			defer h.mu.Unlock()
			addObj(obj)
		},
		UpdateFunc: func(oldObj, newObj any) {
			h.mu.Lock()
			defer h.mu.Unlock()
			deleteObj(oldObj)
			addObj(newObj)
		},
		DeleteFunc: func(obj any) {
			h.mu.Lock()
			defer h.mu.Unlock()
			deleteObj(obj)
		},
	}
	informer.AddEventHandler(handler)
	return h
}

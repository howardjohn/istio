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
	"istio.io/istio/pkg/util/sets"
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
	return p.PodName
}

func TestDependency(t *testing.T) {
	c := kube.NewFakeClient()
	pods := InformerToWatcher[*corev1.Pod](c.KubeInformer().Core().V1().Pods().Informer())
	services := InformerToWatcher[*corev1.Service](c.KubeInformer().Core().V1().Services().Informer())
	// I now have a stream for PodInfo
	podInfo := Direct[*corev1.Pod, PodInfo](
		pods,
		func(i *corev1.Pod) PodInfo {
			k, _ := cache.MetaNamespaceKeyFunc(i)
			return PodInfo{Name: k, Labels: i.Labels}
		})
	// I now have a stream for ServiceInfo
	svcInfo := Direct[*corev1.Service, ServiceInfo](
		services,
		func(i *corev1.Service) ServiceInfo {
			k, _ := cache.MetaNamespaceKeyFunc(i)
			return ServiceInfo{Name: k, Selector: i.Spec.Selector}
		})

	c.RunAndWait(test.NewStop(t))
	match := func(a PodInfo, b ServiceInfo) bool {
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
		log.Infof("computed new PodSvc: %+v", ps)
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

type Handle[O any] struct {
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

type Watcher[O any] interface {
	// Register a handler. TODO: call it for List() when we register
	Register(f func(O))
	List() []O
	Get(k string) *O
}

type ObjectDependencies[O any] struct {
	o            O
	dependencies sets.String
}

type Joined[A any, B any, O any] struct {
	mu       sync.RWMutex
	objects  map[string]ObjectDependencies[O]
	handlers []func(O)
}

func (j *Joined[A, B, O]) Get(k string) *O {
	o, f := j.objects[k]
	if !f {
		return nil
	}
	return &o.o
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
	return Map(maps.Values(j.objects), func(t ObjectDependencies[O]) O {
		return t.o
	})
}

func Join[A any, B any, O any](
	a Watcher[A],
	b Watcher[B],
	match func(a A, b B) bool,
	conv func(a A, b []B) O,
) Watcher[O] {
	joined := &Joined[A, B, O]{
		objects: make(map[string]ObjectDependencies[O]),
	}
	a.Register(func(ai A) {
		key := Key(ai)
		log.Infof("Join got new A %v", key)
		oOld, f := joined.objects[key]
		bs := b.List()
		matches := []B{}
		matchKeys := sets.New[string]()
		for _, bi := range bs {
			if match(ai, bi) {
				matches = append(matches, bi)
				matchKeys.Insert(Key(bi))
			}
		}
		oNew := conv(ai, matches)
		// Always update, in case dependencies changed
		joined.objects[key] = ObjectDependencies[O]{o: oNew, dependencies: matchKeys}
		if f && Equal(oOld.o, oNew) {
			log.Infof("no changes on A")
			return
		}
		joined.Handle(oNew)
	})
	b.Register(func(bi B) {
		log.Errorf("Join got new B %v", Key(bi))
		for _, ai := range a.List() {
			key := Key(ai)
			oOld, f := joined.objects[key]
			var deps sets.String
			if match(ai, bi) {
				// We know it depends on all old things, and this new key (it may have already depended on this, though)
				deps = oOld.dependencies.Copy().Insert(Key(bi))
				log.Infof("a %v matches b %v", Key(ai), Key(bi))
			} else {
				if !oOld.dependencies.Contains(Key(bi)) {
					log.Infof("entirely skip %v", key)
					continue
				}
				// We know it depends on all old things, and but not this new key
				deps = oOld.dependencies.Copy().Delete(Key(bi))
				log.Infof("a %v does not match b anyhmore %v", Key(ai), Key(bi))
			}

			matches := []B{}
			matchKeys := sets.New[string]()
			for bKey := range deps {
				bip := b.Get(bKey)
				if bip == nil {
					continue
				}
				bi := *bip
				if match(ai, bi) {
					matches = append(matches, bi)
					matchKeys.Insert(Key(bi))
				}
			}
			oNew := conv(ai, matches)

			// Always update, in case dependencies changed
			joined.objects[key] = ObjectDependencies[O]{o: oNew, dependencies: matchKeys}
			if f && Equal(oOld.o, oNew) {
				log.Infof("no changes on B")
				return
			}
			joined.Handle(oNew)
		}
	})
	return joined
}

type InformerWatch[I Object] struct {
	inf cache.SharedInformer
}

func (i InformerWatch[I]) Register(f func(I)) {
	addObj := func(obj any) {
		i := Extract[I](obj)
		f(i)
	}
	deleteObj := func(obj any) {
		i := Extract[I](obj)
		f(i)
	}
	handler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			addObj(obj)
		},
		UpdateFunc: func(oldObj, newObj any) {
			addObj(newObj)
		},
		DeleteFunc: func(obj any) {
			deleteObj(obj)
		},
	}
	i.inf.AddEventHandler(handler)
}

func (i InformerWatch[I]) List() []I {
	return Map(i.inf.GetStore().List(), func(t any) I {
		return t.(I)
	})
}

func (i InformerWatch[I]) Get(k string) *I {
	iff, _, _ := i.inf.GetStore().GetByKey(k)
	r := iff.(I)
	return &r
}

func InformerToWatcher[I Object](informer cache.SharedInformer) Watcher[I] {
	return InformerWatch[I]{informer}
}


func Equal[O any](a, b O) bool {
	ak, ok := any(a).(Equaler[O])
	if ok {
		return ak.Equals(b)
	}
	ao, ok := any(a).(Object)
	if ok {
		return ao.GetResourceVersion() == any(b).(Object).GetResourceVersion()
	}
	panic("Should be Equaler or Object (probably?)")
	return false
}

func Key[O any](a O) string {
	ak, ok := any(a).(Equaler[O])
	if ok {
		return ak.Key()
	}
	ao, ok := any(a).(Object)
	if ok {
		k, _ := cache.MetaNamespaceKeyFunc(ao)
		return k
	}
	panic("Should be Equaler or Object (probably?)")
	return ""
}

func Direct[I any, O any](input Watcher[I], convert func(i I) O) Watcher[O] {
	h := &Handle[O]{
		objects: make(map[string]O),
		mu:      sync.RWMutex{},
	}

	input.Register(func(i I) {
		key := Key(i)
		cur := input.Get(key)
		if cur == nil {
			old := h.objects[key]
			delete(h.objects, key)
			h.Handle(old)
		} else {
			conv := convert(*cur)
			updated := !Equal(conv, h.objects[key])
			h.objects[key] = conv
			if updated {
				h.Handle(conv)
			}
		}
	})
	return h
}

func Map[T, U any](data []T, f func(T) U) []U {
	res := make([]U, 0, len(data))
	for _, e := range data {
		res = append(res, f(e))
	}
	return res
}

func Filter[T any](data []T, f func(T) bool) []T {
	fltd := make([]T, 0, len(data))
	for _, e := range data {
		if f(e) {
			fltd = append(fltd, e)
		}
	}
	return fltd
}

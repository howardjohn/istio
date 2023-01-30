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
	"testing"
	"time"

	"go.uber.org/atomic"
	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"

	meshapi "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh"
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

func meshConfigMapData(cm *corev1.ConfigMap, key string) string {
	if cm == nil {
		return ""
	}

	cfgYaml, exists := cm.Data[key]
	if !exists {
		return ""
	}

	return cfgYaml
}

func makeConfigMapWithName(name, resourceVersion string, data map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       "istio-system",
			Name:            name,
			ResourceVersion: resourceVersion,
		},
		Data: data,
	}
}

func StaticConversion[I Object, O any](name string, f func(i I) *O) func(i I) *StaticKey[O] {
	return func(i I) *StaticKey[O] {
		// We don't care about this object
		if i.GetName() != name {
			return nil
		}
		res := f(i)
		if res == nil {
			return nil
		}
		return &StaticKey[O]{*res, Key[O](name)}
	}
}

func MeshConfigWatcher(c kube.Client, stop <-chan struct{}) Watcher[*meshapi.MeshConfig] {
	inf := WatcherFor[*corev1.ConfigMap](c)
	c.RunAndWait(stop)

	cd := Direct[*corev1.ConfigMap, StaticKey[EqualerString]]("istio ConfigMap", inf, func(i *corev1.ConfigMap) *StaticKey[EqualerString] {
		if i.Name != "istio" { // fake client won't filter.
			return nil
		}
		return &StaticKey[EqualerString]{EqualerString(meshConfigMapData(i, "mesh")), "istio"}
	})
	ud := Direct[*corev1.ConfigMap, StaticKey[EqualerString]]("istio-user ConfigMap", inf, func(i *corev1.ConfigMap) *StaticKey[EqualerString] {
		if i.Name != "istio-user" { // fake client won't filter.
			return nil
		}
		return &StaticKey[EqualerString]{EqualerString(meshConfigMapData(i, "mesh")), "istio-user"}
	})

	combined := Join("Merged MeshConfig", cd, ud,
		func(core, user *StaticKey[EqualerString]) *StaticKey[*meshapi.MeshConfig] {
			mc := mesh.DefaultMeshConfig()
			order := []string{}
			if user != nil {
				order = append(order, string(user.Obj))
			}
			if core != nil {
				order = append(order, string(core.Obj))
			}
			for _, yml := range order {
				mcn, err := mesh.ApplyMeshConfig(yml, mc)
				if err != nil {
					log.Error(err)
					continue
				}
				mc = mcn
			}
			return &StaticKey[*meshapi.MeshConfig]{mc, "mesh"}
		})
	return StripStaticKey(combined)
}

func meshConfig(t *testing.T, c kube.Client) Watcher[*meshapi.MeshConfig] {
	meshh := MeshConfigWatcher(c, test.NewStop(t))
	cur := atomic.NewString("")
	meshh.Register(func(config *meshapi.MeshConfig) {
		cur.Store(config.GetIngressClass())
		log.Infof("New mesh cfg: %v", config.GetIngressClass())
	})

	cmCore := makeConfigMapWithName("istio", "1", map[string]string{
		"mesh": "ingressClass: core",
	})
	cmUser := makeConfigMapWithName("istio-user", "1", map[string]string{
		"mesh": "ingressClass: user",
	})
	cmCoreAlt := makeConfigMapWithName("istio", "1", map[string]string{
		"mesh": "ingressClass: alt",
	})
	cms := c.Kube().CoreV1().ConfigMaps("istio-system")

	t.Log("insert user")
	if _, err := cms.Create(context.Background(), cmUser, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	retry.UntilOrFail(t, func() bool { return cur.Load() == "user" }, retry.Timeout(time.Second))

	t.Log("create core")
	if _, err := cms.Create(context.Background(), cmCore, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	retry.UntilOrFail(t, func() bool { return cur.Load() == "core" }, retry.Timeout(time.Second))

	t.Log("update core to alt")
	if _, err := cms.Update(context.Background(), cmCoreAlt, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	retry.UntilOrFail(t, func() bool { return cur.Load() == "alt" }, retry.Timeout(time.Second))

	t.Log("update core back")
	if _, err := cms.Update(context.Background(), cmCore, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	retry.UntilOrFail(t, func() bool { return cur.Load() == "core" }, retry.Timeout(time.Second))

	t.Log("delete core")
	if err := cms.Delete(context.Background(), cmCoreAlt.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	retry.UntilOrFail(t, func() bool { return cur.Load() == "user" }, retry.Timeout(time.Second))

	t.Log("done")

	return meshh
}

func TestDependency(t *testing.T) {
	c := kube.NewFakeClient()
	defer c.DAG().X()
	meshh := meshConfig(t, c)

	ambientMode := Singleton[meshapi.MeshConfig_AmbientMeshConfig_AmbientMeshMode, meshapi.MeshConfig_AmbientMeshConfig_AmbientMeshMode]("AmbientMode",
		Direct[*meshapi.MeshConfig, meshapi.MeshConfig_AmbientMeshConfig_AmbientMeshMode]("_AmbientMode", meshh, func(m *meshapi.MeshConfig) *meshapi.MeshConfig_AmbientMeshConfig_AmbientMeshMode {
			return Ptr(m.GetAmbientMesh().GetMode())
		}), Identity[meshapi.MeshConfig_AmbientMeshConfig_AmbientMeshMode])
	_ = ambientMode
	_ = meshh
	pods := InformerToWatcher[*corev1.Pod](c.DAG(), c.KubeInformer().Core().V1().Pods().Informer())
	services := InformerToWatcher[*corev1.Service](c.DAG(), c.KubeInformer().Core().V1().Services().Informer())
	// I now have a stream for PodInfo
	podInfo := Direct[*corev1.Pod, PodInfo](
		"PodInfo",
		pods,
		func(i *corev1.Pod) *PodInfo {
			k, _ := cache.MetaNamespaceKeyFunc(i)
			return &PodInfo{Name: k, Labels: i.Labels}
		})
	// I now have a stream for ServiceInfo
	svcInfo := Direct[*corev1.Service, ServiceInfo](
		"ServiceInfo",
		services,
		func(i *corev1.Service) *ServiceInfo {
			k, _ := cache.MetaNamespaceKeyFunc(i)
			return &ServiceInfo{Name: k, Selector: i.Spec.Selector}
		})

	// c.RunAndWait(test.NewStop(t))
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
	podSvc := SingleToMany[PodInfo, ServiceInfo, PodSvc]("PodSvc", podInfo, svcInfo, match, convert)
	_ = Join[PodSvc, *meshapi.MeshConfig, PodSvc]("todo", podSvc, meshh, func(a *PodSvc, b **meshapi.MeshConfig) *PodSvc {
		return nil
	})
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
	time.Sleep(time.Millisecond * 50)
	t.Log("final", podSvc.List())
}

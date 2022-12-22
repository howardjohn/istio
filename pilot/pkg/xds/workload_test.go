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

package xds

import (
	"context"
	"testing"
	"time"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/security/v1beta1"
	"istio.io/istio/pilot/pkg/ambient"
	"istio.io/istio/pilot/pkg/model"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
)

func buildExpect(t *testing.T) func(resp *discovery.DeltaDiscoveryResponse, names ...string) {
	return func(resp *discovery.DeltaDiscoveryResponse, names ...string) {
		t.Helper()
		want := sets.New(names...)
		have := sets.New[string]()
		for _, r := range resp.Resources {
			have.Insert(r.Name)
		}
		assert.Equal(t, sets.SortedList(have), sets.SortedList(want))
	}
}

func buildExpectExpectRemoved(t *testing.T) func(resp *discovery.DeltaDiscoveryResponse, names ...string) {
	return func(resp *discovery.DeltaDiscoveryResponse, names ...string) {
		t.Helper()
		want := sets.New(names...)
		have := sets.New[string]()
		for _, r := range resp.RemovedResources {
			have.Insert(r)
		}
		assert.Equal(t, sets.SortedList(have), sets.SortedList(want))
	}
}

func TestWorkloadReconnect(t *testing.T) {
	expect := buildExpect(t)
	s := NewFakeDiscoveryServer(t, FakeOptions{})
	ads := s.ConnectDeltaADS().WithType(v3.WorkloadType)
	createPod(s, "pod", "sa", "127.0.0.1")
	ads.Request(&discovery.DeltaDiscoveryRequest{
		ResourceNamesSubscribe:   []string{"*"},
		ResourceNamesUnsubscribe: []string{"*"},
	})
	ads.ExpectEmptyResponse()

	// Now subscribe to the pod, should get it back
	resp := ads.RequestResponseAck(&discovery.DeltaDiscoveryRequest{
		ResourceNamesSubscribe: []string{"127.0.0.1"},
	})
	expect(resp, "127.0.0.1")
	ads.Cleanup()

	// Reconnect
	ads = s.ConnectDeltaADS().WithType(v3.WorkloadType)
	ads.Request(&discovery.DeltaDiscoveryRequest{
		ResourceNamesSubscribe:   []string{"*"},
		ResourceNamesUnsubscribe: []string{"*"},
		InitialResourceVersions: map[string]string{
			"127.0.0.1": "",
		},
	})
	expect(ads.ExpectResponse(), "127.0.0.1")
}

func TestWorkload(t *testing.T) {
	t.Run("ondemand", func(t *testing.T) {
		expect := buildExpect(t)
		expectRemoved := buildExpectExpectRemoved(t)
		s := NewFakeDiscoveryServer(t, FakeOptions{})
		ads := s.ConnectDeltaADS().WithType(v3.WorkloadType)

		ads.Request(&discovery.DeltaDiscoveryRequest{
			ResourceNamesSubscribe:   []string{"*"},
			ResourceNamesUnsubscribe: []string{"*"},
		})
		ads.ExpectEmptyResponse()

		// Create pod we are not subscribe to; should be a NOP
		createPod(s, "pod", "sa", "127.0.0.1")
		ads.ExpectNoResponse()

		// Now subscribe to it, should get it back
		resp := ads.RequestResponseAck(&discovery.DeltaDiscoveryRequest{
			ResourceNamesSubscribe: []string{"127.0.0.1"},
		})
		expect(resp, "127.0.0.1")

		// Subscribe to unknown pod
		ads.Request(&discovery.DeltaDiscoveryRequest{
			ResourceNamesSubscribe: []string{"127.0.0.2"},
		})
		// "Removed" is a misnomer, but per the spec this is how we report "not found"
		expectRemoved(ads.ExpectResponse(), "127.0.0.2")

		// Once we create it, we should get a push
		createPod(s, "pod2", "sa", "127.0.0.2")
		expect(ads.ExpectResponse(), "127.0.0.2")

		// TODO: implement pod update; this actually cannot really be done without waypoints or VIPs
		deletePod(s, "pod")
		expectRemoved(ads.ExpectResponse(), "127.0.0.1")

		// Add service: we should not get any new resources, but updates to existing ones
		// Note: we are not subscribed to svc1 explicitly, but it impacts pods we are subscribed to
		createService(s, "svc1", "default", map[string]string{"app": "sa"})
		expect(ads.ExpectResponse(), "127.0.0.2")
		// Creating a pod in the service should send an update as usual
		createPod(s, "pod", "sa", "127.0.0.1")
		expect(ads.ExpectResponse(), "127.0.0.1")
		// Make service not select workload should also update things
		createService(s, "svc1", "default", map[string]string{"app": "not-sa"})
		expect(ads.ExpectResponse(), "127.0.0.1", "127.0.0.2")

		// Now create pods in the service...
		createPod(s, "pod4", "not-sa", "127.0.0.4")
		// Not subscribed, no response
		ads.ExpectNoResponse()

		// Now we subscribe to the service explicitly
		ads.Request(&discovery.DeltaDiscoveryRequest{
			ResourceNamesSubscribe: []string{"10.0.0.1"},
		})
		// Should get updates for all pods in the service
		expect(ads.ExpectResponse(), "127.0.0.4")
		// Adding a pod in the service should trigger an update for that pod, even if we didn't explicitly subscribe
		createPod(s, "pod5", "not-sa", "127.0.0.5")
		expect(ads.ExpectResponse(), "127.0.0.5")

		// And if the service changes to no longer select them, we should see them *removed* (not updated)
		createService(s, "svc1", "default", map[string]string{"app": "nothing"})
		expect(ads.ExpectResponse(), "127.0.0.4", "127.0.0.5")
	})
	t.Run("wildcard", func(t *testing.T) {
		expect := buildExpect(t)
		expectRemoved := buildExpectExpectRemoved(t)
		s := NewFakeDiscoveryServer(t, FakeOptions{})
		ads := s.ConnectDeltaADS().WithType(v3.WorkloadType)

		ads.Request(&discovery.DeltaDiscoveryRequest{
			ResourceNamesSubscribe: []string{"*"},
		})
		ads.ExpectEmptyResponse()

		// Create pod, due to wildcard subscribe we should receive it
		createPod(s, "pod", "sa", "127.0.0.1")
		expect(ads.ExpectResponse(), "127.0.0.1")

		// A new pod should push only that one
		createPod(s, "pod2", "sa", "127.0.0.2")
		expect(ads.ExpectResponse(), "127.0.0.2")

		// TODO: implement pod update; this actually cannot really be done without waypoints or VIPs
		deletePod(s, "pod")
		expectRemoved(ads.ExpectResponse(), "127.0.0.1")

		// Add service: we should not get any new resources, but updates to existing ones
		createService(s, "svc1", "default", map[string]string{"app": "sa"})
		expect(ads.ExpectResponse(), "127.0.0.2")
		// Creating a pod in the service should send an update as usual
		createPod(s, "pod", "sa", "127.0.0.3")
		expect(ads.ExpectResponse(), "127.0.0.3")

		// Make service not select workload should also update things
		createService(s, "svc1", "default", map[string]string{"app": "not-sa"})
		expect(ads.ExpectResponse(), "127.0.0.2", "127.0.0.3")
	})
}

func deletePod(s *FakeDiscoveryServer, name string) {
	err := s.kubeClient.Kube().CoreV1().Pods("default").Delete(context.Background(), name, metav1.DeleteOptions{})
	if err != nil {
		s.t.Fatal(err)
	}
}

func createRBAC(s *FakeDiscoveryServer, name string, ns string) {
	_, err := s.Env().Create(config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.AuthorizationPolicy,
			Name:             name,
			Namespace:        ns,
		},
		Spec: &v1beta1.AuthorizationPolicy{},
	})
	if err != nil {
		s.t.Fatal(err)
	}
}

func createPod(s *FakeDiscoveryServer, name string, sa string, ip string) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels: map[string]string{
				// TODO: shouldn't really need this
				ambient.LabelType: ambient.TypeWorkload,
				"app":             sa,
			},
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: sa,
		},
		Status: corev1.PodStatus{
			PodIP: ip,
			Conditions: []corev1.PodCondition{
				{
					Type:               corev1.PodReady,
					Status:             corev1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
				},
			},
		},
	}
	_, err := s.kubeClient.Kube().CoreV1().Pods("default").Create(context.Background(), pod, metav1.CreateOptions{})
	if err != nil {
		if kerrors.IsAlreadyExists(err) {
			_, err = s.kubeClient.Kube().CoreV1().Pods("default").Update(context.Background(), pod, metav1.UpdateOptions{})
		}
		if err != nil {
			s.t.Fatal(err)
		}
	}
}

// nolint: unparam
func createService(s *FakeDiscoveryServer, name, namespace string, selector map[string]string) {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.0.0.1",
			Ports: []corev1.ServicePort{{
				Name:     "tcp",
				Port:     80,
				Protocol: "TCP",
			}},
			Selector: selector,
			Type:     corev1.ServiceTypeClusterIP,
		},
	}

	_, err := s.kubeClient.Kube().CoreV1().Services(namespace).Create(context.TODO(), service, metav1.CreateOptions{})
	if err != nil {
		if kerrors.IsAlreadyExists(err) {
			_, err = s.kubeClient.Kube().CoreV1().Services(namespace).Update(context.TODO(), service, metav1.UpdateOptions{})
		}
		if err != nil {
			s.t.Fatalf("Cannot create service %s in namespace %s (error: %v)", name, namespace, err)
		}
	}
}

func TestWorkloadRBAC(t *testing.T) {
	expect := buildExpect(t)
	expectRemoved := buildExpectExpectRemoved(t)
	s := NewFakeDiscoveryServer(t, FakeOptions{})
	ads := s.ConnectDeltaADS().WithType(v3.WorkloadRBACType).WithTimeout(time.Second * 10).WithMetadata(model.NodeMetadata{AmbientType: ambient.TypeZTunnel})

	ads.Request(&discovery.DeltaDiscoveryRequest{
		ResourceNamesSubscribe: []string{"*"},
	})
	ads.ExpectEmptyResponse()

	// Create policy, due to wildcard subscribe we should receive it
	createRBAC(s, "policy1", "ns")
	expect(ads.ExpectResponse(), "ns/policy1")

	// A new policy should push only that one
	createRBAC(s, "policy2", "ns")
	expect(ads.ExpectResponse(), "ns/policy2")

	s.Env().Delete(gvk.AuthorizationPolicy, "policy2", "ns", nil)
	expectRemoved(ads.ExpectResponse(), "ns/policy2")

	// Irrelevant update shouldn't push
	createPod(s, "pod", "sa", "127.0.0.1")
	ads.ExpectNoResponse()
}

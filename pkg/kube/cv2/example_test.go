package cv2

import (
	"context"
	"net/netip"
	"strings"
	"testing"
	"time"

	// security "istio.io/api/security/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	meshapi "istio.io/api/mesh/v1alpha1"
	securityclient "istio.io/client-go/pkg/apis/security/v1beta1"
	"istio.io/istio/pilot/pkg/ambient/ambientpod"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/kube"
	kubelabels "istio.io/istio/pkg/kube/labels"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/workloadapi"
	istiolog "istio.io/pkg/log"
)

func meshConfigMapData(cm *corev1.ConfigMap) string {
	if cm == nil {
		return ""
	}

	cfgYaml, exists := cm.Data["mesh"]
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

func testMeshConfig(t *testing.T, c kube.Client, MeshConfig Singleton[meshapi.MeshConfig]) {
	t.Run("MeshConfig", func(t *testing.T) {
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
		retry.UntilOrFail(t, func() bool { return MeshConfig.Get().GetIngressClass() == "user" }, retry.Timeout(time.Second))

		t.Log("create core")
		if _, err := cms.Create(context.Background(), cmCore, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		retry.UntilOrFail(t, func() bool { return MeshConfig.Get().GetIngressClass() == "core" }, retry.Timeout(time.Second))

		t.Log("update core to alt")
		if _, err := cms.Update(context.Background(), cmCoreAlt, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
		retry.UntilOrFail(t, func() bool { return MeshConfig.Get().GetIngressClass() == "alt" }, retry.Timeout(time.Second))

		t.Log("NOP change")
		cmCoreAlt.Annotations = map[string]string{"a": "B"}
		if _, err := cms.Update(context.Background(), cmCoreAlt, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
		retry.UntilOrFail(t, func() bool { return MeshConfig.Get().GetIngressClass() == "alt" }, retry.Timeout(time.Second))

		t.Log("update core back")
		if _, err := cms.Update(context.Background(), cmCore, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
		retry.UntilOrFail(t, func() bool { return MeshConfig.Get().GetIngressClass() == "core" }, retry.Timeout(time.Second))

		t.Log("delete core")
		if err := cms.Delete(context.Background(), cmCoreAlt.Name, metav1.DeleteOptions{}); err != nil {
			t.Fatal(err)
		}
		retry.UntilOrFail(t, func() bool { return MeshConfig.Get().GetIngressClass() == "user" }, retry.Timeout(time.Second))
		t.Log("done")
	})
}

func MeshConfigWatcher(c kube.Client, stop chan struct{}) Singleton[meshapi.MeshConfig] {
	// Register an informer watch...
	ConfigMaps := CollectionFor[*corev1.ConfigMap](c)
	c.RunAndWait(stop)
	// Create a new MeshConfig type. Unlike ConfigMaps, this is derived from other
	MeshConfig := NewSingleton[meshapi.MeshConfig](
		func(ctx HandlerContext) *meshapi.MeshConfig {
			log.Infof("Recomputing mesh config")
			meshCfg := mesh.DefaultMeshConfig()
			cms := []*corev1.ConfigMap{}
			cms = AppendNonNil(cms, FetchOne(ctx, ConfigMaps, FilterName("istio-user")))
			cms = AppendNonNil(cms, FetchOne(ctx, ConfigMaps, FilterName("istio")))

			for _, c := range cms {
				n, err := mesh.ApplyMeshConfig(meshConfigMapData(c), meshCfg)
				if err != nil {
					log.Error(err)
					continue
				}
				meshCfg = n
			}
			return meshCfg
		},
	)
	return MeshConfig
}

func WorkloadWatcher(t test.Failer, c kube.Client, MeshConfig Singleton[meshapi.MeshConfig]) Collection[model.WorkloadInfo] {
	AuthzPolicies := CollectionFor[*securityclient.AuthorizationPolicy](c)
	Services := CollectionFor[*corev1.Service](c)
	Pods := CollectionFor[*corev1.Pod](c)
	Namespaces := CollectionFor[*corev1.Namespace](c)
	c.RunAndWait(test.NewStop(t))

	return NewCollection(Pods, func(ctx HandlerContext, p *corev1.Pod) *model.WorkloadInfo {
		// TODO: only selector ones
		policies := Fetch(ctx, AuthzPolicies, FilterSelects(p.Labels))
		meshCfg := FetchOne(ctx, MeshConfig.AsCollection())
		namespace := Flatten(FetchOne(ctx, Namespaces, FilterName(p.Namespace)))
		services := Fetch(ctx, Services, FilterSelects(p.GetLabels()))
		waypointPods := Fetch(ctx, Pods, FilterLabel(map[string]string{
			constants.ManagedGatewayLabel: constants.ManagedGatewayMeshController,
		}), FilterGeneric(func(a any) bool {
			return a.(*corev1.Pod).Spec.ServiceAccountName == p.Spec.ServiceAccountName
		}))
		w := &workloadapi.Workload{
			Name:           p.Name,
			Namespace:      p.Namespace,
			Address:        parseAddr(p.Status.PodIP).AsSlice(),
			ServiceAccount: p.Spec.ServiceAccountName,
			WaypointAddresses: Map(waypointPods, func(t *corev1.Pod) []byte {
				return netip.MustParseAddr(t.Status.PodIP).AsSlice()
			}),
			Node:       p.Spec.NodeName,
			VirtualIps: constructVIPs(p, services),
			AuthorizationPolicies: Map(policies, func(t *securityclient.AuthorizationPolicy) string {
				return t.Name
			}),
		}

		if td := spiffe.GetTrustDomain(); td != "cluster.local" {
			w.TrustDomain = td
		}
		w.WorkloadName, w.WorkloadType = workloadNameAndType(p)
		w.CanonicalName, w.CanonicalRevision = kubelabels.CanonicalService(p.Labels, w.WorkloadName)

		if ambientpod.ShouldPodBeInIpset(namespace, p, meshCfg.GetAmbientMesh().GetMode().String(), true) {
			w.Protocol = workloadapi.Protocol_HTTP
		}
		// Otherwise supports tunnel directly
		if model.SupportsTunnel(p.Labels, model.TunnelHTTP) {
			w.Protocol = workloadapi.Protocol_HTTP
			w.NativeHbone = true
		}
		return &model.WorkloadInfo{Workload: w}
	})
}

func TestWorkload(t *testing.T) {
	log.SetOutputLevel(istiolog.DebugLevel)
	c := kube.NewFakeClient()
	MeshConfig := MeshConfigWatcher(c, test.NewStop(t))
	testMeshConfig(t, c, MeshConfig)
	Workloads := WorkloadWatcher(t, c, MeshConfig)
	testWorkloads(t, c, Workloads)
}

func testWorkloads(t *testing.T, c kube.Client, Workloads Collection[model.WorkloadInfo]) {
	t.Run("Workloads", func(t *testing.T) {
		t.Log("pod1 create")
		c.Kube().CoreV1().Pods("default").Create(context.Background(), &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "pod1", Labels: map[string]string{"app": "bar"}},
			Status:     corev1.PodStatus{PodIP: "10.0.0.1"},
		}, metav1.CreateOptions{})
		retry.UntilOrFail(t, func() bool {
			return Workloads.GetKey("10.0.0.1") != nil
		}, retry.Timeout(time.Second))

		t.Log("pod2 create")
		c.Kube().CoreV1().Pods("default").Create(context.Background(), &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "pod2", Labels: map[string]string{"app": "bar"}},
			Status:     corev1.PodStatus{PodIP: "10.0.0.2"},
		}, metav1.CreateOptions{})
		retry.UntilOrFail(t, func() bool {
			return Workloads.GetKey("10.0.0.2") != nil
		}, retry.Timeout(time.Second))

		t.Log("pod3 create")
		c.Kube().CoreV1().Pods("default").Create(context.Background(), &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "pod3", Labels: map[string]string{"app": "not-bar"}},
			Status:     corev1.PodStatus{PodIP: "10.0.0.3"},
			Spec:       corev1.PodSpec{ServiceAccountName: "foo"},
		}, metav1.CreateOptions{})
		retry.UntilOrFail(t, func() bool {
			return Workloads.GetKey("10.0.0.3") != nil
		}, retry.Timeout(time.Second))

		t.Log("svc create")
		c.Kube().CoreV1().Services("default").Create(context.Background(), &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "svc1"},
			Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "bar"}, ClusterIP: "10.1.0.1"},
		}, metav1.CreateOptions{})
		retry.UntilOrFail(t, func() bool {
			w := Workloads.GetKey("10.0.0.2")
			if w == nil {
				return false
			}
			_, f := w.VirtualIps["10.1.0.1"]
			return f
		}, retry.Timeout(time.Second))

		t.Log("namespace create NOP")
		c.Kube().CoreV1().Namespaces().Create(context.Background(), &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "not-default", Labels: map[string]string{"istio.io/dataplane-mode": "ambient"}},
		}, metav1.CreateOptions{})

		t.Log("namespace create")
		c.Kube().CoreV1().Namespaces().Create(context.Background(), &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "default", Labels: map[string]string{"istio.io/dataplane-mode": "ambient"}},
		}, metav1.CreateOptions{})
		retry.UntilOrFail(t, func() bool {
			return Workloads.GetKey("10.0.0.3").Protocol == workloadapi.Protocol_HTTP
		}, retry.Timeout(time.Second))

		t.Log("waypoint create")
		c.Kube().CoreV1().Pods("default").Create(context.Background(), &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "waypoint", Labels: map[string]string{constants.ManagedGatewayLabel: constants.ManagedGatewayMeshController}},
			Status:     corev1.PodStatus{PodIP: "10.0.0.3"},
			Spec:       corev1.PodSpec{ServiceAccountName: "foo"},
		}, metav1.CreateOptions{})
		retry.UntilOrFail(t, func() bool {
			return len(Workloads.GetKey("10.0.0.3").WaypointAddresses) == 1
		}, retry.Timeout(time.Second))

		for _, wl := range Workloads.List(metav1.NamespaceAll) {
			t.Logf("Final workload: %+v", wl)
		}
		t.Log(Workloads.GetKey("10.0.0.1"))
	})
}

func constructVIPs(p *corev1.Pod, services []*corev1.Service) map[string]*workloadapi.PortList {
	vips := map[string]*workloadapi.PortList{}
	for _, svc := range services {
		for _, vip := range getVIPs(svc) {
			if vips[vip] == nil {
				vips[vip] = &workloadapi.PortList{}
			}
			for _, port := range svc.Spec.Ports {
				if port.Protocol != corev1.ProtocolTCP {
					continue
				}
				targetPort, err := controller.FindPort(p, &port)
				if err != nil {
					log.Debug(err)
					continue
				}
				vips[vip].Ports = append(vips[vip].Ports, &workloadapi.Port{
					ServicePort: uint32(port.Port),
					TargetPort:  uint32(targetPort),
				})
			}
		}
	}
	return vips
}

func getVIPs(svc *corev1.Service) []string {
	res := []string{}
	if svc.Spec.ClusterIP != "" && svc.Spec.ClusterIP != "None" {
		res = append(res, svc.Spec.ClusterIP)
	}
	for _, ing := range svc.Status.LoadBalancer.Ingress {
		res = append(res, ing.IP)
	}
	return res
}

func workloadNameAndType(pod *corev1.Pod) (string, workloadapi.WorkloadType) {
	if len(pod.GenerateName) == 0 {
		return pod.Name, workloadapi.WorkloadType_POD
	}

	// if the pod name was generated (or is scheduled for generation), we can begin an investigation into the controlling reference for the pod.
	var controllerRef metav1.OwnerReference
	controllerFound := false
	for _, ref := range pod.GetOwnerReferences() {
		if ref.Controller != nil && *ref.Controller {
			controllerRef = ref
			controllerFound = true
			break
		}
	}

	if !controllerFound {
		return pod.Name, workloadapi.WorkloadType_POD
	}

	// heuristic for deployment detection
	if controllerRef.Kind == "ReplicaSet" && strings.HasSuffix(controllerRef.Name, pod.Labels["pod-template-hash"]) {
		name := strings.TrimSuffix(controllerRef.Name, "-"+pod.Labels["pod-template-hash"])
		return name, workloadapi.WorkloadType_DEPLOYMENT
	}

	if controllerRef.Kind == "Job" {
		// figure out how to go from Job -> CronJob
		return controllerRef.Name, workloadapi.WorkloadType_JOB
	}

	if controllerRef.Kind == "CronJob" {
		// figure out how to go from Job -> CronJob
		return controllerRef.Name, workloadapi.WorkloadType_CRONJOB
	}

	return pod.Name, workloadapi.WorkloadType_POD
}

func parseAddr(s string) netip.Addr {
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}
	}
	return a
}

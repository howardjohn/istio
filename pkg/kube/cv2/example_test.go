package cv2

import (
	"context"
	"testing"
	"time"

	"go.uber.org/atomic"
	meshapi "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/retry"
	istiolog "istio.io/pkg/log"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func meshConfig(t *testing.T, c kube.Client) Singleton[meshapi.MeshConfig] {
	meshh := MeshConfigWatcher(c, test.NewStop(t))
	cur := atomic.NewString("")
	_ = cur
	//meshh.Register(func(config *meshapi.MeshConfig) {
	//	cur.Store(config.GetIngressClass())
	//	log.Infof("New mesh cfg: %v", config.GetIngressClass())
	//})

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
	time.Sleep(time.Millisecond * 50)
	t.Log(meshh.Get().GetIngressClass())
	retry.UntilOrFail(t, func() bool { return meshh.Get().GetIngressClass() == "user" }, retry.Timeout(time.Second))

	t.Log("create core")
	if _, err := cms.Create(context.Background(), cmCore, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	retry.UntilOrFail(t, func() bool { return meshh.Get().GetIngressClass() == "core" }, retry.Timeout(time.Second))
	time.Sleep(time.Millisecond * 50)

	t.Log("update core to alt")
	if _, err := cms.Update(context.Background(), cmCoreAlt, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	retry.UntilOrFail(t, func() bool { return meshh.Get().GetIngressClass() == "alt" }, retry.Timeout(time.Second))
	time.Sleep(time.Millisecond * 50)

	t.Log("NOP change")
	cmCoreAlt.Annotations = map[string]string{"a": "B"}
	if _, err := cms.Update(context.Background(), cmCoreAlt, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	retry.UntilOrFail(t, func() bool { return meshh.Get().GetIngressClass() == "alt" }, retry.Timeout(time.Second))
	time.Sleep(time.Millisecond * 50)

	t.Log("update core back")
	if _, err := cms.Update(context.Background(), cmCore, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	retry.UntilOrFail(t, func() bool { return meshh.Get().GetIngressClass() == "core" }, retry.Timeout(time.Second))
	time.Sleep(time.Millisecond * 50)

	t.Log("delete core")
	if err := cms.Delete(context.Background(), cmCoreAlt.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	retry.UntilOrFail(t, func() bool { return meshh.Get().GetIngressClass() == "user" }, retry.Timeout(time.Second))
	time.Sleep(time.Millisecond * 50)

	t.Log("done")

	return meshh
}

func MeshConfigWatcher(c kube.Client, stop chan struct{}) Singleton[meshapi.MeshConfig] {
	// Register an informer watch...
	ConfigMaps := CollectionFor[*corev1.ConfigMap](c)
	c.RunAndWait(stop)
	// Create a new MeshConfig type. Unlike ConfigMaps, this is derived from other
	MeshConfig := NewSingleton[meshapi.MeshConfig](
		func(ctx HandlerContext) *meshapi.MeshConfig {
			log.Errorf("howardjohn: Computing mesh config")
			meshCfg := mesh.DefaultMeshConfig()
			cms := []*corev1.ConfigMap{}
			log.Errorf("howardjohn: fetch user")
			cms = AppendNonNil(cms, FetchOne[*corev1.ConfigMap](ctx, ConfigMaps, FilterName("istio-user")))
			log.Errorf("howardjohn: fetch core")
			cms = AppendNonNil(cms, FetchOne[*corev1.ConfigMap](ctx, ConfigMaps, FilterName("istio")))
			//if f := FetchOneNamed[*corev1.ConfigMap](ctx, "istio-user"); f != nil {
			//	cms = append(cms, *f)
			//}
			//if f := FetchOneNamed[*corev1.ConfigMap](ctx, "istio"); f != nil {
			//	cms = append(cms, *f)
			//}
			log.Errorf("howardjohn: -1: %v", meshCfg.GetIngressClass())
			for i, c := range cms {
				n, err := mesh.ApplyMeshConfig(meshConfigMapData(c), meshCfg)
				if err != nil {
					log.Error(err)
					continue
				}
				meshCfg = n
				log.Errorf("howardjohn: %v: %v/%v", i, meshCfg.GetIngressClass(), meshConfigMapData(c))
			}
			log.Errorf("howardjohn: computed to %v", meshCfg.GetIngressClass())
			return meshCfg
		},
		//DependOnCollection[*corev1.ConfigMap](ConfigMaps, FilterName("istio")),
		//DependOnCollection[*corev1.ConfigMap](ConfigMaps, FilterName("istio-user")),
	)
	return MeshConfig
}

func TestWorkload(t *testing.T) {
	log.SetOutputLevel(istiolog.DebugLevel)
	c := kube.NewFakeClient()
	meshConfig(t, c)
	//Services := Watch[Services]()
	//Pods := Watch[Pods]()
	//Namespaces := Watch[Namespaces]()
	//Workloads := Handler(func(p Pod) Workload {
	//	// Read from MeshConfig. There is only one of these, so we don't need any filters.
	//	mtlsMode := FetchOneNamed(MeshConfig).MtlsMode;
	//	// Fetch a list of Services, but only ones in the same namespace as Pod that select Pod
	//	services := Fetch(Services, select.Namespace(p.Namespace), select.SelectorLabels(p.labels))
	//		namespace := FetchOneNamed(Namespace, select.Name(p.Namespace))
	//			protocol := "Plaintext"
	//			if mtlsMode == ALWAYS || (mtlsMode == LABEL && namespace.Labels[MtlsEnabled] == "true") {
	//				protocol = "Mtls"
	//			}
	//			return Workload {
	//				Name: p.Name,
	//				Namespace: p.Namespace,
	//				IP: p.Spec.IP,
	//				Services: FetchIP(services),
	//				Protocol: protocol,
	//			}
	//		})
}

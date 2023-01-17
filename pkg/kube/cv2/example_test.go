package cv2

import (
	"testing"

	meshapi "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/kube"
	"istio.io/pkg/log"
	corev1 "k8s.io/api/core/v1"
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


func TestWorkload(t *testing.T) {
	c := kube.NewFakeClient()
	// Register an informer watch...
	ConfigMaps := CollectionFor[*corev1.ConfigMap](c)
	// Create a new MeshConfig type. Unlike ConfigMaps, this is derived from other
	NewSingleton[*meshapi.MeshConfig](
		func(ctx HandlerContext) **meshapi.MeshConfig {
			meshCfg := mesh.DefaultMeshConfig()
			cms := []*corev1.ConfigMap{}
			// TODO: named, have 2 deps
			if f := FetchOneNamed[*corev1.ConfigMap](ctx, "istio"); f != nil {
				cms = append(cms, *f)
			}
			for _, c := range cms {
				n, err := mesh.ApplyMeshConfig(meshConfigMapData(c), meshCfg)
				if err != nil {
					log.Error(err)
					continue
				}
				meshCfg = n
			}
			return &meshCfg
		},
		DependOnCollection[*corev1.ConfigMap](ConfigMaps, FilterName("istio")),
	)
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

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

package controller

import (
	"context"
	"fmt"

	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	appsv1ac "k8s.io/client-go/applyconfigurations/apps/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"
	"sigs.k8s.io/gateway-api/apis/v1alpha2"
	gateway "sigs.k8s.io/gateway-api/apis/v1beta1"
	"sigs.k8s.io/yaml"

	meshapi "istio.io/api/mesh/v1alpha1"
	istiogw "istio.io/istio/pilot/pkg/config/kube/gateway"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/cv2"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/test/util/tmpl"
	istiolog "istio.io/pkg/log"
)

type WaypointProxyController struct {
	client  kubelib.Client
	patcher istiogw.Patcher

	cluster cluster.ID

	injectConfig func() inject.WebhookConfig
}

var (
	waypointLog = istiolog.RegisterScope("waypointproxy", "", 0)
	waypointFM  = "waypoint proxy controller"
)

func NewWaypointProxyController(client kubelib.Client, clusterID cluster.ID,
	config func() inject.WebhookConfig, addHandler func(func()),
) *WaypointProxyController {
	rc := &WaypointProxyController{
		client:       client,
		cluster:      clusterID,
		injectConfig: config,
		patcher: func(gvr schema.GroupVersionResource, name string, namespace string, data []byte, subresources ...string) error {
			c := client.Dynamic().Resource(gvr).Namespace(namespace)
			t := true
			_, err := c.Patch(
				context.Background(),
				name,
				types.ApplyPatchType,
				data,
				metav1.PatchOptions{
					Force:        &t,
					FieldManager: waypointFM,
				},
				subresources...)
			return err
		},
	}

	//// On injection template change, requeue all gateways
	//addHandler(func() {
	//	gws, _ := rc.gateways.List(klabels.Everything())
	//	for _, gw := range gws {
	//		rc.queue.AddObject(gw)
	//	}
	//})

	Gateways := cv2.CollectionFor[*gateway.Gateway](client)
	Deployments := cv2.CollectionFor[*appsv1.Deployment](client)
	ServiceAccounts := cv2.CollectionFor[*corev1.ServiceAccount](client)
	Waypoints := cv2.NewCollection(Gateways, func(ctx cv2.HandlerContext, gw *gateway.Gateway) *Waypoint {
		log.Infof("handling Gateway %v", gw.Name)
		// TODO: injectConfig as singleton
		if rc.injectConfig().Values.Struct().GetGlobal().GetHub() == "" {
			log.Errorf("no inject config")
			// Mostly used to avoid issues with local runs
			return nil
		}
		log := waypointLog.WithLabels("gateway", gw.Name)
		if gw.Spec.GatewayClassName != "istio-mesh" {
			log.Warnf("mismatched class %q", gw.Spec.GatewayClassName)
			return nil
		}

		gatewaySA := gw.Annotations["istio.io/service-account"]
		forSa := gatewaySA
		if gatewaySA == "" {
			gatewaySA = "namespace"
		}
		gatewaySA += "-waypoint"

		input := MergedInput{
			Namespace:         gw.Namespace,
			GatewayName:       gw.Name,
			UID:               string(gw.UID),
			ServiceAccount:    gatewaySA,
			Cluster:           rc.cluster.String(),
			ProxyConfig:       rc.injectConfig().MeshConfig.GetDefaultConfig(),
			ForServiceAccount: forSa,
		}
		proxySa := renderServiceAccountApply(input)
		proxyDeploy, err := renderDeploymentApply(input, rc.injectConfig())
		if err != nil {
			log.Errorf("failed to render: %v", err)
			// TODO: we may need better error management
			return nil
		}
		gatewayStatus := renderGatewayApply(gw, gw.Annotations["istio.io/service-account"])
		return &Waypoint{
			Deployment:     proxyDeploy,
			ServiceAccount: proxySa,
			GatewayStatus:  gatewayStatus,
		}
	})
	cv2.NewSyncer(Waypoints, ServiceAccounts,
		func(g Waypoint, l *corev1.ServiceAccount) bool {
			liveac, _ := corev1ac.ExtractServiceAccount(l, waypointFM)
			return controllers.Equal(g.ServiceAccount, liveac)
		}, func(waypoint Waypoint) {
			_, err := rc.client.Kube().
				CoreV1().
				ServiceAccounts(*waypoint.ServiceAccount.Namespace).
				Apply(context.Background(), waypoint.ServiceAccount, metav1.ApplyOptions{
					Force: true, FieldManager: waypointFM,
				})
			if err != nil {
				log.Errorf("waypoint service account patch error: %v", err)
			}
		})
	cv2.NewSyncer(Waypoints, Deployments,
		func(g Waypoint, l *appsv1.Deployment) bool {
			liveac, _ := appsv1ac.ExtractDeployment(l, waypointFM)
			log.Errorf("howardjohn: compare %v", cmp.Diff(g.Deployment, liveac))
			return controllers.Equal(g.Deployment, liveac)
		}, func(waypoint Waypoint) {
			log.Infof("Applying deployment %v", *waypoint.Deployment.Name)
			_, err := rc.client.Kube().
				AppsV1().
				Deployments(*waypoint.Deployment.Namespace).
				Apply(context.Background(), waypoint.Deployment, metav1.ApplyOptions{
					Force: true, FieldManager: waypointFM,
				})
			if err != nil {
				log.Errorf("waypoint deployment patch error: %v", err)
			}
		})
	Waypoints.Register(func(o cv2.Event[Waypoint]) {
		if o.New == nil {
			// Kubernetes will prune things by GC, no need to explicitly remove
			return
		}
		obj := *o.New
		if err := rc.applyObject(obj.GatewayStatus, "status"); err != nil {
			log.Errorf("update gateway status: %v", err)
		}
	})

	return rc
}

type Waypoint struct {
	Deployment     *appsv1ac.DeploymentApplyConfiguration
	ServiceAccount *corev1ac.ServiceAccountApplyConfiguration
	GatewayStatus  *gateway.Gateway
}

func (w Waypoint) ResourceName() string {
	return w.GatewayStatus.Namespace + "/" + w.GatewayStatus.Name
}

func (rc *WaypointProxyController) Run(stop <-chan struct{}) {
	kubelib.WaitForCacheSync(stop, rc.informerSynced)
	waypointLog.Infof("controller start to run")

	<-stop
}

func (rc *WaypointProxyController) informerSynced() bool {
	// TODO
	return true
}

func renderGatewayApply(
	gw *gateway.Gateway,
	gatewaySA string,
) *gateway.Gateway {
	msg := fmt.Sprintf("Deployed waypoint proxy to %q namespace", gw.Namespace)
	if gatewaySA != "" {
		msg += fmt.Sprintf(" for %q service account", gatewaySA)
	}
	if gw == nil {
		return nil
	}
	conditions := map[string]*istiogw.Condition{
		string(v1alpha2.GatewayConditionReady): {
			Reason:  string(v1alpha2.GatewayReasonReady),
			Message: msg,
		},
		string(v1alpha2.GatewayConditionAccepted): {
			Reason:  string(v1alpha2.GatewayReasonAccepted),
			Message: msg,
		},
	}
	return &gateway.Gateway{
		TypeMeta: metav1.TypeMeta{
			Kind:       gvk.KubernetesGateway.Kind,
			APIVersion: gvk.KubernetesGateway.Group + "/" + gvk.KubernetesGateway.Version,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      gw.Name,
			Namespace: gw.Namespace,
		},
		Status: gateway.GatewayStatus{
			Conditions: istiogw.SetConditions(gw.Generation, gw.Status.Conditions, conditions),
		},
	}
}

// applyObject renders an object with the given input and (server-side) applies the results to the cluster.
func (rc *WaypointProxyController) applyObject(
	obj controllers.Object,
	subresources ...string,
) error {
	// TODO: use library options when available https://github.com/kubernetes-sigs/gateway-api/issues/1639
	j, err := config.ToJSON(obj)
	if err != nil {
		return err
	}

	gvr, err := controllers.ObjectToGVR(obj)
	if err != nil {
		return err
	}
	waypointLog.Debugf("applying %v", string(j))

	return rc.patcher(gvr, obj.GetName(), obj.GetNamespace(), j, subresources...)
}

func renderServiceAccountApply(input MergedInput) *corev1ac.ServiceAccountApplyConfiguration {
	return corev1ac.ServiceAccount(input.ServiceAccount, input.Namespace).
		WithLabels(map[string]string{istiogw.GatewayNameLabel: input.GatewayName}).
		WithOwnerReferences(metav1ac.OwnerReference().
			WithName(input.GatewayName).
			WithUID(types.UID(input.UID)).
			WithKind(gvk.KubernetesGateway.Kind).
			WithAPIVersion(gvk.KubernetesGateway.GroupVersion()))
}

func renderDeploymentApply(
	input MergedInput,
	cfg inject.WebhookConfig,
) (*appsv1ac.DeploymentApplyConfiguration, error) {
	// TODO watch for template changes, update the Deployment if it does
	podTemplate := cfg.Templates["waypoint"]
	if podTemplate == nil {
		return nil, fmt.Errorf("no waypoint template defined")
	}
	input.Image = inject.ProxyImage(
		cfg.Values.Struct(),
		cfg.MeshConfig.GetDefaultConfig().GetImage(),
		nil,
	)
	input.ImagePullPolicy = cfg.Values.Struct().Global.GetImagePullPolicy()
	waypointBytes, err := tmpl.Execute(podTemplate, input)
	if err != nil {
		return nil, err
	}

	proxyPod, err := unmarshalDeployApply([]byte(waypointBytes))
	if err != nil {
		return nil, fmt.Errorf("render: %v\n%v", err, waypointBytes)
	}
	return proxyPod, nil
}

func unmarshalDeployApply(dyaml []byte) (*appsv1ac.DeploymentApplyConfiguration, error) {
	deploy := &appsv1ac.DeploymentApplyConfiguration{}
	if err := yaml.Unmarshal(dyaml, deploy); err != nil {
		return nil, err
	}

	return deploy, nil
}

type MergedInput struct {
	GatewayName string

	Namespace         string
	UID               string
	ServiceAccount    string
	Cluster           string
	Image             string
	ImagePullPolicy   string
	ProxyConfig       *meshapi.ProxyConfig
	ForServiceAccount string
}

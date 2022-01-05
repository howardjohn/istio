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

package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/config/kube/crdclient"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pilot/pkg/status"
	"istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/retry"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	metadatafake "k8s.io/client-go/metadata/fake"
	"sigs.k8s.io/gateway-api/apis/v1alpha2"
	"sigs.k8s.io/yaml"

	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/kube"
	istiolog "istio.io/pkg/log"
)



func TestEndToEnd(t *testing.T) {
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	//fake, _ := kube.NewDefaultClient()
	fake := kube.NewFakeClient()


	for _, s := range collections.All.All() {
		createCRD(t, fake, s.Resource())
	}
	configClient, err := crdclient.New(fake, "", "cluster.local")
	if err != nil {
		t.Fatal(err)
	}
	fake.RunAndWait(stop)
	go configClient.Run(stop)
	retry.UntilOrFail(t, configClient.HasSynced)

	dc := NewDeploymentController(fake)
	dc.patcher = func(gvr schema.GroupVersionResource, name string, namespace string, data []byte, subresources ...string) error {
		cfgs, _, err := crd.ParseInputs(string(data))
		if err != nil || len(cfgs) == 0 {
			return nil
		}
		// TODO: get current config, apply on top of it so we do not overwrite
		_, e := configClient.UpdateStatus(cfgs[0])
		return e
	}
	fake.RunAndWait(stop)
	go dc.Run(stop)
	retry.UntilOrFail(t, dc.queue.HasSynced)

	ds := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{KubeClientModifier: func(c *kube.Client) {
		*c = fake
	}})


	c := NewController(fake, configClient, controller.Options{DomainSuffix: "cluster.local"})
	c.SetStatusWrite(true, status.NewManager(configClient))
	go c.Run(stop)
	fake.RunAndWait(stop)
	retry.UntilOrFail(t, c.HasSynced)

	_, err = fake.GatewayAPI().GatewayV1alpha2().Gateways("default").Create(context.Background(),
		&v1alpha2.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default",
				Namespace: "default",
			},
			Spec: v1alpha2.GatewaySpec{
				GatewayClassName: "istio",
				Listeners: []v1alpha2.Listener{{
					Name:     "default",
					Port:     80,
					Protocol: "TCP",
				}},
			},
		},
		metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Second)
	gw, _ := fake.GatewayAPI().GatewayV1alpha2().Gateways("default").Get(context.Background(), "default", metav1.GetOptions{})
	debug, _ := json.MarshalIndent(gw, "howardjohn", "  ")
	log.Errorf("howardjohn: %s", debug)
	c.Recompute(model.NewGatewayContext(ds.PushContext()))
	time.Sleep(time.Second)
	gw, _ = fake.GatewayAPI().GatewayV1alpha2().Gateways("default").Get(context.Background(), "default", metav1.GetOptions{})
	debug, _ = json.MarshalIndent(gw, "howardjohn", "  ")
	log.Errorf("howardjohn: %s", debug)
}

func TestConfigureIstioGateway(t *testing.T) {
	tests := []struct {
		name string
		gw   v1alpha2.Gateway
	}{
		{
			"simple",
			v1alpha2.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default",
					Namespace: "default",
				},
				Spec: v1alpha2.GatewaySpec{},
			},
		},
		{
			"manual-ip",
			v1alpha2.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default",
					Namespace: "default",
				},
				Spec: v1alpha2.GatewaySpec{
					Addresses: []v1alpha2.GatewayAddress{{
						Type:  func() *v1alpha2.AddressType { x := v1alpha2.IPAddressType; return &x }(),
						Value: "1.2.3.4",
					}},
				},
			},
		},
		{
			"cluster-ip",
			v1alpha2.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "default",
					Namespace:   "default",
					Annotations: map[string]string{"networking.istio.io/service-type": string(corev1.ServiceTypeClusterIP)},
				},
				Spec: v1alpha2.GatewaySpec{
					Listeners: []v1alpha2.Listener{{
						Name: "http",
						Port: v1alpha2.PortNumber(80),
					}},
				},
			},
		},
		{
			"multinetwork",
			v1alpha2.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default",
					Namespace: "default",
					Labels:    map[string]string{"topology.istio.io/network": "network-1"},
				},
				Spec: v1alpha2.GatewaySpec{
					Listeners: []v1alpha2.Listener{{
						Name: "http",
						Port: v1alpha2.PortNumber(80),
					}},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			d := &DeploymentController{
				client:    kube.NewFakeClient(),
				templates: processTemplates(),
				patcher: func(gvr schema.GroupVersionResource, name string, namespace string, data []byte, subresources ...string) error {
					b, err := yaml.JSONToYAML(data)
					if err != nil {
						return err
					}
					buf.Write(b)
					buf.Write([]byte("---\n"))
					return nil
				},
			}
			err := d.configureIstioGateway(istiolog.FindScope(istiolog.DefaultScopeName), tt.gw)
			if err != nil {
				t.Fatal(err)
			}

			resp := timestampRegex.ReplaceAll(buf.Bytes(), []byte("lastTransitionTime: fake"))
			util.CompareContent(t, resp, filepath.Join("testdata", "deployment", tt.name+".yaml"))
		})
	}
}


func createCRD(t test.Failer, client kube.Client, r resource.Schema) {
	t.Helper()
	crd := &v1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s.%s", r.Plural(), r.Group()),
		},
	}
	if _, err := client.Ext().ApiextensionsV1().CustomResourceDefinitions().Create(context.TODO(), crd, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}

	// Metadata client fake is not kept in sync, so if using a fake clinet update that as well
	fmc, ok := client.Metadata().(*metadatafake.FakeMetadataClient)
	if !ok {
		return
	}
	fmg := fmc.Resource(collections.K8SApiextensionsK8SIoV1Customresourcedefinitions.Resource().GroupVersionResource())
	fmd, ok := fmg.(metadatafake.MetadataClient)
	if !ok {
		return
	}
	if _, err := fmd.CreateFake(&metav1.PartialObjectMetadata{
		TypeMeta:   crd.TypeMeta,
		ObjectMeta: crd.ObjectMeta,
	}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
}

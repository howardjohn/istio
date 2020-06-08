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

// Package controller provides an implementation of the config store and cache
// using Kubernetes Custom Resources and the informer framework from Kubernetes
package controller

import (
	"context"

	"istio.io/client-go/pkg/informers/externalversions"
	"k8s.io/client-go/tools/cache"

	"istio.io/api/label"
	versionedclient "istio.io/client-go/pkg/clientset/versioned"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeSchema "k8s.io/apimachinery/pkg/runtime/schema"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"  // import GKE cluster authentication plugin
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc" // import OIDC cluster authentication plugin, e.g. for Tectonic
	"k8s.io/client-go/rest"

	"istio.io/pkg/ledger"
	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/model"
	controller2 "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/resource"
)

// Client is a basic REST client for CRDs implementing config store
type Client struct {
	crdClient *apiextensionsclient.Clientset

	schemas collection.Schemas

	// domainSuffix for the config metadata
	domainSuffix string

	// Ledger for tracking config distribution
	configLedger ledger.Ledger

	// revision for this control plane instance. We will only read configs that match this revision.
	revision       string
	informers      externalversions.SharedInformerFactory
	gvrMap         map[resource.GroupVersionKind]externalversions.GenericInformer
}

func (cl *Client) RegisterEventHandler(kind resource.GroupVersionKind, handler func(model.Config, model.Config, model.Event)) {
	panic("implement me")
}

func (cl *Client) Run(stop <-chan struct{}) {
	panic("implement me")
}

func (cl *Client) HasSynced() bool {
	panic("implement me")
}

var _ model.ConfigStoreCache = &Client{}

var scope = log.RegisterScope("kube", "Kubernetes client messages", 0)

type translation func(r runtime.Object) *model.Config

// NewForConfig creates a client to the Kubernetes API using a rest config.
func NewForConfig(cfg *rest.Config, schemas collection.Schemas, configLedger ledger.Ledger, revision string, options controller2.Options) (model.ConfigStoreCache, error) {
	ic, err := versionedclient.NewForConfig(cfg)
	informers := externalversions.NewSharedInformerFactory(ic, options.ResyncPeriod)

	crdClient, err := apiextensionsclient.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	informerMap := map[resource.GroupVersionKind]externalversions.GenericInformer{}
	for _, r := range schemas.All() {
		gvr := kubeSchema.GroupVersionResource{
			Group:    r.Resource().Group(),
			Version:  r.Resource().Version(),
			Resource: r.Resource().Plural(),
		}
		i, err := informers.ForResource(gvr)
		if err != nil {
			return nil, err
		}
		var stop chan struct{}
		go i.Informer().Run(stop)
		cache.WaitForCacheSync(stop, i.Informer().HasSynced)
		informerMap[r.Resource().GroupVersionKind()] = i
	}


	out := &Client{
		crdClient:      crdClient,
		domainSuffix:   options.DomainSuffix,
		configLedger:   configLedger,
		schemas:        schemas,
		revision:       revision,
		informers:      informers,
		gvrMap:         informerMap,
	}

	return out, nil
}

func (cl *Client) setup() {

}

// Schemas for the store
func (cl *Client) Schemas() collection.Schemas {
	return cl.schemas
}

// Get implements store interface
func (cl *Client) Get(typ resource.GroupVersionKind, name, namespace string) *model.Config {
	informer, f := cl.gvrMap[typ]
	if !f {
		scope.Warnf("unknown type: %s", typ)
		return nil
	}

	obj, err := informer.Lister().ByNamespace(namespace).Get(name)
	if err != nil {
		scope.Warna(err)
		return nil
	}

	return TranslateObject(obj, typ, cl.domainSuffix)
}


// Create implements store interface
func (cl *Client) Create(config model.Config) (string, error) {
	return "", nil
}

// Update implements store interface
func (cl *Client) Update(config model.Config) (string, error) {
	return "", nil
}

// Delete implements store interface
func (cl *Client) Delete(typ resource.GroupVersionKind, name, namespace string) error {
	return nil
}

func (cl *Client) Version() string {
	return cl.configLedger.RootHash()
}

func (cl *Client) GetResourceAtVersion(version string, key string) (resourceVersion string, err error) {
	return cl.configLedger.GetPreviousValue(version, key)
}

func (cl *Client) GetLedger() ledger.Ledger {
	return cl.configLedger
}

func (cl *Client) SetLedger(l ledger.Ledger) error {
	cl.configLedger = l
	return nil
}

// List implements store interface
func (cl *Client) List(kind resource.GroupVersionKind, namespace string) ([]model.Config, error) {
	return nil, nil
}

func (cl *Client) objectInRevision(o *model.Config) bool {
	configEnv, f := o.Labels[label.IstioRev]
	if !f {
		// This is a global object, and always included
		return true
	}
	// Otherwise, only return if the
	return configEnv == cl.revision
}

// KnownCRDs returns all CRDs present in the cluster
func (cl *Client) KnownCRDs() (map[string]struct{}, error) {
	res, err := cl.crdClient.ApiextensionsV1beta1().CustomResourceDefinitions().List(context.TODO(), meta_v1.ListOptions{})
	if err != nil {
		return nil, err
	}
	mp := map[string]struct{}{}
	for _, r := range res.Items {
		mp[r.Name] = struct{}{}
	}
	return mp, nil
}

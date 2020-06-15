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

// Package crdclient provides an implementation of the config store and cache
// using Kubernetes Custom Resources and the informer framework from Kubernetes
//
// This code relies heavily on code generation for performance reasons; to implement the
// Istio store interface, we need to take dynamic inputs. Using the dynamic informers results in poor
// performance, as the cache will store unstructured objects which need to be marshalled on each Get/List call.
// Using istio/client-go directly will cache objects marshalled, allowing us to have cheap Get/List calls,
// at the expense of some code gen.
package crdclient

import (
	"context"
	"errors"
	"fmt"
	"time"

	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"

	"istio.io/api/label"
	versionedclient "istio.io/client-go/pkg/clientset/versioned"
	"istio.io/client-go/pkg/informers/externalversions"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"  // import GKE cluster authentication plugin
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc" // import OIDC cluster authentication plugin, e.g. for Tectonic
	"k8s.io/client-go/rest"

	"istio.io/pkg/ledger"
	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	controller2 "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/queue"
)

var scope = log.RegisterScope("kube", "Kubernetes client messages", 0)

// Client is a client for Istio CRDs, implementing config store cache
// This is used for CRUD operators on Istio configuration, as well as handling of events on config changes
type Client struct {
	// Client to read CRDs. This is used to determine the set of support CRDs
	crdClient *apiextensionsclient.Clientset

	// schemas defines the set of schemas used by this client.
	// Note: this must be a subset of the schemas defined in the codegen
	schemas collection.Schemas

	// domainSuffix for the config metadata
	domainSuffix string

	// Ledger for tracking config distribution
	configLedger ledger.Ledger

	// revision for this control plane instance. We will only read configs that match this revision.
	revision string

	// kinds keeps track of all cache handlers for known types
	kinds map[resource.GroupVersionKind]*cacheHandler
	queue queue.Instance

	// The istio/client-go client we will use to access objects
	ic versionedclient.Interface
}

var _ model.ConfigStoreCache = &Client{}

// Validate we are ready to handle events. Until the informers are synced, we will block the queue
func (cl *Client) checkReadyForEvents(curr interface{}) error {
	if !cl.HasSynced() {
		return errors.New("waiting till full synchronization")
	}
	_, err := cache.DeletionHandlingMetaNamespaceKeyFunc(curr)
	if err != nil {
		log.Infof("Error retrieving key: %v", err)
	}
	return nil
}

func (cl *Client) RegisterEventHandler(kind resource.GroupVersionKind, handler func(model.Config, model.Config, model.Event)) {
	h, exists := cl.kinds[kind]
	if !exists {
		return
	}

	h.handlers = append(h.handlers, handler)
}

// Start the queue and all informers. Callers should  wait for HasSynced() before depending on results.
func (cl *Client) Run(stop <-chan struct{}) {
	log.Infoa("Starting Pilot K8S CRD controller")

	go func() {
		cache.WaitForCacheSync(stop, cl.HasSynced)
		cl.queue.Run(stop)
	}()

	for _, ctl := range cl.kinds {
		go ctl.informer.Informer().Run(stop)
	}

	<-stop
	log.Info("controller terminated")
}

func (cl *Client) HasSynced() bool {
	for kind, ctl := range cl.kinds {
		if !ctl.informer.Informer().HasSynced() {
			log.Infof("controller %q is syncing...", kind)
			return false
		}
	}
	return true
}

// NewForConfig creates a client to the Kubernetes API using a rest config.
func NewForConfig(cfg *rest.Config, configLedger ledger.Ledger,
	revision string, options controller2.Options) (model.ConfigStoreCache, error) {
	ic, err := versionedclient.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	return New(ic, configLedger, revision, options)
}

func New(ic versionedclient.Interface, configLedger ledger.Ledger,
	revision string, options controller2.Options) (model.ConfigStoreCache, error) {

	informers := externalversions.NewSharedInformerFactory(ic, options.ResyncPeriod)

	out := &Client{
		// TODO use CRD client
		crdClient:    nil,
		domainSuffix: options.DomainSuffix,
		configLedger: configLedger,
		schemas:      collections.Pilot,
		revision:     revision,
		queue:        queue.NewQueue(1 * time.Second),
		kinds:        map[resource.GroupVersionKind]*cacheHandler{},
		ic:           ic,
	}
	// TODO known CRDs only
	for _, r := range out.schemas.All() {
		ch, err := createCacheHandler(out, r, informers)
		if err != nil {
			return nil, err
		}
		out.kinds[r.Resource().GroupVersionKind()] = ch
	}

	return out, nil
}

// Schemas for the store
func (cl *Client) Schemas() collection.Schemas {
	return cl.schemas
}

// Get implements store interface
func (cl *Client) Get(typ resource.GroupVersionKind, name, namespace string) *model.Config {
	h, f := cl.kinds[typ]
	if !f {
		scope.Warnf("unknown type: %s", typ)
		return nil
	}

	obj, err := h.informer.Lister().ByNamespace(namespace).Get(name)
	if err != nil {
		// TODO we should be returning errors not logging
		scope.Warna(err)
		return nil
	}

	cfg := TranslateObject(obj, typ, cl.domainSuffix)
	if !cl.objectInRevision(cfg) {
		return nil
	}
	if features.EnableCRDValidation {
		schema, _ := cl.Schemas().FindByGroupVersionKind(typ)
		if err = schema.Resource().ValidateProto(cfg.Name, cfg.Namespace, cfg.Spec); err != nil {
			handleValidationFailure(cfg, err)
			return nil
		}

	}
	return cfg
}

// Create implements store interface
func (cl *Client) Create(config model.Config) (string, error) {
	if config.Spec == nil {
		return "", fmt.Errorf("nil spec for %v/%v", config.Name, config.Namespace)
	}

	meta, err := create(cl.ic, config, getObjectMetadata(config))
	if err != nil {
		return "", err
	}
	return meta.GetResourceVersion(), nil
}

// Update implements store interface
func (cl *Client) Update(config model.Config) (string, error) {
	if config.Spec == nil {
		return "", fmt.Errorf("nil spec for %v/%v", config.Name, config.Namespace)
	}

	meta, err := update(cl.ic, config, getObjectMetadata(config))
	if err != nil {
		return "", err
	}
	return meta.GetResourceVersion(), nil
}

// Delete implements store interface
func (cl *Client) Delete(typ resource.GroupVersionKind, name, namespace string) error {
	return delete(cl.ic, typ, name, namespace)
}

// List implements store interface
func (cl *Client) List(kind resource.GroupVersionKind, namespace string) ([]model.Config, error) {
	h, f := cl.kinds[kind]
	if !f {
		return nil, fmt.Errorf("unrecognized type: %s", kind)
	}

	list, err := h.informer.Lister().ByNamespace(namespace).List(klabels.Everything())
	if err != nil {
		return nil, err
	}
	out := make([]model.Config, 0, len(list))
	for _, item := range list {
		cfg := TranslateObject(item, kind, cl.domainSuffix)
		if features.EnableCRDValidation {
			schema, _ := cl.Schemas().FindByGroupVersionKind(kind)
			if err = schema.Resource().ValidateProto(cfg.Name, cfg.Namespace, cfg.Spec); err != nil {
				handleValidationFailure(cfg, err)
				// DO NOT RETURN ERROR: if a single object is bad, it'll be ignored (with a log message), but
				// the rest should still be processed.
				continue
			}

		}

		if cl.objectInRevision(cfg) {
			out = append(out, *cfg)
		}
	}

	return out, err
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
	res, err := cl.crdClient.ApiextensionsV1beta1().CustomResourceDefinitions().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	mp := map[string]struct{}{}
	for _, r := range res.Items {
		mp[r.Name] = struct{}{}
	}
	return mp, nil
}

func TranslateObject(r runtime.Object, gvk resource.GroupVersionKind, domainSuffix string) *model.Config {
	translateFunc, f := translationMap[gvk]
	if !f {
		log.Errorf("unknown type %v", gvk)
		return nil
	}
	c := translateFunc(r)
	c.Domain = domainSuffix
	return c
}

func getObjectMetadata(config model.Config) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:        config.Name,
		Namespace:   config.Namespace,
		Labels:      config.Labels,
		Annotations: config.Annotations,
	}
}

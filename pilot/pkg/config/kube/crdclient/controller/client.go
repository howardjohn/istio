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
	"errors"
	"fmt"
	"reflect"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"

	"istio.io/client-go/pkg/informers/externalversions"
	"istio.io/pkg/monitoring"

	"istio.io/api/label"
	versionedclient "istio.io/client-go/pkg/clientset/versioned"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeSchema "k8s.io/apimachinery/pkg/runtime/schema"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"  // import GKE cluster authentication plugin
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc" // import OIDC cluster authentication plugin, e.g. for Tectonic
	"k8s.io/client-go/rest"

	"istio.io/pkg/ledger"
	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	controller2 "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/queue"
	"istio.io/istio/pkg/util/gogoprotomarshal"
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
	revision string
	kinds    map[resource.GroupVersionKind]*cacheHandler
	queue    queue.Instance
}

func incrementEvent(kind, event string) {
	k8sEvents.With(typeTag.Value(kind), eventTag.Value(event)).Increment()
}

func (cl *Client) tryLedgerPut(obj interface{}, kind string) {
	iobj := obj.(metav1.Object)
	key := model.Key(kind, iobj.GetName(), iobj.GetNamespace())
	_, err := cl.configLedger.Put(key, iobj.GetResourceVersion())
	if err != nil {
		scope.Errorf("Failed to update %s in ledger, status will be out of date.", key)
	}
}

func (cl *Client) tryLedgerDelete(obj interface{}, kind string) {
	iobj := obj.(metav1.Object)
	key := model.Key(kind, iobj.GetName(), iobj.GetNamespace())
	err := cl.configLedger.Delete(key)
	if err != nil {
		scope.Errorf("Failed to delete %s in ledger, status will be out of date.", key)
	}
}

type cacheHandler struct {
	client        *Client
	informer      externalversions.GenericInformer
	handlers      []func(model.Config, model.Config, model.Event)
	schema        collection.Schema
	dynamicClient dynamic.NamespaceableResourceInterface
}

func (cl *Client) createCacheHandler(schema collection.Schema,
	informers externalversions.SharedInformerFactory, dynamicClient dynamic.Interface) (*cacheHandler, error) {
	gvr := kubeSchema.GroupVersionResource{
		Group:    schema.Resource().Group(),
		Version:  schema.Resource().Version(),
		Resource: schema.Resource().Plural(),
	}
	i, err := informers.ForResource(gvr)
	if err != nil {
		return nil, err
	}
	// TODO
	//var stop chan struct{}
	//go i.Informer().Run(stop)
	//cache.WaitForCacheSync(stop, i.Informer().HasSynced)
	h := &cacheHandler{
		client:        cl,
		schema:        schema,
		informer:      i,
		dynamicClient: dynamicClient.Resource(gvr),
	}
	kind := schema.Resource().Kind()
	i.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		// TODO: filtering functions to skip over un-referenced resources (perf)
		AddFunc: func(obj interface{}) {
			incrementEvent(kind, "add")
			cl.tryLedgerPut(obj, kind)
			cl.queue.Push(func() error {
				return h.onEvent(nil, obj, model.EventAdd)
			})
		},
		UpdateFunc: func(old, cur interface{}) {
			cl.tryLedgerPut(cur, kind)
			if !reflect.DeepEqual(old, cur) {
				incrementEvent(kind, "update")
				cl.queue.Push(func() error {
					return h.onEvent(old, cur, model.EventUpdate)
				})
			} else {
				incrementEvent(kind, "updatesame")
			}
		},
		DeleteFunc: func(obj interface{}) {
			incrementEvent(kind, "delete")
			cl.tryLedgerDelete(obj, kind)
			cl.queue.Push(func() error {
				return h.onEvent(nil, obj, model.EventDelete)
			})
		},
	})
	return h, nil
}

func (h *cacheHandler) onEvent(old interface{}, curr interface{}, event model.Event) error {
	if err := h.client.checkReadyForEvents(curr); err != nil {
		return err
	}

	var currItem, oldItem runtime.Object

	ok := false

	if currItem, ok = curr.(runtime.Object); !ok {
		log.Warnf("New Object can not be converted to runtime Object %v, is type %T", curr, curr)
		return nil
	}
	currConfig := TranslateObject(currItem, h.schema.Resource().GroupVersionKind(), h.client.domainSuffix)

	var oldConfig model.Config
	if old != nil {
		if oldItem, ok = old.(runtime.Object); !ok {
			log.Warnf("Old Object can not be converted to runtime Object %v, is type %T", old, old)
			return nil
		}
		oldConfig = *TranslateObject(oldItem, h.schema.Resource().GroupVersionKind(), h.client.domainSuffix)
	}

	for _, f := range h.handlers {
		f(oldConfig, *currConfig, event)
	}
	return nil
}

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

var _ model.ConfigStoreCache = &Client{}

var scope = log.RegisterScope("kube", "Kubernetes client messages", 0)

// NewForConfig creates a client to the Kubernetes API using a rest config.
func NewForConfig(cfg *rest.Config, schemas collection.Schemas, configLedger ledger.Ledger,
	revision string, options controller2.Options) (model.ConfigStoreCache, error) {
	ic, err := versionedclient.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	//crdClient, err := apiextensionsclient.NewForConfig(cfg)
	//if err != nil {
	//	return nil, err
	//}

	dynClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	return New(ic, dynClient, schemas, configLedger, revision, options)
}

func New(ic versionedclient.Interface, dynClient dynamic.Interface, schemas collection.Schemas, configLedger ledger.Ledger,
	revision string, options controller2.Options) (model.ConfigStoreCache, error) {

	informers := externalversions.NewSharedInformerFactory(ic, options.ResyncPeriod)

	out := &Client{
		// TODO use CRD client
		crdClient:    nil,
		domainSuffix: options.DomainSuffix,
		configLedger: configLedger,
		schemas:      schemas,
		revision:     revision,
		queue:        queue.NewQueue(1 * time.Second),
		kinds:        map[resource.GroupVersionKind]*cacheHandler{},
	}
	// TODO known CRDs only
	for _, r := range schemas.All() {
		ch, err := out.createCacheHandler(r, informers, dynClient)
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
	return nil
}

var (
	typeTag  = monitoring.MustCreateLabel("type")
	eventTag = monitoring.MustCreateLabel("event")
	nameTag  = monitoring.MustCreateLabel("name")

	k8sEvents = monitoring.NewSum(
		"pilot_k8s_cfg_events",
		"Events from k8s config.",
		monitoring.WithLabels(typeTag, eventTag),
	)

	k8sErrors = monitoring.NewGauge(
		"pilot_k8s_object_errors",
		"Errors converting k8s CRDs",
		monitoring.WithLabels(nameTag),
	)

	k8sTotalErrors = monitoring.NewSum(
		"pilot_total_k8s_object_errors",
		"Total Errors converting k8s CRDs",
	)
)

func handleValidationFailure(obj *model.Config, err error) {

	key := obj.Namespace + "/" + obj.Name
	log.Debugf("CRD validation failed: %s (%v): %v", key, obj.GroupVersionKind(), err)
	k8sErrors.With(nameTag.Value(key)).Record(1)

	k8sTotalErrors.Increment()
}

// Create implements store interface
func (cl *Client) Create(config model.Config) (string, error) {
	h, f := cl.kinds[config.GroupVersionKind()]
	if !f {
		return "", fmt.Errorf("unrecognized type: %s", config.GroupVersionKind())
	}

	u, err := toUnstructured(&config)
	if err != nil {
		return "", fmt.Errorf("unstructured conversion: %v", err)
	}

	o, err := h.dynamicClient.Namespace(config.Namespace).Create(context.TODO(), u, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("create %v/%v: %v", config.Name, config.Namespace, err)
	}
	return o.GetResourceVersion(), nil
}

// IstioKind is the generic Kubernetes API object wrapper
type UnstructuredKind struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Spec              map[string]interface{} `json:"spec"`
}

func toUnstructured(cfg *model.Config) (*unstructured.Unstructured, error) {
	spec, err := gogoprotomarshal.ToJSONMap(cfg.Spec)
	if err != nil {
		return nil, err
	}
	namespace := cfg.Namespace
	if namespace == "" {
		namespace = metav1.NamespaceDefault
	}
	uk := &UnstructuredKind{
		TypeMeta: metav1.TypeMeta{
			Kind:       cfg.Type,
			APIVersion: cfg.Group + "/" + cfg.Version,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:              cfg.Name,
			Namespace:         namespace,
			ResourceVersion:   cfg.ResourceVersion,
			Labels:            cfg.Labels,
			Annotations:       cfg.Annotations,
			CreationTimestamp: metav1.NewTime(cfg.CreationTimestamp),
		},
		Spec: spec,
	}
	o, err := runtime.DefaultUnstructuredConverter.ToUnstructured(uk)
	return &unstructured.Unstructured{Object: o}, err
}

// Update implements store interface
func (cl *Client) Update(config model.Config) (string, error) {
	// TODO
	return "", nil
}

// Delete implements store interface
func (cl *Client) Delete(typ resource.GroupVersionKind, name, namespace string) error {
	// TODO
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

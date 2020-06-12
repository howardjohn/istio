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
package crdclient

import (
	"context"
	"errors"
	"fmt"
	"time"

	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"

	mixerclientv1 "istio.io/api/mixer/v1/config/client"
	networkingv1alpha3 "istio.io/api/networking/v1alpha3"
	policyv1beta1 "istio.io/api/policy/v1beta1"
	securityv1beta1 "istio.io/api/security/v1beta1"

	"istio.io/client-go/pkg/informers/externalversions"
	"istio.io/pkg/monitoring"

	"istio.io/api/label"
	clientconfigv1alpha3 "istio.io/client-go/pkg/apis/config/v1alpha2"
	clientnetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	clientsecurityv1beta1 "istio.io/client-go/pkg/apis/security/v1beta1"
	versionedclient "istio.io/client-go/pkg/clientset/versioned"

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
	ic       versionedclient.Interface
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

	return New(ic, schemas, configLedger, revision, options)
}

func New(ic versionedclient.Interface, schemas collection.Schemas, configLedger ledger.Ledger,
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
		ic:           ic,
	}
	// TODO known CRDs only
	for _, r := range schemas.All() {
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

// Avoid using dynamic client for optimization. This could/should probably be codegen, but we don't have all the metadata and its not
// so hard to maintain.
func create(ic versionedclient.Interface, config model.Config, objMeta metav1.ObjectMeta) (metav1.Object, error) {
	switch config.GroupVersionKind() {
	case collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind():
		return ic.NetworkingV1alpha3().DestinationRules(config.Namespace).Create(context.TODO(), &clientnetworkingv1alpha3.DestinationRule{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*networkingv1alpha3.DestinationRule)),
		}, metav1.CreateOptions{})
	case collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind():
		return ic.NetworkingV1alpha3().VirtualServices(config.Namespace).Create(context.TODO(), &clientnetworkingv1alpha3.VirtualService{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*networkingv1alpha3.VirtualService)),
		}, metav1.CreateOptions{})
	case collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind():
		return ic.NetworkingV1alpha3().Gateways(config.Namespace).Create(context.TODO(), &clientnetworkingv1alpha3.Gateway{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*networkingv1alpha3.Gateway)),
		}, metav1.CreateOptions{})
	case collections.IstioNetworkingV1Alpha3Workloadentries.Resource().GroupVersionKind():
		return ic.NetworkingV1alpha3().WorkloadEntries(config.Namespace).Create(context.TODO(), &clientnetworkingv1alpha3.WorkloadEntry{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*networkingv1alpha3.WorkloadEntry)),
		}, metav1.CreateOptions{})
	case collections.IstioNetworkingV1Alpha3Envoyfilters.Resource().GroupVersionKind():
		return ic.NetworkingV1alpha3().EnvoyFilters(config.Namespace).Create(context.TODO(), &clientnetworkingv1alpha3.EnvoyFilter{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*networkingv1alpha3.EnvoyFilter)),
		}, metav1.CreateOptions{})
	case collections.IstioNetworkingV1Alpha3Sidecars.Resource().GroupVersionKind():
		return ic.NetworkingV1alpha3().Sidecars(config.Namespace).Create(context.TODO(), &clientnetworkingv1alpha3.Sidecar{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*networkingv1alpha3.Sidecar)),
		}, metav1.CreateOptions{})
	case collections.IstioNetworkingV1Alpha3Serviceentries.Resource().GroupVersionKind():
		return ic.NetworkingV1alpha3().ServiceEntries(config.Namespace).Create(context.TODO(), &clientnetworkingv1alpha3.ServiceEntry{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*networkingv1alpha3.ServiceEntry)),
		}, metav1.CreateOptions{})
	case collections.IstioSecurityV1Beta1Authorizationpolicies.Resource().GroupVersionKind():
		return ic.SecurityV1beta1().AuthorizationPolicies(config.Namespace).Create(context.TODO(), &clientsecurityv1beta1.AuthorizationPolicy{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*securityv1beta1.AuthorizationPolicy)),
		}, metav1.CreateOptions{})
	case collections.IstioSecurityV1Beta1Requestauthentications.Resource().GroupVersionKind():
		return ic.SecurityV1beta1().RequestAuthentications(config.Namespace).Create(context.TODO(), &clientsecurityv1beta1.RequestAuthentication{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*securityv1beta1.RequestAuthentication)),
		}, metav1.CreateOptions{})
	case collections.IstioSecurityV1Beta1Peerauthentications.Resource().GroupVersionKind():
		return ic.SecurityV1beta1().PeerAuthentications(config.Namespace).Create(context.TODO(), &clientsecurityv1beta1.PeerAuthentication{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*securityv1beta1.PeerAuthentication)),
		}, metav1.CreateOptions{})
	case collections.IstioSecurityV1Beta1Peerauthentications.Resource().GroupVersionKind():
		return ic.ConfigV1alpha2().AttributeManifests(config.Namespace).Create(context.TODO(), &clientconfigv1alpha3.AttributeManifest{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*policyv1beta1.AttributeManifest)),
		}, metav1.CreateOptions{})
	case collections.IstioPolicyV1Beta1Attributemanifests.Resource().GroupVersionKind():
		return ic.ConfigV1alpha2().AttributeManifests(config.Namespace).Create(context.TODO(), &clientconfigv1alpha3.AttributeManifest{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*policyv1beta1.AttributeManifest)),
		}, metav1.CreateOptions{})
	case collections.IstioConfigV1Alpha2Httpapispecs.Resource().GroupVersionKind():
		return ic.ConfigV1alpha2().HTTPAPISpecs(config.Namespace).Create(context.TODO(), &clientconfigv1alpha3.HTTPAPISpec{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*mixerclientv1.HTTPAPISpec)),
		}, metav1.CreateOptions{})
	case collections.K8SConfigIstioIoV1Alpha2Httpapispecbindings.Resource().GroupVersionKind():
		return ic.ConfigV1alpha2().HTTPAPISpecBindings(config.Namespace).Create(context.TODO(), &clientconfigv1alpha3.HTTPAPISpecBinding{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*mixerclientv1.HTTPAPISpecBinding)),
		}, metav1.CreateOptions{})
	case collections.IstioPolicyV1Beta1Handlers.Resource().GroupVersionKind():
		return ic.ConfigV1alpha2().Handlers(config.Namespace).Create(context.TODO(), &clientconfigv1alpha3.Handler{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*policyv1beta1.Handler)),
		}, metav1.CreateOptions{})
	case collections.IstioPolicyV1Beta1Instances.Resource().GroupVersionKind():
		return ic.ConfigV1alpha2().Instances(config.Namespace).Create(context.TODO(), &clientconfigv1alpha3.Instance{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*policyv1beta1.Instance)),
		}, metav1.CreateOptions{})
	case collections.IstioMixerV1ConfigClientQuotaspecs.Resource().GroupVersionKind():
		return ic.ConfigV1alpha2().QuotaSpecs(config.Namespace).Create(context.TODO(), &clientconfigv1alpha3.QuotaSpec{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*mixerclientv1.QuotaSpec)),
		}, metav1.CreateOptions{})
	case collections.IstioMixerV1ConfigClientQuotaspecbindings.Resource().GroupVersionKind():
		return ic.ConfigV1alpha2().QuotaSpecBindings(config.Namespace).Create(context.TODO(), &clientconfigv1alpha3.QuotaSpecBinding{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*mixerclientv1.QuotaSpecBinding)),
		}, metav1.CreateOptions{})
	case collections.IstioPolicyV1Beta1Rules.Resource().GroupVersionKind():
		return ic.ConfigV1alpha2().Rules(config.Namespace).Create(context.TODO(), &clientconfigv1alpha3.Rule{
			ObjectMeta: objMeta,
			Spec:       *(config.Spec.(*policyv1beta1.Rule)),
		}, metav1.CreateOptions{})
	default:
		return nil, fmt.Errorf("unsupported type: %v", config.GroupVersionKind())
	}
}

// Create implements store interface
func (cl *Client) Create(config model.Config) (string, error) {
	if config.Spec == nil {
		return "", fmt.Errorf("nil spec for %v/%v", config.Name, config.Namespace)
	}
	objMeta := metav1.ObjectMeta{
		Name:        config.Name,
		Namespace:   config.Namespace,
		Labels:      config.Labels,
		Annotations: config.Annotations,
	}

	meta, err := create(cl.ic, config, objMeta)
	if err != nil {
		return "", err
	}
	return meta.GetResourceVersion(), nil
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

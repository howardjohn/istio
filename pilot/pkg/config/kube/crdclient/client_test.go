package crdclient

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/tools/cache"

	extfake "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/test/util/retry"
)

func makeClient(t *testing.T, schemas collection.Schemas) model.ConfigStoreCache {
	extFake := extfake.NewSimpleClientset()
	scheme := runtime.NewScheme()
	dynFake := dynamicfake.NewSimpleDynamicClient(scheme)
	for _, s := range schemas.All() {
		extFake.ApiextensionsV1beta1().CustomResourceDefinitions().Create(context.TODO(), &v1beta1.CustomResourceDefinition{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("%s.%s", s.Resource().Plural(), s.Resource().Group()),
			},
		}, metav1.CreateOptions{})
	}
	stop := make(chan struct{})
	config, err := New(dynFake, extFake, &model.DisabledLedger{}, "", controller.Options{})
	if err != nil {
		t.Fatal(err)
	}
	go config.Run(stop)
	cache.WaitForCacheSync(stop, config.HasSynced)
	t.Cleanup(func() {
		close(stop)
	})
	return config
}

// Ensure that the client can run without CRDs present
func TestClientNoCRDs(t *testing.T) {
	schema := collection.NewSchemasBuilder().MustAdd(collections.IstioNetworkingV1Alpha3Sidecars).Build()
	store := makeClient(t, schema)
	retry.UntilSuccessOrFail(t, func() error {
		if !store.HasSynced() {
			return fmt.Errorf("store has not synced yet")
		}
		return nil
	}, retry.Timeout(time.Second))
	r := collections.IstioNetworkingV1Alpha3Virtualservices.Resource()
	configMeta := model.ConfigMeta{
		Name:      "name",
		Namespace: "ns",
		Type:      r.Kind(),
		Group:     r.Group(),
		Version:   r.Version(),
	}
	pb, err := r.NewProtoInstance()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.Create(model.Config{
		ConfigMeta: configMeta,
		Spec:       pb,
	}); err != nil {
		t.Fatalf("Create => got %v", err)
	}
	retry.UntilSuccessOrFail(t, func() error {
		l, err := store.List(r.GroupVersionKind(), "ns")
		if err == nil {
			return fmt.Errorf("expected error, but got none")
		}
		if len(l) != 0 {
			return fmt.Errorf("expected no items returned for unknown CRD")
		}
		return nil
	}, retry.Timeout(time.Second*5), retry.Converge(5))
}

// CheckIstioConfigTypes validates that an empty store can do CRUD operators on all given types
func TestClient(t *testing.T) {
	store := makeClient(t, collections.PilotServiceApi)
	timeout := retry.Timeout(time.Millisecond * 200)
	for _, c := range collections.PilotServiceApi.All() {
		name := c.Resource().Kind()
		configName := "name"
		configNamespace := "namespace"
		t.Run(name, func(t *testing.T) {
			r := c.Resource()
			if r.IsClusterScoped() {
				configNamespace = ""
			}
			configMeta := model.ConfigMeta{
				Type:              r.Kind(),
				Name:              configName,
				Namespace:         configNamespace,
				Group:             r.Group(),
				Version:           r.Version(),
				ResourceVersion:   "123",
				CreationTimestamp: time.Now(),
			}

			pb, err := r.NewProtoInstance()
			if err != nil {
				t.Fatal(err)
			}

			if _, err := store.Create(model.Config{
				ConfigMeta: configMeta,
				Spec:       pb,
			}); err != nil {
				t.Fatalf("Create(%v) => got %v", name, err)
			}
			// Kubernetes is eventually consistent, so we allow a short time to pass before we get
			retry.UntilSuccessOrFail(t, func() error {
				cfg := store.Get(r.GroupVersionKind(), configName, configMeta.Namespace)
				if cfg == nil || !reflect.DeepEqual(cfg.ConfigMeta, configMeta) {
					return fmt.Errorf("get(%v) => got unexpected object %v", name, cfg)
				}
				return nil
			}, timeout)

			// Validate it shows up in List
			retry.UntilSuccessOrFail(t, func() error {
				cfgs, err := store.List(r.GroupVersionKind(), configNamespace)
				if err != nil {
					return err
				}
				if len(cfgs) != 1 {
					return fmt.Errorf("expected 1 config, got %v", len(cfgs))
				}
				for _, cfg := range cfgs {
					if !reflect.DeepEqual(cfg.ConfigMeta, configMeta) {
						return fmt.Errorf("get(%v) => got %v", name, cfg)
					}
				}
				return nil
			}, timeout)

			// Check we can remove items
			if err := store.Delete(r.GroupVersionKind(), configName, configNamespace); err != nil {
				t.Fatalf("failed to delete: %v", err)
			}
			retry.UntilSuccessOrFail(t, func() error {
				cfg := store.Get(r.GroupVersionKind(), configName, configNamespace)
				if cfg != nil {
					return fmt.Errorf("get(%v) => got %v, expected item to be deleted", name, cfg)
				}
				return nil
			}, timeout)
		})
	}
}

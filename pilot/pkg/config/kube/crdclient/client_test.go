package crdclient

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	istiofake "istio.io/client-go/pkg/clientset/versioned/fake"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/test/util/retry"
)

func makeClient(t *testing.T) model.ConfigStoreCache {
	fakeClient := istiofake.NewSimpleClientset()
	stop := make(chan struct{})
	schemas := collections.Pilot
	config, err := New(fakeClient, schemas, &model.DisabledLedger{}, "", controller.Options{})
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

// CheckIstioConfigTypes validates that an empty store can do CRUD operators on all given types
func TestClient(t *testing.T) {
	store := makeClient(t)
	configName := "name"
	configNamespace := "namespace"
	timeout := retry.Timeout(time.Millisecond * 200)
	for _, c := range collections.Pilot.All() {
		name := c.Resource().Kind()
		t.Run(name, func(t *testing.T) {
			r := c.Resource()
			configMeta := model.ConfigMeta{
				Type:    r.Kind(),
				Name:    configName,
				Group:   r.Group(),
				Version: r.Version(),
			}
			if !r.IsClusterScoped() {
				configMeta.Namespace = configNamespace
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
				cfg := store.Get(r.GroupVersionKind(), configName, configNamespace)
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

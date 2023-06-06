package cv2_test

import (
	"testing"
	"time"

	extensions "istio.io/api/extensions/v1alpha1"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/cv2"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

func TestNewConfigStore(t *testing.T) {
	store := memory.MakeSkipValidation(collections.Pilot)
	configController := memory.NewController(store)
	go configController.Run(test.NewStop(t))
	Configs := cv2.NewConfigStore(gvk.WasmPlugin, configController)

	tt := assert.NewTracker[string](t)
	Configs.Register(TrackerHandler[config.Config](tt))

	assert.Equal(t, Configs.List(""), nil)

	pA := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.WasmPlugin,
			Name:             "a",
			Namespace:        "ns",
		},
	}
	pA2 := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.WasmPlugin,
			Name:             "a",
			Namespace:        "ns",
		},
		Spec: &extensions.WasmPlugin{ImagePullSecret: "foo"},
	}
	pB := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.WasmPlugin,
			Name:             "b",
			Namespace:        "ns",
		},
	}
	configController.Create(pA)
	tt.WaitOrdered("add/ns/a")
	assert.Equal(t, sanitizes(Configs.List("")), []config.Config{pA})

	configController.Update(pA2)
	tt.WaitOrdered("update/ns/a")
	assert.Equal(t, sanitizes(Configs.List("")), []config.Config{pA2})

	configController.Create(pB)
	tt.WaitOrdered("add/ns/b")
	assert.Equal(t, sanitizes(Configs.List("")), []config.Config{pA2, pB})

	assert.Equal(t, sanitize(Configs.GetKey("ns/b")), &pB)
	assert.Equal(t, sanitize(Configs.GetKey("ns/a")), &pA2)

	t.Run("running register", func(t *testing.T) {
		t.Skip("not supported")
		tt2 := assert.NewTracker[string](t)
		Configs.Register(TrackerHandler[config.Config](tt2))
		tt2.WaitOrdered("add/ns/a", "add/ns/b")
	})

	configController.Delete(gvk.WasmPlugin, pB.Name, pB.Namespace, nil)
	tt.WaitOrdered("delete/ns/b")
}

func sanitizes(c []config.Config) []config.Config {
	return slices.Map(c, func(e config.Config) config.Config {
		e = e.DeepCopy()
		e.CreationTimestamp = time.Time{}
		e.ResourceVersion = ""
		return e
	})
}

func sanitize(e *config.Config) *config.Config {
	if e == nil {
		return nil
	}
	e2 := (*e).DeepCopy()
	e2.CreationTimestamp = time.Time{}
	e2.ResourceVersion = ""
	return &e2
}

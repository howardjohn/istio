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

package cv2_test

import (
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/cv2"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

func TestSingleton(t *testing.T) {
	c := kube.NewFakeClient()
	ConfigMaps := cv2.NewInformer[*corev1.ConfigMap](c)
	c.RunAndWait(test.NewStop(t))
	cmt := clienttest.NewWriter[*corev1.ConfigMap](t, c)
	ConfigMapNames := cv2.NewSingleton[string](
		func(ctx cv2.HandlerContext) *string {
			cms := cv2.Fetch(ctx, ConfigMaps)
			return ptr.Of(slices.Join(",", slices.Map(cms, func(c *corev1.ConfigMap) string {
				return config.NamespacedName(c).String()
			})...))
		},
	)
	tt := assert.NewTracker[string](t)
	ConfigMapNames.Register(TrackerHandler[string](tt))
	tt.WaitOrdered("add/")

	assert.Equal(t, *ConfigMapNames.Get(), "")

	cmt.Create(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "a",
			Namespace: "ns",
		},
	})
	tt.WaitOrdered("update/ns/a")
	assert.Equal(t, *ConfigMapNames.Get(), "ns/a")
}

func TestNewStatic(t *testing.T) {
	tt := assert.NewTracker[string](t)
	s := cv2.NewStatic[string](nil)
	s.Register(TrackerHandler[string](tt))

	assert.Equal(t, s.Get(), nil)

	s.Set(cv2.Ptr("foo"))
	assert.Equal(t, s.Get(), cv2.Ptr("foo"))
	tt.WaitOrdered("add/foo")

	s.Set(nil)
	assert.Equal(t, s.Get(), nil)
	tt.WaitOrdered("delete/foo")

	s.Set(cv2.Ptr("bar"))
	assert.Equal(t, s.Get(), cv2.Ptr("bar"))
	tt.WaitOrdered("add/bar")

	s.Set(cv2.Ptr("bar2"))
	assert.Equal(t, s.Get(), cv2.Ptr("bar2"))
	tt.WaitOrdered("update/bar2")
}

// TrackerHandler returns an object handler that records each event
func TrackerHandler[T any](tracker *assert.Tracker[string]) func(cv2.Event[T]) {
	return func(o cv2.Event[T]) {
		tracker.Record(fmt.Sprintf("%v/%v", o.Event, cv2.GetKey(o.Latest())))
	}
}

func BatchedTrackerHandler[T any](tracker *assert.Tracker[string]) func([]cv2.Event[T]) {
	return func(o []cv2.Event[T]) {
		tracker.Record(slices.Join(",", slices.Map(o, func(o cv2.Event[T]) string {
			return fmt.Sprintf("%v/%v", o.Event, cv2.GetKey(o.Latest()))
		})...))
	}
}

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
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/cv2"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
)

func TestSynchronizer(t *testing.T) {
	t.Skip("client-go has no Apply support")
	c := kube.NewFakeClient()
	Secrets := cv2.NewInformer[*corev1.Secret](c)
	GeneratedConfigMap := cv2.NewCollection[*corev1.Secret, corev1ac.ConfigMapApplyConfiguration](Secrets, func(ctx cv2.HandlerContext, i *corev1.Secret) *corev1ac.ConfigMapApplyConfiguration {
		m := map[string]string{}
		for k, v := range i.Data {
			m[k] = string(v)
		}
		return corev1ac.ConfigMap(i.Name, i.Namespace).
			WithData(m)
	})
	applies := atomic.NewInt32(0)
	LiveConfigMaps := cv2.NewInformer[*corev1.ConfigMap](c)
	// TODO: this is currently broken because client-go doesn't support managed fields
	cv2.NewSyncer(
		GeneratedConfigMap,
		LiveConfigMaps,
		func(gen corev1ac.ConfigMapApplyConfiguration, live *corev1.ConfigMap) bool {
			liveac, err := corev1ac.ExtractConfigMap(live, "istio")
			if err != nil {
				t.Fatal(err)
			}
			return controllers.Equal(&gen, liveac)
		},
		func(gen corev1ac.ConfigMapApplyConfiguration) {
			c.Kube().CoreV1().ConfigMaps(*gen.Namespace).Apply(context.Background(), &gen, metav1.ApplyOptions{
				DryRun:       nil,
				Force:        true,
				FieldManager: "istio",
			})
			applies.Inc()
		},
	)
	c.RunAndWait(test.NewStop(t))
	c.Kube().CoreV1().Secrets("default").Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "name"},
		Data: map[string][]byte{
			"key": []byte("value"),
		},
	}, metav1.CreateOptions{})
	check := func(key string, data map[string]string) {
		retry.UntilSuccessOrFail(t, func() error {
			lcmp := LiveConfigMaps.GetKey(cv2.Key[*corev1.ConfigMap](key))
			if lcmp == nil {
				return fmt.Errorf("configmap not found")
			}
			lcm := *lcmp
			if !reflect.DeepEqual(lcm.Data, data) {
				return fmt.Errorf("unexpected data %+v", lcm.Data)
			}
			return nil
		}, retry.Timeout(time.Second))
	}

	assert.EventuallyEqual(t, func() int32 { return applies.Load() }, int32(1))
	check("default/name", map[string]string{"key": "value"})

	t.Log("update input")
	c.Kube().CoreV1().Secrets("default").Update(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "name"},
		Data: map[string][]byte{
			"key": []byte("value2"),
		},
	}, metav1.UpdateOptions{})
	assert.EventuallyEqual(t, func() int32 { return applies.Load() }, int32(2))
	check("default/name", map[string]string{"key": "value"})
	time.Sleep(time.Second)
}

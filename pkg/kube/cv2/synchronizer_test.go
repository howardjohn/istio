package cv2_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"go.uber.org/atomic"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/cv2"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/pkg/log"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
)

func TestSynchronizer(t *testing.T) {
	c := kube.NewFakeClient()
	Secrets := cv2.CollectionFor[*corev1.Secret](c)
	GeneratedConfigMap := cv2.NewCollection[*corev1.Secret, corev1ac.ConfigMapApplyConfiguration](Secrets, func(ctx cv2.HandlerContext, i *corev1.Secret) *corev1ac.ConfigMapApplyConfiguration {
		m := map[string]string{}
		for k, v := range i.Data {
			m[k] = string(v)
		}
		return corev1ac.ConfigMap(i.Name, i.Namespace).
			WithData(m)
	})
	applies := atomic.NewInt32(0)
	LiveConfigMaps := cv2.CollectionFor[*corev1.ConfigMap](c)
	cv2.NewSynchronizer(
		GeneratedConfigMap,
		LiveConfigMaps,
		func(gen corev1ac.ConfigMapApplyConfiguration, live *corev1.ConfigMap) bool {
			liveac, err := corev1ac.ExtractConfigMap(live, "istio")
			if err != nil {
				t.Fatal(err)
			}
			log.Errorf("howardjohn: compare\n%v\n%v\n%v\n%v", gen.Data, liveac.Data, live.Data, live.ManagedFields)
			return controllers.Equal(&gen, liveac)
		},
		func(gen corev1ac.ConfigMapApplyConfiguration) {
			log.Errorf("howardjohn: apply %v", gen.Data)
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
	assert := func(key string, data map[string]string) {
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
	retry.UntilEqualOrFail(t, 1, func() int32 { return applies.Load() }, retry.Timeout(time.Second))
	assert("default/name", map[string]string{"key": "value"})

	t.Log("update input")
	c.Kube().CoreV1().Secrets("default").Update(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "name"},
		Data: map[string][]byte{
			"key": []byte("value2"),
		},
	}, metav1.UpdateOptions{})
	retry.UntilEqualOrFail(t, 2, func() int32 { return applies.Load() }, retry.Timeout(time.Second))
	assert("default/name", map[string]string{"key": "value"})
	time.Sleep(time.Second)
}

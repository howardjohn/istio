package cv2_test

import (
	"fmt"
	"testing"

	"istio.io/istio/pkg/kube/cv2"
	"istio.io/istio/pkg/test/util/assert"
)

func TestNewStatic(t *testing.T) {
	events := []string{}
	s := cv2.NewStatic[string](nil)
	s.Register(func(o cv2.Event[string]) {
		events = append(events, fmt.Sprintf("%v/%v", o.Event, o.Latest()))
	})
	assert.Equal(t, s.Get(), nil)
	s.Set(cv2.Ptr("foo"))
	assert.Equal(t, s.Get(), cv2.Ptr("foo"))
	s.Set(nil)
	assert.Equal(t, s.Get(), nil)
	s.Set(cv2.Ptr("bar"))
	assert.Equal(t, s.Get(), cv2.Ptr("bar"))
	s.Set(cv2.Ptr("bar2"))
	assert.Equal(t, s.Get(), cv2.Ptr("bar2"))
	assert.Equal(t, events, []string{"add/foo", "delete/foo", "add/bar", "update/bar2"})
}

package kube

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestInformerFor(t *testing.T) {
	c := NewFakeClient()
	InformerFor[*corev1.Pod](c)
}

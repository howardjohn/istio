package controller

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	listerv1 "k8s.io/client-go/listers/core/v1"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
)

func (c *Controller) WorkloadInformation() []model.WorkloadInfo {
	c.pods.Lock()
	defer c.pods.Unlock()
	infos := []model.WorkloadInfo{}

	pl := listerv1.NewPodLister(c.pods.informer.GetIndexer())
	pods, _ := pl.Pods(metav1.NamespaceAll).List(klabels.Everything())
	for _, pod := range pods {
		if !IsPodReady(pod) {
			continue
		}
		if pod.Spec.HostNetwork {
			// TODO we could include this but need to be more careful
			continue
		}
		wl := model.WorkloadInfo{
			Name:      pod.Name,
			Namespace: pod.Namespace,
			Address:   pod.Status.PodIP,
			Identity:  kube.SecureNamingSAN(pod),
			Node:      pod.Spec.NodeName,
			Protocol:  model.GetTLSModeFromEndpointLabels(pod.Labels),
		}

		infos = append(infos, wl)
	}
	return infos
}

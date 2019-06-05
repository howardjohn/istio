package model_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/fakes"
)

func createService(hostname string, namespace string) (*model.Service, []*model.ServiceInstance) {
	servicePort := &model.Port{
		Name:     "default",
		Port:     8080,
		Protocol: model.ProtocolHTTP,
	}
	service := &model.Service{
		Hostname:    model.Hostname(hostname),
		Address:     "1.1.1.1",
		ClusterVIPs: make(map[string]string),
		Ports:       model.PortList{servicePort},
		Attributes: model.ServiceAttributes{
			Name:      hostname,
			Namespace: namespace,
		},
	}
	instances := []*model.ServiceInstance{
		{
			Service: service,
			Endpoint: model.NetworkEndpoint{
				Address:     "192.168.1.1",
				Port:        10001,
				ServicePort: servicePort,
			},
		},
		{
			Service: service,
			Endpoint: model.NetworkEndpoint{
				Address:     "192.168.1.2",
				Port:        10001,
				ServicePort: servicePort,
			},
		},
	}
	return service, instances
}

func createSidecarConfig(egress string) *model.Config {
	return &model.Config{
		ConfigMeta: model.ConfigMeta{
			Namespace: "default",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{egress},
				},
			},
		},
	}
}

func TestInstancesForService(t *testing.T) {
	defaultService, defaultInstances := createService("example.com", "default")
	altDefaultService, altDefaultInstances := createService("google.com", "default")
	otherService, otherInstances := createService("google.com", "other")

	allServices := []*model.Service{defaultService, altDefaultService, otherService}
	allInstances := flatten(defaultInstances, altDefaultInstances, otherInstances)

	ps := initTestContext(allServices, allInstances)

	tests := []struct {
		egress   string
		expected []*model.ServiceInstance
	}{
		{
			egress:   "*/*",
			expected: allInstances,
		},
		{
			egress:   "default/*",
			expected: flatten(defaultInstances, altDefaultInstances),
		},
		{
			egress:   "default/example.com",
			expected: defaultInstances,
		},
		{
			egress:   "*/google.com",
			expected: flatten(altDefaultInstances, otherInstances),
		},
	}
	for _, tt := range tests {
		t.Run(tt.egress, func(t *testing.T) {
			sidecar := createSidecarConfig(tt.egress)
			sc := model.ConvertToSidecarScope(ps, sidecar, sidecar.Namespace)

			got, err := sc.InstancesForService(ps.Env, "foo.com", 8080, nil)
			if err != nil {
				t.Fatal(err)
			}

			if !reflect.DeepEqual(tt.expected, got) {
				t.Errorf("Expected %v, got %v", outputInstances(tt.expected), outputInstances(got))
			}
		})
	}
}

func flatten(i ...[]*model.ServiceInstance) []*model.ServiceInstance {
	res := []*model.ServiceInstance{}
	for _, instances := range i {
		res = append(res, instances...)
	}
	return res
}

func outputInstances(instances []*model.ServiceInstance) string {
	res := []string{}
	for _, i := range instances {
		res = append(res, fmt.Sprintf("%v.%v-%v", i.Service.Hostname, i.Service.Attributes.Namespace, i.Endpoint.Address))
	}
	return strings.Join(res, ", ")
}

func initTestContext(services []*model.Service, instances []*model.ServiceInstance) *model.PushContext {
	serviceDiscovery := &fakes.ServiceDiscovery{}

	serviceDiscovery.ServicesReturns(services, nil)
	serviceDiscovery.GetProxyServiceInstancesReturns(instances, nil)
	serviceDiscovery.InstancesByPortReturns(instances, nil)

	configStore := &fakes.IstioConfigStore{}

	ps := model.NewPushContext()
	meshConfig := model.DefaultMeshConfig()
	env := &model.Environment{
		IstioConfigStore: configStore,
		Mesh:             &meshConfig,
		ServiceDiscovery: serviceDiscovery,
	}
	_ = ps.InitContext(env)

	return ps
}

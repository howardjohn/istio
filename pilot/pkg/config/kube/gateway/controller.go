package gateway

import (
	"fmt"
	"strings"

	"k8s.io/client-go/kubernetes"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema"
	"istio.io/istio/pkg/config/schemas"
	"istio.io/pkg/log"
)

type controller struct {
	client kubernetes.Interface
	cache  model.ConfigStoreCache
}

func (c controller) ConfigDescriptor() schema.Set {
	return schema.Set{schemas.VirtualService, schemas.Gateway}
}

func (c controller) Get(typ, name, namespace string) *model.Config {
	panic("implement me")
}

func (c controller) List(typ, namespace string) ([]model.Config, error) {
	if typ != schemas.Gateway.Type && typ != schemas.VirtualService.Type {
		return nil, errUnsupportedOp
	}
	log.Errorf("howardjohn: calling List of type %v", typ)

	gw := []model.Config{}
	vs := []model.Config{}
	gws, err := c.cache.List(schemas.KubernetesGateway.Type, namespace)
	routes, err := c.cache.List(schemas.KubernetesHttpRoute.Type, namespace)
	if err != nil {
		return nil, err
	}
	routeToGateway := map[string]string{}
	for _, obj := range gws {
		kgw := obj.Spec.(*v1alpha3.KubernetesGateway)
		name := obj.Name + "-" + constants.KubernetesGatewayName
		var servers []*v1alpha3.Server
		for _, listener := range kgw.Listeners {
			for _, p := range listener.Protocols {
				servers = append(servers, &v1alpha3.Server{
					Port: &v1alpha3.Port{
						Number:   uint32(listener.Port),
						Protocol: p,
						Name:     fmt.Sprintf("%v-%v-gateway-%s-%s", strings.ToLower(p), listener.Port, obj.Name, obj.Namespace),
					},
					Hosts: []string{listener.Address},
				})
			}

			for _, route := range listener.Routes {
				routeToGateway[route] = name
			}
		}
		gatewayConfig := model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:      schemas.Gateway.Type,
				Group:     schemas.Gateway.Group,
				Version:   schemas.Gateway.Version,
				Name:      name,
				Namespace: obj.Namespace,
				Domain:    "cluster.local",
			},
			Spec: &v1alpha3.Gateway{
				Servers:  servers,
				Selector: labels.Instance{constants.IstioLabel: "ingressgateway"},
			},
		}
		gw = append(gw, gatewayConfig)
	}

	for _, obj := range routes {
		kr := obj.Spec.(*v1alpha3.KubernetesHttpRoute)
		var http []*v1alpha3.HTTPRoute
		for _, rule := range kr.Rules {
			http = append(http, &v1alpha3.HTTPRoute{
				Match: []*v1alpha3.HTTPMatchRequest{
					{
						Uri: &v1alpha3.StringMatch{
							MatchType: &v1alpha3.StringMatch_Prefix{Prefix: rule.Path},
						},
					},
				},
				Route: []*v1alpha3.HTTPRouteDestination{
					{
						Destination: &v1alpha3.Destination{
							Host: rule.Backend.Service,
							Port: &v1alpha3.PortSelector{
								Number: uint32(rule.Backend.Port),
							},
						},
					},
				},
			})
		}
		virtualConfig := model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:      schemas.VirtualService.Type,
				Group:     schemas.VirtualService.Group,
				Version:   schemas.VirtualService.Version,
				Name:      obj.Name + "-" + constants.KubernetesGatewayName,
				Namespace: obj.Namespace,
				Domain:    "cluster.local",
			},
			Spec: &v1alpha3.VirtualService{
				Hosts:    []string{kr.Host},
				Gateways: []string{routeToGateway[obj.Name]},
				Http:     http,
			},
		}
		vs = append(vs, virtualConfig)
	}
	switch typ {
	case schemas.Gateway.Type:
		return gw, nil
	case schemas.VirtualService.Type:
		return vs, nil
	}
	return nil, errUnsupportedOp
}

var (
	errUnsupportedOp = fmt.Errorf("unsupported operation: the gateway config store is a read-only view")
)

func (c controller) Create(config model.Config) (revision string, err error) {
	return "", errUnsupportedOp
}

func (c controller) Update(config model.Config) (newRevision string, err error) {
	return "", errUnsupportedOp
}

func (c controller) Delete(typ, name, namespace string) error {
	return errUnsupportedOp
}

func (c controller) Version() string {
	panic("implement me")
}

func (c controller) GetResourceAtVersion(version string, key string) (resourceVersion string, err error) {
	panic("implement me")
}

func (c controller) RegisterEventHandler(typ string, handler func(model.Config, model.Event)) {
	log.Errorf("howardjohn: RegisterEventHandler for type=%v", typ)
	c.cache.RegisterEventHandler(typ, func(config model.Config, event model.Event) {
		log.Errorf("howardjohn: EventHandler called for %v", config.Name)
		handler(config, event)
	})
}

func (c controller) Run(stop <-chan struct{}) {
	log.Errorf("howardjohn: Run")
}

func (c controller) HasSynced() bool {
	return c.cache.HasSynced()
}

func NewController(client kubernetes.Interface, c model.ConfigStoreCache) model.ConfigStoreCache {

	return &controller{client, c}
}

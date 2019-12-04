package gateway

import (
	"fmt"

	"k8s.io/client-go/kubernetes"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/protocol"
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
	cfgs, err := c.cache.List(schemas.KubernetesGateway.Type, namespace)
	if err != nil {
		return nil, err
	}
	for _, obj := range cfgs {
		gatewayConfig := model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:      schemas.Gateway.Type,
				Group:     schemas.Gateway.Group,
				Version:   schemas.Gateway.Version,
				Name:      obj.Name + "-" + constants.KubernetesGatewayName,
				Namespace: obj.Namespace,
				Domain:    "cluster.local",
			},
			Spec: &v1alpha3.Gateway{
				Servers: []*v1alpha3.Server{
					{
						Port: &v1alpha3.Port{
							Number:   80,
							Protocol: string(protocol.HTTP),
							Name:     fmt.Sprintf("http-80-gateway-%s-%s", obj.Name, obj.Namespace),
						}},
				},
				Selector: labels.Instance{constants.IstioLabel: constants.IstioIngressLabelValue},
			},
		}
		gw = append(gw, gatewayConfig)
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

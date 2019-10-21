package fuzz

import (
	"encoding/json"
	"fmt"

	"github.com/gogo/protobuf/proto"
	"istio.io/istio/galley/pkg/config/meshcfg"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/fakes"
	"istio.io/istio/pkg/config/schema"
	"istio.io/istio/pkg/config/schemas"
)

func doFuzz(data []byte, c schema.Instance) int {
	instance, err := c.Make()
	if err != nil {
		// TODO(https://github.com/gogo/protobuf/issues/579) make this panic
		panic(err)
		//return 0
	}
	defer func() {
		if r := recover(); r != nil {
			debug, _ := json.MarshalIndent(instance, "", "  ")
			fmt.Println("Validation crashed:", c.MessageName)
			fmt.Println("Failed message:", string(debug))
			panic(r)
		}
	}()

	if err := proto.Unmarshal(data, instance); err != nil {
		panic(err)
	}
	if err := c.Validate("name", "ns", instance); err != nil {
		return 0
	}
	return 1
}

func FuzzValidation(data []byte) int {
	res := 0
	for _, p := range schemas.Istio {
		res |= doFuzz(data, p)
	}
	return res
}

func FuzzComplex(data []byte) int {
	configs, err := BytesToCRDs(data)
	if err != nil {
		return 0
	}
	if len(configs) < 2 {
		return 0
	}
	types := []string{}
	for _, config := range configs {
		s, _ := schemas.Istio.GetByType(config.Type)
		types = append(types, s.Type)
		if err := s.Validate("default", "default", config.Spec); err != nil {
			return 0
		}
	}

	proxy := model.Proxy{
		Type:        model.SidecarProxy,
		IPAddresses: []string{"1.1.1.1"},
		ID:          "v0.default",
		DNSDomain:   "default.example.org",
		Metadata: &model.NodeMetadata{
			ConfigNamespace: "not-default",
			IstioVersion:    "1.3.0",
		},
		ConfigNamespace: "not-default",
	}
	env := buildListenerEnv(configs)
	if err := env.PushContext.InitContext(&env, nil, nil); err != nil {
		panic(err)
	}
	proxy.SetSidecarScope(env.PushContext)

	configgen := core.NewConfigGenerator([]string{})
	clusters := configgen.BuildClusters(&env, &proxy, env.PushContext)
	for _, r := range clusters {
		if err := r.Validate(); err != nil {
			panic(err)
		}
	}

	listeners := configgen.BuildListeners(&env, &proxy, env.PushContext)
	for _, r := range listeners {
		if err := r.Validate(); err != nil {
			panic(err)
		}
	}
	if len(listeners) == 2 && len(clusters) == 4 {
		fmt.Println("Boring", types)
		return 0
	}
	fmt.Println("Generated config!", len(listeners), len(clusters), types)

	//routes := configgen.BuildHTTPRoutes(&env, &proxy, env.PushContext, "route")
	//fmt.Println("Built routes:", routes)
	//if err := routes.Validate(); err != nil {
	//	panic(err)
	//}
	return 1
}

func buildListenerEnv(cfg []*model.Config) model.Environment {
	serviceDiscovery := new(fakes.ServiceDiscovery)

	configStore := &fakes.IstioConfigStore{
		ListStub: func(typ, namespace string) (configs []model.Config, e error) {
			res := []model.Config{}
			for _, c := range cfg {
				if c.Type == typ {
					res = append(res, *c)
				}
			}
			return res, nil
		},
	}

	mesh := meshcfg.Default()
	env := model.Environment{
		PushContext:      model.NewPushContext(),
		ServiceDiscovery: serviceDiscovery,
		IstioConfigStore: configStore,
		Mesh:             mesh,
	}

	return env
}

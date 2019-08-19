package fuzz

import (
	"fmt"

	"github.com/gogo/protobuf/proto"
	"github.com/google/gofuzz"
	"github.com/icrowley/fake"
	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core"
	"istio.io/istio/pkg/config/schema"
	"istio.io/istio/pkg/config/schemas"

	"math/rand"
	"testing"
)

var (

	debug = false
)

func TestListener(t *testing.T) {
	f := fuzz.
		New().
		RandSource(rand.NewSource(1)).
		NilChance(0).
		Funcs(
			func(i *v1alpha3.ServiceEntry, c fuzz.Continue) {
				i.Hosts = []string{fake.DomainName()}
				i.Resolution = v1alpha3.ServiceEntry_Resolution(c.Int31n(3))
			},
		)

	valid := 0
	for i := 0; i < 10000; i++ {
		log("")

		se := &v1alpha3.ServiceEntry{}
		f.Fuzz(se)
		log("Created: %v", se)

		if err := safeValidate(schemas.ServiceEntry, se); err != nil {
			log("failed validation: %v", err)
			continue
		}

		env := buildListenerEnv(toConfigs(schemas.ServiceEntry))
		if err := env.PushContext.InitContext(&env); err != nil {
			log("failed init: %v", err)
			continue
		}
		proxy := model.Proxy{
			Type:        model.SidecarProxy,
			IPAddresses: []string{"1.1.1.1"},
			ID:          "v0.default",
			DNSDomain:   "default.example.org",
			Metadata: map[string]string{
				model.NodeMetadataConfigNamespace: "not-default",
				"ISTIO_PROXY_VERSION":             "1.3",
			},
			ConfigNamespace: "not-default",
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

		route := configgen.BuildHTTPRoutes(&env, &proxy, env.PushContext, "route")
		if err := route.Validate(); err != nil {
			panic(err)
		}

		valid++
	}
	fmt.Println("Valid:", valid)
}

func safeValidate(schema schema.Instance, msg proto.Message) (e error) {
	defer func() {
		if r := recover(); r != nil {
			e = fmt.Errorf("panic: %v", r)
		}
	}()
	if err := schema.Validate("default", "default", msg); err != nil {
		return err
	}
	return nil
}

func toConfigs(schema schema.Instance, spec ...proto.Message) []*model.Config {
	config := []*model.Config{}
	for n, s := range spec {
		config = append(config, &model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:      schema.Type,
				Version:   schema.Version,
				Name:      "default" + string(n),
				Namespace: "default",
			},
			Spec: s,
		})
	}
	return config
}

func log(format string, a ...interface{}) {
	if debug {
		fmt.Printf(format+"\n", a...)
	}
}
package fuzz

import (
	"encoding/binary"
	"fmt"

	"github.com/gogo/protobuf/proto"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/schema"
	"istio.io/istio/pkg/config/schemas"
)

// First byte describes the number of inputs
// Each input consists of 1 byte describing the schema index, then 4 bytes describing the size (N), then N bytes for the content
func BytesToCRDs(input []byte) ([]*model.Config, error) {
	crds := schema.Set{
		schemas.ServiceEntry,
		schemas.Sidecar,
		schemas.Gateway,
		schemas.VirtualService,
		schemas.ServiceEntry,
	}
	if len(input) < 1 {
		return nil, fmt.Errorf("input must have at least 1 byte")
	}
	numInputs := input[0]
	index := input[1:]
	configs := []*model.Config{}
	for numInputs > 0 {
		if len(index) == 0 {
			return nil, fmt.Errorf("ran out of input with %v left", numInputs)
		}
		numInputs--
		schemaIndex := uint8(index[0])
		index = index[1:]

		configSchema := crds[int(schemaIndex)%len(crds)]

		if len(index) < 4 {
			return nil, fmt.Errorf("ran out of bytes getting configSize")
		}
		configSize := binary.BigEndian.Uint32(index[0:4])
		index = index[4:]

		if configSize > uint32(len(index)) {
			return nil, fmt.Errorf("ran out of bytes getting config. Have %v, need %v", uint32(len(index)), configSize)
		}
		configBytes := index[0:configSize]
		index = index[configSize:]

		instance, err := configSchema.Make()
		if err != nil {
			panic(err)
		}

		if err := proto.Unmarshal(configBytes, instance); err != nil {
			return nil, fmt.Errorf("invalid proto generated: %v", err)
		}
		configs = append(configs, toConfig(configSchema, instance))
	}
	return configs, nil
}

func toConfig(schema schema.Instance, spec proto.Message) *model.Config {
	return &model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      schema.Type,
			Version:   schema.Version,
			Name:      "default",
			Namespace: "default",
		},
		Spec: spec,
	}
}

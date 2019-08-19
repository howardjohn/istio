package fuzz

import (
	"reflect"
	"testing"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/schemas"
)

func TestBytesToCRDs(t *testing.T) {
	cases := []struct {
		name     string
		input    []byte
		expected []*model.Config
		error    bool
	}{
		{
			name:     "empty config",
			input:    []byte{0},
			expected: []*model.Config{},
		},
		{
			name:  "invalid length",
			input: []byte{1},
			error: true,
		},
		{
			name: "valid empty input",
			input: []byte{
				1,          // 1 input
				0,          // Schema 0
				0, 0, 0, 3, // 3 bytes long
				10, 1, 97, // A valid VirtualService
			},
			expected: []*model.Config{toConfig(schemas.VirtualService, &v1alpha3.VirtualService{Hosts: []string{"a"}})},
		},
		{
			name: "short size",
			input: []byte{
				1,    // 1 input
				0,    // Schema 0
				0, 0, // should be 4 bytes
			},
			error: true,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := BytesToCRDs(tt.input)
			if tt.error != (err != nil) {
				t.Fatalf("Expected error to be %v, got %v", tt.error, err)
			}
			if !reflect.DeepEqual(tt.expected, resp) {
				t.Fatalf("Expected %v, got %v", tt.expected, resp)
			}
		})
	}
}

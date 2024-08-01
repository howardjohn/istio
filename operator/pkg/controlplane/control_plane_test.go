// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controlplane

import (
	"testing"

	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/translate"
)

func TestNewIstioOperator(t *testing.T) {
	coreComponentOptions := &options{
		InstallSpec: v1alpha1.IstioOperatorSpec{},
		Translator:  &translate.Translator{},
	}
	tests := []struct {
		desc              string
		inTranslator      *translate.Translator
		wantIstioOperator *IstioControlPlane
		wantErr           error
	}{
		{
			desc: "core-components",
			inTranslator: &translate.Translator{
				ComponentMaps: map[name.ComponentName]*translate.ComponentMaps{
					"Pilot": {
						ResourceName: "test-resource",
					},
				},
			},
			wantErr: nil,
			wantIstioOperator: &IstioControlPlane{
				components: []*istioComponent{
					{
						options:       coreComponentOptions,
						ComponentName: name.IstioBaseComponentName,
					},
					{
						options:       coreComponentOptions,
						ResourceName:  "test-resource",
						ComponentName: name.PilotComponentName,
					},
					{
						ComponentName: name.CNIComponentName,
						options:       coreComponentOptions,
					},
					{
						ComponentName: name.IstiodRemoteComponentName,
						options:       coreComponentOptions,
					},
					{
						ComponentName: name.ZtunnelComponentName,
						options:       coreComponentOptions,
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			gotOperator, err := NewIstioControlPlane(v1alpha1.IstioOperatorSpec{}, tt.inTranslator, nil)
			if (err != nil && tt.wantErr == nil) || (err == nil && tt.wantErr != nil) {
				t.Fatalf("%s: wanted err %v, got err %v", tt.desc, tt.wantErr, err)
			}
			if !gotOperator.componentsEqual(tt.wantIstioOperator.components) {
				t.Fatalf("not equal %v vs %v", gotOperator, tt.wantIstioOperator)
			}
		})
	}
}

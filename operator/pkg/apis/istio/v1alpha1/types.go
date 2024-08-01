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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// IstioOperatorSpec defines the desired installed state of Istio components.
// The spec is a used to define a customization of the default profile values that are supplied with each Istio release.
// Because the spec is a customization API, specifying an empty IstioOperatorSpec results in a default Istio
// component values.
//
//	apiVersion: install.istio.io/v1alpha1
//	kind: IstioOperator
//	spec:
//	  profile: default
//	  hub: gcr.io/istio-testing
//	  tag: latest
//	  revision: 1-8-0
//	  meshConfig:
//	    accessLogFile: /dev/stdout
//	    enableTracing: true
//	  components:
//	    egressGateways:
//	    - name: istio-egressgateway
//	      enabled: true
type IstioOperator struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the implementation of this definition.
	Spec IstioOperatorSpec `json:"spec,omitempty"`
}

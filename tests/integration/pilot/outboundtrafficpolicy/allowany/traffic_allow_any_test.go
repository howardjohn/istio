// Copyright 2019 Istio Authors
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

package allowany

import (
	"istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"os"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/tests/integration/pilot/outboundtrafficpolicy"
)

func TestMain(m *testing.M) {
	var ist istio.Instance
	framework.NewSuite("outbound_traffic_policy_allow_any", m).
		// RequireEnvironment(environment.Kube).
		Setup(istio.SetupOnKube(&ist, setupConfig)).
		Run()
}

func setupConfig(cfg *istio.Config) {
	if cfg == nil {
		return
	}
	cfg.Values["global.outboundTrafficPolicy.mode"] = "ALLOW_ANY"
	cfg.Values["pilot.env.PILOT_ENABLE_FALLTHROUGH_ROUTE"] = "true"
}

func TestOutboundTrafficPolicyAllowAny(t *testing.T) {
	expected := map[model.Protocol][]string{
		model.ProtocolHTTP:  {"200"},
		model.ProtocolHTTPS: {"200"},
	}
	if err := os.Setenv("PILOT_ENABLE_FALLTHROUGH_ROUTE", "true"); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	meshConfig := model.DefaultMeshConfig()
	meshConfig.OutboundTrafficPolicy = &v1alpha1.MeshConfig_OutboundTrafficPolicy{Mode: v1alpha1.MeshConfig_OutboundTrafficPolicy_ALLOW_ANY}
	outboundtrafficpolicy.RunExternalRequestTest(t, expected, &meshConfig)
}

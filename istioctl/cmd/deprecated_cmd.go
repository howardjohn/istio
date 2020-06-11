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

// DEPRECATED - These commands are deprecated and will be removed in future releases.

package cmd

import (
	"istio.io/istio/pilot/pkg/config/kube/crdclient"
	"istio.io/istio/pilot/pkg/model"
	controller2 "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config/schema/collections"
	kubecfg "istio.io/istio/pkg/kube"
)

var (
	// Create a model.ConfigStore (or sortedConfigStore)
	configStoreFactory = newConfigStore
)

func newConfigStore() (model.ConfigStore, error) {
	cfg, err := kubecfg.BuildClientConfig(kubeconfig, configContext)
	if err != nil {
		return nil, err
	}

	return crdclient.NewForConfig(cfg, collections.Pilot, &model.DisabledLedger{}, "", controller2.Options{DomainSuffix: ""})
}

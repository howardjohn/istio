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

package cv2

import (
	"fmt"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/slices"
	istiolog "istio.io/pkg/log"
)

var _ Collection[config.Config] = &configStoreWatch{}

func (i configStoreWatch) AddDependency(chain []string) {
	panic("!")
}

func (i configStoreWatch) _internalHandler() {}

func (i configStoreWatch) Name() string {
	return fmt.Sprintf("configStoreWatch[%v]", i.gvk)
}

func (i configStoreWatch) List(namespace string) []config.Config {
	res := i.cs.List(i.gvk, namespace)
	slices.SortFunc(res, func(a, b config.Config) bool {
		return GetKey(a) < GetKey(b)
	})
	return res
}

func (i configStoreWatch) GetKey(k Key[config.Config]) *config.Config {
	ns, name := controllers.SplitKeyFunc(string(k))
	return i.cs.Get(i.gvk, name, ns)
}

func (i configStoreWatch) Register(f func(o Event[config.Config])) {
	batchedRegister[config.Config](i, f)
}

func (i configStoreWatch) RegisterBatch(f func(o []Event[config.Config])) {
	i.cs.RegisterEventHandler(i.gvk, func(c config.Config, c2 config.Config, event model.Event) {
		o := Event[config.Config]{
			Event: controllers.EventType(event),
		}
		switch event {
		case model.EventUpdate:
			o.Old = &c
			o.New = &c2
		case model.EventAdd:
			o.New = &c2
		case model.EventDelete:
			o.Old = &c2
		}
		i.log.WithLabels("key", GetKey(o.Latest()), "type", o.Event).Debugf("handling event")
		f([]Event[config.Config]{o})
	})
}

type configStoreWatch struct {
	gvk config.GroupVersionKind
	cs  model.ConfigStoreController
	log *istiolog.Scope
}

func NewConfigStore(gvk config.GroupVersionKind, cs model.ConfigStoreController) Collection[config.Config] {
	return configStoreWatch{gvk, cs, log.WithLabels("owner", fmt.Sprintf("ConfigStore[%v]", gvk))}
}

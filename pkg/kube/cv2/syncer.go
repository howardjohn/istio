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

	"istio.io/istio/pkg/kube/controllers"
)

func NewSyncer[G, L any](
	g Collection[G],
	l Collection[L],
	compare func(G, L) bool,
	apply func(G),
) {
	log := log.WithLabels("type", fmt.Sprintf("sync[%T]", *new(G)))
	g.Register(func(o Event[G]) {
		// TODO: we need not just Apply but also delete
		switch o.Event {
		case controllers.EventDelete:
		default:
			gen := o.Latest()
			genKey := GetKey(gen)
			// TODO: can we make this Key conversion in a safer manner
			live := l.GetKey(Key[L](genKey))
			changed := true
			if live != nil {
				changed = !compare(gen, *live)
			}
			log.WithLabels("changed", changed).Infof("generated update")
			if changed {
				apply(gen)
			}
		}
	})
	l.Register(func(o Event[L]) {
		// TODO: we need not just Apply but also delete
		switch o.Event {
		case controllers.EventDelete:
		default:
			live := o.Latest()
			liveKey := GetKey(live)
			// TODO: can we make this Key conversion in a safer manner
			gen := g.GetKey(Key[G](liveKey))
			if gen == nil {
				return
			}
			changed := !compare(*gen, live)
			log.WithLabels("changed", changed).Infof("live update")
			if changed {
				apply(*gen)
			}
		}
	})
}

package cv2

import (
	"fmt"

	"istio.io/istio/pkg/kube/controllers"
)

func NewSynchronizer[G, L any](
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

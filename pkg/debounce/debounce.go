package debounce

import (
	"istio.io/istio/pkg/log"
	"k8s.io/utils/clock"
	"time"
)

type Options[T any] struct {
	Base  time.Duration
	Work  func(T)
	Stop  <-chan struct{}
	Input chan T
	Merge func(existing T, incoming T) T
	Time  clock.Clock
}

func Do[T any](o Options[T]) {
	if o.Time == nil {
		o.Time = clock.RealClock{}
	}
	var empty T
	var cur T
	log.WithLabels("t", o.Time.Now())
	var MaxDebounce = 3*time.Second
	var ItemDebounce = 1*time.Second
	var maxExpired clock.Timer
	var itemExpired clock.Timer
	for {
		select {
		case <-safeC(maxExpired):
			log.WithLabels("t", o.Time.Now()).Errorf("howardjohn: max expired: %v", cur)
			o.Work(cur)
			cur = empty
			safeStop(itemExpired)
		case <-safeC(itemExpired):
			log.WithLabels("t", o.Time.Now()).Errorf("howardjohn: item expired: %v", cur)
			o.Work(cur)
			cur = empty
			safeStop(maxExpired)
		case r := <-o.Input:
			log.WithLabels("t", o.Time.Now()).Errorf("howardjohn: got input %v", r)
			cur = o.Merge(cur, r)
			if maxExpired == nil {
				maxExpired = o.Time.NewTimer(MaxDebounce)
			}
			if itemExpired == nil {
				itemExpired = o.Time.NewTimer(ItemDebounce)
			}
		case <-o.Stop:
			return
		}
	}
}

func safeStop(t clock.Timer) {
	if !t.Stop() {
		<-t.C()
	}
}

func safeC(t clock.Timer) <-chan time.Time {
	if t != nil {
		return t.C()
	}
	var maxc <-chan time.Time
	return maxc
}

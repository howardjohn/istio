package debounce

import (
	"time"

	"k8s.io/utils/clock"

	"istio.io/istio/pkg/log"
)

type Options[T any] struct {
	Base  time.Duration
	Work  func(T)
	Stop  <-chan struct{}
	Input chan T
	Merge func(existing T, incoming T) T
	Time  clock.Clock

	TestingOnlyOnWork func()
}

func Do[T any](o Options[T]) {
	if o.Time == nil {
		o.Time = clock.RealClock{}
	}
	var empty T
	var cur T
	log.WithLabels("t", o.Time.Now())
	MaxDebounce := 3 * time.Second
	ItemDebounce := 1 * time.Second
	var maxExpired clock.Timer
	var itemExpired clock.Timer
	for {
		select {
		case <-safeC(maxExpired):
			log.WithLabels("t", o.Time.Now()).Errorf("howardjohn: max expired: %v", cur)
			o.Work(cur)
			cur = empty
			itemExpired = safeStop(itemExpired)
			maxExpired = safeStop(maxExpired)
		case <-safeC(itemExpired):
			log.WithLabels("t", o.Time.Now()).Errorf("howardjohn: item expired: %v", cur)
			o.Work(cur)
			cur = empty
			itemExpired = safeStop(itemExpired)
			maxExpired = safeStop(maxExpired)
		case r := <-o.Input:
			log.WithLabels("t", o.Time.Now()).Errorf("howardjohn: got input %v", r)
			cur = o.Merge(cur, r)
			if maxExpired == nil {
				maxExpired = o.Time.NewTimer(MaxDebounce)
			}
			if itemExpired == nil {
				itemExpired = o.Time.NewTimer(ItemDebounce)
			} else {
				itemExpired.Reset(ItemDebounce)
			}
			o.TestingOnlyOnWork()
		case <-o.Stop:
			return
		}
	}
}

func safeStop(t clock.Timer) clock.Timer {
	if !t.Stop() {
		select {
		case <-t.C():
		default:
		}
	}
	return nil
}

func safeC(t clock.Timer) <-chan time.Time {
	if t != nil {
		return t.C()
	}
	var maxc <-chan time.Time
	return maxc
}

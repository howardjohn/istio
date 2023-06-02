package debounce

import (
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	testclock "k8s.io/utils/clock/testing"
	"testing"
	"time"
)

func TestDebounce(t *testing.T) {
	clock := testclock.NewFakeClock(time.Unix(0, 0))
	tt := assert.NewTracker[int](t)
	work := make(chan int)
	opts := Options[int]{
		Base: 0,
		Work: func(i int) {
			t.Logf("Doing %v", i)
			tt.Record(i)
		},
		Stop:  test.NewStop(t),
		Input: work,
		Merge: func(existing int, incoming int) int {
			return existing + incoming
		},
		Time: clock,
	}
	go Do(opts)
	work <- 1
	tt.Empty()
	clock.Step(time.Second)
	tt.WaitOrdered(1)
	clock.Step(time.Second * 10)
	tt.Empty()

	time.Sleep(time.Second)
	work <- 2
	work <- 3
	tt.Empty()
	clock.Step(time.Second)
	tt.WaitOrdered(5)

	work <- 1
	clock.Step(time.Second)
	work <- 1
	clock.Step(time.Second)
	work <- 1
	clock.Step(time.Second)
	work <- 1
	clock.Step(time.Second)
	tt.WaitOrdered(3)
	tt.WaitOrdered(1)
	tt.Empty()
	time.Sleep(time.Second)
}

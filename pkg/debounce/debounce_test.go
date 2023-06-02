package debounce

import (
	"testing"
	"time"

	testclock "k8s.io/utils/clock/testing"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

type wf = func(send func(int), clock *testclock.FakeClock, tt *assert.Tracker[int])

func runDebounceTest(t *testing.T, name string, testFn wf) {
	t.Run(name, func(t *testing.T) {
		c := testclock.NewFakeClock(time.Unix(0, 0))
		tt := assert.NewTracker[int](t)
		work := make(chan int)
		sends := make(chan struct{})
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
			Time: c,
			TestingOnlyOnWork: func() {
				sends <- struct{}{}
			},
		}
		send := func(i int) {
			work <- i
			<-sends
		}
		go Do(opts)
		testFn(send, c, tt)
	})
}

func TestDebounce(t *testing.T) {
	runDebounceTest(t, "single", func(send func(int), clock *testclock.FakeClock, tt *assert.Tracker[int]) {
		send(1)
		tt.Empty()              // Should not do anything yet
		clock.Step(time.Second) // Item debounce trigger
		tt.WaitOrdered(1)       // Now we should see it processed

		// Even after a lot of time passes, no more events
		clock.Step(time.Second * 10)
		tt.Empty()
	})

	runDebounceTest(t, "merge", func(send func(int), clock *testclock.FakeClock, tt *assert.Tracker[int]) {
		send(2)
		send(3)
		tt.Empty()
		clock.Step(time.Second)
		tt.WaitOrdered(5)
	})

	runDebounceTest(t, "max time", func(send func(int), clock *testclock.FakeClock, tt *assert.Tracker[int]) {
		for i := 0; i < 7; i++ {
			send(1)
			clock.Step(time.Millisecond * 550)
		}
		clock.Step(time.Second)
		tt.WaitOrdered(6)
		tt.WaitOrdered(1)
		tt.Empty()
	})
}

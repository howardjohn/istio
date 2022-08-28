package sel_test

import (
	"testing"

	"istio.io/istio/pkg/sel"
)

func TestRandom(t *testing.T) {
	e := Expectation(t, true)
	c1 := make(chan int)
	c2 := make(chan string)
	go func() {
		c2 <- "foo"
	}()
	sel.Random(
		sel.Channel[int]{c1, func(i int) {
			Expectation(t, false).Apply()
		}},
		sel.Channel[string]{c2, func(i string) {
			e.Apply()
		}},
	)
}

type Must struct {
	sent bool
	expect bool
}

func Expectation(t *testing.T, must bool) *Must {
	m := &Must{sent: false, expect: must}
	if must {
		t.Cleanup(func() {
			if !m.sent {
				t.Fatalf("never sent")
			}
		})
	}
	return m
}

func (m *Must) Apply() {
	if m.sent {
		panic("Apply() called twice")
	}
	m.sent = true
}

func TestPrioritized(t *testing.T) {
	e := Expectation(t, true)
	c1 := make(chan int, 1)
	c2 := make(chan string, 1)
	unsent := make(chan string, 1)
	c2 <- "foo"
	c1 <- 1
	chans := []sel.ChannelType{
		sel.Channel[string]{unsent, func(i string) {
			Expectation(t, false).Apply()
		}},
		sel.Channel[int]{c1, func(i int) {
			e.Apply()
		}},
		sel.Channel[string]{c2, func(i string) {
			Expectation(t, false).Apply()
		}},
	}
	sel.Prioritized(chans...)
}

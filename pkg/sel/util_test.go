package sel_test

import (
	"testing"

	"istio.io/istio/pkg/sel"
)

func TestRandom(t *testing.T) {
	e := Expectation(t)
	c1 := make(chan int)
	c2 := make(chan string)
	go func() {
		c2 <- "foo"
	}()
	sel.Random(
		sel.Channel[int]{c1, func(i int) {
			t.Fatalf("unexpected esnd")
		}},
		sel.Channel[string]{c2, func(i string) {
			e.Call()
		}},
	)
}

type Must struct {
	calls    int
	expected int
	t        *testing.T
}

func Expectation(t *testing.T) *Must {
	m := &Must{calls: 0, expected: 1, t: t}
	t.Cleanup(func() {
		m.Assert()
	})
	return m
}

func (m *Must) Reset() *Must {
	m.calls = 0
	return m
}

func (m *Must) Expect(calls int) *Must {
	m.expected = calls
	return m
}

func (m *Must) Call() *Must {
	m.calls++
	return m
}

func (m *Must) Assert() {
	if m.calls != m.expected {
		m.t.Fatalf("expected %v calls, got %v", m.expected, m.calls)
	}
}

func TestPrioritized(t *testing.T) {
	e := Expectation(t)
	c1 := make(chan int, 1)
	c2 := make(chan string, 1)
	unsent := make(chan string, 1)
	c2 <- "foo"
	c1 <- 1
	chans := []sel.ChannelType{
		sel.Channel[string]{unsent, func(i string) {
			t.Fatalf("unexpected send")
		}},
		sel.Channel[int]{c1, func(i int) {
			e.Call()
		}},
		sel.Channel[string]{c2, func(i string) {
			t.Fatalf("unexpected send")
		}},
	}
	sel.Prioritized(chans...)
}

func TestRepeat(t *testing.T) {
	c1 := make(chan int, 1)
	e1, e2 := Expectation(t).Expect(3), Expectation(t).Expect(1)
	c2 := make(chan string, 1)
	unsent := make(chan string, 1)

	chans := []sel.ChannelType{
		sel.Channel[string]{unsent, func(i string) {
			t.Fatalf("unexpected send")
		}},
		sel.Channel[int]{c1, func(i int) {
			e1.Call()
		}},
		sel.Channel[string]{c2, func(i string) {
			e2.Call()
		}},
	}
	done := make(chan struct{})
	go func() {
		sel.Repeat(sel.Random, chans...)
		close(done)
	}()

	c1 <- 1
	c1 <- 1
	c1 <- 1
	c2 <- "s"
	close(c2)
	<-done
}

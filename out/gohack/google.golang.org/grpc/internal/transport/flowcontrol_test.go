package transport

import (
	"testing"
	"time"
)

func TestQuota(t *testing.T) {
	s := make(chan struct{})
	wq := newWriteQuota(0, s)
	go func() {
		wq.get(1)
		t.Log("got quota 1")
	}()

	go func() {
		wq.get(1)
		t.Log("got quota 2")
	}()
	time.Sleep(time.Millisecond*100)
	wq.realReplenish(10)
	time.Sleep(time.Second*2)
}

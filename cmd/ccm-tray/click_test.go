package main

import (
	"sync"
	"testing"
	"time"
)

// Regression: systray.ResetMenu closes every removed item's ClickedCh. A
// handler that cannot tell a close from a click quit the whole app on the first
// menu rebuild, which happened after every successful account switch.
func TestOnClickIgnoresChannelClose(t *testing.T) {
	ch := make(chan struct{})
	var calls int

	done := make(chan struct{})
	go func() {
		onClick(ch, func() { calls++ })
		close(done)
	}()

	close(ch)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("onClick did not return when the channel closed")
	}
	if calls != 0 {
		t.Errorf("handler ran %d times on a channel close, want 0", calls)
	}
}

func TestOnClickRunsForEachClick(t *testing.T) {
	ch := make(chan struct{})
	var mu sync.Mutex
	var calls int

	done := make(chan struct{})
	go func() {
		onClick(ch, func() { mu.Lock(); calls++; mu.Unlock() })
		close(done)
	}()

	for i := 0; i < 3; i++ {
		ch <- struct{}{}
	}
	close(ch)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("onClick did not return when the channel closed")
	}

	mu.Lock()
	defer mu.Unlock()
	if calls != 3 {
		t.Errorf("handler ran %d times, want 3", calls)
	}
}

// A slow handler must not drop the close signal that follows it.
func TestOnClickReturnsAfterSlowHandler(t *testing.T) {
	ch := make(chan struct{})
	done := make(chan struct{})
	go func() {
		onClick(ch, func() { time.Sleep(50 * time.Millisecond) })
		close(done)
	}()

	ch <- struct{}{}
	close(ch)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("onClick did not return after a slow handler")
	}
}

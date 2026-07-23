package wago

import (
	"strings"
	"testing"
)

func TestInvocationLeaseStateMachine(t *testing.T) {
	t.Run("entry then close publication", func(t *testing.T) {
		in := &Instance{resourcesClosed: true}
		if err := in.beginInvocation(); err != nil {
			t.Fatalf("beginInvocation: %v", err)
		}
		if got := in.invocationState.Load(); got != 1 {
			t.Fatalf("state after entry = %#x, want 1", got)
		}
		if previous := in.closeInvocationEntry(); previous != 1 {
			t.Fatalf("close previous state = %#x, want 1", previous)
		}
		if got := in.invocationState.Load(); got != instanceInvocationClosed|1 {
			t.Fatalf("published close state = %#x, want CLOSED|1", got)
		}
	})

	t.Run("rejected post-close entry does not mutate count", func(t *testing.T) {
		in := &Instance{resourcesClosed: true}
		in.invocationState.Store(instanceInvocationClosed | 1)
		if err := in.beginInvocation(); err == nil || !strings.Contains(err.Error(), "closed") {
			t.Fatalf("beginInvocation error = %v, want closed", err)
		}
		if got := in.invocationState.Load(); got != instanceInvocationClosed|1 {
			t.Fatalf("rejected entry changed state to %#x, want CLOSED|1", got)
		}
		in.endInvocation()
		if got := in.invocationState.Load(); got != instanceInvocationClosed {
			t.Fatalf("final invocation release state = %#x, want CLOSED", got)
		}
	})

	t.Run("count overflow is rejected without publishing close", func(t *testing.T) {
		in := &Instance{resourcesClosed: true}
		in.invocationState.Store(instanceInvocationCount)
		if err := in.beginInvocation(); err == nil || !strings.Contains(err.Error(), "too many") {
			t.Fatalf("beginInvocation overflow error = %v, want explicit limit", err)
		}
		if got := in.invocationState.Load(); got != instanceInvocationCount {
			t.Fatalf("overflow rejection changed state to %#x, want %#x", got, instanceInvocationCount)
		}
	})
}

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

// The shared stage harness writes in prepare mode and loads the same artifact
// in a separate wago_precompiled binary. No compiler metadata is reconstructed.
func TestRV64HostEventCompilerFreeArtifact(t *testing.T) {
	c := rv64TableStageCompile(t, "host-event-loop", hostEventLoopModule(), true)
	var got []int32
	in, err := Instantiate(c, testImports("env.event", I32HostEvent(func(v int32) { got = append(got, v) })))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if in.syncMode || len(in.hostLog) == 0 {
		t.Fatal("artifact event mode")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if out, err := in.Invoke("run", 3); err != nil || len(out) != 0 || !slices.Equal(got, []int32{3, 2, 1}) {
		t.Fatal("artifact ordered events", out, err, got)
	}
}
func TestRV64HostEventCancelCloseAndReentry(t *testing.T) {
	c := rv64TableStageCompile(t, "host-event-lifecycle", hostEventLoopModule(), true)
	defer c.Close()
	var in *Instance
	calls := 0
	var nestedErr error
	var closeErr error
	in, err := Instantiate(c, testImports("env.event", I32HostEvent(func(int32) {
		calls++
		if calls == 1 {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			_, nestedErr = in.InvokeContext(ctx, "run", 0)
			cancel()
			closeErr = in.Close()
		}
	})))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := in.InvokeContext(cancelled, "run", 2); !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatal("cancelled event admission", err, calls)
	}
	if _, err := in.Invoke("run", 2); err != nil || calls != 2 || closeErr != nil {
		t.Fatal("active replay Close", err, calls, closeErr)
	}
	if !errors.Is(nestedErr, context.DeadlineExceeded) {
		t.Fatal("capability-free nested call acquired live gate", nestedErr)
	}
	if _, err := in.Invoke("run", 1); err == nil || calls != 2 {
		t.Fatal("closed event instance entered", err, calls)
	}
}

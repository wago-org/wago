package wagobench

import (
	"context"
	wago "github.com/wago-org/wago"
	"os"
	"runtime"
	"testing"
)

// This separate diagnostic distinguishes extra GC observations from engine teardown.
func TestSharingGCObservation(t *testing.T) {
	if os.Getenv("WAGO_SHARING_MEMORY") != "1" {
		t.Skip("diagnostic")
	}
	fs := sharingFixtures()
	cfg := wago.NewRuntimeConfig().WithFunctionWorkers(1)
	rt := wago.NewRuntime(wago.WithRuntimeConfig(cfg))
	var outputs []*wago.Module
	for j := 0; j < 4; j++ {
		for _, f := range fs {
			c, e := rt.Compile(f.bytes)
			if e != nil {
				t.Fatal(e)
			}
			outputs = append(outputs, c)
		}
	}
	sharingSnapshot(t, "gc_retained", outputs)
	for _, c := range outputs {
		if e := c.Close(); e != nil {
			t.Fatal(e)
		}
	}
	outputs = nil
	for _, phase := range []string{"engine_alive_gc0", "engine_alive_gc1", "engine_alive_gc2", "engine_alive_gc3"} {
		sharingSnapshot(t, phase, nil)
	}
	runtime.KeepAlive(rt)
	if e := rt.CloseContext(context.Background()); e != nil {
		t.Fatal(e)
	}
	rt = nil
	sharingSnapshot(t, "engine_closed_extra_gc", nil)
	runtime.KeepAlive(fs)
}

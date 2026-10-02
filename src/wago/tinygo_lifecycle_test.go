//go:build tinygo

package wago

import (
	"context"
	gruntime "runtime"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// TestTinyGoOperationLeaseSurvivesCollection keeps the explicit operation lease
// as the only live owner of its Runtime across a collection. Runtime public
// operations use this value shape for deferred lease release.
func TestTinyGoOperationLeaseSurvivesCollection(t *testing.T) {
	for i := 0; i < 100; i++ {
		operation, err := rootlessRuntimeOperation()
		if err != nil {
			t.Errorf("begin operation %d: %v", i, err)
			return
		}
		gruntime.GC()
		operation.end()
	}
}

//go:noinline
func rootlessRuntimeOperation() (runtimeOperation, error) {
	rt := NewRuntime()
	_, operation, err := rt.beginOperationGeneration("test", false)
	return operation, err
}

// TestTinyGoRuntimeCloseTaskSurvivesCollection isolates the queued task as the
// only owner of the Runtime shutdown graph before the TinyGo scheduler runs it.
func TestTinyGoRuntimeCloseTaskSurvivesCollection(t *testing.T) {
	for i := 0; i < 100; i++ {
		closed, err := rootlessRuntimeClose()
		if err != nil {
			t.Errorf("Close %d: %v", i, err)
			return
		}
		gruntime.GC()
		gruntime.Gosched()
		<-closed
	}
}

//go:noinline
func rootlessRuntimeClose() (<-chan struct{}, error) {
	rt := NewRuntime()
	rt.storeHooks(&hookRegistry{onRuntimeClose: []func(RuntimeCloseEvent){func(RuntimeCloseEvent) {}}})
	if err := rt.Close(); err != nil {
		return nil, err
	}
	return rt.Closed(), nil
}

// TestTinyGoRuntimeInstantiateCloseStability is the smallest public lifecycle
// that reproduced the linux/amd64 TinyGo release-profile crash: compile once,
// then repeatedly instantiate and close the same module. Collections between
// iterations make stale heap references fail at their owning transition.
func TestTinyGoRuntimeInstantiateCloseStability(t *testing.T) {
	rt := NewRuntime()
	mod, err := rt.Compile(benchAddOneModule())
	if err != nil {
		t.Errorf("Compile: %v", err)
		return
	}
	for i := 0; i < 100; i++ {
		in, err := rt.Instantiate(context.Background(), mod)
		if err != nil {
			t.Errorf("Instantiate %d: %v", i, err)
			return
		}
		if err := in.Close(); err != nil {
			t.Errorf("Close %d: %v", i, err)
			return
		}
		gruntime.GC()
	}
}

func TestTinyGoDecodeStability(t *testing.T) {
	for i := 0; i < 10_000; i++ {
		source := benchAddOneModule()
		if _, err := wasm.DecodeModule(source); err != nil {
			t.Errorf("DecodeModule %d: %v", i, err)
			return
		}
	}
}

func TestTinyGoPackageCompileCloseStability(t *testing.T) {
	for i := 0; i < 1_000; i++ {
		compiled, err := Compile(benchAddOneModule())
		if err != nil {
			t.Errorf("Compile %d: %v", i, err)
			return
		}
		if err := compiled.Close(); err != nil {
			t.Errorf("Close %d: %v", i, err)
			return
		}
	}
}

// TestTinyGoRuntimeCloseOverlapStability mirrors benchmark calibration: each
// round publishes asynchronous Runtime shutdown, then the next round immediately
// starts constructing and recycling runtime resources.
func TestTinyGoRuntimeCloseOverlapStability(t *testing.T) {
	tinyGoRuntimeCloseOverlap(t, 100, 100, false, true)
}

func TestTinyGoRuntimeCloseOverlapCompileOnly(t *testing.T) {
	tinyGoRuntimeCloseOverlap(t, 100, 0, false, true)
}

func TestTinyGoRuntimeCloseOverlapSingleInstance(t *testing.T) {
	tinyGoRuntimeCloseOverlap(t, 100, 1, false, true)
}

func TestTinyGoRuntimeCloseOwnsInstanceStability(t *testing.T) {
	tinyGoRuntimeCloseOverlap(t, 100, 1, false, false)
}

func TestTinyGoRuntimeSynchronousCloseStability(t *testing.T) {
	tinyGoRuntimeCloseOverlap(t, 100, 100, true, true)
}

func tinyGoRuntimeCloseOverlap(t *testing.T, rounds, instances int, synchronous, closeInstances bool) {
	t.Helper()
	for round := 0; round < rounds; round++ {
		rt := NewRuntime()
		mod, err := rt.Compile(benchAddOneModule())
		if err != nil {
			t.Errorf("round %d Compile: %v", round, err)
			return
		}
		for i := 0; i < instances; i++ {
			in, err := rt.Instantiate(context.Background(), mod)
			if err != nil {
				t.Errorf("round %d Instantiate %d: %v", round, i, err)
				return
			}
			if closeInstances {
				if err := in.Close(); err != nil {
					t.Errorf("round %d Close %d: %v", round, i, err)
					return
				}
			}
		}
		if synchronous {
			if err := rt.CloseContext(context.Background()); err != nil {
				t.Errorf("round %d Runtime.CloseContext: %v", round, err)
				return
			}
		} else if err := rt.Close(); err != nil {
			t.Errorf("round %d Runtime.Close: %v", round, err)
			return
		}
		gruntime.Gosched()
	}
}

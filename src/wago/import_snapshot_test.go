package wago

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestImportSnapshotDeclarationCallbacks(t *testing.T) {
	for _, nilEvent := range []bool{false, true} {
		t.Run(fmt.Sprint(nilEvent), func(t *testing.T) {
			im := NewImports()
			im.HostFunc("a.b", "c", CallerHostCallFunc(func(Caller, HostCall) {})).Params(ValI64)
			im.HostFunc("a", "b.c", func(x int32) int32 { return x })
			var event I32HostEvent
			if !nilEvent {
				event = func(int32) {}
			}
			im.I32Event("module\x00.with:separator", "event", event)
			for i := 0; i < 2; i++ {
				bindings, err := im.snapshot()
				if nilEvent {
					if err == nil || !strings.Contains(err.Error(), "deferred host callback is nil") {
						t.Fatalf("nil event: %v", err)
					}
				} else if err != nil || len(bindings) != 3 {
					t.Fatalf("snapshot: %d, %v", len(bindings), err)
				}
			}
		})
	}
}

func TestImportSnapshotConcurrentAndAllocationBudget(t *testing.T) {
	im := NewImports()
	for i := 0; i < 46; i++ {
		im.HostFunc("wasi_snapshot_preview1", fmt.Sprintf("diagnostic_%02d", i), CallerHostCallFunc(func(Caller, HostCall) {})).Params(ValI32, ValI32).Results(ValI32)
	}
	im.I32Event("env", "event", func(int32) {})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if bindings, err := im.snapshot(); err != nil || len(bindings) != 47 {
					t.Errorf("snapshot: %d, %v", len(bindings), err)
					return
				}
			}
		}()
	}
	wg.Wait()
	if got := testing.AllocsPerRun(100, func() {
		if _, err := im.snapshot(); err != nil {
			t.Fatal(err)
		}
	}); got != 0 {
		t.Fatalf("snapshot allocated %v times; want zero", got)
	}
}

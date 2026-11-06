//go:build linux && (amd64 || arm64) && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"strings"
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestSharedMemoryRollbackReleasesOwnedBacking(t *testing.T) {
	for _, tc := range []struct {
		name     string
		memories [][]byte
	}{
		{"primary", [][]byte{{0x03, 0x01, 0x01}}},
		{"with-secondary", [][]byte{{0x03, 0x01, 0x01}, {0x01, 0x01, 0x01}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// (module (memory 1 1 shared) [(memory 1 1)]
			//   (data (i32.const 65536) "x"))
			source := wasmtest.Module(
				wasmtest.Section(5, wasmtest.Vec(tc.memories...)),
				wasmtest.Section(11, wasmtest.Vec([]byte{0x00, 0x41, 0x80, 0x80, 0x04, 0x0b, 0x01, 'x'})),
			)
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
			compiled, err := Compile(cfg, source)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			before := coreruntime.ProcessNativeMemoryStats()
			if !before.Supported {
				t.Skip("native memory registry is unavailable")
			}
			for i := 0; i < 16; i++ {
				in, err := Instantiate(compiled)
				if in != nil {
					_ = in.Close()
					t.Fatal("instantiation unexpectedly succeeded")
				}
				if err == nil || !strings.Contains(err.Error(), "active data segment 0 out of bounds") {
					t.Fatalf("instantiation %d: expected data-segment bounds error, got %v", i, err)
				}
				if after := coreruntime.ProcessNativeMemoryStats(); after.Active != before.Active {
					t.Fatalf("instantiation %d leaked native memory: before %+v, after %+v", i, before, after)
				}
			}
		})
	}
}

func TestSharedMemoryRollbackControlAllocationFailure(t *testing.T) {
	// Leave room for the shared backing, but not its separate control mapping.
	// Consume the reuse cache first so it cannot bypass the registry limit.
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	source := wasmtest.Module(wasmtest.Section(5, wasmtest.Vec([]byte{0x03, 0x01, 0x01})))
	compiled, err := Compile(cfg, source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	stats := coreruntime.ProcessNativeMemoryStats()
	if !stats.Supported {
		t.Skip("native memory registry is unavailable")
	}
	var held []*coreruntime.JobMemory
	defer func() {
		for _, memory := range held {
			if err := memory.Close(); err != nil {
				t.Error(err)
			}
		}
	}()
	for {
		memory, err := coreruntime.AcquireJobMemoryGrowable(0, 0)
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, memory)
		if coreruntime.ProcessNativeMemoryStats().Registered == stats.Capacity-1 {
			break
		}
	}
	before := coreruntime.ProcessNativeMemoryStats()
	in, err := Instantiate(compiled)
	if in != nil {
		_ = in.Close()
		t.Fatal("instantiation unexpectedly succeeded")
	}
	if !errors.Is(err, coreruntime.ErrResourceLimit) || !strings.Contains(err.Error(), "allocate threaded instance control") {
		t.Fatalf("expected control-mapping resource-limit error, got %v", err)
	}
	if after := coreruntime.ProcessNativeMemoryStats(); after.Active != before.Active {
		t.Fatalf("failed control allocation retained shared backing: before %+v, after %+v", before, after)
	}
	// Rollback may cache the backing. Reusing it must still yield valid storage.
	memory, err := coreruntime.AcquireJobMemoryGrowable(65536, 65536)
	if err != nil {
		t.Fatal(err)
	}
	held = append(held, memory)
	if pages := memory.CurrentPages(); pages != 1 {
		t.Fatalf("reused shared backing has %d pages, want 1", pages)
	}
}

package wago

import (
	"context"
	"testing"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestReviewSharedProviderUnsharedImport(t *testing.T) {
	if !SupportedFeatures().IsEnabled(CoreFeatureThreads) {
		t.Skip("threads backend is unavailable")
	}
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	memImport := append(wasmtest.Name("env"), wasmtest.Name("mem")...)
	memImport = append(memImport, 2, 3, 1, 1)
	producerBytes := wasmtest.Module(
		wasmtest.Section(2, wasmtest.Vec(memImport)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("mem", 2, 0))),
	)
	pc, err := Compile(cfg, producerBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	hostMemory, err := NewSharedMemory(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer hostMemory.Close()
	p, err := Instantiate(pc, NewImports().Memory("env", "mem", hostMemory))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	memory, err := p.ExportedMemory("mem")
	if err != nil {
		t.Fatal(err)
	}
	cc, err := Compile(cfg, importMemModule())
	if err != nil {
		t.Fatal(err)
	}
	defer cc.Close()
	consumer, err := Instantiate(cc, NewImports().Memory("env", "mem", memory))
	if consumer != nil {
		defer consumer.Close()
	}
	if err == nil {
		t.Fatal("shared Wasm memory was accepted for an unshared import")
	}
}

func TestSharedHostMemoryExportKeepsWaitCapability(t *testing.T) {
	m, err := NewSharedMemory(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err := m.share(nil, memoryDef{Min: 1, Max: 1, HasMax: true}); err != nil {
		t.Fatal(err)
	}
	if got, err := m.wait32(context.Background(), 0, 1, 0); err != nil || got != memoryWaitNotEqual {
		t.Fatalf("wait after unshared export = %d, %v", got, err)
	}
	if err := m.validateLimits(1, 1, true, false, true); err == nil {
		t.Fatal("unshared export satisfied a shared import")
	}
}

func TestMemoryTypeFlagsSurviveImporterOverflow(t *testing.T) {
	state := &memoryState{}
	defer memoryImporterOverflow.Delete(state)
	flags := memoryStateShared | memoryStateWasmShared | memoryStateAddr64 | memoryStateLimitsKnown | memoryStateClosed | memoryStateWasmTypeKnown | memoryStateDeclaredShared
	state.set(flags, true)
	state.setDeclaredLimits(1<<48, true)
	for _, count := range []uint32{0, 62, 63, 64, 254, 255, 256, 63, 62, 0} {
		state.setImporterCount(count)
		if got := state.importerCount(); got != count {
			t.Fatalf("importer count = %d, want %d", got, count)
		}
		if uint16(state.meta>>memoryStateFlagsShift) != flags || state.declaredMaximum() != 1<<48 {
			t.Fatal("importer count changed memory type or maximum")
		}
	}
}

func TestConflictingMemoryReexports(t *testing.T) {
	if !SupportedFeatures().IsEnabled(CoreFeatureThreads) {
		t.Skip("threads backend is unavailable")
	}
	for _, sharedFirst := range []bool{false, true} {
		name := "unshared_first"
		if sharedFirst {
			name = "shared_first"
		}
		t.Run(name, func(t *testing.T) {
			memory, err := NewSharedMemory(1, 1)
			if err != nil {
				t.Fatal(err)
			}
			defer memory.Close()
			var instances [2]*Instance
			for i, shared := range []bool{sharedFirst, !sharedFirst} {
				flags := byte(1)
				if shared {
					flags = 3
				}
				entry := append(wasmtest.Name("env"), wasmtest.Name("mem")...)
				entry = append(entry, 2, flags, 1, 1)
				data := wasmtest.Module(
					wasmtest.Section(2, wasmtest.Vec(entry)),
					wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("mem", 2, 0))),
				)
				code, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2|CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit), data)
				if err != nil {
					t.Fatal(err)
				}
				defer code.Close()
				instances[i], err = Instantiate(code, NewImports().Memory("env", "mem", memory))
				if err != nil {
					t.Fatal(err)
				}
				defer instances[i].Close()
			}
			first, err := instances[0].ExportedMemory("mem")
			if err != nil {
				t.Fatal(err)
			}
			if first != memory {
				t.Fatal("export changed memory identity")
			}
			state := memory.state.Load()
			state.mu.Lock()
			meta, owner := state.meta, state.owner
			state.mu.Unlock()
			backing := memory.jm
			if exported, err := instances[1].ExportedMemory("mem"); err == nil || exported != nil {
				t.Fatalf("conflicting export = %p, %v; want nil and an error", exported, err)
			}
			if memory.state.Load() != state || memory.jm != backing {
				t.Fatal("rejected export changed backing or state identity")
			}
			state.mu.Lock()
			unchanged := state.meta == meta && state.owner == owner
			state.mu.Unlock()
			if !unchanged {
				t.Fatal("rejected export changed memory state")
			}
			if again, err := instances[0].ExportedMemory("mem"); err != nil || again != first {
				t.Fatalf("original export after rejection = %p, %v", again, err)
			}
		})
	}
}

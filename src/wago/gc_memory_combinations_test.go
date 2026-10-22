//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo && !wago_precompiled

package wago

import (
	"context"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcMemoryCombinationSource(wide, shared bool, maximum uint32) []byte {
	return gcMemoryCombinationCallSource(wide, shared, maximum, false)
}

func gcMemoryCombinationCallSource(wide, shared bool, maximum uint32, calls bool) []byte {
	flags := byte(1)
	address := wasm.I32
	constant := byte(0x41)
	if wide {
		flags |= 4
		address = wasm.I64
		constant = 0x42
	}
	if shared {
		flags |= 2
	}
	if maximum == 0 {
		flags &^= 1
	}
	memory := []byte{flags, 1}
	if maximum != 0 {
		memory = append(memory, wasmtest.ULEB(maximum)...)
	}
	memorySection := wasmtest.Section(5, wasmtest.Vec(memory))
	var importSection []byte
	if shared {
		entry := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
		entry = append(entry, 2)
		importSection = wasmtest.Section(2, wasmtest.Vec(append(entry, memory...)))
		memorySection = nil
	}
	// Keep the first object in an exact local root while the second allocation
	// can move it. Linear memory addresses use the memory's declared width.
	store, load := []byte{0x36, 2, 0}, []byte{0x28, 2, 0}
	if shared {
		store, load = []byte{0xfe, 0x17, 2, 0}, []byte{0xfe, 0x10, 2, 0}
	}
	run := []byte{1, 1, 0x63, 0, 0x41, 42, 0xfb, 0, 0, 0x21, 0, constant, 0, 0x41, 7}
	run = append(run, store...)
	run = append(run, 0x41, 5, 0xfb, 0, 0, 0x1a, 0x20, 0)
	if calls {
		callee := byte(4)
		if shared {
			callee = 2
		}
		run = append(run, 0x10, callee)
	} else {
		run = append(run, 0xfb, 2, 0, 0)
	}
	run = append(run, constant, 0)
	run = append(run, load...)
	run = append(run, 0x6a, 0x0b)
	probe := append([]byte{0x20, 0}, load...)
	probe = append(probe, 0x0b)
	functions := [][]byte{wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(3), wasmtest.ULEB(4)}
	exports := [][]byte{wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("probe", 0, 1), wasmtest.ExportEntry("grow", 0, 2), wasmtest.ExportEntry("size", 0, 3)}
	code := [][]byte{append(wasmtest.ULEB(uint32(len(run))), run...), wasmtest.Code(probe), wasmtest.Code([]byte{0x20, 0, 0x40, 0, 0x0b}), wasmtest.Code([]byte{0x3f, 0, 0x0b})}
	if shared {
		functions, exports, code = functions[:2], exports[:2], code[:2]
	}
	types := [][]byte{
		{0x5f, 1, 0x7f, 1},
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{address}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{address}, []wasm.ValType{address}),
		wasmtest.FuncType(nil, []wasm.ValType{address}),
	}
	if calls {
		types = append(types, []byte{0x60, 1, 0x63, 0, 1, 0x7f})
		functions = append(functions, wasmtest.ULEB(5))
		code = append(code, wasmtest.Code([]byte{0x41, 9, 0xfb, 0, 0, 0x1a, 0x20, 0, 0xfb, 2, 0, 0, 0x0b}))
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		importSection,
		wasmtest.Section(3, wasmtest.Vec(functions...)),
		memorySection,
		wasmtest.Section(7, wasmtest.Vec(exports...)),
		wasmtest.Section(10, wasmtest.Vec(code...)),
	)
}

func TestGCMemoryFeatureCombinations(t *testing.T) {
	for _, tc := range []struct {
		name         string
		wide, shared bool
		maximum      uint32
	}{
		{"memory32", false, false, 2}, {"memory64", true, false, 2}, {"unbounded-memory32", false, false, 0}, {"unbounded-memory64", true, false, 0}, {"large-declared-maximum", false, false, 257}, {"shared-memory32", false, true, 2}, {"memory32-call", false, false, 2}, {"shared-memory32-call", false, true, 2},
	} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/artifact%t", tc.name, artifact), func(t *testing.T) {
				cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
				calls := tc.name == "memory32-call" || tc.name == "shared-memory32-call"
				c, err := cfg.Compile(gcMemoryCombinationCallSource(tc.wide, tc.shared, tc.maximum, calls))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				if artifact {
					blob, err := c.MarshalBinary()
					if err != nil {
						t.Fatal(err)
					}
					c, err = LoadTrustedArtifact(blob)
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()
				}
				rt := NewRuntime(WithRuntimeConfig(cfg))
				defer rt.Close()
				mod, err := rt.Module(c)
				if err != nil {
					t.Fatal(err)
				}
				defer mod.Close()
				imports := NewImports()
				if tc.shared {
					memory, err := NewSharedMemory(1, tc.maximum)
					if err != nil {
						t.Fatal(err)
					}
					defer memory.Close()
					imports.Memory("env", "memory", memory)
				}
				in, err := rt.Instantiate(context.Background(), mod, WithImports(imports), WithGC(GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 32, ForceMajorEveryMinor: true, VerifyAfterCollect: true}))
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				check := func(name string, want uint64, args ...uint64) {
					t.Helper()
					got, err := in.Invoke(name, args...)
					if err != nil || len(got) != 1 || got[0] != want {
						t.Fatalf("%s=%v,%v want [%d]", name, got, err, want)
					}
				}
				check("run", 49)
				if !tc.shared {
					check("grow", 1, 1)
					check("size", 2)
				}
				check("probe", 7, 0)
				check("run", 49)
				if tc.wide {
					if _, err := in.Invoke("probe", 1<<32); err == nil {
						t.Fatal("memory64 address wrapped to memory32")
					}
				}
			})
		}
	}
}

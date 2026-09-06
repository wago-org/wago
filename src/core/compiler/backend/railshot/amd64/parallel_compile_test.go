//go:build linux && amd64

package amd64

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestParallelFuncResultSizeAMD64(t *testing.T) {
	if got, want := unsafe.Sizeof(funcResult{}), uintptr(56); got != want {
		t.Fatalf("funcResult size = %d, want %d", got, want)
	}
}

func TestCompactFuncResultRangeAMD64(t *testing.T) {
	start, end, ok := compactFuncResultRange(int(^uint32(0))-7, 7)
	if !ok || start != ^uint32(0)-7 || end != ^uint32(0) {
		t.Fatalf("boundary range = (%d, %d, %v)", start, end, ok)
	}
	if _, _, ok := compactFuncResultRange(int(^uint32(0))-7, 8); ok {
		t.Fatal("overflowing worker range accepted")
	}
	if _, _, ok := compactFuncResultRange(-1, 1); ok {
		t.Fatal("negative worker range accepted")
	}
	if value, ok := compactFuncResultValue(int(^uint32(0))); !ok || value != ^uint32(0) {
		t.Fatalf("boundary value = (%d, %v)", value, ok)
	}
	if _, ok := compactFuncResultValue(int(uint64(^uint32(0)) + 1)); ok {
		t.Fatal("overflowing metadata value accepted")
	}
}

func TestInlineTargetSizeAMD64(t *testing.T) {
	if got, want := unsafe.Sizeof(inlineTarget{}), uintptr(56); got != want {
		t.Fatalf("inlineTarget size = %d, want %d", got, want)
	}
}

func TestCompileWorkersDeterministic(t *testing.T) {
	corpus := filepath.Join("..", "..", "..", "..", "..", "..", "bench", "corpus")
	for _, name := range []string{
		"tiny.wasm",
		"fib_rec.wasm",      // recursion and direct-call relocations
		"dispatch.wasm",     // call_indirect
		"many_funcs.wasm",   // enough functions to exercise every worker
		"globals.wasm",      // mutable globals
		"memory_tree.wasm",  // memory plus recursion
		"branches.wasm",     // structured control flow
		"json-as-simd.wasm", // SIMD, memory, globals, calls, and auto-inlining
	} {
		t.Run(name, func(t *testing.T) {
			m := readParallelTestModule(t, filepath.Join(corpus, name))
			want, wantStats := compileWorkerTestModule(t, m, 1)
			for _, workers := range []int{2, 4, 8} {
				for repeat := 0; repeat < 5; repeat++ {
					got, gotStats := compileWorkerTestModule(t, m, workers)
					assertCompiledModuleEqual(t, got, want)
					if !equalWorkerModuleStatsAMD64(gotStats, wantStats) {
						t.Fatalf("workers=%d repeat=%d: stats differ\n got: %#v\nwant: %#v", workers, repeat, gotStats, wantStats)
					}
				}
			}
		})
	}
}

func TestCompileWorkersCompactSharedAdaptersDeterministicAMD64(t *testing.T) {
	corpus := filepath.Join("..", "..", "..", "..", "..", "..", "bench", "corpus")
	for _, name := range []string{"many_funcs.wasm", "json-as-simd.wasm"} {
		t.Run(name, func(t *testing.T) {
			m := readParallelTestModule(t, filepath.Join(corpus, name))
			want, wantStats := compileWorkerTestModuleCompact(t, m, 1, true)
			got, gotStats := compileWorkerTestModuleCompact(t, m, 4, true)
			assertCompiledModuleEqual(t, got, want)
			if !equalWorkerModuleStatsAMD64(gotStats, wantStats) {
				t.Fatalf("Size stats differ\n got: %#v\nwant: %#v", gotStats, wantStats)
			}
		})
	}
}

func equalWorkerModuleStatsAMD64(a, b *ModuleStats) bool {
	aCopy, bCopy := *a, *b
	aCopy.Funcs = append([]*CodegenStats(nil), a.Funcs...)
	bCopy.Funcs = append([]*CodegenStats(nil), b.Funcs...)
	aCopy.NativeSize.CompilerCodeArenaBytes = 0
	bCopy.NativeSize.CompilerCodeArenaBytes = 0
	aCopy.Compile.StageNanos = [shared.CompileStageCount]uint64{}
	bCopy.Compile.StageNanos = [shared.CompileStageCount]uint64{}
	aCopy.Compile.NodeScratchReserved, bCopy.Compile.NodeScratchReserved = 0, 0
	aCopy.Compile.NodeScratchPeak, bCopy.Compile.NodeScratchPeak = 0, 0
	aCopy.Compile.NodeScratchRetained, bCopy.Compile.NodeScratchRetained = 0, 0
	aCopy.Compile.NodeScratchDiscarded, bCopy.Compile.NodeScratchDiscarded = 0, 0
	aCopy.Compile.ControlScratchReserved, bCopy.Compile.ControlScratchReserved = 0, 0
	aCopy.Compile.ControlScratchPeak, bCopy.Compile.ControlScratchPeak = 0, 0
	aCopy.Compile.ControlScratchRetained, bCopy.Compile.ControlScratchRetained = 0, 0
	aCopy.Compile.ControlScratchDiscarded, bCopy.Compile.ControlScratchDiscarded = 0, 0
	for i := range aCopy.Funcs {
		if aCopy.Funcs[i] == nil || bCopy.Funcs[i] == nil {
			continue
		}
		aFunc, bFunc := *aCopy.Funcs[i], *bCopy.Funcs[i]
		aFunc.CompileNanos, bFunc.CompileNanos = 0, 0
		aCopy.Funcs[i], bCopy.Funcs[i] = &aFunc, &bFunc
	}
	return reflect.DeepEqual(&aCopy, &bCopy)
}

func BenchmarkCompileModuleCompactionAMD64(b *testing.B) {
	corpus := filepath.Join("..", "..", "..", "..", "..", "..", "bench", "corpus")
	for _, name := range []string{"many_funcs.wasm", "json-as.wasm"} {
		m := readParallelTestModule(b, filepath.Join(corpus, name))
		b.Run(name, func(b *testing.B) {
			for _, compact := range []bool{false, true} {
				label := "off"
				if compact {
					label = "on"
				}
				b.Run(label, func(b *testing.B) {
					before := nativeCompactionEnabled
					nativeCompactionEnabled = compact
					b.Cleanup(func() { nativeCompactionEnabled = before })
					b.ReportAllocs()
					for i := 0; i < b.N; i++ {
						cm, err := CompileModuleWith(m, CompileOptions{Workers: 1})
						if err != nil {
							b.Fatal(err)
						}
						if cm.CodeImage != nil {
							if err := cm.CodeImage.Close(); err != nil {
								b.Fatal(err)
							}
						}
					}
				})
			}
		})
	}
}

func TestCompileWorkersCorpusParity(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping whole-corpus compiler parity in short mode")
	}
	corpus := filepath.Join("..", "..", "..", "..", "..", "..", "bench", "corpus")
	for _, name := range []string{
		"tiny.wasm", "fib_rec.wasm", "many_funcs.wasm",
		"json-as.wasm", "blake-as.wasm", "lua.wasm", "sqlite3.wasm",
		"ruby.wasm", "esbuild.wasm",
	} {
		t.Run(name, func(t *testing.T) {
			m := readParallelTestModule(t, filepath.Join(corpus, name))
			serial, err := CompileModuleWith(m, CompileOptions{Workers: 1})
			if err != nil {
				t.Fatalf("serial compile: %v", err)
			}
			parallel, err := CompileModuleWith(m, CompileOptions{Workers: 8})
			if err != nil {
				t.Fatalf("parallel compile: %v", err)
			}
			assertCompiledModuleEqual(t, parallel, serial)
		})
	}
}

func readParallelTestModule(t testing.TB, path string) *wasm.Module {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m, err := frontend.DecodeValidate(data)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func compileWorkerTestModule(t *testing.T, m *wasm.Module, workers int) (*encoder.CompiledModule, *ModuleStats) {
	return compileWorkerTestModuleCompact(t, m, workers, false)
}

func compileWorkerTestModuleCompact(t *testing.T, m *wasm.Module, workers int, compact bool) (*encoder.CompiledModule, *ModuleStats) {
	t.Helper()
	stats := &ModuleStats{}
	cm, err := CompileModuleWith(m, CompileOptions{Workers: workers, Stats: stats, CompactNative: compact})
	if err != nil {
		t.Fatalf("workers=%d: compile: %v", workers, err)
	}
	return cm, stats
}

func assertCompiledModuleEqual(t *testing.T, got, want *encoder.CompiledModule) {
	t.Helper()
	if len(got.Code) != len(want.Code) {
		t.Fatalf("code length = %d, want %d", len(got.Code), len(want.Code))
	}
	if !bytes.Equal(got.Code, want.Code) {
		for i := range got.Code {
			if got.Code[i] != want.Code[i] {
				t.Fatalf("code differs at byte %d: got %#02x want %#02x", i, got.Code[i], want.Code[i])
			}
		}
		t.Fatal("code differs")
	}
	if !reflect.DeepEqual(got.Entry, want.Entry) {
		t.Fatalf("Entry differs\n got: %v\nwant: %v", got.Entry, want.Entry)
	}
	if !reflect.DeepEqual(got.InternalEntry, want.InternalEntry) {
		t.Fatalf("InternalEntry differs\n got: %v\nwant: %v", got.InternalEntry, want.InternalEntry)
	}
	if !reflect.DeepEqual(got.DirectPrepared, want.DirectPrepared) {
		t.Fatalf("DirectPrepared differs\n got: %v\nwant: %v", got.DirectPrepared, want.DirectPrepared)
	}
	if got.PreparedIsolatedTables != want.PreparedIsolatedTables {
		t.Fatalf("PreparedIsolatedTables = %v, want %v", got.PreparedIsolatedTables, want.PreparedIsolatedTables)
	}
}

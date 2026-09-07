//go:build (linux || darwin) && arm64

package arm64

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
	encoder "github.com/wago-org/wago/src/core/encoder/arm64"
)

var parallelStackOutlierSinkArm64 *encoder.CompiledModule

func BenchmarkRailshotCompileParallelStackOutliersArm64(b *testing.B) {
	body := make([]byte, 1, 1+(64<<10)*3+1) // no locals
	for range 64 << 10 {
		body = append(body, 0x41, 0x00, 0x1a) // i32.const 0; drop
	}
	body = append(body, 0x0b)
	defs := make([]funcDef, 8)
	for i := range defs {
		defs[i].body = body
	}
	m := modFuncs(b, defs...)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		cm, err := CompileModuleWith(m, CompileOptions{Workers: 4})
		if err != nil {
			b.Fatal(err)
		}
		parallelStackOutlierSinkArm64 = cm
	}
}

func TestParallelFuncResultSizeArm64(t *testing.T) {
	if got, want := unsafe.Sizeof(funcResult{}), uintptr(48); got != want {
		t.Fatalf("funcResult size = %d, want %d", got, want)
	}
}

func TestCompactFuncResultRangeArm64(t *testing.T) {
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

func TestCompileWorkersDeterministicArm64(t *testing.T) {
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
			m := readParallelTestModuleArm64(t, filepath.Join(corpus, name))
			want, wantStats := compileWorkerTestModuleArm64(t, m, 1)
			for _, workers := range []int{2, 4, 8} {
				for repeat := 0; repeat < 5; repeat++ {
					got, gotStats := compileWorkerTestModuleArm64(t, m, workers)
					assertCompiledModuleEqualArm64(t, got, want)
					if !equalWorkerModuleStatsARM64(gotStats, wantStats) {
						t.Fatalf("workers=%d repeat=%d: stats differ\n got: %#v\nwant: %#v", workers, repeat, gotStats, wantStats)
					}
				}
			}
		})
	}
}

func TestCompileWorkersSizeSharedAdaptersDeterministicArm64(t *testing.T) {
	corpus := filepath.Join("..", "..", "..", "..", "..", "..", "bench", "corpus")
	for _, name := range []string{"many_funcs.wasm", "json-as-simd.wasm"} {
		t.Run(name, func(t *testing.T) {
			m := readParallelTestModuleArm64(t, filepath.Join(corpus, name))
			want, wantStats := compileWorkerTestModuleCompactArm64(t, m, 1, true)
			got, gotStats := compileWorkerTestModuleCompactArm64(t, m, 4, true)
			assertCompiledModuleEqualArm64(t, got, want)
			if !equalWorkerModuleStatsARM64(gotStats, wantStats) {
				t.Fatalf("Size stats differ\n got: %#v\nwant: %#v", gotStats, wantStats)
			}
		})
	}
}

func equalWorkerModuleStatsARM64(a, b *ModuleStats) bool {
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

func readParallelTestModuleArm64(t testing.TB, path string) *wasm.Module {
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

func BenchmarkCompileModuleCompactionArm64(b *testing.B) {
	corpus := filepath.Join("..", "..", "..", "..", "..", "..", "bench", "corpus")
	for _, name := range []string{"many_funcs.wasm", "json-as.wasm"} {
		m := readParallelTestModuleArm64(b, filepath.Join(corpus, name))
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

func compileWorkerTestModuleArm64(t *testing.T, m *wasm.Module, workers int) (*encoder.CompiledModule, *ModuleStats) {
	return compileWorkerTestModuleCompactArm64(t, m, workers, false)
}

func compileWorkerTestModuleCompactArm64(t *testing.T, m *wasm.Module, workers int, compact bool) (*encoder.CompiledModule, *ModuleStats) {
	t.Helper()
	stats := &ModuleStats{}
	cm, err := CompileModuleWith(m, CompileOptions{Workers: workers, Stats: stats, CompactNative: compact})
	if err != nil {
		t.Fatalf("workers=%d: compile: %v", workers, err)
	}
	return cm, stats
}

func assertCompiledModuleEqualArm64(t *testing.T, got, want *encoder.CompiledModule) {
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
	if got.PreparedIsolatedTables != want.PreparedIsolatedTables {
		t.Fatalf("PreparedIsolatedTables = %v, want %v", got.PreparedIsolatedTables, want.PreparedIsolatedTables)
	}
}

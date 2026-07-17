//go:build (linux || darwin) && arm64

package arm64

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/arm64"
)

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
					if !reflect.DeepEqual(gotStats, wantStats) {
						t.Fatalf("workers=%d repeat=%d: stats differ\n got: %#v\nwant: %#v", workers, repeat, gotStats, wantStats)
					}
				}
			}
		})
	}
}

func TestCompileWorkersLowestIndexErrorArm64(t *testing.T) {
	results := make([]funcResult, 8)
	results[7].err = errors.New("late index")
	results[2].err = errors.New("first index")
	idx, err := firstFuncError(results)
	if idx != 2 || err == nil || err.Error() != "first index" {
		t.Fatalf("firstFuncError = (%d, %v), want (2, first index)", idx, err)
	}
}

func readParallelTestModuleArm64(t *testing.T, path string) *wasm.Module {
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

func compileWorkerTestModuleArm64(t *testing.T, m *wasm.Module, workers int) (*encoder.CompiledModule, *ModuleStats) {
	t.Helper()
	stats := &ModuleStats{}
	cm, err := CompileModuleWith(m, CompileOptions{Workers: workers, Stats: stats})
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
}

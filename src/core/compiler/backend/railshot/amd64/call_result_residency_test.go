//go:build (linux || darwin) && amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoderamd64 "github.com/wago-org/wago/src/core/encoder/amd64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func callResultResidencyModule(t testing.TB, calls int) *wasm.Module {
	t.Helper()
	body := []byte{0x00, 0x41, 0x01}
	for range calls {
		body = append(body, 0x10, 0x01)
	}
	body = append(body, 0x0b)
	return modFuncs(t,
		funcDef{results: []wasm.ValType{wasm.I32}, body: body},
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}},
	)
}

func TestCallResultResidencyAMD64(t *testing.T) {
	compile := func(m *wasm.Module, on bool, workers int) (*encoderamd64.CompiledModule, CodegenStats) {
		var ms ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{
			Workers: workers,
			Optimizations: map[string]bool{
				"call-result-residency": on,
				"inline":                false,
			},
			Stats: &ms,
		})
		if err != nil {
			t.Fatal(err)
		}
		return cm, *ms.Funcs[0]
	}

	m := callResultResidencyModule(t, 16)
	off, offStats := compile(m, false, 1)
	on, onStats := compile(m, true, 1)
	if got := runCompiledAmd64u(t, off); got != 17 {
		t.Fatalf("disabled result = %d, want 17", got)
	}
	if got := runCompiledAmd64u(t, on); got != 17 {
		t.Fatalf("enabled result = %d, want 17", got)
	}
	if got := onStats.Peephole["call-result-resident"]; got != 16 {
		t.Fatalf("call-result-resident hits = %d, want 16", got)
	}
	if got := onStats.CallTraffic.RegisterResultMoves; got != 0 {
		t.Fatalf("resident result moves = %d, want 0", got)
	}
	if got := offStats.CallTraffic.RegisterResultMoves; got != 16 {
		t.Fatalf("fallback result moves = %d, want 16", got)
	}
	if onStats.CodeBytes >= offStats.CodeBytes {
		t.Fatalf("code bytes off=%d on=%d, want reduction", offStats.CodeBytes, onStats.CodeBytes)
	}
	parallel, _ := compile(m, true, 2)
	if !bytes.Equal(on.Code, parallel.Code) {
		t.Fatal("serial and parallel result-residency code differ")
	}
}

func TestCallPairResultResidencyAMD64(t *testing.T) {
	m := modFuncs(t,
		funcDef{results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x41, 0x07, 0x10, 0x01, 0x6a, 0x0b}},
		funcDef{
			params:  []wasm.ValType{wasm.I32},
			results: []wasm.ValType{wasm.I32, wasm.I32},
			body:    []byte{0x00, 0x20, 0x00, 0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b},
		},
	)
	var ms ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{
		Optimizations: map[string]bool{"call-result-residency": true, "inline": false},
		Stats:         &ms,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := runCompiledAmd64u(t, cm); got != 15 {
		t.Fatalf("pair result = %d, want 15", got)
	}
	if got := ms.Funcs[0].Peephole["call-result-resident"]; got != 2 {
		t.Fatalf("call-result-resident hits = %d, want 2", got)
	}
}

func TestCallMixedResultResidencyAMD64(t *testing.T) {
	m := modFuncs(t,
		funcDef{results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x41, 0x07, 0x10, 0x01, 0x1a, 0x0b}},
		funcDef{
			params:  []wasm.ValType{wasm.I32},
			results: []wasm.ValType{wasm.I32, wasm.F64},
			body:    []byte{0x00, 0x20, 0x00, 0x44, 0, 0, 0, 0, 0, 0, 0, 0, 0x0b},
		},
	)
	var ms ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{
		Optimizations: map[string]bool{"call-result-residency": true, "inline": false},
		Stats:         &ms,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := runCompiledAmd64u(t, cm); got != 7 {
		t.Fatalf("mixed result = %d, want 7", got)
	}
	if got := ms.Funcs[0].Peephole["call-result-resident"]; got != 1 {
		t.Fatalf("call-result-resident hits = %d, want 1", got)
	}
}

func TestCallResultResidencySelfRecursiveFallbackAMD64(t *testing.T) {
	m := modFuncs(t, funcDef{results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x10, 0x00, 0x0b}})
	var ms ModuleStats
	if _, err := CompileModuleWith(m, CompileOptions{
		Optimizations: map[string]bool{"call-result-residency": true, "inline": false},
		Stats:         &ms,
	}); err != nil {
		t.Fatal(err)
	}
	if got := ms.Funcs[0].Peephole["call-result-resident"]; got != 0 {
		t.Fatalf("self-recursive residency hits = %d, want 0", got)
	}
	if got := ms.Funcs[0].CallTraffic.RegisterResultMoves; got != 1 {
		t.Fatalf("self-recursive result moves = %d, want fallback move", got)
	}
}

func BenchmarkCallResultResidencyAMD64(b *testing.B) {
	m := callResultResidencyModule(b, 16)
	for _, tc := range []struct {
		name string
		on   bool
	}{{"copied", false}, {"resident", true}} {
		b.Run(tc.name, func(b *testing.B) {
			var ms ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{
				Optimizations: map[string]bool{"call-result-residency": tc.on, "inline": false},
				Stats:         &ms,
			})
			if err != nil {
				b.Fatal(err)
			}
			eng, err := coreruntime.NewEngine()
			if err != nil {
				b.Fatal(err)
			}
			defer eng.Close()
			jm, err := coreruntime.NewJobMemory(65536)
			if err != nil {
				b.Fatal(err)
			}
			defer jm.Close()
			arena, err := coreruntime.NewArena(4096)
			if err != nil {
				b.Fatal(err)
			}
			defer arena.Close()
			code, entry, err := coreruntime.MapCode(cm.Code)
			if err != nil {
				b.Fatal(err)
			}
			defer coreruntime.Unmap(code)
			args, results, trap := arena.Alloc(8), arena.Alloc(8), arena.Alloc(8)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(ms.Funcs[0].CodeBytes), "native-bytes")
			if got := binary.LittleEndian.Uint64(results); got != 17 {
				b.Fatalf("result = %d, want 17", got)
			}
		})
	}
}

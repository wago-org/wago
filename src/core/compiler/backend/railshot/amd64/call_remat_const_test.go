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

func callConstRematModule(t testing.TB, zeroArg bool) *wasm.Module {
	t.Helper()
	if zeroArg {
		return modFuncs(t,
			funcDef{results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x41, 0x07, 0x10, 0x01, 0x6a, 0x0b}},
			funcDef{results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x41, 0x05, 0x0b}},
		)
	}
	return modFuncs(t,
		funcDef{results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x41, 0x07, 0x41, 0x05, 0x10, 0x01, 0x6a, 0x0b}},
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x20, 0x00, 0x0b}},
	)
}

func BenchmarkCallConstRematerializationAMD64(b *testing.B) {
	body := []byte{0x00, 0x41, 0x00}
	for range 32 {
		body = append(body, 0x41, 0x07, 0x41, 0x05, 0x10, 0x01, 0x6a, 0x6a)
	}
	body = append(body, 0x0b)
	m := modFuncs(b,
		funcDef{results: []wasm.ValType{wasm.I32}, body: body},
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x20, 0x00, 0x0b}},
	)
	for _, tc := range []struct {
		name string
		on   bool
	}{{"stored", false}, {"rematerialized", true}} {
		b.Run(tc.name, func(b *testing.B) {
			var ms ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{"call-remat-const": tc.on, "inline": false}, Stats: &ms})
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
			if got := binary.LittleEndian.Uint64(results); got != 384 {
				b.Fatalf("result = %d, want 384", got)
			}
		})
	}
}

func TestCallConstRematerializationAMD64(t *testing.T) {
	m := callConstRematModule(t, false)
	compile := func(on bool, workers int) (*encoderamd64.CompiledModule, CodegenStats) {
		var ms ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{
			Workers: workers,
			Optimizations: map[string]bool{
				"call-remat-const": on,
				"inline":           false,
			},
			Stats: &ms,
		})
		if err != nil {
			t.Fatal(err)
		}
		return cm, *ms.Funcs[0]
	}
	off, offStats := compile(false, 1)
	on, onStats := compile(true, 1)
	if got := runCompiledAmd64u(t, off); got != 12 {
		t.Fatalf("disabled result = %d, want 12", got)
	}
	if got := runCompiledAmd64u(t, on); got != 12 {
		t.Fatalf("enabled result = %d, want 12", got)
	}
	if got := onStats.Peephole["call-remat-const"]; got != 1 {
		t.Fatalf("call-remat-const hits = %d, want 1", got)
	}
	if onStats.CodeBytes >= offStats.CodeBytes {
		t.Fatalf("code bytes off=%d on=%d, want reduction", offStats.CodeBytes, onStats.CodeBytes)
	}
	parallel, _ := compile(true, 2)
	if !bytes.Equal(on.Code, parallel.Code) {
		t.Fatal("serial and parallel rematerialization code differ")
	}
}

func TestCallConstRematerializationZeroArgFallbackAMD64(t *testing.T) {
	m := callConstRematModule(t, true)
	var offStats, onStats ModuleStats
	off, err := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{"call-remat-const": false, "inline": false}, Stats: &offStats})
	if err != nil {
		t.Fatal(err)
	}
	on, err := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{"call-remat-const": true, "inline": false}, Stats: &onStats})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(off.Code, on.Code) {
		t.Fatal("zero-argument fallback changed code")
	}
	if got := onStats.Funcs[0].Peephole["call-remat-const"]; got != 0 {
		t.Fatalf("zero-argument hits = %d, want 0", got)
	}
}

func TestCallConstRematerializationMixedAMD64(t *testing.T) {
	caller := []byte{0x00, 0x41, 0x07, 0x44, 0, 0, 0, 0, 0, 0, 0, 0, 0x41, 0x05, 0x10, 0x01, 0x6a, 0x0b}
	m := modFuncs(t,
		funcDef{results: []wasm.ValType{wasm.I32}, body: caller},
		funcDef{params: []wasm.ValType{wasm.F64, wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x20, 0x01, 0x0b}},
	)
	var ms ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{"call-remat-const": true, "inline": false}, Stats: &ms})
	if err != nil {
		t.Fatal(err)
	}
	if got := runCompiledAmd64u(t, cm); got != 12 {
		t.Fatalf("mixed result = %d, want 12", got)
	}
	if got := ms.Funcs[0].Peephole["call-remat-const"]; got != 1 {
		t.Fatalf("mixed hits = %d, want 1", got)
	}
}

func TestCallConstRematerializationEHPolicyFallbackAMD64(t *testing.T) {
	f := fn{policy: currentCodegenPolicy(), moduleEH: true}
	roots := []*elem{
		{kind: ekValue, st: storage{kind: stConst, typ: mtI32, cval: 7}},
		{kind: ekValue, st: storage{kind: stConst, typ: mtI32, cval: 5}},
	}
	if got := f.planCallConstRemat(roots, 1); got.root != nil {
		t.Fatal("EH module admitted call rematerialization")
	}
}

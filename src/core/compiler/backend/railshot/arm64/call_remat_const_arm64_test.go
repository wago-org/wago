//go:build (linux || darwin) && arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
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

func BenchmarkCallConstRematerializationArm64(b *testing.B) {
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

func TestCallConstRematerializationArm64(t *testing.T) {
	m := callConstRematModule(t, false)
	compile := func(on bool, workers int) (*a64.CompiledModule, CodegenStats) {
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
	_, offStats := compile(false, 1)
	on, onStats := compile(true, 1)
	if got, err := runArm64WrapperWithOptions(t, m, CompileOptions{Optimizations: map[string]bool{"call-remat-const": false, "inline": false}}); err != nil || got != 12 {
		t.Fatalf("disabled result = %d, err=%v, want 12", got, err)
	}
	if got, err := runArm64WrapperWithOptions(t, m, CompileOptions{Optimizations: map[string]bool{"call-remat-const": true, "inline": false}}); err != nil || got != 12 {
		t.Fatalf("enabled result = %d, err=%v, want 12", got, err)
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

func TestCallConstRematerializationZeroArgFallbackArm64(t *testing.T) {
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

func TestCallConstRematerializationMixedArm64(t *testing.T) {
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
	if got, err := runArm64WrapperWithOptions(t, m, CompileOptions{Optimizations: map[string]bool{"call-remat-const": true, "inline": false}}); err != nil || got != 12 {
		t.Fatalf("mixed result = %d, err=%v, want 12", got, err)
	}
	if len(cm.Code) == 0 || ms.Funcs[0].Peephole["call-remat-const"] != 1 {
		t.Fatalf("mixed code=%d hits=%d, want nonempty/1", len(cm.Code), ms.Funcs[0].Peephole["call-remat-const"])
	}
}

func TestCallConstRematerializationEHPolicyFallbackArm64(t *testing.T) {
	f := fn{policy: currentCodegenPolicy(), moduleEH: true}
	roots := []*elem{
		{kind: ekValue, st: storage{kind: stConst, typ: mtI32, cval: 7}},
		{kind: ekValue, st: storage{kind: stConst, typ: mtI32, cval: 5}},
	}
	if got := f.planCallRemat(roots, 1, false); got.root != nil {
		t.Fatal("EH module admitted call rematerialization")
	}
}

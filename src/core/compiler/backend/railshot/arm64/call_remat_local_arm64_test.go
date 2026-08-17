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

func callLocalRematModule(t testing.TB) *wasm.Module {
	t.Helper()
	return modFuncs(t,
		funcDef{results: []wasm.ValType{wasm.I32}, body: []byte{
			0x01, 0x01, 0x7f, // one i32 local
			0x41, 0x07, 0x21, 0x00, // local = 7
			0x20, 0x00, 0x41, 0x05, 0x10, 0x01, // local, call id(5)
			0x41, 0x09, 0x21, 0x00, // overwrite local after the call
			0x6a, 0x0b,
		}},
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x20, 0x00, 0x0b}},
	)
}

func BenchmarkCallLocalRematerializationArm64(b *testing.B) {
	body := []byte{0x01, 0x01, 0x7f, 0x41, 0x01, 0x21, 0x00}
	for range 16 {
		body = append(body, 0x20, 0x00, 0x20, 0x00, 0x10, 0x01, 0x6a, 0x21, 0x00)
	}
	body = append(body, 0x20, 0x00, 0x0b)
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
			cm, err := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{"call-remat-local": tc.on, "inline": false}, Stats: &ms})
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
			if got := binary.LittleEndian.Uint64(results); got != 65536 {
				b.Fatalf("result = %d, want 65536", got)
			}
		})
	}
}

func TestCallLocalRematerializationArm64(t *testing.T) {
	m := callLocalRematModule(t)
	compile := func(on bool, workers int) (*a64.CompiledModule, CodegenStats) {
		var ms ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{
			Workers: workers,
			Optimizations: map[string]bool{
				"call-remat-local": on,
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
	if got, err := runArm64WrapperWithOptions(t, m, CompileOptions{Optimizations: map[string]bool{"call-remat-local": false, "inline": false}}); err != nil || got != 12 {
		t.Fatalf("disabled result = %d, err=%v, want 12", got, err)
	}
	if got, err := runArm64WrapperWithOptions(t, m, CompileOptions{Optimizations: map[string]bool{"call-remat-local": true, "inline": false}}); err != nil || got != 12 {
		t.Fatalf("enabled result = %d, err=%v, want 12", got, err)
	}
	if got := onStats.Peephole["call-remat-local"]; got != 1 {
		t.Fatalf("call-remat-local hits = %d, want 1", got)
	}
	if onStats.CodeBytes >= offStats.CodeBytes {
		t.Fatalf("code bytes off=%d on=%d, want reduction", offStats.CodeBytes, onStats.CodeBytes)
	}
	parallel, _ := compile(true, 2)
	if !bytes.Equal(on.Code, parallel.Code) {
		t.Fatal("serial and parallel rematerialization code differ")
	}
}

func TestCallLocalRematerializationFrameRefArm64(t *testing.T) {
	f := fn{policy: currentCodegenPolicy()}
	roots := []*elem{
		{kind: ekValue, st: storage{kind: stLocalRef, typ: mtI64, idx: 3}},
		{kind: ekValue, st: storage{kind: stConst, typ: mtI32, cval: 5}},
	}
	r := f.planCallRemat(roots, 1, false)
	if r.root == nil || r.st.kind != stLocalRef || r.st.idx != 3 {
		t.Fatalf("recipe = %+v, want local 3 frame reference", r)
	}
}

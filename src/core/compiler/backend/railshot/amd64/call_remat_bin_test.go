//go:build (linux || darwin) && amd64

package amd64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoderamd64 "github.com/wago-org/wago/src/core/encoder/amd64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func callBinRematModule(t testing.TB) *wasm.Module {
	t.Helper()
	body := []byte{0x00}
	for x := byte(0); x < 10; x++ {
		body = append(body, 0x20, x, 0x1a, 0x20, x, 0x1a)
	}
	body = append(body,
		0x20, 0x0a, 0x20, 0x0b, 0x6a, // local 10 + local 11
		0x41, 0x05, 0x10, 0x01, // call id(5)
		0x6a, 0x0b,
	)
	params := make([]wasm.ValType, 12)
	for i := range params {
		params[i] = wasm.I32
	}
	return modFuncs(t,
		funcDef{params: params, results: []wasm.ValType{wasm.I32}, body: body},
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x20, 0x00, 0x0b}},
	)
}

func BenchmarkCallBinaryRematerializationAMD64(b *testing.B) {
	m := callBinRematModule(b)
	for _, tc := range []struct {
		name string
		on   bool
	}{{"stored", false}, {"rematerialized", true}} {
		b.Run(tc.name, func(b *testing.B) {
			var ms ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{"call-remat-bin": tc.on, "inline": false}, Stats: &ms})
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
			args, results, trap := arena.Alloc(12*8), arena.Alloc(8), arena.Alloc(8)
			args[10*8], args[11*8] = 7, 3
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(ms.Funcs[0].CodeBytes), "native-bytes")
			if results[0] != 15 {
				b.Fatalf("result = %d, want 15", results[0])
			}
		})
	}
}

func TestCallBinaryRematerializationAMD64(t *testing.T) {
	m := callBinRematModule(t)
	compile := func(on bool, workers int) (*encoderamd64.CompiledModule, CodegenStats) {
		var ms ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{
			Workers: workers,
			Optimizations: map[string]bool{
				"call-remat-bin": on,
				"inline":         false,
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
	args := make([]uint64, 12)
	args[10], args[11] = 7, 3
	if got := runCompiledAmd64u(t, off, args...); got != 15 {
		t.Fatalf("disabled result = %d, want 15", got)
	}
	if got := runCompiledAmd64u(t, on, args...); got != 15 {
		t.Fatalf("enabled result = %d, want 15", got)
	}
	if got := onStats.Peephole["call-remat-bin"]; got != 1 {
		t.Fatalf("call-remat-bin hits = %d, want 1 (all: %v)", got, onStats.Peephole)
	}
	if onStats.CodeBytes >= offStats.CodeBytes {
		t.Fatalf("code bytes off=%d on=%d, want reduction", offStats.CodeBytes, onStats.CodeBytes)
	}
	parallel, _ := compile(true, 2)
	if !bytes.Equal(on.Code, parallel.Code) {
		t.Fatal("serial and parallel rematerialization code differ")
	}
}

func TestCallBinaryRematerializationBoundedFallbackAMD64(t *testing.T) {
	f := fn{policy: currentCodegenPolicy()}
	local := &elem{kind: ekValue, st: storage{kind: stLocalRef, typ: mtI32, idx: 0}}
	reg := &elem{kind: ekValue, st: storage{kind: stLocalReg, typ: mtI32, reg: RAX, idx: 1}}
	inner := &elem{kind: ekDeferred, op: opAdd, typ: mtI32, arg0: local, arg1: local}
	arg := &elem{kind: ekValue, st: storage{kind: stConst, typ: mtI32, cval: 5}}
	for _, root := range []*elem{
		{kind: ekDeferred, op: opAdd, typ: mtI32, arg0: local, arg1: reg},
		{kind: ekDeferred, op: opAdd, typ: mtI32, arg0: inner, arg1: local},
	} {
		if got := f.planCallRemat([]*elem{root, arg}, 1, false); got.root != nil {
			t.Fatalf("unbounded/register recipe admitted: %+v", got)
		}
	}
}

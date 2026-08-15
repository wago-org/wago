//go:build (linux || darwin) && amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoderamd64 "github.com/wago-org/wago/src/core/encoder/amd64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func deferredCallArgumentModule(t testing.TB, calls int) *wasm.Module {
	t.Helper()
	body := []byte{0x00}
	for i := 0; i < calls; i++ {
		body = append(body, 0x20, 0x00, 0x41, 0x01, 0x6a, 0x10, 0x01)
		if i+1 != calls {
			body = append(body, 0x1a)
		}
	}
	body = append(body, 0x0b)
	return modFuncs(t,
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: body},
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x20, 0x00, 0x41, 0x02, 0x6c, 0x0b}},
	)
}

func TestDeferredCallArgumentDirectAMD64(t *testing.T) {
	m := deferredCallArgumentModule(t, 1)
	compile := func(on bool, workers int) (*encoderamd64.CompiledModule, *CodegenStats) {
		var ms ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{Workers: workers, Optimizations: map[string]bool{"call-arg-direct": on, "inline": false}, Stats: &ms})
		if err != nil {
			t.Fatal(err)
		}
		return cm, ms.Funcs[0]
	}
	off, offStats := compile(false, 1)
	on, onStats := compile(true, 1)
	if got := runCompiledAmd64u(t, off, 7); got != 16 {
		t.Fatalf("disabled result = %d, want 16", got)
	}
	if got := runCompiledAmd64u(t, on, 7); got != 16 {
		t.Fatalf("enabled result = %d, want 16", got)
	}
	if onStats.Peephole["call-arg-direct"] != 1 || onStats.CallTraffic.IntegerCallArgumentMoves != 0 {
		t.Fatalf("enabled direct argument stats = peep %v traffic %+v", onStats.Peephole, onStats.CallTraffic)
	}
	if offStats.CallTraffic.IntegerCallArgumentMoves != 1 || onStats.CodeBytes >= offStats.CodeBytes {
		t.Fatalf("fallback/direct moves=%d/%d bytes=%d/%d", offStats.CallTraffic.IntegerCallArgumentMoves, onStats.CallTraffic.IntegerCallArgumentMoves, offStats.CodeBytes, onStats.CodeBytes)
	}
	parallel, _ := compile(true, 2)
	if !bytes.Equal(on.Code, parallel.Code) {
		t.Fatal("serial and parallel direct-argument code differ")
	}
}

func TestDeferredCallArgumentDirectPhysicalNearMissesAMD64(t *testing.T) {
	root := &elem{kind: ekDeferred, op: opAdd, typ: mtI32}
	selection, err := optimizationBindings.Resolve(map[string]bool{"call-arg-direct": true})
	if err != nil {
		t.Fatal(err)
	}
	f := fn{policy: shared.DefaultCodegenPolicy(selection)}
	if !f.canCondenseCallArgTo(root, RAX) {
		t.Fatal("free ABI target was rejected")
	}
	for _, tc := range []struct {
		name  string
		block func()
	}{
		{"occupied", func() { f.regUser[RAX] = &elem{} }},
		{"pinned", func() { f.pinned = f.pinned.add(RAX) }},
		{"local-pin", func() { f.pinnedLocalMask = f.pinnedLocalMask.add(RAX) }},
		{"reserved", func() { f.reserved = f.reserved.add(RAX) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f.regUser[RAX], f.pinned, f.pinnedLocalMask, f.reserved = nil, 0, 0, 0
			tc.block()
			if f.canCondenseCallArgTo(root, RAX) {
				t.Fatal("blocked ABI target was admitted")
			}
		})
	}
}

func BenchmarkDeferredCallArgumentDirectAMD64(b *testing.B) {
	m := deferredCallArgumentModule(b, 16)
	for _, tc := range []struct {
		name string
		on   bool
	}{{"copied", false}, {"direct", true}} {
		b.Run(tc.name, func(b *testing.B) {
			var ms ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{"call-arg-direct": tc.on, "inline": false}, Stats: &ms})
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
			binary.LittleEndian.PutUint64(args, 7)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if err := eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(ms.Funcs[0].CodeBytes), "native-bytes")
			if got := binary.LittleEndian.Uint64(results); got != 16 {
				b.Fatalf("result = %d, want 16", got)
			}
		})
	}
}

func BenchmarkDeferredCallArgumentDirectCompileAMD64(b *testing.B) {
	m := deferredCallArgumentModule(b, 16)
	for _, tc := range []struct {
		name string
		on   bool
	}{{"copied", false}, {"direct", true}} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				if _, err := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{"call-arg-direct": tc.on, "inline": false}}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

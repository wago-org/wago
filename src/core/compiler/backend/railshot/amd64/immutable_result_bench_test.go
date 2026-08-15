//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/wasmtest"
)

func immutableIndirectResultModule(t testing.TB, calls int) *wasm.Module {
	t.Helper()
	call := []byte{0x20, 0x00, 0x41, 0x00, 0x11, 0x00, 0x00}
	body := make([]byte, 0, calls*6+1)
	for i := 0; i < calls; i++ {
		body = append(body, call...)
		if i+1 != calls {
			body = append(body, 0x1a)
		}
	}
	body = append(body, 0x0b)
	elem := []byte{0x00, 0x41, 0x00, 0x0b, 0x02, 0x01, 0x02}
	raw := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x02})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(9, wasmtest.Vec(elem)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(body),
			wasmtest.Code([]byte{0x20, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}),
		)),
	)
	m, err := wasm.DecodeModule(raw)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestImmutableIndirectResultModuleExec(t *testing.T) {
	out, err := runIndirectTail(t, immutableIndirectResultModule(t, 64), []int{1, 2}, 7)
	if err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(out); got != 7 {
		t.Fatalf("result = %d, want 7", got)
	}
}

func BenchmarkImmutableIndirectResultResidencyAMD64(b *testing.B) {
	m := immutableIndirectResultModule(b, 64)
	for _, tc := range []struct {
		name string
		on   bool
	}{{"copied", false}, {"resident", true}} {
		b.Run(tc.name, func(b *testing.B) {
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{"call-result-residency": tc.on, "inline": false}})
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
			code, base, err := coreruntime.MapCode(cm.Code)
			if err != nil {
				b.Fatal(err)
			}
			defer coreruntime.Unmap(code)
			table := arena.Alloc(8 + 2*coreruntime.TableEntryBytes)
			binary.LittleEndian.PutUint32(table, 2)
			binary.LittleEndian.PutUint32(table[4:], 2)
			for i, fidx := range []int{1, 2} {
				off := 8 + i*coreruntime.TableEntryBytes
				binary.LittleEndian.PutUint64(table[off+coreruntime.TableEntryCodePtrOffset:], uint64(base)+uint64(cm.InternalEntry[fidx]))
				binary.LittleEndian.PutUint64(table[off+coreruntime.TableEntrySigKeyOffset:], m.StructuralTypeKey(m.FuncTypes[fidx].Index))
				binary.LittleEndian.PutUint64(table[off+coreruntime.TableEntryHomeLinMemOffset:], uint64(jm.LinMemBase())|uint64(1)<<63)
			}
			jm.SetTablePtr(uintptr(unsafe.Pointer(&table[0])))
			args, results, trap := arena.Alloc(128), arena.Alloc(128), arena.Alloc(8)
			binary.LittleEndian.PutUint64(args, 7)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if err := eng.Call(base+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(stats.Funcs[0].CodeBytes), "native-bytes")
			if got := binary.LittleEndian.Uint32(results); got != 7 {
				b.Fatalf("result = %d, want 7", got)
			}
		})
	}
}

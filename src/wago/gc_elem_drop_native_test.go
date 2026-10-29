//go:build linux && (amd64 || arm64) && !tinygo

package wago

import (
	"encoding/binary"
	"fmt"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcElemDropModule(imported bool) []byte {
	body := func(code []byte) []byte { return wasmtest.Code(append(code, 0x0b)) }
	bulk := func(op, index byte) []byte { return []byte{0xfc, op, index} }
	init := func(index, count byte) []byte { return []byte{0x41, 0, 0x41, 0, 0x41, count, 0xfc, 12, index, 0} }
	loop := []byte{1, 1, 0x7f, 0x02, 0x40, 0x03, 0x40, 0x20, 2, 0x20, 0, 0x4f, 0x0d, 1, 0x20, 1, 0x45, 0x20, 1, 0x41, 2, 0x46, 0x20, 2, 0x41, 1, 0x71, 0x45, 0x71, 0x72, 0x04, 0x40, 0xfc, 13, 1, 0x05, 0xfc, 13, 0, 0x0b, 0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x0c, 0, 0x0b, 0x0b, 0x20, 2, 0x0b}
	loop = append(wasmtest.ULEB(uint32(len(loop))), loop...)
	live := []byte{1, 1, 0x7e, 0x20, 0, 0x42, 1, 0x7c, 0x22, 1, 0xfc, 13, 1, 0x20, 1, 0x7c, 0x0b}
	live = append(wasmtest.ULEB(uint32(len(live))), live...)
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x7f, 1}, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, nil), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64})))}
	if imported {
		entry := append(wasmtest.Name("env"), wasmtest.Name("table")...)
		entry = append(entry, 1, 0x70, 0, 1)
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(entry)))
	}
	sections = append(sections, tableTestFuncSection(1, 3, 2, 2, 2, 2, 4, 2, 2, 2, 5, 2))
	if !imported {
		sections = append(sections, wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0, 1})))
	}
	sections = append(sections,
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1), wasmtest.ExportEntry("init", 0, 2), wasmtest.ExportEntry("drop-zero", 0, 3), wasmtest.ExportEntry("drop", 0, 4), wasmtest.ExportEntry("init-empty", 0, 5), wasmtest.ExportEntry("loop", 0, 6), wasmtest.ExportEntry("zero-init", 0, 7), wasmtest.ExportEntry("drop-empty", 0, 8), wasmtest.ExportEntry("trap-before", 0, 9), wasmtest.ExportEntry("live", 0, 10), wasmtest.ExportEntry("gc", 0, 11))),
		wasmtest.Section(9, wasmtest.Vec(tableTestDeclarativeElem(0), tableTestPassiveElem(0), tableTestPassiveElem(), tableTestPassiveElem(0))),
		wasmtest.Section(10, wasmtest.Vec(body([]byte{0x41, 7}), body([]byte{0x20, 0, 0x11, 1, 0}), body(init(1, 1)), body(bulk(13, 0)), body(bulk(13, 1)), body(init(2, 1)), loop, body(init(1, 0)), body(bulk(13, 2)), body([]byte{0x41, 1, 0x41, 0, 0x6d, 0x1a, 0xfc, 13, 1}), live, body([]byte{0x41, 1, 0xfb, 7, 0, 0xfb, 15, 0x1a}))))
	return wasmtest.Module(sections...)
}

func gcElemDropConfig() *RuntimeConfig {
	return NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit)
}
func gcElemDropLength(in *Instance, index int) uint32 {
	p := unsafe.Slice((*byte)(offHeapPtr(in.jm.PassiveElemPtr()+uintptr(index*coreruntime.PassiveElemDescBytes))), coreruntime.PassiveElemDescBytes)
	return binary.LittleEndian.Uint32(p[8:])
}

func TestGCNonrootElemDropLifecycle(t *testing.T) {
	for _, imported := range []bool{false, true} {
		t.Run(fmt.Sprint("imported=", imported), func(t *testing.T) {
			c, err := Compile(gcElemDropConfig(), gcElemDropModule(imported))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if c.stagedGCArrayProduct() != stagedGCArrayProductGeneric {
				t.Fatalf("not generic array helper product: %v", c.stagedGCArrayProduct())
			}

			var options InstantiateOptions
			if imported {
				table, err := NewTable(1, 1)
				if err != nil {
					t.Fatal(err)
				}
				defer table.Close()
				options.Imports = NewImports().Table("env", "table", table)
			}
			for _, compiled := range []*Compiled{c} {
				in, err := Instantiate(compiled, options)
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				other, err := Instantiate(compiled, options)
				if err != nil {
					t.Fatal(err)
				}
				defer other.Close()
				if in.existingGCArrayElementState() != nil {
					t.Fatal("nonzero segment unexpectedly owns checked roots")
				}
				if in.jm.PassiveElemPtr() == other.jm.PassiveElemPtr() {
					t.Fatal("instances share mutable element descriptors")
				}
				for _, x := range []struct {
					i    int
					want uint32
				}{{0, 0}, {1, 1}, {2, 0}, {3, 1}} {
					if n := gcElemDropLength(in, x.i); n != x.want {
						t.Fatalf("segment%d length%d want%d", x.i, n, x.want)
					}
				}
				_, err = in.Invoke("trap-before")
				tableTestExpectTrap(t, err, TrapDivZero)
				if gcElemDropLength(in, 1) != 1 {
					t.Fatal("earlier trap dropped segment")
				}
				if _, err = in.Invoke("init"); err != nil {
					t.Fatal(err)
				}
				if got, err := in.Invoke("call", 0); err != nil || len(got) != 1 || got[0] != 7 {
					t.Fatalf("initialized funcref:%v,%v", got, err)
				}
				for _, name := range []string{"drop", "drop", "drop-empty", "drop-empty", "drop-zero", "zero-init"} {
					if _, err = in.Invoke(name); err != nil {
						t.Fatalf("%s:%v", name, err)
					}
				}
				for _, name := range []string{"init", "init-empty"} {
					_, err = in.Invoke(name)
					tableTestExpectTrap(t, err, TrapTableOutOfBounds)
				}
				if gcElemDropLength(in, 1) != 0 || gcElemDropLength(in, 3) != 1 || gcElemDropLength(other, 1) != 1 {
					t.Fatal("drop changed another segment or instance")
				}
				if _, err = other.Invoke("init"); err != nil {
					t.Fatal(err)
				}
				if got, err := in.Invoke("live", 1<<40); err != nil || len(got) != 1 || got[0] != 2*((1<<40)+1) {
					t.Fatalf("live prefix/local:%v,%v", got, err)
				}
				for mode := uint64(0); mode < 3; mode++ {
					if got, err := in.Invoke("loop", 32, mode); err != nil || len(got) != 1 || got[0] != 32 {
						t.Fatalf("loop mode%d:%v,%v", mode, got, err)
					}
				}
			}
		})
	}
}

func BenchmarkGCNonrootElemDrop(b *testing.B) {
	for mode, name := range []string{"native-candidate", "helper-zero", "mixed"} {
		b.Run(name, func(b *testing.B) {
			c, err := Compile(gcElemDropConfig(), gcElemDropModule(false))
			if err != nil {
				b.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c)
			if err != nil {
				b.Fatal(err)
			}
			defer in.Close()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if got, err := in.Invoke("loop", 1024, uint64(mode)); err != nil || len(got) != 1 || got[0] != 1024 {
					b.Fatalf("%v,%v", got, err)
				}
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/1024, "ns/drop")
			b.ReportMetric(float64(c.CodeSize()), "native-B")
		})
	}
}
func BenchmarkGCNonrootElemDropCompile(b *testing.B) {
	cfg, data := gcElemDropConfig(), gcElemDropModule(false)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		c, err := Compile(cfg, data)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(float64(c.CodeSize()), "native-B")
		c.Close()
	}
}

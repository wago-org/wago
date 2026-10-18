//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcConstructorIfModule(typ wasm.ValType, kind string) []byte {
	return gcConstructorIfDensityModule(typ, kind, 1)
}

func gcConstructorIfDensityModule(typ wasm.ValType, kind string, functions int) []byte {
	var vt byte
	switch typ {
	case wasm.I32:
		vt = 0x7f
	case wasm.I64:
		vt = 0x7e
	case wasm.F32:
		vt = 0x7d
	case wasm.F64:
		vt = 0x7c
	}
	fields := []byte{0x5f, 2, vt, 1, vt, 1}
	choose := []byte{1, 1, 0x63, 0}
	imported := uint32(0)
	if kind == "host" {
		imported = 1
		for i := 0; i < 8; i++ {
			choose = append(choose, 0x20, 0, 0x1a, 0x20, 1, 0x1a, 0x20, 2, 0x1a)
		}
		choose = append(choose, 0x10, 0)
	}
	if kind == "trap" {
		fields = []byte{0x5f, 3, 0x7f, 1, vt, 1, vt, 1}
		choose = append(choose, 0x41, 1, 0x20, 2, 0x6d)
	}
	choose = append(choose, 0x20, 0, 0x20, 1)
	if kind == "limit17" {
		fields[1] = 17
		for i := 0; i < 15; i++ {
			fields = append(fields, vt, 1)
			choose = append(choose, 0x20, 0)
		}
	}
	choose = append(choose, 0xfb, 0, 0, 0x21, 3, 0x20, 2, 0x04, vt)
	if kind == "nested" {
		choose = append(choose, 0x02, vt)
	}
	firstField := byte(0)
	if kind == "trap" {
		firstField = 1
	}
	choose = append(choose, 0x20, 3, 0xfb, 2, 0, firstField)
	if kind == "nested" {
		choose = append(choose, 0x0b)
	}
	choose = append(choose, 0x05, 0x20, 3, 0xfb, 2, 0, firstField+1, 0x0b)
	if kind == "later" {
		choose = append(choose, 0x20, 3, 0xfb, 2, 0, 0, 0x1a)
	}
	choose = append(choose, 0x0b)
	run := []byte{1, 1, vt, 0x03, 0x40, 0x20, 1, 0x20, 2, 0x20, 3, 0x10, byte(imported), 0x21, 4,
		0x20, 0, 0x41, 1, 0x6b, 0x22, 0, 0x0d, 0, 0x0b, 0x20, 4, 0x0b}
	funcs, bodies := make([][]byte, 0, functions+1), make([][]byte, 0, functions+1)
	for i := 0; i < functions; i++ {
		funcs = append(funcs, []byte{1})
		bodies = append(bodies, append(wasmtest.ULEB(uint32(len(choose))), choose...))
	}
	funcs = append(funcs, []byte{2})
	bodies = append(bodies, append(wasmtest.ULEB(uint32(len(run))), run...))
	types := [][]byte{fields,
		wasmtest.FuncType([]wasm.ValType{typ, typ, wasm.I32}, []wasm.ValType{typ}),
		wasmtest.FuncType([]wasm.ValType{wasm.I32, typ, typ, wasm.I32}, []wasm.ValType{typ})}
	if imported != 0 {
		types = append(types, wasmtest.FuncType(nil, nil))
	}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(types...))}
	if imported != 0 {
		entry := append(wasmtest.Name("env"), wasmtest.Name("touch")...)
		entry = append(entry, 0, 3)
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(entry)))
	}
	sections = append(sections,
		wasmtest.Section(3, wasmtest.Vec(funcs...)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("choose", 0, imported), wasmtest.ExportEntry("run", 0, uint32(functions)+imported))),
		wasmtest.Section(10, wasmtest.Vec(bodies...)),
	)
	return wasmtest.Module(sections...)
}

func TestGCConstructorIf(t *testing.T) {
	for _, typ := range []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64} {
		for _, kind := range []string{"terminal", "nested", "later", "limit17", "host"} {
			t.Run(fmt.Sprintf("%v/%s", typ, kind), func(t *testing.T) {
				c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcConstructorIfModule(typ, kind))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				c = publicArtifactRoundTrip(t, c)
				defer c.Close()
				for _, profile := range []GCProfile{GCProfileThroughput, GCProfileTiny} {
					for _, stress := range []bool{false, true} {
						opts := InstantiateOptions{GC: GCConfig{Profile: profile, CollectEveryAlloc: stress, StressNurseryBytes: 64, VerifyAfterCollect: true}}
						if kind == "host" {
							opts.Imports = testImports("env.touch", slotHostFunc(func(HostModule, []uint64, []uint64) {}))
						}
						in, err := Instantiate(c, opts)
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						a, b := uint64(0x7ff0123456789abc), uint64(0xfff8123456789abc)
						if typ == wasm.I32 || typ == wasm.F32 {
							a, b = 0x7fa12345, 0xffc12345
						}
						for _, cond := range []uint64{0, 1, 0xffffffff, 0} {
							want := a
							if cond == 0 {
								want = b
							}
							before := in.gc.Stats()
							got, err := in.Invoke("run", 64, a, b, cond)
							if err != nil || len(got) != 1 || got[0] != want {
								t.Fatalf("profile%d stress%v cond%x: %x,%v want%x", profile, stress, cond, got, err, want)
							}
							if got := in.gc.Stats().Allocations - before.Allocations; got != 64 {
								t.Fatalf("allocation charges%d want64", got)
							}
							if err := in.CollectGC(); err != nil {
								t.Fatal(err)
							}
						}
					}
				}
			})
		}
	}
}

func TestGCConstructorIfUnusedTrap(t *testing.T) {
	for _, typ := range []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64} {
		c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcConstructorIfModule(typ, "trap"))
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		for _, profile := range []GCProfile{GCProfileThroughput, GCProfileTiny} {
			in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: profile, VerifyAfterCollect: true}})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			before := in.gc.Stats().Allocations
			if _, err := in.Invoke("choose", 17, 31, 0); err == nil {
				t.Fatal("unused operand lost its divide-by-zero trap")
			}
			if in.gc.Stats().Allocations != before {
				t.Fatal("allocation ran before a constructor operand trapped")
			}
			if got, err := in.Invoke("choose", 17, 31, 1); err != nil || len(got) != 1 || got[0] != 17 {
				t.Fatalf("nontrapping operand: %v, %v", got, err)
			}
		}
	}
}

func BenchmarkGCConstructorIf(b *testing.B) {
	for _, cond := range []uint64{0, 1} {
		b.Run(fmt.Sprint(cond), func(b *testing.B) {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcConstructorIfModule(wasm.I64, "terminal"))
			if err != nil {
				b.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				b.Fatal(err)
			}
			defer in.Close()
			want := uint64(31)
			if cond != 0 {
				want = 17
			}
			if got, err := in.Invoke("run", 1024, 17, 31, cond); err != nil || len(got) != 1 || got[0] != want {
				b.Fatalf("preflight: %v, %v", got, err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			b.ReportMetric(float64(len(c.code)), "code-B")
			for i := 0; i < b.N; i++ {
				got, err := in.Invoke("run", 1024, 17, 31, cond)
				if err != nil || len(got) != 1 || got[0] != want {
					b.Fatalf("%v, %v", got, err)
				}
			}
		})
	}
}

func BenchmarkGCConstructorIfCompile(b *testing.B) {
	for _, functions := range []int{1, 4, 16, 256} {
		b.Run(fmt.Sprint(functions), func(b *testing.B) {
			data := gcConstructorIfDensityModule(wasm.I64, "terminal", functions)
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c, err := Compile(cfg, data)
				if err != nil {
					b.Fatal(err)
				}
				b.ReportMetric(float64(len(c.code)), "code-B")
				c.Close()
			}
		})
	}
}

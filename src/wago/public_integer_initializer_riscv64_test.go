//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64IntegerInitializerModule(vector bool) []byte {
	results := []wasm.ValType{wasm.I32, wasm.I64}
	if vector {
		results = append(results, wasm.V128)
	}
	narrow := []byte{0x23, 0, 0x41, 3, 0x6b, 0x41, 0xff, 0xff, 0xff, 0xff, 7, 0x6c, 0x41, 5, 0x6a, 0x0b}
	wide := []byte{0x23, 1, 0x42, 3, 0x7e, 0x42, 2, 0x7d, 0x42, 0x7f, 0x7c, 0x0b}
	globals := [][]byte{wasmtest.GlobalEntry(wasm.I32, false, narrow), wasmtest.GlobalEntry(wasm.I64, false, wide)}
	get := []byte{0x23, 2, 0x23, 3}
	if vector {
		globals = append(globals, wasmtest.GlobalEntry(wasm.V128, false, append([]byte{0xfd, 12}, append(make([]byte, 16), 0x0b)...)))
		get = append(get, 0x23, 4)
	}
	get = append(get, 0x0b)
	offset := []byte{0x23, 0, 0x41, 3, 0x6c, 0x41, 1, 0x6b, 0x0b}
	elem := append(append([]byte{0}, offset...), 1, 1)
	data := append(append([]byte{0}, offset...), 1, 0xa5)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, results), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "narrow", wasm.I32, false), wasmtest.GlobalImportEntry("env", "wide", wasm.I64, false))),
		wasmtest.Section(3, []byte{3, 0, 1, 2}), wasmtest.Section(4, []byte{1, 0x70, 1, 4, 4}), wasmtest.Section(5, []byte{1, 1, 1, 1}),
		wasmtest.Section(6, wasmtest.Vec(globals...)), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("get", 0, 0), wasmtest.ExportEntry("call", 0, 2), wasmtest.ExportEntry("memory", 2, 0))),
		wasmtest.Section(9, wasmtest.Vec(elem)), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(get), wasmtest.Code([]byte{0x20, 0, 0x41, 10, 0x6a, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x11, 1, 0, 0x0b}))), wasmtest.Section(11, wasmtest.Vec(data)),
	)
}

func TestRV64PublicIntegerInitializers(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("vector%t-artifact%t", vector, artifact), func(t *testing.T) {
				c := rv64TableStageCompile(t, fmt.Sprintf("integer-init-vector%t", vector), rv64IntegerInitializerModule(vector), artifact)
				if !c.requiredFeatures.IsEnabled(CoreFeatureExtendedConst) || len(c.Globals[2].InitExpr) == 0 || c.Data[0].Offset.Base != 0xffffffff || len(c.Data[0].Offset.Expr) == 0 || len(c.Elems[0].Offset.Expr) == 0 {
					t.Fatal("integer program/placeholder/feature metadata")
				}
				n := NewGlobalI32(1, false)
				defer n.Close()
				binary.LittleEndian.PutUint64(n.cell, 0xfeedface00000001)
				w := NewGlobalI64(-9223372036854775807, false)
				defer w.Close()
				im := NewImports().Global("env", "narrow", n).Global("env", "wide", w)

				in, err := Instantiate(c, im)
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				_ = c.Close()
				want := []uint64{7, 0x8000000000000000}
				if vector {
					want = append(want, 0, 0)
				}
				importedGlobalInvoke(t, in, "get", want)
				if in.Memory().UnsafeBytes()[2] != 0xa5 || in.Memory().UnsafeBytes()[0] != 0 {
					t.Fatal("arithmetic data offset/placeholder")
				}
				if got, err := in.Invoke("call", 0xdeadbeef00000005, 2); err != nil || !slices.Equal(got, []uint64{15}) {
					t.Fatal("arithmetic table offset", got, err)
				}
				_ = in.Close()
				if n.owner.importers != 0 || w.owner.importers != 0 {
					t.Fatal("integer initializer imports retained")
				}
			})
		}
	}
}

func rv64BoundedIntegerSource(body []byte) []byte {
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, body))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("get", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0, 0x0b}))))
}

func TestRV64PublicIntegerInitializerFoldedBounds(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			body := []byte{0x41, 0x81, 0x80, 0}
			for range 41 {
				body = append(body, 0x41, 0, 0x6a)
			}
			body = append(body, 0x0b)
			if len(body) != 128 {
				t.Fatal("byte boundary fixture")
			}
			c := rv64TableStageCompile(t, "integer-init-folded128", rv64BoundedIntegerSource(body), artifact)
			if !c.requiredFeatures.IsEnabled(CoreFeatureExtendedConst) || len(c.Globals[0].InitExpr) != 0 || c.Globals[0].Bits != 1 {
				t.Fatal("folded expression lost feature evidence")
			}
			in, err := Instantiate(c)
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			importedGlobalInvoke(t, in, "get", []uint64{1})
		})
	}
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
		return
	}
	for _, depth := range []int{8, 9} {
		var body []byte
		for i := 0; i < depth; i++ {
			body = append(body, 0x41, 1)
		}
		for i := 1; i < depth; i++ {
			body = append(body, 0x6a)
		}
		body = append(body, 0x0b)
		c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), rv64BoundedIntegerSource(body))
		if c != nil {
			c.Close()
		}
		if (err == nil) != (depth == 8) {
			t.Fatal("source depth admission before folding", depth, err)
		}
	}
	over := []byte{0x41, 1}
	for range 42 {
		over = append(over, 0x41, 0, 0x6a)
	}
	over = append(over, 0x0b)
	if len(over) != 129 {
		t.Fatal("byte boundary fixture")
	}
	if c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), rv64BoundedIntegerSource(over)); c != nil || err == nil {
		if c != nil {
			c.Close()
		}
		t.Fatal("source byte admission before folding", err)
	}
	if c, err := Compile(NewRuntimeConfig().WithFeature(CoreFeatureExtendedConst, false), rv64BoundedIntegerSource([]byte{0x41, 1, 0x41, 2, 0x6a, 0x0b})); c != nil || err == nil {
		if c != nil {
			c.Close()
		}
		t.Fatal("disabled source feature admitted", err)
	}
}

func TestRV64PublicIntegerInitializerIndex255(t *testing.T) {
	rv64SWARImportedInitializerIndexBoundary(t, true)
}

func TestRV64PublicIntegerInitializerRuntimeBounds(t *testing.T) {
	for _, depth := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("depth%t-artifact%t", depth, artifact), func(t *testing.T) {
				body := []byte{0x23, 0x80, 0x80, 0}
				for range 41 {
					body = append(body, 0x41, 0, 0x6a)
				}
				body = append(body, 0x0b)
				want := uint64(1)
				if depth {
					body = nil
					for range 8 {
						body = append(body, 0x23, 0)
					}
					for range 7 {
						body = append(body, 0x6a)
					}
					body = append(body, 0x0b)
					want = 8
				}
				source := rv64ReplaceTableStageSection(t, rv64IntegerInitializerModule(false), 6, wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, body), wasmtest.GlobalEntry(wasm.I64, false, []byte{0x42, 0, 0x0b}))))
				c := rv64TableStageCompile(t, fmt.Sprintf("integer-init-runtime-bound-depth%t", depth), source, artifact)
				n := NewGlobalI32(1, false)
				defer n.Close()
				w := NewGlobalI64(1, false)
				defer w.Close()
				in, err := Instantiate(c, NewImports().Global("env", "narrow", n).Global("env", "wide", w))
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				importedGlobalInvoke(t, in, "get", []uint64{want, 0})
			})
		}
	}
}

func TestRV64PublicIntegerInitializerFailureOrder(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			first := []byte{0, 0x41, 2, 0x0b, 1, 0xa5}
			failing := []byte{0, 0x23, 0, 0x41, 2, 0x6c, 0x41, 1, 0x6a, 0x0b, 2, 99, 100}
			source := rv64SWARImportedInitializerModule(true, first, failing)
			c := rv64TableStageCompile(t, "integer-init-failure-order", source, artifact)
			memory, err := NewSharedMemory(1, 1)
			if err != nil {
				t.Fatal(err)
			}
			defer memory.Close()
			im, objects, _ := rv64SWARInitializerImports(t, 32767)
			im.Memory("env", "memory", memory)
			for range 3 {
				clear(memory.UnsafeBytes())
				failed, err := Instantiate(c, im)
				if failed != nil {
					defer failed.Close()
				}
				if failed != nil || err == nil || memory.UnsafeBytes()[2] != 0xa5 || memory.UnsafeBytes()[65535] != 0 {
					t.Fatal("arithmetic data bounds/order", err)
				}
				if memory.state.Load().importerCount() != 0 {
					t.Fatal("failed expression retained memory")
				}
				for _, g := range objects {
					if g.owner.importers != 0 {
						t.Fatal("failed expression retained global")
					}
				}
			}
		})
	}
}

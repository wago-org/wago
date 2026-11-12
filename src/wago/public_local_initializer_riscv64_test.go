//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"fmt"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64LocalInitializerModule(vector, borrowed, expression bool) []byte {
	types := []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}
	if vector {
		types = append(types, wasm.V128)
	}
	n := len(types)
	var imports, globals, exports [][]byte
	var get, set []byte
	for i, typ := range types {
		if borrowed && i == 2 {
			imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name("memory")...), 2, 1, 1, 1))
		}
		imports = append(imports, wasmtest.GlobalImportEntry("env", fmt.Sprintf("g%d", i), typ, false))
		exports = append(exports, wasmtest.ExportEntry(fmt.Sprintf("local%d", i), 3, uint32(3*n+i)))
		get = append(get, 0x23, byte(3*n+i))
		set = append(set, 0x20, byte(i), 0x24, byte(3*n+i))
	}
	for pass := range 3 {
		for i, typ := range types {
			globals = append(globals, wasmtest.GlobalEntry(typ, pass == 2, []byte{0x23, byte(pass*n + i), 0x0b}))
		}
	}
	globals = append(globals, wasmtest.GlobalEntry(wasm.I32, false, []byte{0x23, byte(2 * n), 0x41, 2, 0x6c, 0x41, 1, 0x6a, 0x0b}))
	get = append(get, 0x0b)
	set = append(set, get...)
	offset := []byte{0x23, byte(4 * n)}
	if expression {
		offset = append(offset, 0x41, 1, 0x6a)
	}
	offset = append(offset, 0x0b)
	elem := append(append([]byte{0}, offset...), 1, 0)
	data := append(append([]byte{0}, offset...), 1, 0xa5)
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, types), wasmtest.FuncType(types, types), wasmtest.FuncType([]wasm.ValType{wasm.I32}, types))), wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, []byte{3, 0, 1, 2}), wasmtest.Section(4, []byte{1, 0x70, 1, 17, 17})}
	if !borrowed {
		sections = append(sections, wasmtest.Section(5, []byte{1, 1, 1, 1}))
	}
	exports = append(exports, wasmtest.ExportEntry("get", 0, 0), wasmtest.ExportEntry("set", 0, 1), wasmtest.ExportEntry("call", 0, 2), wasmtest.ExportEntry("memory", 2, 0))
	sections = append(sections, wasmtest.Section(6, wasmtest.Vec(globals...)), wasmtest.Section(7, wasmtest.Vec(exports...)), wasmtest.Section(9, wasmtest.Vec(elem)), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(get), wasmtest.Code(set), wasmtest.Code([]byte{0x20, 0, 0x11, 0, 0, 0x0b}))), wasmtest.Section(11, wasmtest.Vec(data)))
	return wasmtest.Module(sections...)
}

func TestRV64PublicLocalInitializers(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, borrowed := range []bool{false, true} {
			for _, expression := range []bool{false, true} {
				for _, artifact := range []bool{false, true} {
					t.Run(fmt.Sprintf("vector%t-borrowed%t-expression%t-artifact%t", vector, borrowed, expression, artifact), func(t *testing.T) {
						key := fmt.Sprintf("local-init-vector%t-borrowed%t-expression%t", vector, borrowed, expression)
						c := rv64TableStageCompile(t, key, rv64LocalInitializerModule(vector, borrowed, expression), artifact)
						if !c.requiredFeatures.IsEnabled(CoreFeatureExtendedConstExpressions) {
							t.Fatal("local feature metadata missing")
						}
						im, objects, want := rv64SWARInitializerImports(t, 7)
						if !vector {
							want = want[:4]
						}
						if borrowed {
							memory, err := NewSharedMemory(1, 1)
							if err != nil {
								t.Fatal(err)
							}
							defer memory.Close()
							im.Memory("env", "memory", memory)
						}
						a, err := Instantiate(c, im)
						if err != nil {
							t.Fatal(err)
						}
						defer a.Close()
						b, err := Instantiate(c, im)
						if err != nil {
							t.Fatal(err)
						}
						defer b.Close()
						c.Close()
						importedGlobalInvoke(t, a, "get", want)
						importedGlobalInvoke(t, b, "get", want)
						offset := uint64(15)
						if expression {
							offset++
						}
						if a.Memory().UnsafeBytes()[offset] != 0xa5 || a.Memory().UnsafeBytes()[0] != 0 {
							t.Fatal("local data offset")
						}
						importedGlobalInvoke(t, a, "call", want, offset)
						changed := []uint64{0xaaaaaaaa00000003, 4, 0xbbbbbbbb80000000, 0x8000000000000000, 0xffffffffffffffff, 1}
						if !vector {
							changed = changed[:4]
						}
						normalized := slices.Clone(changed)
						normalized[0] = 3
						normalized[2] = 0x80000000
						importedGlobalInvoke(t, a, "set", normalized, changed...)
						importedGlobalInvoke(t, b, "call", want, offset)
						for i, typ := range []ValType{ValI32, ValI64, ValF32, ValF64} {
							if readGlobalObject(objects[i], typ) != want[i] {
								t.Fatal("local copy aliases import", i)
							}
						}
						if vector {
							bits := objects[4].GetV128()
							if binary.LittleEndian.Uint64(bits[:]) != want[4] || binary.LittleEndian.Uint64(bits[8:]) != want[5] {
								t.Fatal("local vector aliases import")
							}
						}
						a.Close()
						b.Close()
						for _, g := range objects {
							if g.owner.importers != 0 {
								t.Fatal("local initialization retained imports")
							}
						}
						if c.codeCache.mem != nil {
							t.Fatal("local initializer retained mapping")
						}
					})
				}
			}
		}
	}
}

func rv64CompactLocalInitializerModule() []byte {
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(4, []byte{1, 0x70, 1, 3, 3}), wasmtest.Section(5, []byte{1, 1, 1, 1}), wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, []byte{0x41, 2, 0x0b}), wasmtest.GlobalEntry(wasm.I32, false, []byte{0x23, 0, 0x0b}))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("get", 0, 0))), wasmtest.Section(9, []byte{1, 0, 0x23, 1, 0x0b, 1, 0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 1, 0x0b}))), wasmtest.Section(11, []byte{1, 0, 0x23, 1, 0x0b, 1, 42}))
}

func TestRV64PublicLocalInitializerIndex255(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			globals := [][]byte{wasmtest.GlobalEntry(wasm.I32, false, []byte{0x41, 7, 0x0b})}
			for i := 1; i < 256; i++ {
				body := append([]byte{0x23}, wasmtest.ULEB(uint32(i-1))...)
				globals = append(globals, wasmtest.GlobalEntry(wasm.I32, false, append(body, 0x0b)))
			}
			source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(5, []byte{1, 1, 1, 1}), wasmtest.Section(6, wasmtest.Vec(globals...)), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("get", 0, 0), wasmtest.ExportEntry("memory", 2, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0xff, 1, 0x0b}))), wasmtest.Section(11, []byte{1, 0, 0x23, 0xff, 1, 0x0b, 1, 42}))
			c := rv64TableStageCompile(t, "local-init-index255", source, artifact)
			in, err := Instantiate(c)
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			importedGlobalInvoke(t, in, "get", []uint64{7})
			if in.Memory().UnsafeBytes()[7] != 42 {
				t.Fatal("final local offset")
			}
		})
	}
}

func TestRV64PublicLocalInitializerArithmetic(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			source := rv64IntegerInitializerModule(false)
			globals := wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, []byte{0x23, 0, 0x0b}), wasmtest.GlobalEntry(wasm.I64, false, []byte{0x23, 1, 0x0b}), wasmtest.GlobalEntry(wasm.I32, false, []byte{0x23, 2, 0x41, 3, 0x6b, 0x41, 0xff, 0xff, 0xff, 0xff, 7, 0x6c, 0x41, 5, 0x6a, 0x0b}), wasmtest.GlobalEntry(wasm.I64, false, []byte{0x23, 3, 0x42, 3, 0x7e, 0x42, 2, 0x7d, 0x42, 0x7f, 0x7c, 0x0b}))
			source = rv64ReplaceTableStageSection(t, source, 6, wasmtest.Section(6, globals))
			source = rv64ReplaceTableStageSection(t, source, 10, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 4, 0x23, 5, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x41, 10, 0x6a, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x11, 1, 0, 0x0b}))))
			c := rv64TableStageCompile(t, "local-init-arithmetic", source, artifact)
			n := NewGlobalI32(1, false)
			defer n.Close()
			binary.LittleEndian.PutUint64(n.cell, 0xfeedface00000001)
			w := NewGlobalI64(-9223372036854775807, false)
			defer w.Close()
			in, err := Instantiate(c, NewImports().Global("env", "narrow", n).Global("env", "wide", w))
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			importedGlobalInvoke(t, in, "get", []uint64{7, 0x8000000000000000})
		})
	}
}

func TestRV64PublicLocalInitializerFailureOrder(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			source := rv64LocalInitializerModule(true, true, false)
			// Use a declarative element so the data failure, after all globals initialize,
			// witnesses the successful earlier segment and whole-range second check.
			source = rv64ReplaceTableStageSection(t, source, 9, wasmtest.Section(9, []byte{1, 3, 0, 1, 0}))
			source = rv64ReplaceTableStageSection(t, source, 11, wasmtest.Section(11, wasmtest.Vec([]byte{0, 0x41, 2, 0x0b, 1, 0xa5}, []byte{0, 0x23, 20, 0x0b, 2, 99, 100})))
			c := rv64TableStageCompile(t, "local-init-failure-order", source, artifact)
			memory, err := NewSharedMemory(1, 1)
			if err != nil {
				t.Fatal(err)
			}
			defer memory.Close()
			im, objects, _ := rv64SWARInitializerImports(t, 32767)
			im.Memory("env", "memory", memory)
			for range 3 {
				clear(memory.UnsafeBytes())
				in, err := Instantiate(c, im)
				if in != nil {
					defer in.Close()
				}
				if in != nil || err == nil || memory.UnsafeBytes()[2] != 0xa5 || memory.UnsafeBytes()[65535] != 0 {
					t.Fatal("local data bounds/order", err)
				}
				if memory.state.Load().importerCount() != 0 {
					t.Fatal("failed local initializer retained memory")
				}
				for _, g := range objects {
					if g.owner.importers != 0 {
						t.Fatal("failed local initializer retained globals")
					}
				}
			}
			c.Close()
			if c.codeCache.mem != nil {
				t.Fatal("failed local initializer retained mapping")
			}
		})
	}
}

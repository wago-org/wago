//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"runtime"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64SWARImportedInitializerModule(borrowed bool, segments ...[]byte) []byte {
	types := []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64, wasm.V128}
	var imports, globals, exports [][]byte
	var get, set []byte
	for i, typ := range types {
		if borrowed && i == 2 {
			imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name("memory")...), 2, 1, 1, 1))
		}
		imports = append(imports, wasmtest.GlobalImportEntry("env", fmt.Sprintf("g%d", i), typ, false))
		globals = append(globals, wasmtest.GlobalEntry(typ, true, []byte{0x23, byte(i), 0x0b}))
		exports = append(exports, wasmtest.ExportEntry(fmt.Sprintf("local%d", i), 3, uint32(i+5)))
		get = append(get, 0x23, byte(i+5))
		set = append(set, 0x20, byte(i), 0x24, byte(i+5))
	}
	get = append(get, 0x0b)
	set = append(set, get...)
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, types), wasmtest.FuncType(types, types), wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil))),
		wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, []byte{3, 0, 1, 2}),
	}
	if !borrowed {
		sections = append(sections, wasmtest.Section(5, []byte{1, 1, 1, 1}))
	}
	sections = append(sections, wasmtest.Section(6, wasmtest.Vec(globals...)))
	exports = append(exports, wasmtest.ExportEntry("get", 0, 0), wasmtest.ExportEntry("set", 0, 1), wasmtest.ExportEntry("init", 0, 2), wasmtest.ExportEntry("memory", 2, 0))
	if segments == nil {
		segments = [][]byte{{0, 0x23, 0, 0x0b, 2, 42, 43}, {1, 2, 44, 45}}
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(exports...)), wasmtest.Section(12, wasmtest.ULEB(uint32(len(segments)))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(get), wasmtest.Code(set), wasmtest.Code([]byte{0x20, 0, 0x41, 0, 0x41, 2, 0xfc, 8, 1, 0, 0x0b}))), wasmtest.Section(11, wasmtest.Vec(segments...)))
	return wasmtest.Module(sections...)
}

func rv64SWARInitializerImports(t *testing.T, offset int32) (*Imports, []*Global, []uint64) {
	t.Helper()
	want := []uint64{uint64(uint32(offset)), 0x8000000000000001, 0x7f800001, 0x7ff0000000000001, 0x0123456789abcdef, 0xfedcba9876543210}
	var vector V128
	binary.LittleEndian.PutUint64(vector[:], want[4])
	binary.LittleEndian.PutUint64(vector[8:], want[5])
	objects := []*Global{NewGlobalI32(offset, false), NewGlobalI64(int64(want[1]), false), NewGlobalF32(math.Float32frombits(uint32(want[2])), false), NewGlobalF64(math.Float64frombits(want[3]), false), NewGlobalV128(vector, false)}
	imports := NewImports()
	for i, g := range objects {
		t.Cleanup(func() { g.Close() })
		imports.Global("env", fmt.Sprintf("g%d", i), g)
	}
	binary.LittleEndian.PutUint64(objects[0].cell, 0xfeedface00000000|want[0])
	binary.LittleEndian.PutUint64(objects[2].cell, 0xdeadbeef00000000|want[2])
	return imports, objects, want
}

func TestRV64PublicSWARImportedInitializers(t *testing.T) {
	for _, borrowed := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("borrowed%t-artifact%t", borrowed, artifact), func(t *testing.T) {
				c := rv64TableStageCompile(t, fmt.Sprintf("swar-init-borrowed%t", borrowed), rv64SWARImportedInitializerModule(borrowed), artifact)
				im, objects, want := rv64SWARInitializerImports(t, 7)
				var memory *Memory
				if borrowed {
					var err error
					memory, err = NewSharedMemory(1, 1)
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
				_ = c.Close()
				runtime.GC()
				importedGlobalInvoke(t, a, "get", want)
				importedGlobalInvoke(t, b, "get", want)
				if !bytes.Equal(a.Memory().UnsafeBytes()[7:9], []byte{42, 43}) {
					t.Fatal("active global offset ignored")
				}
				if _, err := a.Invoke("init", 19); err != nil {
					t.Fatal("original passive data index", err)
				}
				if !bytes.Equal(a.Memory().UnsafeBytes()[19:21], []byte{44, 45}) {
					t.Fatal("passive data index remapped")
				}
				changed := []uint64{0xaaaaaaaa00000003, 4, 0xbbbbbbbb80000000, 0x8000000000000000, 0xffffffffffffffff, 1}
				normalized := slices.Clone(changed)
				normalized[0] = 3
				normalized[2] = 0x80000000
				importedGlobalInvoke(t, a, "set", normalized, changed...)
				importedGlobalInvoke(t, b, "get", want)
				for i, typ := range []ValType{ValI32, ValI64, ValF32, ValF64} {
					if got := readGlobalObject(objects[i], typ); got != want[i] {
						t.Fatal("copied scalar aliases source", i, got)
					}
				}
				vector := objects[4].GetV128()
				if binary.LittleEndian.Uint64(vector[:]) != want[4] || binary.LittleEndian.Uint64(vector[8:]) != want[5] {
					t.Fatal("copied vector aliases source")
				}
				got, err := a.GlobalV128("local4")
				if err != nil || binary.LittleEndian.Uint64(got[:]) != changed[4] || binary.LittleEndian.Uint64(got[8:]) != changed[5] {
					t.Fatal("vector global API", err)
				}
			})
		}
	}
}

func TestRV64PublicSWARImportedInitializerBounds(t *testing.T) {
	for _, empty := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("empty%t-artifact%t", empty, artifact), func(t *testing.T) {
				segment := []byte{0, 0x23, 0, 0x0b, 2, 42, 43}
				if empty {
					segment = []byte{0, 0x23, 0, 0x0b, 0}
				}
				c := rv64TableStageCompile(t, fmt.Sprintf("swar-init-bounds-empty%t", empty), rv64SWARImportedInitializerModule(true, segment, []byte{1, 2, 44, 45}), artifact)
				for _, offset := range []int32{0, 65534, 65535, 65536, 65537, -1} {
					t.Run(fmt.Sprint(offset), func(t *testing.T) {
						memory, err := NewSharedMemory(1, 1)
						if err != nil {
							t.Fatal(err)
						}
						defer memory.Close()
						im, objects, _ := rv64SWARInitializerImports(t, offset)
						im.Memory("env", "memory", memory)
						before := slices.Clone(memory.UnsafeBytes())
						in, err := Instantiate(c, im)
						if in != nil {
							defer in.Close()
						}
						valid := offset >= 0 && (empty && offset <= 65536 || !empty && offset <= 65534)
						if valid != (err == nil) || valid != (in != nil) {
							t.Fatal("data offset bound", err)
						}
						if !valid {
							if !bytes.Equal(before, memory.UnsafeBytes()) {
								t.Fatal("failing segment partially wrote memory")
							}
							for _, g := range objects {
								if g.owner.importers != 0 {
									t.Fatal("failed initialization retained globals")
								}
							}
						}
					})
				}
			})
		}
	}
}

func TestRV64PublicSWARImportedInitializerArtifacts(t *testing.T) {
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
		t.Skip("source metadata mutations")
	}
	c := rv64TableStageCompile(t, "swar-init-artifact", rv64SWARImportedInitializerModule(false), false)
	for index, mutate := range []func(*Compiled){
		func(v *Compiled) { v.Globals[9].InitGlobal = 0 }, func(v *Compiled) { v.Globals[9].InitGlobal = 9 }, func(v *Compiled) { v.Globals[9].InitGlobal = -1 }, func(v *Compiled) { v.GlobalImports[4].Mutable = true; v.Globals[4].Mutable = true }, func(v *Compiled) { v.Globals[9].InitExpr = []byte{0x23, 4, 0x0b} }, func(v *Compiled) { v.Data[0].Offset.Global = 4 }, func(v *Compiled) { v.Data[0].Offset.Global = 5 }, func(v *Compiled) { v.Data[0].Offset.Expr = []byte{0x23, 0, 0x0b} },
	} {
		v := *c.executionView()
		v.Globals = slices.Clone(v.Globals)
		v.GlobalImports = slices.Clone(v.GlobalImports)
		v.Data = slices.Clone(v.Data)
		mutate(&v)
		if err := railshotValidateCompiled(&v); err == nil {
			t.Fatal("invalid SWAR initializer artifact admitted")
		}
		blob, err := marshalCompiled(&v)
		if err != nil {
			continue
		}
		loaded, err := LoadTrustedArtifact(blob)
		if err == nil && index == 4 {
			// The codec encodes HasInitGlobal as a single tag and omits the
			// conflicting expression; verify the normalized supported shape.
			repaired := loaded.executionView()
			if err := railshotValidateCompiled(repaired); err != nil || !repaired.Globals[9].HasInitGlobal || repaired.Globals[9].InitGlobal != 4 || len(repaired.Globals[9].InitExpr) != 0 {
				loaded.Close()
				t.Fatal("codec did not normalize copied-global metadata", err)
			}
			loaded.Close()
			continue
		}
		if loaded != nil {
			loaded.Close()
		}
		if err == nil {
			t.Fatal("invalid SWAR initializer loaded", index)
		}
	}
}

func TestRV64PublicSWARImportedInitializerRetiredSource(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			types := []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64, wasm.V128}
			var imports, exports [][]byte
			var body []byte
			for i, typ := range types {
				name := fmt.Sprintf("g%d", i)
				imports = append(imports, wasmtest.GlobalImportEntry("env", name, typ, false))
				exports = append(exports, wasmtest.ExportEntry(name, 3, uint32(i)))
				body = append(body, 0x23, byte(i))
			}
			body = append(body, 0x0b)
			exports = append(exports, wasmtest.ExportEntry("get", 0, 0))
			source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, types))), wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(exports...)), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
			p := rv64TableStageCompile(t, "swar-init-source", source, artifact)
			c := rv64TableStageCompile(t, "swar-init-retired-consumer", rv64SWARImportedInitializerModule(false), artifact)
			_, objects, want := rv64SWARInitializerImports(t, 7)
			values := NewImports()
			for i, typ := range []ValType{ValI32, ValI64, ValF32, ValF64, ValV128} {
				value := GlobalImport{Type: typ}
				if typ == ValV128 {
					value.V128 = objects[i].GetV128()
				} else {
					value.Bits = binary.LittleEndian.Uint64(objects[i].cell)
				}
				values.Global("env", fmt.Sprintf("g%d", i), value)
			}
			producer, err := Instantiate(p, values)
			if err != nil {
				t.Fatal(err)
			}
			defer producer.Close()
			borrowed := NewImports()
			for i := range types {
				g, err := producer.ExportedGlobalObject(fmt.Sprintf("g%d", i))
				if err != nil {
					t.Fatal(err)
				}
				if g.instanceOwner() != producer {
					t.Fatal("value import lost arena owner")
				}
				borrowed.Global("env", fmt.Sprintf("g%d", i), g)
			}
			consumer, err := Instantiate(c, borrowed)
			if err != nil {
				t.Fatal(err)
			}
			defer consumer.Close()
			_ = producer.Close()
			_ = p.Close()
			_ = c.Close()
			runtime.GC()
			if !producer.hasPhysicalResources() || p.codeCache.mem == nil {
				t.Fatal("imported initializer source retired early")
			}
			importedGlobalInvoke(t, consumer, "get", want)
			if !bytes.Equal(consumer.Memory().UnsafeBytes()[7:9], []byte{42, 43}) {
				t.Fatal("retired source offset")
			}
			_ = consumer.Close()
			if producer.hasPhysicalResources() || p.codeCache.mem != nil || c.codeCache.mem != nil {
				t.Fatal("initializer source owner not released")
			}
		})
	}
}

func TestRV64PublicSWARImportedInitializerFailureOrder(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			failing := append([]byte{0, 0x41}, wasmtest.SLEB32(65535)...)
			failing = append(failing, 0x0b, 2, 99, 100)
			c := rv64TableStageCompile(t, "swar-init-ordered-failure", rv64SWARImportedInitializerModule(true, []byte{0, 0x23, 0, 0x0b, 2, 42, 43}, failing), artifact)
			memory, err := NewSharedMemory(1, 1)
			if err != nil {
				t.Fatal(err)
			}
			defer memory.Close()
			im, objects, _ := rv64SWARInitializerImports(t, 7)
			im.Memory("env", "memory", memory)
			for range 3 {
				for i := range memory.UnsafeBytes() {
					memory.UnsafeBytes()[i] = 0xcc
				}
				before := slices.Clone(memory.UnsafeBytes())
				before[7], before[8] = 42, 43
				in, err := Instantiate(c, im)
				if in != nil {
					defer in.Close()
				}
				if in != nil || err == nil || !bytes.Equal(memory.UnsafeBytes(), before) {
					t.Fatal("data source order/whole segment bounds", err)
				}
				if memory.state.Load().importerCount() != 0 {
					t.Fatal("failed initialization retained memory")
				}
				for _, g := range objects {
					if g.owner.importers != 0 {
						t.Fatal("failed initialization retained globals")
					}
				}
			}
			_ = c.Close()
			if c.codeCache.mem != nil {
				t.Fatal("failed data initializer retained mapping")
			}
		})
	}
}

func TestRV64PublicSWARImportedInitializerIndexBoundary(t *testing.T) {
	rv64SWARImportedInitializerIndexBoundary(t, false)
}

func rv64SWARImportedInitializerIndexBoundary(t *testing.T, arithmetic bool) {
	for _, copyLocal := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("copy%t-artifact%t", copyLocal, artifact), func(t *testing.T) {
				count, vectorIndex, offsetIndex, resultIndex := 256, 254, 255, 254
				if copyLocal {
					count, offsetIndex, resultIndex = 255, 253, 255
				}
				var imports [][]byte
				bindings := NewImports()
				other := NewGlobalI32(9, false)
				defer other.Close()
				offset := NewGlobalI32(7, false)
				defer offset.Close()
				var bits V128
				binary.LittleEndian.PutUint64(bits[:], 0xfedcba9876543210)
				binary.LittleEndian.PutUint64(bits[8:], 0x0123456789abcdef)
				vector := NewGlobalV128(bits, false)
				defer vector.Close()
				for i := 0; i < count; i++ {
					typ := wasm.I32
					g := other
					if i == vectorIndex {
						typ = wasm.V128
						g = vector
					} else if i == offsetIndex {
						g = offset
					}
					name := fmt.Sprintf("g%d", i)
					imports = append(imports, wasmtest.GlobalImportEntry("env", name, typ, false))
					bindings.Global("env", name, g)
				}
				sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}))), wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(5, []byte{1, 1, 1, 1})}
				if copyLocal {
					expr := append([]byte{0x23}, wasmtest.ULEB(uint32(vectorIndex))...)
					expr = append(expr, 0x0b)
					sections = append(sections, wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.V128, false, expr))))
				}
				body := append([]byte{0x23}, wasmtest.ULEB(uint32(resultIndex))...)
				body = append(body, 0x0b)
				segment := append([]byte{0, 0x23}, wasmtest.ULEB(uint32(offsetIndex))...)
				if arithmetic {
					segment = append(segment, 0x41, 2, 0x6c, 0x41, 1, 0x6a)
				}
				segment = append(segment, 0x0b, 1, 42)
				sections = append(sections, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("get", 0, 0), wasmtest.ExportEntry("memory", 2, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))), wasmtest.Section(11, wasmtest.Vec(segment)))
				key := fmt.Sprintf("swar-init-boundary-copy%t", copyLocal)
				if arithmetic {
					key += "-arithmetic"
				}
				c := rv64TableStageCompile(t, key, wasmtest.Module(sections...), artifact)
				if len(c.Globals) != 256 {
					t.Fatal("global carrier limit")
				}
				in, err := Instantiate(c, bindings)
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				importedGlobalInvoke(t, in, "get", []uint64{0xfedcba9876543210, 0x0123456789abcdef})
				position := 7
				if arithmetic {
					position = 15
				}
				if in.Memory().UnsafeBytes()[position] != 42 || in.Memory().UnsafeBytes()[9] != 0 {
					t.Fatal("global offset namespace")
				}
			})
		}
	}
}

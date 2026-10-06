//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"errors"
	"runtime"
	"slices"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64PublicSWARGlobalExportsModule() []byte {
	types := []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64, wasm.V128, wasm.V128}
	initial := []uint64{0xffffffff, 0x8000000000000001, 0x7f800001, 0x7ff0000000000001, 0x8899aabbccddeeff, 0x0807060504030201, 0x7ff0000000000001, 0x80000000deadbeef}
	globals := [][]byte{
		wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x7f, 0x0b}),
		wasmtest.GlobalEntry(wasm.I64, true, append(append([]byte{0x42}, wasmtest.SLEB64(-9223372036854775807)...), 0x0b)),
	}
	for i, typ := range []wasm.ValType{wasm.F32, wasm.F64, wasm.V128, wasm.V128} {
		width := 4
		prefix := []byte{0x43}
		offset := i + 2
		if typ == wasm.F64 {
			width = 8
			prefix = []byte{0x44}
		}
		if typ == wasm.V128 {
			width = 16
			prefix = []byte{0xfd, 12}
			offset = 4 + (i-2)*2
		}
		raw := make([]byte, width)
		if width == 4 {
			binary.LittleEndian.PutUint32(raw, uint32(initial[offset]))
		} else {
			binary.LittleEndian.PutUint64(raw, initial[offset])
			if width == 16 {
				binary.LittleEndian.PutUint64(raw[8:], initial[offset+1])
			}
		}
		globals = append(globals, wasmtest.GlobalEntry(typ, i != 3, append(append(prefix, raw...), 0x0b)))
	}
	get := []byte{}
	for i := range types {
		get = append(get, 0x23, byte(i))
	}
	set := slices.Clone(get)
	for i := range 5 {
		set = append(set, 0x20, byte(i), 0x24, byte(i))
	}
	set = append(set, 0x0b)
	get = append(get, 0x0b)
	exports := [][]byte{wasmtest.ExportEntry("get", 0, 0), wasmtest.ExportEntry("set", 0, 1)}
	for i := range types {
		exports = append(exports, wasmtest.ExportEntry("g"+string(rune('0'+i)), 3, uint32(i)))
	}
	exports = append(exports, wasmtest.ExportEntry("alias", 3, 4))
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, types), wasmtest.FuncType(types[:5], types))), wasmtest.Section(3, []byte{2, 0, 1}), wasmtest.Section(6, wasmtest.Vec(globals...)), wasmtest.Section(7, wasmtest.Vec(exports...)), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(get), wasmtest.Code(set))))
}
func rv64PublicVector(lo, hi uint64) V128 {
	var value V128
	binary.LittleEndian.PutUint64(value[:], lo)
	binary.LittleEndian.PutUint64(value[8:], hi)
	return value
}
func TestRV64PublicSWAROwnedGlobalExportsHostAccessAndArtifactIsolation(t *testing.T) {
	initial := []uint64{0xffffffff, 0x8000000000000001, 0x7f800001, 0x7ff0000000000001, 0x8899aabbccddeeff, 0x0807060504030201, 0x7ff0000000000001, 0x80000000deadbeef}
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			c, err := Compile(nil, rv64PublicSWARGlobalExportsModule())
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if artifact {
				blob, err := c.MarshalBinary()
				if err != nil {
					t.Fatal(err)
				}
				loaded, err := LoadTrustedArtifact(blob)
				if loaded != nil {
					defer loaded.Close()
				}
				if err != nil {
					t.Fatal(err)
				}
				if err := c.Close(); err != nil {
					t.Fatal(err)
				}
				c = loaded
			}
			a, err := Instantiate(c)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			b, err := Instantiate(c)
			if err != nil {
				t.Fatal(err)
			}
			defer b.Close()
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			runtime.GC()
			got, err := a.Invoke("get")
			if err != nil || !slices.Equal(got, initial) {
				t.Fatal("literal exported global carriers", got, err)
			}
			for i := range 4 {
				bits, err := a.Global("g" + string(rune('0'+i)))
				if err != nil || bits != initial[i] {
					t.Fatal("scalar host global bits", i, bits, err)
				}
			}
			oldVec := rv64PublicVector(initial[4], initial[5])
			if value, err := a.GlobalV128("g4"); err != nil || value != oldVec {
				t.Fatal("vector host global bits", value, err)
			}
			object, err := a.ExportedGlobalObject("g4")
			if err != nil {
				t.Fatal(err)
			}
			alias, err := a.ExportedGlobalObject("alias")
			if err != nil || object != alias || object.GetV128() != oldVec {
				t.Fatal("vector global alias identity", err)
			}
			if unknown, err := a.ExportedGlobalObject("missing"); unknown != nil || err == nil {
				t.Fatal("unknown exported global resolved")
			}
			if _, err := a.Global("g4"); err == nil {
				t.Fatal("scalar accessor accepted a vector")
			}
			if _, err := a.GlobalV128("g0"); err == nil {
				t.Fatal("vector accessor accepted a scalar")
			}
			if err := a.SetGlobal("g4", 1); err == nil {
				t.Fatal("scalar setter accepted a vector")
			}
			if err := a.SetGlobalV128("g0", oldVec); err == nil {
				t.Fatal("vector setter accepted a scalar")
			}
			immutable, err := a.ExportedGlobalObject("g5")
			if err != nil {
				t.Fatal(err)
			}
			if err := immutable.SetV128(oldVec); err == nil {
				t.Fatal("immutable object mutated")
			}
			if err := a.SetGlobalV128("g5", oldVec); err == nil {
				t.Fatal("immutable instance global mutated")
			}
			replacement := rv64PublicVector(0x8000000000000000, 0xffffffffffffffff)
			if err := object.SetV128(replacement); err != nil {
				t.Fatal(err)
			}
			want := slices.Clone(initial)
			want[4], want[5] = 0x8000000000000000, 0xffffffffffffffff
			got, err = a.Invoke("get")
			if err != nil || !slices.Equal(got, want) {
				t.Fatal("object setter did not reach guest cell", got, err)
			}
			replacement = rv64PublicVector(0x7ff0000000000042, 0xff8000017f800001)
			if err := a.SetGlobalV128("g4", replacement); err != nil {
				t.Fatal(err)
			}
			want[4], want[5] = 0x7ff0000000000042, 0xff8000017f800001
			if err := a.SetGlobal("g0", 0xdeadbeef80000000); err != nil {
				t.Fatal(err)
			}
			want[0] = 0x80000000
			if err := a.SetGlobal("g2", 0xaabbccddff800001); err != nil {
				t.Fatal(err)
			}
			want[2] = 0xff800001
			// Keep the old six logical values live across all five mutable cell writes.
			args := []uint64{0xdeadbeefffffffff, 0x123456789abcdef0, 0xaabbccdd80000000, 0x8000000000000000, 0x0807060504030201, 0x8899aabbccddeeff}
			got, err = a.Invoke("set", args...)
			if err != nil || !slices.Equal(got, want) {
				t.Fatal("aliased old exports across guest writes", got, err)
			}
			newValues := append([]uint64{0xffffffff, args[1], 0x80000000, args[3], args[4], args[5]}, initial[6:]...)
			got, err = a.Invoke("get")
			if err != nil || !slices.Equal(got, newValues) {
				t.Fatal("guest update did not reach exported cells", got, err)
			}
			if object.GetV128() != rv64PublicVector(args[4], args[5]) {
				t.Fatal("guest update lost raw exported vector halves")
			}
			got, err = b.Invoke("get")
			if err != nil || !slices.Equal(got, initial) {
				t.Fatal("artifact sibling globals aliased", got, err)
			}
			if err := a.Close(); err != nil {
				t.Fatal(err)
			}
			runtime.GC()
			if object.GetV128() != (V128{}) || object.SetV128(replacement) == nil || len(object.cell) != 0 {
				t.Fatal("closed owner retained callable host global access")
			}
			got, err = b.Invoke("get")
			if err != nil || !slices.Equal(got, initial) {
				t.Fatal("sibling retirement changed globals", got, err)
			}
		})
	}
}
func TestRV64PublicSWARGlobalExportMetadataBounds(t *testing.T) {
	c, err := Compile(nil, rv64PublicSWARGlobalExportsModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	base := c.executionView()
	for _, index := range []int{-1, len(base.Globals)} {
		view := *base
		view.GlobalExports = map[string]int{"bad": index}
		if err := railshotValidateCompiled(&view); !errors.Is(err, railshot.ErrUnsupported) {
			t.Fatal("global export index escaped bounded profile", index, err)
		}
	}
	view := *base
	view.GlobalExports = make(map[string]int, railshot.MaxFunctions+1)
	for i := range railshot.MaxFunctions + 1 {
		view.GlobalExports[string(rune(i))] = 0
	}
	if err := railshotValidateCompiled(&view); !errors.Is(err, railshot.ErrUnsupported) {
		t.Fatal("global export count escaped bounded profile", err)
	}
	if c.codeCache.base != 0 {
		t.Fatal("metadata rejection changed deferred code ownership")
	}
}

func TestRV64PublicSWARScalarGlobalBorrowersRetainRetiredVectorProducer(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled, err := Compile(nil, rv64PublicSWARGlobalExportsModule())
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			if artifact {
				blob, err := compiled.MarshalBinary()
				if err != nil {
					t.Fatal(err)
				}
				loaded, err := LoadTrustedArtifact(blob)
				if loaded != nil {
					defer loaded.Close()
				}
				if err != nil {
					t.Fatal(err)
				}
				if err := compiled.Close(); err != nil {
					t.Fatal(err)
				}
				compiled = loaded
			}
			producer, err := Instantiate(compiled)
			if err != nil {
				t.Fatal(err)
			}
			defer producer.Close()
			if err := compiled.Close(); err != nil {
				t.Fatal(err)
			}
			imports := NewImports()
			objects := make([]*Global, 4)
			for i := range objects {
				name := "g" + string(rune('0'+i))
				objects[i], err = producer.ExportedGlobalObject(name)
				if err != nil {
					t.Fatal(err)
				}
				imports.Global("env", name, objects[i])
			}
			consumers := make([]*Instance, 2)
			codes := make([]*Compiled, 2)
			for i := range consumers {
				codes[i], err = Compile(nil, publicImportedGlobalModule(true, 0, true, byte(44+i*11)))
				if err != nil {
					t.Fatal(err)
				}
				defer codes[i].Close()
				if i == 1 {
					blob, err := codes[i].MarshalBinary()
					if err != nil {
						t.Fatal(err)
					}
					loaded, err := LoadTrustedArtifact(blob)
					if loaded != nil {
						defer loaded.Close()
					}
					if err != nil {
						t.Fatal(err)
					}
					if err := codes[i].Close(); err != nil {
						t.Fatal(err)
					}
					codes[i] = loaded
				}
				consumers[i], err = Instantiate(codes[i], imports)
				if err != nil {
					t.Fatal(err)
				}
				defer consumers[i].Close()
			}
			runtime.GC()
			args := []uint64{0xdeadbeef80000000, 0x123456789abcdef0, 0xabcdef017f800001, 0xfff0000000000001}
			normalized := []uint64{0x80000000, args[1], 0x7f800001, args[3]}
			importedGlobalInvoke(t, consumers[0], "set", append(slices.Clone(normalized), 44), args...)
			want := append(slices.Clone(normalized), 0x8899aabbccddeeff, 0x0807060504030201, 0x7ff0000000000001, 0x80000000deadbeef)
			got, err := producer.Invoke("get")
			if err != nil || !slices.Equal(got, want) {
				t.Fatal("scalar borrower damaged vector producer neighbors", got, err)
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			runtime.GC()
			if !producer.hasPhysicalResources() {
				t.Fatal("logical SIMD producer close unmapped borrowed scalar cells")
			}
			if objects[0].Get() != 0 || objects[0].Set(7) == nil {
				t.Fatal("closed producer global remained host accessible")
			}
			if late, err := Instantiate(codes[0], imports); late != nil || err == nil {
				t.Fatal("closed SIMD producer accepted a new global borrower")
			}
			for i, consumer := range consumers {
				importedGlobalInvoke(t, consumer, "get", append(slices.Clone(normalized), uint64(44+i*11)))
			}
			args = []uint64{0xdeadbeefffffffff, 0x8000000000000001, 0xdeadbeef80000000, 0x7ff0000000000042}
			normalized = []uint64{0xffffffff, args[1], 0x80000000, args[3]}
			importedGlobalInvoke(t, consumers[1], "set", append(slices.Clone(normalized), 55), args...)
			importedGlobalInvoke(t, consumers[0], "get", append(slices.Clone(normalized), 44))
			if err := consumers[0].Close(); err != nil {
				t.Fatal(err)
			}
			if !producer.hasPhysicalResources() {
				t.Fatal("first global borrower released SIMD producer storage")
			}
			importedGlobalInvoke(t, consumers[1], "get", append(slices.Clone(normalized), 55))
			if err := consumers[1].Close(); err != nil {
				t.Fatal(err)
			}
			if producer.hasPhysicalResources() {
				t.Fatal("final global borrower retained retired SIMD producer storage")
			}
			for _, object := range objects {
				if len(object.cell) != 0 {
					t.Fatal("retired exported object still names an arena cell")
				}
			}
		})
	}
}

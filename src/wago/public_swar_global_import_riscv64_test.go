//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"errors"
	"math"
	"runtime"
	"slices"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Six imports, a duplicate vector binding, then two owned globals make all
// three index spaces and both shared/owned state observable in one invocation.
func rv64PublicSWARImportedGlobalsModule(ownedMemory bool) []byte {
	memoryMode := 0
	if ownedMemory {
		memoryMode = 1
	}
	return rv64PublicSWARGlobalStateModule(memoryMode, 0)
}

// Modes 0/1 retain the qualified no-memory/owned-memory fixtures byte-for-byte.
// Mode 2 imports memory at a specified import-section position and adds real
// pair stores and memory.grow while imported vectors remain live.
func rv64PublicSWARGlobalStateModule(memoryMode, memoryImportPosition int) []byte {
	return rv64PublicSWARStateHostModule(memoryMode, memoryImportPosition, -1)
}

func rv64PublicSWARStateHostModule(memoryMode, memoryImportPosition, hostImportPosition int) []byte {
	types := []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64, wasm.V128, wasm.V128, wasm.V128, wasm.V128, wasm.I32}
	var imports [][]byte
	for i, typ := range types[:6] {
		imports = append(imports, wasmtest.GlobalImportEntry("env", "g"+string(rune('0'+i)), typ, i != 5))
	}
	imports = append(imports, wasmtest.GlobalImportEntry("env", "g4", wasm.V128, true))
	functionSignatures := [][]byte{wasmtest.FuncType(nil, types), wasmtest.FuncType(types[:5], types), wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.V128})}
	if memoryMode == 2 {
		memory := append(append(wasmtest.Name("env"), wasmtest.Name("memory")...), 2, 1, 1, 4)
		before := append([][]byte(nil), imports[:memoryImportPosition]...)
		before = append(before, memory)
		imports = append(before, imports[memoryImportPosition:]...)
		functionSignatures = append(functionSignatures, wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.V128}, []wasm.ValType{wasm.V128}), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.V128, wasm.I32}))
	}
	functionImportCount := uint32(0)
	if hostImportPosition >= 0 {
		functionImportCount = 1
		host := append(append(wasmtest.Name("env"), wasmtest.Name("host")...), 0, byte(len(functionSignatures)))
		before := append([][]byte(nil), imports[:hostImportPosition]...)
		before = append(before, host)
		imports = append(before, imports[hostImportPosition:]...)
		hostTypes := types[:4]
		functionSignatures = append(functionSignatures, wasmtest.FuncType(hostTypes, hostTypes), wasmtest.FuncType(append(append([]wasm.ValType(nil), types[:5]...), wasm.I32), []wasm.ValType{wasm.V128, wasm.V128, wasm.V128, wasm.I32, wasm.I64, wasm.F32, wasm.F64, wasm.V128, wasm.V128, wasm.V128}))
	}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(functionSignatures...)), wasmtest.Section(2, wasmtest.Vec(imports...))}
	functionTypes := []byte{3, 0, 1, 2}
	if memoryMode != 0 {
		functionTypes = []byte{4, 0, 1, 2, 3}
	}
	if memoryMode == 2 {
		functionTypes = []byte{6, 0, 1, 2, 3, 4, 5}
	}
	if hostImportPosition >= 0 {
		functionTypes[0]++
		functionTypes = append(functionTypes, byte(len(functionSignatures)-1))
	}
	sections = append(sections, wasmtest.Section(3, functionTypes))
	if memoryMode == 1 {
		sections = append(sections, wasmtest.Section(5, []byte{1, 1, 1, 2}))
	}
	literal := append(append([]byte{0xfd, 12}, make([]byte, 16)...), 0x0b)
	sections = append(sections, wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.V128, true, literal), wasmtest.GlobalEntry(wasm.I32, false, []byte{0x41, 33, 0x0b}))))
	functionExport := func(name string, index uint32) []byte {
		return wasmtest.ExportEntry(name, 0, index+functionImportCount)
	}
	exports := [][]byte{functionExport("get", 0), functionExport("set", 1), functionExport("private", 2)}
	for i := range types {
		exports = append(exports, wasmtest.ExportEntry("g"+string(rune('0'+i)), 3, uint32(i)))
	}
	if memoryMode != 0 {
		exports = append(exports, wasmtest.ExportEntry("memory", 2, 0), functionExport("load", 3))
	}
	if memoryMode == 2 {
		exports = append(exports, functionExport("store", 4), functionExport("grow", 5))
	}
	if hostImportPosition >= 0 {
		exports = append(exports, functionExport("host", uint32(functionTypes[0]-1)))
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(exports...)))
	var get []byte
	for i := range types {
		get = append(get, 0x23, byte(i))
	}
	set := slices.Clone(get)
	for i := range 5 {
		set = append(set, 0x20, byte(i), 0x24, byte(i))
	}
	set = append(set, 0x0b)
	bodies := [][]byte{wasmtest.Code(append(get, 0x0b)), wasmtest.Code(set), wasmtest.Code([]byte{0x23, 7, 0x20, 0, 0x24, 7, 0x0b})}
	if memoryMode != 0 {
		bodies = append(bodies, wasmtest.Code([]byte{0x20, 0, 0xfd, 0, 0, 0, 0x0b}))
	}
	if memoryMode == 2 {
		bodies = append(bodies, wasmtest.Code([]byte{0x23, 4, 0x20, 0, 0x20, 1, 0xfd, 11, 0, 0, 0x0b}), wasmtest.Code([]byte{0x23, 4, 0x20, 0, 0x40, 0, 0x0b}))
	}
	if hostImportPosition >= 0 {
		body := []byte{0x20, 4, 0x23, 4, 0x23, 7}
		for i := range 4 {
			body = append(body, 0x20, byte(i))
		}
		// An observable write after the callback detects interrupted guest resume.
		body = append(body, 0x10, 0, 0x41, 0xc0, 0, 0x41, 9, 0x3a, 0, 0, 0x20, 5, 0xfd, 0, 0, 0, 0x23, 4, 0x23, 7, 0x0b)
		bodies = append(bodies, wasmtest.Code(body))
	}
	return wasmtest.Module(append(sections, wasmtest.Section(10, wasmtest.Vec(bodies...)))...)
}

func rv64PublicSWARGlobalImportCompile(t *testing.T, artifact, ownedMemory bool) *Compiled {
	t.Helper()
	c, err := Compile(nil, rv64PublicSWARImportedGlobalsModule(ownedMemory))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if !artifact {
		return c
	}
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTrustedArtifact(blob)
	if loaded != nil {
		t.Cleanup(func() { _ = loaded.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	return loaded
}

func rv64PublicSWARImportedGlobalWant(bits []uint64, private V128) []uint64 {
	return append(append(slices.Clone(bits), bits[4], bits[5]), binary.LittleEndian.Uint64(private[:]), binary.LittleEndian.Uint64(private[8:]), 33)
}

func TestRV64PublicSWARImportedGlobalsRawBitsAliasesAndOwnedState(t *testing.T) {
	c := rv64PublicSWARGlobalImportCompile(t, false, true)
	loaded := rv64PublicSWARGlobalImportCompile(t, true, true)
	bits := []uint64{0xffffffff, 0x8000000000000001, 0x7f800001, 0x7ff0000000000001, 0x8899aabbccddeeff, 0x0807060504030201, 0x7ff0000000000001, 0x80000000deadbeef}
	objects := []*Global{NewGlobalI32(-1, true), NewGlobalI64(-9223372036854775807, true), NewGlobalF32(math.Float32frombits(uint32(bits[2])), true), NewGlobalF64(math.Float64frombits(bits[3]), true), NewGlobalV128(rv64PublicVector(bits[4], bits[5]), true), NewGlobalV128(rv64PublicVector(bits[6], bits[7]), false)}
	imports := NewImports()
	for i, g := range objects {
		t.Cleanup(func() { _ = g.Close() })
		imports.Global("env", "g"+string(rune('0'+i)), g)
	}
	if in, err := Instantiate(c); in != nil || err == nil {
		t.Fatal("missing global bindings admitted")
	}
	instances := make([]*Instance, 2)
	for i, code := range []*Compiled{c, loaded} {
		in, err := Instantiate(code, imports)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = in.Close() })
		instances[i] = in
		if err := code.Close(); err != nil {
			t.Fatal(err)
		}
		for _, index := range []int{4, 6} {
			g, err := in.ExportedGlobalObject("g" + string(rune('0'+index)))
			if err != nil || g != objects[4] {
				t.Fatal("duplicate vector import/reexport lost identity", index, err)
			}
		}
	}
	a, b := instances[0], instances[1]
	runtime.GC()
	for _, g := range objects {
		if err := g.Close(); err == nil {
			t.Fatal("closed host storage while native borrowers live")
		}
	}
	want := rv64PublicSWARImportedGlobalWant(bits, V128{})
	importedGlobalInvoke(t, a, "get", want)
	importedGlobalInvoke(t, b, "get", want)
	// Raw dirty cell upper bits must be ignored by native narrow loads.
	binary.LittleEndian.PutUint64(objects[0].cell, 0xaabbccddffffffff)
	binary.LittleEndian.PutUint64(objects[2].cell, 0xdeadbeef7f800001)
	importedGlobalInvoke(t, a, "get", want)
	args := []uint64{0xdeadbeef80000000, 0x123456789abcdef0, 0xaabbccddff800001, 0xfff0000000000001, 0x7ff0000000000042, 0xff8000017f800001}
	importedGlobalInvoke(t, a, "set", want, args...)
	bits = append([]uint64{0x80000000, args[1], 0xff800001, args[3], args[4], args[5]}, bits[6:]...)
	want = rv64PublicSWARImportedGlobalWant(bits, V128{})
	importedGlobalInvoke(t, b, "get", want)
	if binary.LittleEndian.Uint64(objects[0].cell) != bits[0] || binary.LittleEndian.Uint64(objects[2].cell) != bits[2] || objects[4].GetV128() != rv64PublicVector(bits[4], bits[5]) {
		t.Fatal("guest write did not normalize complete narrow cells or preserve both vector halves")
	}
	private := rv64PublicVector(0x8000000000000000, 0xffffffffffffffff)
	importedGlobalInvoke(t, a, "private", []uint64{0, 0}, binary.LittleEndian.Uint64(private[:]), binary.LittleEndian.Uint64(private[8:]))
	importedGlobalInvoke(t, a, "get", rv64PublicSWARImportedGlobalWant(bits, private))
	importedGlobalInvoke(t, b, "get", want)
	memory, err := a.ExportedMemory("memory")
	if err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint64(memory.UnsafeBytes()[3:], args[4])
	binary.LittleEndian.PutUint64(memory.UnsafeBytes()[11:], args[5])
	importedGlobalInvoke(t, a, "load", args[4:], 0xaabbccdd00000003)
	importedGlobalInvoke(t, b, "load", []uint64{0, 0}, 3)
	if err := a.SetGlobalV128("g5", private); err == nil {
		t.Fatal("immutable imported vector mutated")
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	importedGlobalInvoke(t, b, "get", want)
	if err := objects[4].Close(); err == nil {
		t.Fatal("first borrower close released shared vector")
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	for _, g := range objects {
		if err := g.Close(); err != nil {
			t.Fatal("last borrower retained host storage", err)
		}
	}
}

func TestRV64PublicSWARImportedGlobalsRetainRetiredVectorProducer(t *testing.T) {
	producerCode, err := Compile(nil, rv64PublicSWARGlobalExportsModule())
	if err != nil {
		t.Fatal(err)
	}
	defer producerCode.Close()
	producer, err := Instantiate(producerCode)
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	imports := NewImports()
	var objects []*Global
	for i := range 6 {
		g, err := producer.ExportedGlobalObject("g" + string(rune('0'+i)))
		if err != nil {
			t.Fatal(err)
		}
		objects = append(objects, g)
		imports.Global("env", "g"+string(rune('0'+i)), g)
	}
	codes := []*Compiled{rv64PublicSWARGlobalImportCompile(t, false, false), rv64PublicSWARGlobalImportCompile(t, true, false)}
	var borrowers []*Instance
	for _, c := range codes {
		in, err := Instantiate(c, imports)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = in.Close() })
		borrowers = append(borrowers, in)
	}
	if err := producerCode.Close(); err != nil {
		t.Fatal(err)
	}
	if err := producer.Close(); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	if !producer.hasPhysicalResources() || objects[4].GetV128() != (V128{}) || objects[4].SetV128(V128{}) == nil {
		t.Fatal("logical producer retirement lost retained storage or kept host access")
	}
	if late, err := Instantiate(codes[0], imports); late != nil || err == nil {
		t.Fatal("late importer attached a retired producer")
	}
	initial := []uint64{0xffffffff, 0x8000000000000001, 0x7f800001, 0x7ff0000000000001, 0x8899aabbccddeeff, 0x0807060504030201, 0x7ff0000000000001, 0x80000000deadbeef}
	want := rv64PublicSWARImportedGlobalWant(initial, V128{})
	importedGlobalInvoke(t, borrowers[0], "get", want)
	args := []uint64{0xaabbccdd80000000, 9, 0xdeadbeef80000000, 0x8000000000000000, 0xffffffffffffffff, 0x7ff0000000000001}
	importedGlobalInvoke(t, borrowers[0], "set", want, args...)
	updated := append([]uint64{0x80000000, 9, 0x80000000, args[3], args[4], args[5]}, initial[6:]...)
	want = rv64PublicSWARImportedGlobalWant(updated, V128{})
	if err := borrowers[0].Close(); err != nil {
		t.Fatal(err)
	}
	if !producer.hasPhysicalResources() {
		t.Fatal("first borrower released producer")
	}
	if err := codes[1].Close(); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	importedGlobalInvoke(t, borrowers[1], "get", want)
	if err := borrowers[1].Close(); err != nil {
		t.Fatal(err)
	}
	if producer.hasPhysicalResources() {
		t.Fatal("last borrower retained producer")
	}
	for _, g := range objects {
		if len(g.cell) != 0 {
			t.Fatal("released producer global names freed storage")
		}
	}
}

func TestRV64PublicSWARGlobalImportMetadataAndBindings(t *testing.T) {
	c := rv64PublicSWARGlobalImportCompile(t, false, false)
	base := c.executionView()
	for _, mutate := range []func(*Compiled){
		func(v *Compiled) { v.GlobalImports = slices.Clone(v.GlobalImports); v.GlobalImports[4].Type = ValI64 },
		func(v *Compiled) { v.GlobalImports = slices.Clone(v.GlobalImports); v.GlobalImports[4].Mutable = false },
		func(v *Compiled) {
			v.Globals = append(slices.Clone(v.Globals), make([]GlobalDef, 257-len(v.Globals))...)
		},
		func(v *Compiled) {
			v.Globals = slices.Clone(v.Globals)
			v.Globals[8].HasInitGlobal = true
			v.Globals[8].InitGlobal = 0
		},
	} {
		value := *base
		mutate(&value)
		if err := railshotValidateCompiled(&value); !errors.Is(err, railshot.ErrUnsupported) {
			t.Fatal("unsupported global metadata admitted", err)
		}
	}
	for _, wrong := range []GlobalImport{{Type: ValI64, Mutable: true}, {Type: ValV128, Mutable: false}} {
		imports := NewImports()
		for i, typ := range []ValType{ValI32, ValI64, ValF32, ValF64, ValV128, ValV128} {
			imports.Global("env", "g"+string(rune('0'+i)), GlobalImport{Type: typ, Mutable: i != 5})
		}
		imports.Global("env", "g4", wrong)
		if in, err := Instantiate(c, imports); in != nil || err == nil {
			t.Fatal("wrong global type/mutability admitted")
		}
		if c.codeCache != nil && c.codeCache.sealed {
			t.Fatal("invalid binding published executable code")
		}
	}
}

func TestRV64PublicSWARImportedGlobalsMaterializedVectorRetirement(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			c := rv64PublicSWARGlobalImportCompile(t, artifact, false)
			bits := []uint64{0xffffffff, 9, 0x7f800001, 0x7ff0000000000001, 0x8899aabbccddeeff, 0x0807060504030201, 0x7ff0000000000001, 0x80000000deadbeef}
			values := NewImports()
			types := []ValType{ValI32, ValI64, ValF32, ValF64, ValV128, ValV128}
			for i, typ := range types {
				value := GlobalImport{Type: typ, Mutable: i != 5}
				if i < 4 {
					value.Bits = bits[i]
				} else {
					offset := 4 + (i-4)*2
					value.V128 = rv64PublicVector(bits[offset], bits[offset+1])
				}
				values.Global("env", "g"+string(rune('0'+i)), value)
			}
			producer, err := Instantiate(c, values)
			if err != nil {
				t.Fatal(err)
			}
			defer producer.Close()
			imports := NewImports()
			var vector *Global
			for i := range types {
				g, err := producer.ExportedGlobalObject("g" + string(rune('0'+i)))
				if err != nil || g.instanceOwner() != producer {
					t.Fatal("materialized import lost its instance owner", err)
				}
				imports.Global("env", "g"+string(rune('0'+i)), g)
				if i == 4 {
					vector = g
				}
			}
			alias, err := producer.ExportedGlobalObject("g6")
			if err != nil || alias != vector || vector.GetV128() != rv64PublicVector(bits[4], bits[5]) {
				t.Fatal("materialized duplicate vector alias", err)
			}
			borrower, err := Instantiate(c, imports)
			if err != nil {
				t.Fatal(err)
			}
			defer borrower.Close()
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			runtime.GC()
			if !producer.hasPhysicalResources() || vector.GetV128() != (V128{}) || vector.SetV128(V128{}) == nil {
				t.Fatal("materialized vector retirement lost retained arena or retained host access")
			}
			want := rv64PublicSWARImportedGlobalWant(bits, V128{})
			importedGlobalInvoke(t, borrower, "get", want)
			args := []uint64{0xdeadbeefffffffff, 11, 0xdeadbeef80000000, 0xfff0000000000001, 0xffffffffffffffff, 0x8000000000000000}
			importedGlobalInvoke(t, borrower, "set", want, args...)
			updated := append([]uint64{0xffffffff, 11, 0x80000000, args[3], args[4], args[5]}, bits[6:]...)
			importedGlobalInvoke(t, borrower, "get", rv64PublicSWARImportedGlobalWant(updated, V128{}))
			if err := borrower.Close(); err != nil {
				t.Fatal(err)
			}
			if producer.hasPhysicalResources() {
				t.Fatal("materialized vector final borrower retained arena")
			}
			if len(vector.cell) != 0 {
				t.Fatal("released materialized vector names freed storage")
			}
		})
	}
}

func TestRV64PublicSWARGlobalImportLastDirectoryCell(t *testing.T) {
	imports := make([][]byte, 256)
	for i := range imports {
		imports[i] = wasmtest.GlobalImportEntry("env", "v", wasm.V128, false)
	}
	source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}))), wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("last", 0, 0), wasmtest.ExportEntry("global", 3, 255))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0xff, 1, 0x0b}))))
	c, err := Compile(nil, source)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
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
	vector := rv64PublicVector(0x80000000deadbeef, 0x7ff0000000000001)
	values := NewImports().Global("env", "v", GlobalImport{Type: ValV128, V128: vector})
	for _, code := range []*Compiled{c, loaded} {
		in, err := Instantiate(code, values)
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		importedGlobalInvoke(t, in, "last", []uint64{0x80000000deadbeef, 0x7ff0000000000001})
		if g, err := in.GlobalV128("global"); err != nil || g != vector {
			t.Fatal("last imported directory cell/reexport", err)
		}
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRV64PublicSWARImportedGlobalDataOffsetRemainsLiteral(t *testing.T) {
	c := rv64PublicSWARGlobalImportCompile(t, false, true)
	view := *c.executionView()
	view.GlobalImports = slices.Clone(view.GlobalImports)
	view.Globals = slices.Clone(view.Globals)
	view.GlobalImports[0].Mutable = false
	view.Globals[0].Mutable = false
	if !rv64ImportedInitializer(&view, 0, ValI32) {
		t.Fatal("fixture must have an admissible immutable imported I32")
	}
	if err := railshotValidateCompiled(&view); err != nil {
		t.Fatal("base vector metadata rejected", err)
	}
	view.Data = []DataInit{{Bytes: []byte{1}, Offset: OffsetInit{HasGlobal: true, Global: 0}}}
	if err := railshotValidateCompiled(&view); !errors.Is(err, railshot.ErrUnsupported) {
		t.Fatal("SIMD imported-global data offset admitted", err)
	}
	view.Data[0].Offset = OffsetInit{}
	if err := railshotValidateCompiled(&view); err != nil {
		t.Fatal("literal active data offset rejected", err)
	}
}

func TestRV64PublicSWARImportedGlobalMixturesRemainClosed(t *testing.T) {
	global := wasmtest.GlobalImportEntry("env", "v", wasm.V128, true)
	host := append(append(wasmtest.Name("env"), wasmtest.Name("host")...), 0, 0)
	memory := append(append(wasmtest.Name("env"), wasmtest.Name("memory")...), 2, 1, 1, 2)
	table := append(append(wasmtest.Name("env"), wasmtest.Name("table")...), 1, 0x6f, 0, 1)
	for _, entries := range [][][]byte{{global, table}, {global, host, table}, {host, global, table}, {global, memory, host, table}, {memory, global, table}} {
		functionIndex := uint32(0)
		for _, entry := range entries {
			if slices.Equal(entry, host) {
				functionIndex++
			}
		}
		source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}))), wasmtest.Section(2, wasmtest.Vec(entries...)), wasmtest.Section(3, []byte{1, 1}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, functionIndex))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x0b}))))
		m, err := wasm.DecodeModule(source)
		if err != nil {
			t.Fatal(err)
		}
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal("mixture fixture must be normally valid", err)
		}
		if c, err := Compile(nil, source); c != nil || !errors.Is(err, railshot.ErrUnsupported) {
			t.Fatal("pending mixed import profile admitted", err)
		}
	}
}

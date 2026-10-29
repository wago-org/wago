//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"math"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// The local fifth global makes imported/defined index ordering observable.
func publicImportedGlobalModule(imported bool, memory int, mutable bool, localInitial ...byte) []byte {
	types := []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}
	results := append(slices.Clone(types), wasm.I32)
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, results), wasmtest.FuncType(types, results)))}
	if imported {
		var imports [][]byte
		for i, typ := range types {
			imports = append(imports, wasmtest.GlobalImportEntry("env", "g"+string(rune('0'+i)), typ, mutable))
		}
		if memory >= 2 {
			entry := append(append(wasmtest.Name("env"), wasmtest.Name("memory")...), 2, 1, 1, 4)
			switch memory {
			case 3:
				imports = append([][]byte{entry}, imports...)
			case 4:
				imports = append(append(append([][]byte{}, imports[:2]...), entry), imports[2:]...)
			default:
				imports = append(imports, entry)
			}
		}
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(imports...)))
	}
	sections = append(sections, wasmtest.Section(3, []byte{2, 0, 1}))
	if memory == 1 {
		sections = append(sections, wasmtest.Section(5, []byte{1, 1, 1, 4}))
	}
	var globals [][]byte
	if !imported {
		globals = append(globals,
			wasmtest.GlobalEntry(wasm.I32, mutable, []byte{0x41, 7, 0x0b}),
			wasmtest.GlobalEntry(wasm.I64, mutable, []byte{0x42, 9, 0x0b}),
			wasmtest.GlobalEntry(wasm.F32, mutable, []byte{0x43, 0, 0, 0, 0x80, 0x0b}),
			wasmtest.GlobalEntry(wasm.F64, mutable, []byte{0x44, 0, 0, 0, 0, 0, 0, 0, 0x80, 0x0b}),
		)
	}
	local := byte(33)
	if len(localInitial) != 0 {
		local = localInitial[0]
	}
	globals = append(globals, wasmtest.GlobalEntry(wasm.I32, false, []byte{0x41, local, 0x0b}))
	sections = append(sections, wasmtest.Section(6, wasmtest.Vec(globals...)))
	exports := [][]byte{wasmtest.ExportEntry("get", 0, 0), wasmtest.ExportEntry("set", 0, 1)}
	for i := range types {
		exports = append(exports, wasmtest.ExportEntry("g"+string(rune('0'+i)), 3, uint32(i)))
	}
	if memory != 0 {
		exports = append(exports, wasmtest.ExportEntry("memory", 2, 0))
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(exports...)))
	get := []byte{0x23, 0, 0x23, 1, 0x23, 2, 0x23, 3, 0x23, 4, 0x0b}
	set := slices.Clone(get)
	if mutable {
		set = append([]byte{0x20, 0, 0x24, 0, 0x20, 1, 0x24, 1, 0x20, 2, 0x24, 2, 0x20, 3, 0x24, 3}, get...)
	}
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(get), wasmtest.Code(set))))
	return wasmtest.Module(sections...)
}

func importedGlobalCompile(t *testing.T, imported bool, memory int, mutable bool) *Compiled {
	t.Helper()
	c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), publicImportedGlobalModule(imported, memory, mutable))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func importedGlobalInvoke(t *testing.T, in *Instance, name string, want []uint64, args ...uint64) {
	t.Helper()
	got, err := in.Invoke(name, args...)
	if err != nil || !slices.Equal(got, want) {
		t.Fatal(name, got, want, err)
	}
}

func TestPublicImportedGlobalValueReexportRetainsMaterializedCells(t *testing.T) {
	c := importedGlobalCompile(t, true, 0, true)
	imports := NewImports()
	types := []ValType{ValI32, ValI64, ValF32, ValF64}
	bits := []uint64{0xdeadbeefffffffff, 0x8000000000000001, 0xbeefcafe7f800001, 0x7ff0000000000001}
	for i, typ := range types {
		imports.Global("env", "g"+string(rune('0'+i)), GlobalImport{Type: typ, Mutable: true, Bits: bits[i]})
	}
	p, err := Instantiate(c, imports)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	borrow := NewImports()
	var objects []*Global
	for i := range types {
		g, err := p.ExportedGlobalObject("g" + string(rune('0'+i)))
		if err != nil {
			t.Fatal(err)
		}
		if g.instanceOwner() != p {
			t.Fatal("materialized value global lost its arena owner")
		}
		objects = append(objects, g)
		borrow.Global("env", "g"+string(rune('0'+i)), g)
	}
	in, err := Instantiate(c, borrow)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if !p.hasPhysicalResources() {
		t.Fatal("value-global re-export failed to retain producer arena")
	}
	if late, err := Instantiate(c, borrow); late != nil || err == nil {
		t.Fatal("late global import attached a closed producer")
	}
	want := []uint64{0xffffffff, bits[1], uint64(uint32(bits[2])), bits[3], 33}
	importedGlobalInvoke(t, in, "get", want)
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if p.hasPhysicalResources() {
		t.Fatal("final global importer retained producer arena")
	}
	for _, g := range objects {
		if len(g.cell) != 0 {
			t.Fatal("released materialized global still names an arena cell")
		}
	}
}

func TestPublicImportedGlobalHostObjectBitsAndArtifact(t *testing.T) {
	c := importedGlobalCompile(t, true, 1, true)
	globals := []*Global{NewGlobalI32(-1, true), NewGlobalI64(-2, true), NewGlobalF32(math.Float32frombits(0x7f800001), true), NewGlobalF64(math.Float64frombits(0x7ff0000000000001), true)}
	imports := NewImports()
	for i, g := range globals {
		imports.Global("env", "g"+string(rune('0'+i)), g)
		defer g.Close()
	}

	in, err := Instantiate(c, imports)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	for _, g := range globals {
		if err := g.Close(); err == nil {
			t.Fatal("host closed live imported global")
		}
	}
	want := []uint64{0xffffffff, ^uint64(1), 0x7f800001, 0x7ff0000000000001, 33}
	importedGlobalInvoke(t, in, "get", want)
	args := []uint64{0xffffaaaa80000001, 0xffffffffffffffff, 0xbeefdead80000000, 0xfff80000deadbeef}
	want = []uint64{0x80000001, args[1], 0x80000000, args[3], 33}
	importedGlobalInvoke(t, in, "set", want, args...)
	for i, g := range globals {
		if g.Get() != want[i] {
			t.Fatal("guest global write did not reach host object")
		}
	}
	// Dirty incoming upper bits are ignored by every narrow global consumer;
	// narrow writes replace the complete shared eight-byte cell canonically.
	for _, i := range []int{0, 2} {
		if binary.LittleEndian.Uint64(globals[i].cell) != want[i] {
			t.Fatal("narrow shared cell upper word was not normalized")
		}
	}
	if err := globals[0].Set(0xaaaaaaaaffffffff); err != nil {
		t.Fatal(err)
	}
	want[0] = 0xffffffff
	importedGlobalInvoke(t, in, "get", want)
	fn, err := in.WasmFunc("get")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fn.Invoke(); err != nil {
		t.Fatal(err)
	}
	if n := testing.AllocsPerRun(100, func() {
		if _, err := fn.Invoke(); err != nil {
			panic(err)
		}
	}); n != 0 {
		t.Fatal("warm imported-global call allocation", n)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	for _, g := range globals {
		if err := g.Close(); err != nil {
			t.Fatal("global importer failed to detach", err)
		}
	}
}

func TestPublicImportedGlobalBindingRejectsMissingTypeAndMutability(t *testing.T) {
	c := importedGlobalCompile(t, true, 0, true)
	if in, err := Instantiate(c); in != nil || err == nil {
		t.Fatal("missing globals accepted")
	}
	for _, bad := range []GlobalImport{{Type: ValI64, Mutable: true}, {Type: ValI32, Mutable: false}} {
		imports := NewImports().Global("env", "g0", bad).Global("env", "g1", GlobalImport{Type: ValI64, Mutable: true}).Global("env", "g2", GlobalImport{Type: ValF32, Mutable: true}).Global("env", "g3", GlobalImport{Type: ValF64, Mutable: true})
		if in, err := Instantiate(c, imports); in != nil || err == nil {
			t.Fatal("global type/mutability mismatch accepted")
		}
	}
}

func TestPublicImportedGlobalMixedMemoryOrderingPrivateContextAndRoots(t *testing.T) {
	for _, memoryOrder := range []int{2, 3, 4} {
		p, err := Instantiate(importedGlobalCompile(t, false, 1, true))
		if err != nil {
			t.Fatal(err)
		}
		defer p.Close()
		imports := NewImports()
		for i := 0; i < 4; i++ {
			g, err := p.ExportedGlobalObject("g" + string(rune('0'+i)))
			if err != nil {
				t.Fatal(err)
			}
			imports.Global("env", "g"+string(rune('0'+i)), g)
		}
		memory, err := p.ExportedMemory("memory")
		if err != nil {
			t.Fatal(err)
		}
		imports.Memory("env", "memory", memory)
		var consumers []*Instance
		for _, initial := range []byte{44, 55} {
			c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), publicImportedGlobalModule(true, memoryOrder, true, initial))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, imports)
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			consumers = append(consumers, in)
		}
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
		for n, in := range consumers {
			initial := uint64(44 + n*11)
			want := []uint64{7, 9, 0x80000000, 0x8000000000000000, initial}
			importedGlobalInvoke(t, in, "get", want)
		}
		for n, in := range consumers {
			initial := uint64(44 + n*11)
			args := []uint64{0xdeadbeefffffffff, 0x8000000000000001, 0xffff00007f800001, 0x7ff0000000000001}
			want := []uint64{0xffffffff, args[1], uint64(uint32(args[2])), args[3], initial}
			importedGlobalInvoke(t, in, "set", want, args...)
		}
		for n, in := range consumers {
			importedGlobalInvoke(t, in, "get", []uint64{0xffffffff, 0x8000000000000001, 0x7f800001, 0x7ff0000000000001, uint64(44 + n*11)})
		}
		if err := consumers[0].Close(); err != nil {
			t.Fatal(err)
		}
		if !p.hasPhysicalResources() {
			t.Fatal("first borrower released shared producer")
		}
		if err := consumers[1].Close(); err != nil {
			t.Fatal(err)
		}
		if p.hasPhysicalResources() || memory.jobMemory() != nil {
			t.Fatal("final mixed-resource borrower retained producer")
		}
	}
}

func publicGlobalBoundaryModule(importsCount int, local bool) []byte {
	imports := make([][]byte, importsCount)
	for i := range imports {
		imports[i] = wasmtest.GlobalImportEntry("env", "shared", wasm.I32, false)
	}
	parts := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(imports...)),
		wasmtest.Section(3, []byte{1, 0}),
	}
	if local {
		parts = append(parts, wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, []byte{0x41, 7, 0x0b}))))
	}
	parts = append(parts, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("get", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(append(append([]byte{0x23}, wasmtest.ULEB(uint32(importsCount-1))...), 0x0b)))))
	return wasmtest.Module(parts...)
}

func TestPublicImportedGlobalBoundaryAndDuplicateAliasRoot(t *testing.T) {
	c, err := Compile(nil, publicGlobalBoundaryModule(256, false))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	g := NewGlobalI32(42, false)
	defer g.Close()
	in, err := Instantiate(c, NewImports().Global("env", "shared", g))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	g.owner.mu.Lock()
	count := g.owner.importers
	g.owner.mu.Unlock()
	if count != 1 {
		t.Fatal("duplicate global keys retained multiple importer roots", count)
	}
	importedGlobalInvoke(t, in, "get", []uint64{42})
	for _, data := range [][]byte{publicGlobalBoundaryModule(257, false), publicGlobalBoundaryModule(256, true)} {
		compiled, err := Compile(nil, data)
		if err != nil {
			t.Fatal("global declarations above 256", err)
		}
		instance, err := Instantiate(compiled, NewImports().Global("env", "shared", g))
		if err != nil {
			compiled.Close()
			t.Fatal("global declarations above 256", err)
		}
		importedGlobalInvoke(t, instance, "get", []uint64{42})
		instance.Close()
		compiled.Close()
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if err := g.Close(); err != nil {
		t.Fatal("duplicate alias roots failed to detach", err)
	}
}

func TestPublicImportedGlobalRollbackLeavesHostCellsAndDetaches(t *testing.T) {
	data := publicImportedGlobalModule(true, 1, true)
	data = append(data, wasmtest.Section(11, []byte{1, 0, 0x41, 0x80, 0x80, 4, 0x0b, 1, 0x5a})...)
	c, err := Compile(nil, data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	objects := []*Global{NewGlobalI32(-1, true), NewGlobalI64(-2, true), NewGlobalF32(-0.5, true), NewGlobalF64(-1.5, true)}
	imports := NewImports()
	for i, g := range objects {
		imports.Global("env", "g"+string(rune('0'+i)), g)
		defer g.Close()
	}
	before := []uint64{objects[0].Get(), objects[1].Get(), objects[2].Get(), objects[3].Get()}
	if in, err := Instantiate(c, imports); in != nil || err == nil {
		t.Fatal("bad initializer admitted")
	}
	for i, g := range objects {
		if g.Get() != before[i] {
			t.Fatal("failed instantiation modified imported cell")
		}
		if err := g.Close(); err != nil {
			t.Fatal("failed instantiation leaked imported global root", err)
		}
	}
}

//go:build linux && (amd64 || arm64 || (riscv64 && wago_rv64_experimental)) && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"errors"
	"runtime"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func ownedTableDeclaration(size uint32) []byte {
	return wasmtest.Section(4, append([]byte{1, 0x70, 0}, wasmtest.ULEB(size)...))
}
func ownedTableElements(offset int32, functions ...uint32) []byte {
	e := append([]byte{0, 0x41}, wasmtest.SLEB32(offset)...)
	e = append(e, 0x0b)
	e = append(e, wasmtest.ULEB(uint32(len(functions)))...)
	for _, f := range functions {
		e = append(e, wasmtest.ULEB(f)...)
	}
	return wasmtest.Section(9, wasmtest.Vec(e))
}

func publicOwnedTableModule(seed byte, importedMemory bool, size uint32, offset int32, functions ...uint32) []byte {
	types := [][]byte{
		wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), // equivalent duplicate
		wasmtest.FuncType([]wasm.ValType{wasm.F32}, []wasm.ValType{wasm.F32}),
	}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(types...))}
	if importedMemory {
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("memory")...), 2, 1, 1, 4))))
	}
	sections = append(sections, wasmtest.Section(3, []byte{3, 0, 1, 3}), ownedTableDeclaration(size), wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, []byte{0x41, seed, 0x0b}))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 0))))
	if functions != nil {
		sections = append(sections, ownedTableElements(offset, functions...))
	}
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(
		wasmtest.Code([]byte{0x20, 1, 0x20, 0, 0x11, 2, 0, 0x0b}),
		wasmtest.Code([]byte{0x20, 0, 0x23, 0, 0x6a, 0x0b}),
		wasmtest.Code([]byte{0x20, 0, 0x0b}),
	)))
	return wasmtest.Module(sections...)
}

func ownedTableConfig() *RuntimeConfig {
	return NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit)
}

func ownedTableCompile(t *testing.T, data []byte) *Compiled {
	t.Helper()
	c, e := Compile(ownedTableConfig(), data)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}
func ownedTableTrap(t *testing.T, err error, code coreruntime.TrapCode) *coreruntime.TrapError {
	t.Helper()
	var trap *coreruntime.TrapError
	if !errors.As(err, &trap) || trap.Code != code {
		t.Fatal("table trap", code, err)
	}
	return trap
}

func TestPublicOwnedTableExactSignatureNullBoundsAndArtifact(t *testing.T) {
	c := ownedTableCompile(t, publicOwnedTableModule(7, false, 3, 0, 1, 2))
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTrustedArtifact(blob)
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	in, err := Instantiate(loaded)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if err := loaded.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	if _, err := in.ExportedTable("private"); err == nil {
		t.Fatal("private table escaped through fallback")
	}
	importedGlobalInvoke(t, in, "call", []uint64{12}, 0xfeedface00000000, 0xdeadbeef00000005)
	for _, tc := range []struct {
		index uint64
		code  coreruntime.TrapCode
	}{{1, coreruntime.TrapIndirectWrongSig}, {2, coreruntime.TrapIndirectOutOfBounds}, {3, coreruntime.TrapIndirectOutOfBounds}, {0xffffffffffffffff, coreruntime.TrapIndirectOutOfBounds}} {
		_, err := in.Invoke("call", tc.index, 5)
		trap := ownedTableTrap(t, err, tc.code)
		if len(trap.Frames) != 1 || trap.Frames[0].FunctionIndex != 0 || (runtime.GOARCH == "riscv64" && (trap.Frames[0].ProgramCounter != 5 || !trap.Frames[0].HasProgramCounter)) {
			t.Fatal("indirect trap source", trap.Frames)
		}
		importedGlobalInvoke(t, in, "call", []uint64{12}, 0, 5)
	}
	fn, err := in.WasmFunc("call")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fn.Invoke(0, 5); err != nil {
		t.Fatal(err)
	}
	if n := testing.AllocsPerRun(100, func() {
		v, e := fn.Invoke(0, 5)
		if e != nil || !slices.Equal(v, []uint64{12}) {
			panic("owned table call")
		}
	}); n != 0 {
		t.Fatal("warm indirect call allocation", n)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if in.hasPhysicalResources() {
		t.Fatal("table descriptors retained after final Close")
	}
}

func TestPublicOwnedTableEmptyAndNullPriority(t *testing.T) {
	for _, size := range []uint32{0, 1} {
		c := ownedTableCompile(t, publicOwnedTableModule(7, false, size, 0))
		in, err := Instantiate(c)
		if err != nil {
			t.Fatal(err)
		}
		_, err = in.Invoke("call", 0, 1)
		ownedTableTrap(t, err, coreruntime.TrapIndirectOutOfBounds)
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPublicOwnedTableSharedMemoryPrivateContexts(t *testing.T) {
	providerCode := importedGlobalCompile(t, false, 1, false)
	provider, err := Instantiate(providerCode)
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	memory, err := provider.ExportedMemory("memory")
	if err != nil {
		t.Fatal(err)
	}
	im := NewImports().Memory("env", "memory", memory)
	aCode := ownedTableCompile(t, publicOwnedTableModule(7, true, 1, 0, 1))
	bCode := ownedTableCompile(t, publicOwnedTableModule(9, true, 1, 0, 1))
	a, err := Instantiate(aCode, im)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Instantiate(bCode, im)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if a.jm != b.jm || a.nativeContext == b.nativeContext {
		t.Fatal("fixture did not share home with different contexts")
	}
	if err := provider.Close(); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		importedGlobalInvoke(t, a, "call", []uint64{12}, 0, 5)
		importedGlobalInvoke(t, b, "call", []uint64{14}, 0, 5)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	importedGlobalInvoke(t, b, "call", []uint64{14}, 0, 5)
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if provider.hasPhysicalResources() {
		t.Fatal("last table consumer retained borrowed memory")
	}
}

func TestPublicOwnedTableInitializerFailureReleasesImportedMemory(t *testing.T) {
	for _, tc := range []struct {
		offset    int32
		functions []uint32
	}{{2, []uint32{1}}, {2, []uint32{}}} {
		providerCode := importedGlobalCompile(t, false, 1, false)
		provider, err := Instantiate(providerCode)
		if err != nil {
			t.Fatal(err)
		}
		defer provider.Close()
		memory, err := provider.ExportedMemory("memory")
		if err != nil {
			t.Fatal(err)
		}
		c := ownedTableCompile(t, publicOwnedTableModule(7, true, 1, tc.offset, tc.functions...))
		if in, err := Instantiate(c, NewImports().Memory("env", "memory", memory)); in != nil || err == nil {
			t.Fatal("out-of-bounds table initializer instantiated", in, err)
		}
		importedGlobalInvoke(t, provider, "get", []uint64{7, 9, 0x80000000, 0x8000000000000000, 33})
		if err := provider.Close(); err != nil {
			t.Fatal(err)
		}
		if provider.hasPhysicalResources() {
			t.Fatal("failed table initializer retained borrowed memory")
		}
	}
}

func publicOwnedTableRecursionModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, []byte{1, 0}), ownedTableDeclaration(1), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("recur", 0, 0))), ownedTableElements(0, 0),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x45, 0x04, 0x7f, 0x41, 7, 0x05, 0x20, 0, 0x41, 1, 0x6b, 0x41, 0, 0x11, 0, 0, 0x0b, 0x0b}))),
	)
}

func TestPublicOwnedTableRecursiveStackTrapAndReuse(t *testing.T) {
	c, err := Compile(ownedTableConfig().WithNativeStackBytes(MinNativeStackBytes), publicOwnedTableRecursionModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	importedGlobalInvoke(t, in, "recur", []uint64{7}, 5)
	_, err = in.Invoke("recur", 100000)
	ownedTableTrap(t, err, coreruntime.TrapStackFenceBreached)
	importedGlobalInvoke(t, in, "recur", []uint64{7}, 5)
}

func TestPublicOwnedTableMixed256Slots(t *testing.T) {
	var types []wasm.ValType
	var body []byte
	var args, want []uint64
	for i := range 256 {
		types = append(types, []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}[i%4])
		body = append(body, 0x20)
		body = append(body, wasmtest.ULEB(uint32(i))...)
		bits := []uint64{0xffffffff, 0x8000000000000001, 0x7f800001, 0x7ff0000000000001}[i%4]
		want = append(want, bits)
		if i%2 == 0 {
			bits |= 0xdeadbeef00000000
		}
		args = append(args, bits)
	}
	caller := append(slices.Clone(body), 0x41, 0, 0x11, 1, 0, 0x0b)
	callee := append(slices.Clone(body), 0x0b)
	data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types), wasmtest.FuncType(types, types))), wasmtest.Section(3, []byte{2, 0, 1}), ownedTableDeclaration(1), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("wide", 0, 0))), ownedTableElements(0, 1), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(caller), wasmtest.Code(callee))))
	c := ownedTableCompile(t, data)
	in, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	importedGlobalInvoke(t, in, "wide", want, args...)
	fn, err := in.WasmFunc("wide")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fn.Invoke(args...); err != nil {
		t.Fatal(err)
	}
	if n := testing.AllocsPerRun(100, func() {
		r, e := fn.Invoke(args...)
		if e != nil || !slices.Equal(r, want) {
			panic("wide table call")
		}
	}); n != 0 {
		t.Fatal("wide indirect allocations", n)
	}
}

func TestPublicOwnedTableUnusedLargeSignatureTrapsBeforeArenaWrites(t *testing.T) {
	large := make([]wasm.ValType, 256)
	for i := range large {
		large[i] = wasm.I64
	}
	var body []byte
	for range large {
		body = append(body, 0x42, 0x7f)
	}
	body = append(body, 0x20, 0, 0x11, 2, 0, 0x0b)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType(large, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, []byte{2, 0, 1}), ownedTableDeclaration(2), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 0))), ownedTableElements(0, 1), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body), wasmtest.Code([]byte{0x41, 7, 0x0b}))),
	)
	c := ownedTableCompile(t, data)
	in, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	wantArgs := 8
	if runtime.GOARCH == "riscv64" {
		// RV64 reserves every indirect type, even without a local definition.
		wantArgs = len(large) * 8
	}
	if len(in.serArgs) != wantArgs || len(in.results) != 8 {
		t.Fatal("indirect arena capacities", len(in.serArgs), len(in.results))
	}
	const sentinel = 0x123456789abcdef0
	for _, tc := range []struct {
		index uint64
		code  coreruntime.TrapCode
	}{{0, coreruntime.TrapIndirectWrongSig}, {1, coreruntime.TrapIndirectOutOfBounds}, {2, coreruntime.TrapIndirectOutOfBounds}} {
		binary.LittleEndian.PutUint64(in.results, sentinel)
		for i := 8; i < len(in.serArgs); i++ {
			in.serArgs[i] = 0xa5
		}
		argumentTail := slices.Clone(in.serArgs[8:])
		_, err := in.Invoke("call", tc.index)
		ownedTableTrap(t, err, tc.code)
		if binary.LittleEndian.Uint64(in.results) != sentinel {
			t.Fatal("failed indirect call overwrote adjacent result arena")
		}
		if !slices.Equal(in.serArgs[8:], argumentTail) {
			t.Fatal("failed indirect call marshaled arguments before guards")
		}
	}
}

func TestPublicOwnedTableSameWidthI64F64Mismatch(t *testing.T) {
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64}, []wasm.ValType{wasm.I64}), wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}), wasmtest.FuncType([]wasm.ValType{wasm.F64}, []wasm.ValType{wasm.F64}))),
		wasmtest.Section(3, []byte{2, 0, 2}), ownedTableDeclaration(1), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 0))), ownedTableElements(0, 1),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 1, 0x20, 0, 0x11, 1, 0, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x0b}))),
	)
	c := ownedTableCompile(t, data)
	in, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	_, err = in.Invoke("call", 0, 0x8000000000000001)
	ownedTableTrap(t, err, coreruntime.TrapIndirectWrongSig)
}

//go:build ((linux && amd64) || arm64) && !tinygo

package wago

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	wruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func globalDefEqual(a, b GlobalDef) bool {
	return a.Type == b.Type && a.Mutable == b.Mutable && a.Bits == b.Bits && a.V128 == b.V128 && a.HasInitGlobal == b.HasInitGlobal && a.InitGlobal == b.InitGlobal
}

func TestCompiledGlobalIndexHelpers(t *testing.T) {
	c := &Compiled{
		GlobalImports: []GlobalImportDef{{Module: "env", Name: "seed", Type: ValI32}},
		Globals:       []GlobalDef{{Type: ValI32}, {Type: ValI64, Mutable: true}},
		GlobalExports: map[string]int{"seed": 0, "counter": 1},
	}
	if got := c.ImportedGlobalCount(); got != 1 {
		t.Fatalf("ImportedGlobalCount = %d, want 1", got)
	}
	if got := c.LocalGlobalCount(); got != 1 {
		t.Fatalf("LocalGlobalCount = %d, want 1", got)
	}
	if got := c.GlobalSlot(1); got != 8 {
		t.Fatalf("GlobalSlot(1) = %d, want 8", got)
	}
	g, ok := c.ExportedGlobal("counter")
	if !ok || g.Type != ValI64 || !g.Mutable {
		t.Fatalf("ExportedGlobal(counter) = %+v, %v; want mutable i64", g, ok)
	}
	if _, ok := c.ExportedGlobal("missing"); ok {
		t.Fatal("ExportedGlobal(missing) ok, want false")
	}
}

func TestCompileGlobalMetadataNumericInits(t *testing.T) {
	f32bits := uint32(0x7fc12345)
	f64bits := uint64(0x7ff80000deadbeef)
	f32 := make([]byte, 4)
	binary.LittleEndian.PutUint32(f32, f32bits)
	f64 := make([]byte, 8)
	binary.LittleEndian.PutUint64(f64, f64bits)
	mod := wasmtest.Module(
		wasmtest.Section(6, wasmtest.Vec(
			wasmtest.GlobalEntry(wasm.I32, false, append(append([]byte{0x41}, wasmtest.SLEB32(-1)...), 0x0b)),
			wasmtest.GlobalEntry(wasm.I64, true, append(append([]byte{0x42}, wasmtest.SLEB64(-2)...), 0x0b)),
			wasmtest.GlobalEntry(wasm.F32, false, append(append([]byte{0x43}, f32...), 0x0b)),
			wasmtest.GlobalEntry(wasm.F64, true, append(append([]byte{0x44}, f64...), 0x0b)),
		)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("g32", 3, 0), wasmtest.ExportEntry("g64", 3, 1))),
	)
	c, err := Compile(nil, mod)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Globals) != 4 {
		t.Fatalf("globals = %d, want 4", len(c.Globals))
	}
	want := []GlobalDef{{Type: ValI32, Bits: math.MaxUint32}, {Type: ValI64, Mutable: true, Bits: ^uint64(1)}, {Type: ValF32, Bits: uint64(f32bits)}, {Type: ValF64, Mutable: true, Bits: f64bits}}
	for i := range want {
		if !globalDefEqual(c.Globals[i], want[i]) {
			t.Fatalf("global %d = %+v, want %+v", i, c.Globals[i], want[i])
		}
	}
	if c.GlobalExports["g32"] != 0 || c.GlobalExports["g64"] != 1 {
		t.Fatalf("global exports = %#v", c.GlobalExports)
	}
	if len(c.Exports) != 0 {
		t.Fatalf("function exports = %#v, want empty", c.Exports)
	}
}

func TestV128LocalGlobalGetSetAndAPI(t *testing.T) {
	init := V128{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	updated := V128{0xf0, 0xe1, 0xd2, 0xc3, 0xb4, 0xa5, 0x96, 0x87, 0x78, 0x69, 0x5a, 0x4b, 0x3c, 0x2d, 0x1e, 0x0f}
	v128Const := func(v V128) []byte { return append(append([]byte{0xfd, 0x0c}, v[:]...), 0x0b) }
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}),
			wasmtest.FuncType([]wasm.ValType{wasm.V128}, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.V128, true, v128Const(init)))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("read", 0, 0),
			wasmtest.ExportEntry("write", 0, 1),
			wasmtest.ExportEntry("g", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x23, 0x00, 0x0b}),             // global.get 0
			wasmtest.Code([]byte{0x20, 0x00, 0x24, 0x00, 0x0b}), // local.get 0; global.set 0
		)),
	)
	c, err := Compile(nil, mod)
	if err != nil {
		t.Fatalf("Compile v128 global: %v", err)
	}
	if c.Globals[0].V128 != init {
		t.Fatalf("compiled v128 init = %x, want %x", c.Globals[0].V128, init)
	}
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatalf("Instantiate v128 global: %v", err)
	}
	defer in.Close()
	if got, err := in.GlobalV128("g"); err != nil || got != init {
		t.Fatalf("GlobalV128(g) = %x, %v; want %x", got, err, init)
	}
	res, err := in.Invoke("read")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got := v128FromSlots(res); got != init {
		t.Fatalf("read() = %x, want %x", got, init)
	}
	if _, err := in.Invoke("write", v128Slots(updated)...); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got, err := in.GlobalV128("g"); err != nil || got != updated {
		t.Fatalf("GlobalV128(g) after write = %x, %v; want %x", got, err, updated)
	}
	apiSet := V128{1, 1, 2, 3, 5, 8, 13, 21, 34, 55, 89, 144, 233, 0, 1, 2}
	if err := in.SetGlobalV128("g", apiSet); err != nil {
		t.Fatalf("SetGlobalV128: %v", err)
	}
	if res, err = in.Invoke("read"); err != nil || v128FromSlots(res) != apiSet {
		t.Fatalf("read() after SetGlobalV128 = %x, %v; want %x", v128FromSlots(res), err, apiSet)
	}
}

func TestV128ImportedGlobalSharedObject(t *testing.T) {
	initial := V128{0xaa, 0xbb, 0xcc, 0xdd, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	updated := V128{12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1, 0xdd, 0xcc, 0xbb, 0xaa}
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}),
			wasmtest.FuncType([]wasm.ValType{wasm.V128}, nil),
		)),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "g", wasm.V128, true))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("read", 0, 0),
			wasmtest.ExportEntry("write", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x23, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x24, 0x00, 0x0b}),
		)),
	)
	c, err := Compile(nil, mod)
	if err != nil {
		t.Fatalf("Compile imported v128 global: %v", err)
	}
	g := NewGlobalV128(initial, true)
	defer g.Close()
	in, err := Instantiate(c, InstantiateOptions{Imports: Imports{"env.g": g}})
	if err != nil {
		t.Fatalf("Instantiate imported v128 global: %v", err)
	}
	defer in.Close()
	res, err := in.Invoke("read")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got := v128FromSlots(res); got != initial {
		t.Fatalf("read imported = %x, want %x", got, initial)
	}
	if _, err := in.Invoke("write", v128Slots(updated)...); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := g.GetV128(); got != updated {
		t.Fatalf("host global after wasm write = %x, want %x", got, updated)
	}
}

func v128Slots(v V128) []uint64 {
	return []uint64{binary.LittleEndian.Uint64(v[0:8]), binary.LittleEndian.Uint64(v[8:16])}
}

func v128FromSlots(slots []uint64) V128 {
	var v V128
	if len(slots) >= 2 {
		binary.LittleEndian.PutUint64(v[0:8], slots[0])
		binary.LittleEndian.PutUint64(v[8:16], slots[1])
	}
	return v
}

func TestCompileRejectsGlobalInitializerTypeMismatch(t *testing.T) {
	mod := wasmtest.Module(wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, []byte{0x42, 0x00, 0x0b}))))
	if _, err := Compile(nil, mod); err == nil || !bytes.Contains([]byte(err.Error()), []byte("validate")) {
		t.Fatalf("Compile mismatch error = %v, want validate error", err)
	}
}

func TestCompileAcceptsImportedReferenceGlobalWithInstantiationOwnerGate(t *testing.T) {
	mod := wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "ref", wasm.FuncRef, false))))
	c, err := Compile(nil, mod)
	if err != nil {
		t.Fatalf("Compile imported reference global: %v", err)
	}
	defer c.Close()
	if _, err := Instantiate(c, Imports{"env.ref": GlobalImport{Type: ValFuncRef}}); err == nil || !bytes.Contains([]byte(err.Error()), []byte("explicit store-bound *Global")) {
		t.Fatalf("Instantiate error = %v, want explicit owner rejection", err)
	}
}

func TestCompileRejectsWasm3DecodedProposalFeatureBeforeLegacyDecode(t *testing.T) {
	mod := wasmtest.Module(wasmtest.Section(5, wasmtest.Vec([]byte{0x04, 0x00}))) // memory64 min 0
	if _, err := Compile(nil, mod); err == nil || !bytes.Contains([]byte(err.Error()), []byte("compile: unsupported memory memory64 at memory 0")) {
		t.Fatalf("Compile memory64 error = %v, want frontend support-pass rejection", err)
	}
}

func TestCompileAcceptsMemoryGrow(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x01, 0x40, 0x00, 0x0b}))), // i32.const 1; memory.grow 0; end
	)
	c, err := Compile(nil, mod)
	if err != nil {
		t.Fatalf("Compile memory.grow error = %v, want success", err)
	}
	if c.MemMaxPages <= c.MemMinPages {
		t.Fatalf("memory.grow module max pages = %d, min = %d; want grow headroom", c.MemMaxPages, c.MemMinPages)
	}
}

func TestCompileCapsNoGrowMemoryAtInitialPages(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x3f, 0x00, 0x0b}))), // memory.size 0; end
	)
	c, err := Compile(nil, mod)
	if err != nil {
		t.Fatalf("Compile memory.size error = %v", err)
	}
	if c.MemMaxPages != c.MemMinPages {
		t.Fatalf("no-grow module max pages = %d, min = %d; want initial-only reservation", c.MemMaxPages, c.MemMinPages)
	}
}

func TestConstExprUnsupportedOpcodeHasClearError(t *testing.T) {
	_, err := evalConstExpr([]byte{0x45, 0x0b}, wasm.I32) // i32.eqz is not a const-expression opcode.
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("unsupported const expression opcode 0x45")) {
		t.Fatalf("evalConstExpr unsupported opcode error = %v", err)
	}
}

func TestCompileRejectsMalformedGlobalConstExpressions(t *testing.T) {
	tests := []struct {
		name string
		init []byte
		want string
	}{
		{name: "missing end", init: []byte{0x41, 0x00}, want: "decode"},
		{name: "trailing bytes", init: []byte{0x41, 0x00, 0x0b, 0x00}, want: "decode"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mod := wasmtest.Module(wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, tt.init))))
			if _, err := Compile(nil, mod); err == nil || !bytes.Contains([]byte(err.Error()), []byte(tt.want)) {
				t.Fatalf("Compile error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestCompiledValidateRejectsMalformedMetadata(t *testing.T) {
	base := func() *Compiled {
		return &Compiled{
			Code:       []byte{0xc3},
			Entry:      []int{0},
			Funcs:      []FuncSig{{Results: []ValType{ValI32}}},
			Exports:    map[string]int{"f": 0},
			FuncTypeID: []uint32{1},
			Globals:    []GlobalDef{{Type: ValI32}},
		}
	}
	tests := []struct {
		name string
		mut  func(*Compiled)
		want string
	}{
		{name: "imports count mismatch", mut: func(c *Compiled) { c.NumImports = 1 }, want: "Imports length 0 != NumImports 1"},
		{name: "negative table size", mut: func(c *Compiled) { c.TableSize = -1 }, want: "negative TableSize"},
		{name: "table size without table", mut: func(c *Compiled) { c.TableSize = 1 }, want: "TableSize 1 without table"},
		{name: "table import without table", mut: func(c *Compiled) { c.tableImport = "env.t" }, want: "table import \"env.t\" without table"},
		{name: "imported table max without has max", mut: func(c *Compiled) {
			c.HasTable = true
			c.tableImport = "env.t"
			c.tableImportMin = 1
			c.tableImportMax = 2
		}, want: "imported table max without max flag"},
		{name: "imported table max below min", mut: func(c *Compiled) {
			c.HasTable = true
			c.tableImport = "env.t"
			c.tableImportMin = 3
			c.tableImportMax = 2
			c.tableImportHasMax = true
		}, want: "imported table max 2 < min 3"},
		{name: "elements without table", mut: func(c *Compiled) { c.Elems = []ElemInit{{}} }, want: "element segment(s) without table"},
		{name: "entry funcs mismatch", mut: func(c *Compiled) { c.Entry = nil }, want: "Entry length"},
		{name: "entry at end of code", mut: func(c *Compiled) { c.Entry = []int{1} }, want: "Entry[0] offset 1 out of code range 1"},
		{name: "func type count mismatch", mut: func(c *Compiled) { c.FuncTypeID = nil }, want: "FuncTypeID length"},
		{name: "global export out of range", mut: func(c *Compiled) { c.GlobalExports = map[string]int{"g": 1} }, want: "global export \"g\" index 1 out of range"},
		{name: "element func out of range", mut: func(c *Compiled) {
			c.HasTable = true
			c.TableSize = 1
			c.Elems = []ElemInit{{RefType: ValFuncRef, Mode: ElemModeActive, Values: []RefInit{{FuncIndex: 1}}}}
		}, want: "element 0 function 0 index 1 out of range"},
		{name: "passive element func out of range", mut: func(c *Compiled) {
			c.HasTable = true
			c.TableSize = 1
			c.passiveElems = []ElemInit{{RefType: ValFuncRef, Mode: ElemModePassive, Values: []RefInit{{FuncIndex: 1}}}}
		}, want: "element-state element 0 function 0 index 1 out of range"},
		{name: "global init ref out of range", mut: func(c *Compiled) {
			c.Globals = append(c.Globals, GlobalDef{Type: ValI32, HasInitGlobal: true, InitGlobal: 3})
		}, want: "global 1 initializer references unavailable global 3"},
		{name: "data offset ref not imported", mut: func(c *Compiled) { c.Data = []DataInit{{Offset: OffsetInit{HasGlobal: true, Global: 0}}} }, want: "data 0 offset global 0 must be imported immutable i32"},
		{name: "arena footprint too large", mut: func(c *Compiled) { c.HasTable = true; c.TableSize = wruntime.InstantiateArenaSize }, want: "instantiate arena need"},
		{name: "passive element footprint too large", mut: func(c *Compiled) {
			c.HasTable = true
			c.passiveElems = make([]ElemInit, wruntime.InstantiateArenaSize/wruntime.PassiveElemDescBytes)
		}, want: "instantiate arena need"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := base()
			tt.mut(c)
			err := c.validate()
			if err == nil || !bytes.Contains([]byte(err.Error()), []byte(tt.want)) {
				t.Fatalf("validate error = %v, want %q", err, tt.want)
			}
		})
	}
	t.Run("passive explicit null allowed", func(t *testing.T) {
		c := base()
		c.HasTable = true
		c.TableSize = 1
		c.passiveElems = []ElemInit{{RefType: ValFuncRef, Mode: ElemModePassive, Values: []RefInit{{Null: true}}}}
		if err := c.validate(); err != nil {
			t.Fatalf("validate passive explicit null: %v", err)
		}
	})
}

func TestInstantiateRejectsMalformedCompiledBeforeMapping(t *testing.T) {
	c := &Compiled{Entry: []int{0}, FuncTypeID: []uint32{1}, GlobalExports: map[string]int{"g": 0}}
	_, err := Instantiate(c, InstantiateOptions{Imports: Imports{}})
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("Entry length 1 != Funcs length 0")) {
		t.Fatalf("InstantiateWithImports malformed metadata error = %v, want validate error", err)
	}
}

func TestInstantiateInitializesGlobalSlots(t *testing.T) {
	c := &Compiled{Globals: []GlobalDef{
		{Type: ValI32, Bits: 0x11223344},
		{Type: ValI64, Mutable: true, Bits: 0x0123456789abcdef},
	}}
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if len(in.globalCells) != 2 {
		t.Fatalf("global cells = %d, want 2", len(in.globalCells))
	}
	if got := readGlobalObject(in.globalCells[0], ValI32); got != 0x11223344 {
		t.Fatalf("global 0 slot = %#x, want %#x", got, uint64(0x11223344))
	}
	if got := readGlobalObject(in.globalCells[1], ValI64); got != 0x0123456789abcdef {
		t.Fatalf("global 1 slot = %#x, want %#x", got, uint64(0x0123456789abcdef))
	}
}

func TestInstantiateLateGlobalErrorCleansResources(t *testing.T) {
	before := procSelfMapsCount(t)
	c := &Compiled{
		Code: []byte{0xc3}, // ret; code is mapped before global initialization reaches this malformed reference.
		Globals: []GlobalDef{
			{Type: ValI32, Bits: 1},
			{Type: ValI32, HasInitGlobal: true, InitGlobal: 2},
		},
	}
	for i := 0; i < 5; i++ {
		if in, err := Instantiate(c, InstantiateOptions{}); err == nil {
			in.Close()
			t.Fatal("Instantiate malformed global initializer succeeded, want error")
		} else if !bytes.Contains([]byte(err.Error()), []byte("initializer references unavailable global")) {
			t.Fatalf("Instantiate error = %v, want unavailable global", err)
		}
	}
	after := procSelfMapsCount(t)
	if after > before+2 {
		t.Fatalf("/proc/self/maps entries grew from %d to %d after late instantiate errors; resources were not cleaned up", before, after)
	}
}

func procSelfMapsCount(t *testing.T) int {
	t.Helper()
	b, err := os.ReadFile("/proc/self/maps")
	if err != nil {
		t.Skipf("cannot read /proc/self/maps: %v", err)
	}
	return bytes.Count(b, []byte{'\n'})
}

func TestInstantiateGlobalStorageIsPerInstance(t *testing.T) {
	c := &Compiled{Globals: []GlobalDef{{Type: ValI32, Mutable: true, Bits: 7}}}
	in1, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in1.Close()
	in2, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in2.Close()
	writeGlobalObject(in1.globalCells[0], ValI32, 99)
	if got := readGlobalObject(in2.globalCells[0], ValI32); got != 7 {
		t.Fatalf("instance 2 global = %d, want initial 7", got)
	}
}

func TestGlobalGetSetEndToEnd(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00}, []byte{0x01})),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x29, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("get", 0, 0), wasmtest.ExportEntry("inc", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x23, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x23, 0x00, 0x20, 0x00, 0x6a, 0x24, 0x00, 0x23, 0x00, 0x0b}),
		)),
	)
	c, err := Compile(nil, mod)
	if err != nil {
		t.Fatal(err)
	}
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	res, err := in.Invoke("get")
	if err != nil {
		t.Fatal(err)
	}
	if got := AsI32(res[0]); got != 41 {
		t.Fatalf("get = %d, want 41", got)
	}
	res, err = in.Invoke("inc", I32(1))
	if err != nil {
		t.Fatal(err)
	}
	if got := AsI32(res[0]); got != 42 {
		t.Fatalf("inc = %d, want 42", got)
	}
}

func TestGlobalValidationCompileAlignment(t *testing.T) {
	tests := []struct {
		name    string
		module  []byte
		wantErr bool
	}{
		{
			name: "global.get validates and compiles",
			module: wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
				wasmtest.Section(3, wasmtest.Vec([]byte{0x00})),
				wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, []byte{0x41, 0x01, 0x0b}))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0x00, 0x0b}))),
			),
		},
		{
			name: "global.set validates and compiles",
			module: wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil))),
				wasmtest.Section(3, wasmtest.Vec([]byte{0x00})),
				wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x01, 0x0b}))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x24, 0x00, 0x0b}))),
			),
		},
		{
			name: "immutable global.set rejected by validation",
			module: wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil))),
				wasmtest.Section(3, wasmtest.Vec([]byte{0x00})),
				wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, []byte{0x41, 0x01, 0x0b}))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x24, 0x00, 0x0b}))),
			),
			wantErr: true,
		},
		{
			name: "unknown global rejected by validation",
			module: wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
				wasmtest.Section(3, wasmtest.Vec([]byte{0x00})),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0x00, 0x0b}))),
			),
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Compile(nil, tt.module)
			if !tt.wantErr && err != nil {
				t.Fatalf("Compile error = %v, want nil", err)
			}
			if tt.wantErr && (err == nil || !bytes.Contains([]byte(err.Error()), []byte("validate"))) {
				t.Fatalf("Compile error = %v, want validation error", err)
			}
		})
	}
}

func TestGlobalNumericRoundTrips(t *testing.T) {
	f32bits := uint32(0x3fc00000) // 1.5
	f64bits := math.Float64bits(2.25)
	f32 := make([]byte, 4)
	binary.LittleEndian.PutUint32(f32, f32bits)
	f64 := make([]byte, 8)
	binary.LittleEndian.PutUint64(f64, f64bits)
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}),
			wasmtest.FuncType([]wasm.ValType{wasm.F32}, []wasm.ValType{wasm.F32}),
			wasmtest.FuncType([]wasm.ValType{wasm.F64}, []wasm.ValType{wasm.F64}),
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00}, []byte{0x01}, []byte{0x02})),
		wasmtest.Section(6, wasmtest.Vec(
			wasmtest.GlobalEntry(wasm.I32, false, []byte{0x41, 0x01, 0x0b}),
			wasmtest.GlobalEntry(wasm.I64, true, append(append([]byte{0x42}, wasmtest.SLEB64(0x0102030405060708)...), 0x0b)),
			wasmtest.GlobalEntry(wasm.F32, true, append(append([]byte{0x43}, f32...), 0x0b)),
			wasmtest.GlobalEntry(wasm.F64, true, append(append([]byte{0x44}, f64...), 0x0b)),
		)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("g64", 0, 0), wasmtest.ExportEntry("f32", 0, 1), wasmtest.ExportEntry("f64", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x23, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x24, 0x02, 0x23, 0x02, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x24, 0x03, 0x23, 0x03, 0x0b}),
		)),
	)
	c, err := Compile(nil, mod)
	if err != nil {
		t.Fatal(err)
	}
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if res, err := in.Invoke("g64"); err != nil || AsI64(res[0]) != 0x0102030405060708 {
		t.Fatalf("g64 = %v, %v", res, err)
	}
	if res, err := in.Invoke("f32", F32(3.5)); err != nil || math.Float32bits(AsF32(res[0])) != math.Float32bits(3.5) {
		t.Fatalf("f32 = %v, %v", res, err)
	}
	if res, err := in.Invoke("f64", F64(4.5)); err != nil || math.Float64bits(AsF64(res[0])) != math.Float64bits(4.5) {
		t.Fatalf("f64 = %v, %v", res, err)
	}
}

func TestDataOffsetI32ConstUnchanged(t *testing.T) {
	seg := append([]byte{0x00, 0x41, 0x04, 0x0b}, append(wasmtest.ULEB(2), 'O', 'K')...)
	mod := wasmtest.Module(
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(11, wasmtest.Vec(seg)),
	)
	c, err := Compile(nil, mod)
	if err != nil {
		t.Fatal(err)
	}
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got := string(in.Memory().Bytes()[4:6]); got != "OK" {
		t.Fatalf("data at i32.const offset = %q, want OK", got)
	}
}

func TestElementOffsetI32ConstUnchanged(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00}, []byte{0x01})),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x03})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))),
		wasmtest.Section(9, wasmtest.Vec(append([]byte{0x00, 0x41, 0x01, 0x0b}, wasmtest.Vec(wasmtest.ULEB(0))...))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x07, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x11, 0x00, 0x00, 0x0b}),
		)),
	)
	c, err := Compile(nil, mod)
	if err != nil {
		t.Fatal(err)
	}
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	res, err := in.Invoke("call", I32(1))
	if err != nil {
		t.Fatal(err)
	}
	if got := AsI32(res[0]); got != 7 {
		t.Fatalf("indirect call through i32.const element offset = %d, want 7", got)
	}
}

func TestCompileAcceptsActiveElementExpressionSegments(t *testing.T) {
	expr := []byte{0xd2, 0x00, 0x0b}                                     // ref.func 0; end
	seg := append([]byte{0x04, 0x41, 0x00, 0x0b}, wasmtest.Vec(expr)...) // active expr segment at offset 0
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00})),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
		wasmtest.Section(9, wasmtest.Vec(seg)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x07, 0x0b}))),
	)
	if _, err := Compile(nil, mod); err != nil {
		t.Fatalf("Compile active element expr: %v", err)
	}
}

func TestZeroLengthTableCallIndirectTraps(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x01})),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x00})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x11, 0x00, 0x00, 0x0b}))),
	)
	c, err := Compile(nil, mod)
	if err != nil {
		t.Fatal(err)
	}
	if !c.HasTable || c.TableSize != 0 {
		t.Fatalf("compiled table metadata HasTable=%v TableSize=%d, want true/0", c.HasTable, c.TableSize)
	}
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("call", I32(0)); err == nil {
		t.Fatal("call_indirect through zero-length table returned, want trap")
	}
}

func TestInstantiateRejectsOutOfBoundsActiveDataSegments(t *testing.T) {
	tests := []struct {
		name    string
		mod     []byte
		imports Imports
	}{
		{
			name: "i32 const offset",
			mod: wasmtest.Module(
				wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
				wasmtest.Section(11, wasmtest.Vec(append([]byte{0x00, 0x41}, append(wasmtest.SLEB32(65535), append([]byte{0x0b}, append(wasmtest.ULEB(2), 'O', 'K')...)...)...))),
			),
		},
		{
			name: "imported global offset",
			mod: wasmtest.Module(
				wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "offset", wasm.I32, false))),
				wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
				wasmtest.Section(11, wasmtest.Vec(append([]byte{0x00, 0x23, 0x00, 0x0b}, append(wasmtest.ULEB(2), 'O', 'K')...))),
			),
			imports: Imports{"env.offset": GlobalImport{Type: ValI32, Bits: 65535}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := Compile(nil, tt.mod)
			if err != nil {
				t.Fatal(err)
			}
			in, err := Instantiate(c, InstantiateOptions{Imports: tt.imports})
			if err == nil {
				in.Close()
				t.Fatal("InstantiateWithImports succeeded, want active data out-of-bounds error")
			}
			if !bytes.Contains([]byte(err.Error()), []byte("active data segment")) {
				t.Fatalf("InstantiateWithImports error = %v, want active data segment", err)
			}
		})
	}
}

func TestInstantiateRejectsOutOfBoundsActiveElementSegments(t *testing.T) {
	tests := []struct {
		name    string
		mod     []byte
		imports Imports
	}{
		{
			name: "i32 const offset",
			mod: wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
				wasmtest.Section(3, wasmtest.Vec([]byte{0x00})),
				wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
				wasmtest.Section(9, wasmtest.Vec(append([]byte{0x00, 0x41, 0x01, 0x0b}, wasmtest.Vec(wasmtest.ULEB(0))...))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x07, 0x0b}))),
			),
		},
		{
			name: "imported global offset",
			mod: wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
				wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "slot", wasm.I32, false))),
				wasmtest.Section(3, wasmtest.Vec([]byte{0x00})),
				wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
				wasmtest.Section(9, wasmtest.Vec(append([]byte{0x00, 0x23, 0x00, 0x0b}, wasmtest.Vec(wasmtest.ULEB(0))...))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x07, 0x0b}))),
			),
			imports: Imports{"env.slot": GlobalImport{Type: ValI32, Bits: 1}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := Compile(nil, tt.mod)
			if err != nil {
				t.Fatal(err)
			}
			in, err := Instantiate(c, InstantiateOptions{Imports: tt.imports})
			if err == nil {
				in.Close()
				t.Fatal("InstantiateWithImports succeeded, want active element out-of-bounds error")
			}
			if !bytes.Contains([]byte(err.Error()), []byte("active element segment")) {
				t.Fatalf("InstantiateWithImports error = %v, want active element segment", err)
			}
		})
	}
}

func TestDataOffsetCanUseImportedImmutableGlobal(t *testing.T) {
	seg := append([]byte{0x00, 0x23, 0x00, 0x0b}, append(wasmtest.ULEB(2), 'O', 'K')...)
	mod := wasmtest.Module(
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "offset", wasm.I32, false))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(11, wasmtest.Vec(seg)),
	)
	c, err := Compile(nil, mod)
	if err != nil {
		t.Fatal(err)
	}
	in, err := Instantiate(c, InstantiateOptions{Imports: Imports{"env.offset": GlobalImport{Type: ValI32, Bits: 9}}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got := string(in.Memory().Bytes()[9:11]); got != "OK" {
		t.Fatalf("data at imported-global offset = %q, want OK", got)
	}
}

func TestElementOffsetCanUseImportedImmutableGlobal(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "slot", wasm.I32, false))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00}, []byte{0x01})),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x03})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))),
		wasmtest.Section(9, wasmtest.Vec(append([]byte{0x00, 0x23, 0x00, 0x0b}, wasmtest.Vec(wasmtest.ULEB(0))...))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x07, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x11, 0x00, 0x00, 0x0b}),
		)),
	)
	c, err := Compile(nil, mod)
	if err != nil {
		t.Fatal(err)
	}
	in, err := Instantiate(c, InstantiateOptions{Imports: Imports{"env.slot": GlobalImport{Type: ValI32, Bits: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	res, err := in.Invoke("call", I32(1))
	if err != nil {
		t.Fatal(err)
	}
	if got := AsI32(res[0]); got != 7 {
		t.Fatalf("indirect call through imported-global element offset = %d, want 7", got)
	}
}

func TestLocalGlobalInitializedFromImportedImmutableGlobal(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "seed", wasm.I32, false))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00}, []byte{0x01})),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, []byte{0x23, 0x00, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("imported", 0, 0), wasmtest.ExportEntry("local", 0, 1), wasmtest.ExportEntry("seed", 3, 0), wasmtest.ExportEntry("copied", 3, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0x00, 0x0b}), wasmtest.Code([]byte{0x23, 0x01, 0x0b}))),
	)
	c, err := Compile(nil, mod)
	if err != nil {
		t.Fatal(err)
	}
	in, err := Instantiate(c, InstantiateOptions{Imports: Imports{"env.seed": GlobalImport{Type: ValI32, Bits: 77}}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if res, err := in.Invoke("imported"); err != nil || AsI32(res[0]) != 77 {
		t.Fatalf("imported global function = %v, %v; want 77", res, err)
	}
	if res, err := in.Invoke("local"); err != nil || AsI32(res[0]) != 77 {
		t.Fatalf("local initialized from import = %v, %v; want 77", res, err)
	}
	if got, err := in.Global("copied"); err != nil || AsI32(got) != 77 {
		t.Fatalf("copied exported global = %v, %v; want 77", got, err)
	}
}

func TestCompileRejectsLocalInitializerFromMutableImportedGlobal(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "seed", wasm.I32, true))),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, []byte{0x23, 0x00, 0x0b}))),
	)
	if _, err := Compile(nil, mod); err == nil || !bytes.Contains([]byte(err.Error()), []byte("validate")) {
		t.Fatalf("Compile mutable imported global initializer error = %v, want validate error", err)
	}
}

func TestReadsImportedGlobal(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "seed", wasm.I32, false))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("get", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0x00, 0x0b}))),
	)
	imports := Imports{"env.seed": GlobalImport{Type: ValI32, Bits: 42}}
	got := runImports(t, mod, imports, "get")
	if len(got) != 1 || AsI32(got[0]) != 42 {
		t.Fatalf("get = %v, want i32 42", got)
	}
}

func TestDuplicateImportedGlobalKeysAliasSameObject(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(
			wasmtest.GlobalImportEntry("env", "dup", wasm.I32, true),
			wasmtest.GlobalImportEntry("env", "dup", wasm.I32, true),
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00}, []byte{0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("set0", 0, 0), wasmtest.ExportEntry("get1", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x24, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x23, 0x01, 0x0b}),
		)),
	)
	c, err := Compile(nil, mod)
	if err != nil {
		t.Fatal(err)
	}
	shared := NewGlobalI32(3, true)
	defer shared.Close()
	in, err := Instantiate(c, InstantiateOptions{Imports: Imports{"env.dup": GlobalImport{Global: shared}}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("set0", I32(11)); err != nil {
		t.Fatal(err)
	}
	if res, err := in.Invoke("get1"); err != nil || AsI32(res[0]) != 11 {
		t.Fatalf("get1 after set0 = %v, %v; want aliased 11", res, err)
	}
	if got := AsI32(shared.Get()); got != 11 {
		t.Fatalf("shared host global = %d, want 11", got)
	}
}

func TestImportedMutableGlobalImportAliasesHostObject(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "counter", wasm.I32, true))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("get", 0, 0), wasmtest.ExportEntry("counter", 3, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0x00, 0x0b}))),
	)
	c, err := Compile(nil, mod)
	if err != nil {
		t.Fatal(err)
	}
	shared := NewGlobalI32(10, true)
	defer shared.Close()
	imports := Imports{"env.counter": GlobalImport{Global: shared}}
	in, err := Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if err := shared.Set(I32(99)); err != nil {
		t.Fatal(err)
	}
	if got, err := in.Global("counter"); err != nil || AsI32(got) != 99 {
		t.Fatalf("Global after host-side object mutation = %v, %v; want shared 99", got, err)
	}
	if res, err := in.Invoke("get"); err != nil || AsI32(res[0]) != 99 {
		t.Fatalf("wasm get after host-side object mutation = %v, %v; want 99", res, err)
	}
	if err := in.SetGlobal("counter", I32(15)); err != nil {
		t.Fatalf("SetGlobal imported mutable global: %v", err)
	}
	if got := AsI32(shared.Get()); got != 15 {
		t.Fatalf("host global after instance mutation = %d; want 15", got)
	}
}

func TestImportedGlobalReadWriteThroughWasm(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "counter", wasm.I32, true))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("add", 0, 0), wasmtest.ExportEntry("counter", 3, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0x00, 0x20, 0x00, 0x6a, 0x24, 0x00, 0x23, 0x00, 0x0b}))),
	)
	c, err := Compile(nil, mod)
	if err != nil {
		t.Fatal(err)
	}
	in, err := Instantiate(c, InstantiateOptions{Imports: Imports{"env.counter": GlobalImport{Type: ValI32, Mutable: true, Bits: 10}}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got, err := in.Global("counter"); err != nil || AsI32(got) != 10 {
		t.Fatalf("imported Global initial = %v, %v; want 10", got, err)
	}
	if res, err := in.Invoke("add", I32(5)); err != nil || AsI32(res[0]) != 15 {
		t.Fatalf("add imported global = %v, %v; want 15", res, err)
	}
	if got, err := in.Global("counter"); err != nil || AsI32(got) != 15 {
		t.Fatalf("imported Global after wasm write = %v, %v; want 15", got, err)
	}
	if _, err := Instantiate(c, InstantiateOptions{Imports: Imports{}}); err == nil {
		t.Fatal("InstantiateWithImports missing global succeeded, want error")
	}
	if _, err := Instantiate(c, InstantiateOptions{Imports: Imports{"env.counter": GlobalImport{Type: ValI64, Mutable: true}}}); err == nil {
		t.Fatal("InstantiateWithImports type mismatch succeeded, want error")
	}
	if _, err := Instantiate(c, InstantiateOptions{Imports: Imports{"env.counter": GlobalImport{Type: ValI32}}}); err == nil {
		t.Fatal("InstantiateWithImports mutability mismatch succeeded, want error")
	}
}

func TestGlobalSlotBitsCanonicalize32BitValues(t *testing.T) {
	c := &Compiled{
		GlobalImports: []GlobalImportDef{{Module: "env", Name: "i", Type: ValI32}},
		Globals: []GlobalDef{
			{Type: ValI32},
			{Type: ValF32, Mutable: true, Bits: 0xffff00003f800000},
		},
		GlobalExports: map[string]int{"i": 0, "f": 1},
	}
	in, err := Instantiate(c, InstantiateOptions{Imports: Imports{"env.i": GlobalImport{Type: ValI32, Bits: 0xffff000012345678}}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got := readGlobalObject(in.globalCells[0], ValI32); got != 0x12345678 {
		t.Fatalf("imported i32 raw slot = %#x, want low 32 bits only", got)
	}
	if got := readGlobalObject(in.globalCells[1], ValF32); got != 0x3f800000 {
		t.Fatalf("local f32 raw slot = %#x, want low 32 bits only", got)
	}
	if err := in.SetGlobal("f", 0xffff000040000000); err != nil {
		t.Fatalf("SetGlobal f32: %v", err)
	}
	if got := readGlobalObject(in.globalCells[1], ValF32); got != 0x40000000 {
		t.Fatalf("SetGlobal f32 raw slot = %#x, want low 32 bits only", got)
	}
}

func TestExportedGlobalAccessors(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00}, []byte{0x01})),
		wasmtest.Section(6, wasmtest.Vec(
			wasmtest.GlobalEntry(wasm.I32, false, []byte{0x41, 0x07, 0x0b}),
			wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x29, 0x0b}),
		)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("set", 0, 0), wasmtest.ExportEntry("get", 0, 1), wasmtest.ExportEntry("imm", 3, 0), wasmtest.ExportEntry("mut", 3, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x24, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x23, 0x01, 0x0b}),
		)),
	)
	c, err := Compile(nil, mod)
	if err != nil {
		t.Fatal(err)
	}
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got, err := in.Global("imm"); err != nil || AsI32(got) != 7 {
		t.Fatalf("Global imm = %v, %v; want i32 7", got, err)
	}
	if got, err := in.Global("mut"); err != nil || AsI32(got) != 41 {
		t.Fatalf("Global mut initial = %v, %v; want 41", got, err)
	}
	if err := in.SetGlobal("mut", I32(99)); err != nil {
		t.Fatalf("SetGlobal mut: %v", err)
	}
	if res, err := in.Invoke("get"); err != nil || AsI32(res[0]) != 99 {
		t.Fatalf("wasm get after host write = %v, %v; want 99", res, err)
	}
	if _, err := in.Invoke("set", I32(123)); err != nil {
		t.Fatalf("wasm set: %v", err)
	}
	if got, err := in.Global("mut"); err != nil || AsI32(got) != 123 {
		t.Fatalf("Global mut after wasm write = %v, %v; want 123", got, err)
	}
	if err := in.SetGlobal("imm", I32(1)); err == nil {
		t.Fatal("SetGlobal immutable succeeded, want error")
	}
	if _, err := in.Global("set"); err == nil {
		t.Fatal("Global on function export succeeded, want error")
	}
	if _, err := in.Invoke("get"); err != nil {
		t.Fatalf("function export lookup changed: %v", err)
	}
}

func TestGlobalsInteractWithControlFlowAndLocals(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00})),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x03, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("mix", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x01, // local.get 1
			0x04, 0x40, // if
			0x20, 0x00, 0x24, 0x00, // then: global.set 0 from local 0
			0x05,                         // else
			0x20, 0x00, 0x41, 0x01, 0x6a, // local 0 + 1
			0x21, 0x00, // local.set 0
			0x0b,                               // end if
			0x20, 0x00, 0x23, 0x00, 0x6a, 0x0b, // local 0 + global 0
		}))),
	)
	c, err := Compile(nil, mod)
	if err != nil {
		t.Fatal(err)
	}
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if res, err := in.Invoke("mix", I32(10), I32(1)); err != nil || AsI32(res[0]) != 20 {
		t.Fatalf("mix then branch = %v, %v; want 20", res, err)
	}
	if res, err := in.Invoke("mix", I32(5), I32(0)); err != nil || AsI32(res[0]) != 16 {
		t.Fatalf("mix else branch = %v, %v; want 16", res, err)
	}
}

func TestUnreachableGlobalOpsSkipImmediates(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00}, []byte{0x01})),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x07, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("get_dead", 0, 0), wasmtest.ExportEntry("set_dead", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x00, 0x23, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x00, 0x24, 0x00, 0x0b}),
		)),
	)
	c, err := Compile(nil, mod)
	if err != nil {
		t.Fatal(err)
	}
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("get_dead"); err == nil {
		t.Fatal("unreachable global.get path returned, want trap")
	}
	if _, err := in.Invoke("set_dead"); err == nil {
		t.Fatal("unreachable global.set path returned, want trap")
	}
}

func TestGeneratedGlobalWasmFixtures(t *testing.T) {
	f32const := make([]byte, 4)
	binary.LittleEndian.PutUint32(f32const, math.Float32bits(1.25))
	f64const := make([]byte, 8)
	binary.LittleEndian.PutUint64(f64const, math.Float64bits(2.5))

	t.Run("immutable i32 global exported through function", func(t *testing.T) {
		mod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
			wasmtest.Section(3, wasmtest.Vec([]byte{0x00})),
			wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, []byte{0x41, 0x2a, 0x0b}))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("get", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0x00, 0x0b}))),
		)
		res := runv(t, mod, "get")
		if AsI32(res[0]) != 42 {
			t.Fatalf("get immutable i32 = %v; want 42", res)
		}
	})

	t.Run("mutable counter global", func(t *testing.T) {
		mod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
			wasmtest.Section(3, wasmtest.Vec([]byte{0x00})),
			wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x00, 0x0b}))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("add", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0x00, 0x20, 0x00, 0x6a, 0x24, 0x00, 0x23, 0x00, 0x0b}))),
		)
		c, err := Compile(nil, mod)
		if err != nil {
			t.Fatal(err)
		}
		in, err := Instantiate(c, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		for _, tc := range []struct{ delta, want int32 }{{3, 3}, {4, 7}} {
			res, err := in.Invoke("add", I32(tc.delta))
			if err != nil || AsI32(res[0]) != tc.want {
				t.Fatalf("add(%d) = %v, %v; want %d", tc.delta, res, err, tc.want)
			}
		}
	})

	t.Run("i64 global", func(t *testing.T) {
		mod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}))),
			wasmtest.Section(3, wasmtest.Vec([]byte{0x00})),
			wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I64, false, append(append([]byte{0x42}, wasmtest.SLEB64(0x0102030405060708)...), 0x0b)))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("get", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0x00, 0x0b}))),
		)
		res := runv(t, mod, "get")
		if AsI64(res[0]) != 0x0102030405060708 {
			t.Fatalf("get i64 = %v; want %#x", res, int64(0x0102030405060708))
		}
	})

	t.Run("f32 global", func(t *testing.T) {
		mod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.F32}))),
			wasmtest.Section(3, wasmtest.Vec([]byte{0x00})),
			wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.F32, false, append(append([]byte{0x43}, f32const...), 0x0b)))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("get", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0x00, 0x0b}))),
		)
		res := runv(t, mod, "get")
		if math.Float32bits(AsF32(res[0])) != math.Float32bits(1.25) {
			t.Fatalf("get f32 = %v; want 1.25", res)
		}
	})

	t.Run("f64 global", func(t *testing.T) {
		mod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.F64}))),
			wasmtest.Section(3, wasmtest.Vec([]byte{0x00})),
			wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.F64, false, append(append([]byte{0x44}, f64const...), 0x0b)))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("get", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0x00, 0x0b}))),
		)
		res := runv(t, mod, "get")
		if math.Float64bits(AsF64(res[0])) != math.Float64bits(2.5) {
			t.Fatalf("get f64 = %v; want 2.5", res)
		}
	})

	t.Run("exported global API coverage", func(t *testing.T) {
		mod := wasmtest.Module(
			wasmtest.Section(6, wasmtest.Vec(
				wasmtest.GlobalEntry(wasm.I32, false, []byte{0x41, 0x07, 0x0b}),
				wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x08, 0x0b}),
			)),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("imm", 3, 0), wasmtest.ExportEntry("mut", 3, 1))),
		)
		c, err := Compile(nil, mod)
		if err != nil {
			t.Fatal(err)
		}
		in, err := Instantiate(c, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		if got, err := in.Global("imm"); err != nil || AsI32(got) != 7 {
			t.Fatalf("Global imm = %v, %v; want 7", got, err)
		}
		if err := in.SetGlobal("mut", I32(9)); err != nil {
			t.Fatalf("SetGlobal mut: %v", err)
		}
		if got, err := in.Global("mut"); err != nil || AsI32(got) != 9 {
			t.Fatalf("Global mut = %v, %v; want 9", got, err)
		}
	})
}

func TestGlobalAPIE2EHelpers(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00})),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x0a, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("add", 0, 0), wasmtest.ExportEntry("counter", 3, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0x00, 0x20, 0x00, 0x6a, 0x24, 0x00, 0x23, 0x00, 0x0b}))),
	)
	if res := runv(t, mod, "add", I32(5)); AsI32(res[0]) != 15 {
		t.Fatalf("add global = %v; want 15", res)
	}
	c, err := Compile(nil, mod)
	if err != nil {
		t.Fatal(err)
	}
	in1, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in1.Close()
	in2, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in2.Close()
	if res, err := in1.Invoke("add", I32(1)); err != nil || AsI32(res[0]) != 11 {
		t.Fatalf("in1 first add = %v, %v; want 11", res, err)
	}
	if res, err := in1.Invoke("add", I32(2)); err != nil || AsI32(res[0]) != 13 {
		t.Fatalf("in1 second add = %v, %v; want persistent 13", res, err)
	}
	if got, err := in1.Global("counter"); err != nil || AsI32(got) != 13 {
		t.Fatalf("in1 Global counter = %v, %v; want 13", got, err)
	}
	if got, err := in2.Global("counter"); err != nil || AsI32(got) != 10 {
		t.Fatalf("in2 Global counter = %v, %v; want independent 10", got, err)
	}
	if err := in2.SetGlobal("counter", I32(20)); err != nil {
		t.Fatalf("in2 SetGlobal: %v", err)
	}
	if got, err := in1.Global("counter"); err != nil || AsI32(got) != 13 {
		t.Fatalf("in1 Global after in2 SetGlobal = %v, %v; want 13", got, err)
	}
	if res, err := in2.Invoke("add", I32(1)); err != nil || AsI32(res[0]) != 21 {
		t.Fatalf("in2 add after SetGlobal = %v, %v; want 21", res, err)
	}
}

func TestGlobalsArePerInstanceThroughWasm(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00})),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x00, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("add", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0x00, 0x20, 0x00, 0x6a, 0x24, 0x00, 0x23, 0x00, 0x0b}))),
	)
	c, err := Compile(nil, mod)
	if err != nil {
		t.Fatal(err)
	}
	in1, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in1.Close()
	in2, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in2.Close()
	if res, err := in1.Invoke("add", I32(5)); err != nil || AsI32(res[0]) != 5 {
		t.Fatalf("in1 add = %v, %v", res, err)
	}
	if res, err := in2.Invoke("add", I32(7)); err != nil || AsI32(res[0]) != 7 {
		t.Fatalf("in2 add = %v, %v", res, err)
	}
	if res, err := in1.Invoke("add", I32(0)); err != nil || AsI32(res[0]) != 5 {
		t.Fatalf("in1 persisted = %v, %v", res, err)
	}
}

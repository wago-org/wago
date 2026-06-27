//go:build linux && amd64

package wago

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"testing"

	wasm "github.com/wago-org/wago/src/core/compiler/wasm3"
	wruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func globalDefEqual(a, b GlobalDef) bool {
	return valTypeEqual(a.Type, b.Type) && a.Mutable == b.Mutable && a.Bits == b.Bits && a.HasInitGlobal == b.HasInitGlobal && a.InitGlobal == b.InitGlobal
}

func TestCompiledGlobalIndexHelpers(t *testing.T) {
	c := &Compiled{
		GlobalImports: []GlobalImportDef{{Module: "env", Name: "seed", Type: wasm.I32}},
		Globals:       []GlobalDef{{Type: wasm.I32}, {Type: wasm.I64, Mutable: true}},
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
	if !ok || !valTypeEqual(g.Type, wasm.I64) || !g.Mutable {
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
	c, err := Compile(mod)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Globals) != 4 {
		t.Fatalf("globals = %d, want 4", len(c.Globals))
	}
	want := []GlobalDef{{Type: wasm.I32, Bits: math.MaxUint32}, {Type: wasm.I64, Mutable: true, Bits: ^uint64(1)}, {Type: wasm.F32, Bits: uint64(f32bits)}, {Type: wasm.F64, Mutable: true, Bits: f64bits}}
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

func TestCompileRejectsGlobalInitializerTypeMismatch(t *testing.T) {
	mod := wasmtest.Module(wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, []byte{0x42, 0x00, 0x0b}))))
	if _, err := Compile(mod); err == nil || !bytes.Contains([]byte(err.Error()), []byte("validate")) {
		t.Fatalf("Compile mismatch error = %v, want validate error", err)
	}
}

func TestCompileRejectsUnsupportedGlobalTypes(t *testing.T) {
	tests := []struct {
		name string
		mod  []byte
		want string
	}{
		{
			name: "imported funcref global",
			mod:  wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "ref", wasm.FuncRef, false)))),
			want: "compile: unsupported global type funcref",
		},
		{
			name: "imported v128 global",
			mod:  wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "vec", wasm.V128, false)))),
			want: "compile: unsupported global type v128",
		},
		{
			name: "defined funcref global",
			mod:  wasmtest.Module(wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.FuncRef, false, []byte{0xd0, 0x70, 0x0b})))),
			want: "compile: unsupported global type funcref",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Compile(tt.mod); err == nil || !bytes.Contains([]byte(err.Error()), []byte(tt.want)) {
				t.Fatalf("Compile error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestCompileRejectsWasm3DecodedProposalFeatureBeforeLegacyDecode(t *testing.T) {
	mod := wasmtest.Module(wasmtest.Section(5, wasmtest.Vec([]byte{0x04, 0x00}))) // memory64 min 0
	if _, err := Compile(mod); err == nil || !bytes.Contains([]byte(err.Error()), []byte("compile: unsupported memory memory64 at memory 0")) {
		t.Fatalf("Compile memory64 error = %v, want frontend support-pass rejection", err)
	}
}

func TestCompileRejectsValidatedButUnsupportedBackendGapBeforeCodegen(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x31, 0x00, 0x00, 0x0b}))), // local.get 0; i64.load8_u align=0 offset=0; end
	)
	if _, err := Compile(mod); err == nil || !bytes.Contains([]byte(err.Error()), []byte("compile: unsupported instruction I64Load8U")) {
		t.Fatalf("Compile i64.load8_u error = %v, want frontend support-pass rejection", err)
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
			if _, err := Compile(mod); err == nil || !bytes.Contains([]byte(err.Error()), []byte(tt.want)) {
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
			Funcs:      []FuncSig{{Results: []wasm.ValType{wasm.I32}}},
			Exports:    map[string]int{"f": 0},
			FuncTypeID: []uint32{1},
			Globals:    []GlobalDef{{Type: wasm.I32}},
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
		{name: "elements without table", mut: func(c *Compiled) { c.Elems = []ElemInit{{}} }, want: "element segment(s) without table"},
		{name: "entry funcs mismatch", mut: func(c *Compiled) { c.Entry = nil }, want: "Entry length"},
		{name: "entry at end of code", mut: func(c *Compiled) { c.Entry = []int{1} }, want: "Entry[0] offset 1 out of code range 1"},
		{name: "func type count mismatch", mut: func(c *Compiled) { c.FuncTypeID = nil }, want: "FuncTypeID length"},
		{name: "global export out of range", mut: func(c *Compiled) { c.GlobalExports = map[string]int{"g": 1} }, want: "global export \"g\" index 1 out of range"},
		{name: "element func out of range", mut: func(c *Compiled) { c.HasTable = true; c.TableSize = 1; c.Elems = []ElemInit{{Funcs: []uint32{1}}} }, want: "element 0 function 0 index 1 out of range"},
		{name: "global init ref out of range", mut: func(c *Compiled) {
			c.Globals = append(c.Globals, GlobalDef{Type: wasm.I32, HasInitGlobal: true, InitGlobal: 3})
		}, want: "global 1 initializer references unavailable global 3"},
		{name: "unsupported global type", mut: func(c *Compiled) { c.Globals[0].Type = wasm.FuncRef }, want: "global 0 has unsupported type funcref"},
		{name: "data offset ref not imported", mut: func(c *Compiled) { c.Data = []DataInit{{Offset: OffsetInit{HasGlobal: true, Global: 0}}} }, want: "data 0 offset global 0 must be imported immutable i32"},
		{name: "arena footprint too large", mut: func(c *Compiled) { c.HasTable = true; c.TableSize = wruntime.InstantiateArenaSize }, want: "instantiate arena need"},
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
}

func TestInstantiateRejectsMalformedCompiledBeforeMapping(t *testing.T) {
	c := &Compiled{Entry: []int{0}, FuncTypeID: []uint32{1}, GlobalExports: map[string]int{"g": 0}}
	_, err := InstantiateWithImports(c, Imports{})
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("Entry length 1 != Funcs length 0")) {
		t.Fatalf("InstantiateWithImports malformed metadata error = %v, want validate error", err)
	}
}

func TestInstantiateInitializesGlobalSlots(t *testing.T) {
	c := &Compiled{Globals: []GlobalDef{
		{Type: wasm.I32, Bits: 0x11223344},
		{Type: wasm.I64, Mutable: true, Bits: 0x0123456789abcdef},
	}}
	in, err := Instantiate(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if len(in.globalCells) != 2 {
		t.Fatalf("global cells = %d, want 2", len(in.globalCells))
	}
	if got := readGlobalObject(in.globalCells[0], wasm.I32); got != 0x11223344 {
		t.Fatalf("global 0 slot = %#x, want %#x", got, uint64(0x11223344))
	}
	if got := readGlobalObject(in.globalCells[1], wasm.I64); got != 0x0123456789abcdef {
		t.Fatalf("global 1 slot = %#x, want %#x", got, uint64(0x0123456789abcdef))
	}
}

func TestInstantiateLateGlobalErrorCleansResources(t *testing.T) {
	before := procSelfMapsCount(t)
	c := &Compiled{
		Code: []byte{0xc3}, // ret; code is mapped before global initialization reaches this malformed reference.
		Globals: []GlobalDef{
			{Type: wasm.I32, Bits: 1},
			{Type: wasm.I32, HasInitGlobal: true, InitGlobal: 2},
		},
	}
	for i := 0; i < 5; i++ {
		if in, err := Instantiate(c, nil); err == nil {
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
	c := &Compiled{Globals: []GlobalDef{{Type: wasm.I32, Mutable: true, Bits: 7}}}
	in1, err := Instantiate(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer in1.Close()
	in2, err := Instantiate(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer in2.Close()
	writeGlobalObject(in1.globalCells[0], wasm.I32, 99)
	if got := readGlobalObject(in2.globalCells[0], wasm.I32); got != 7 {
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
	c, err := Compile(mod)
	if err != nil {
		t.Fatal(err)
	}
	in, err := Instantiate(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	res, err := in.Invoke("get")
	if err != nil {
		t.Fatal(err)
	}
	if got := res[0].AsI32(); got != 41 {
		t.Fatalf("get = %d, want 41", got)
	}
	res, err = in.Invoke("inc", I32(1))
	if err != nil {
		t.Fatal(err)
	}
	if got := res[0].AsI32(); got != 42 {
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
			_, err := Compile(tt.module)
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
	c, err := Compile(mod)
	if err != nil {
		t.Fatal(err)
	}
	in, err := Instantiate(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if res, err := in.Invoke("g64"); err != nil || res[0].AsI64() != 0x0102030405060708 {
		t.Fatalf("g64 = %v, %v", res, err)
	}
	if res, err := in.Invoke("f32", F32(3.5)); err != nil || math.Float32bits(res[0].AsF32()) != math.Float32bits(3.5) {
		t.Fatalf("f32 = %v, %v", res, err)
	}
	if res, err := in.Invoke("f64", F64(4.5)); err != nil || math.Float64bits(res[0].AsF64()) != math.Float64bits(4.5) {
		t.Fatalf("f64 = %v, %v", res, err)
	}
}

func TestDataOffsetI32ConstUnchanged(t *testing.T) {
	seg := append([]byte{0x00, 0x41, 0x04, 0x0b}, append(wasmtest.ULEB(2), 'O', 'K')...)
	mod := wasmtest.Module(
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(11, wasmtest.Vec(seg)),
	)
	c, err := Compile(mod)
	if err != nil {
		t.Fatal(err)
	}
	in, err := Instantiate(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got := string(in.LinearMemory()[4:6]); got != "OK" {
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
	c, err := Compile(mod)
	if err != nil {
		t.Fatal(err)
	}
	in, err := Instantiate(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	res, err := in.Invoke("call", I32(1))
	if err != nil {
		t.Fatal(err)
	}
	if got := res[0].AsI32(); got != 7 {
		t.Fatalf("indirect call through i32.const element offset = %d, want 7", got)
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
			imports: Imports{Globals: map[string]GlobalImport{"env.offset": {Type: wasm.I32, Bits: 65535}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := Compile(tt.mod)
			if err != nil {
				t.Fatal(err)
			}
			in, err := InstantiateWithImports(c, tt.imports)
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
			imports: Imports{Globals: map[string]GlobalImport{"env.slot": {Type: wasm.I32, Bits: 1}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := Compile(tt.mod)
			if err != nil {
				t.Fatal(err)
			}
			in, err := InstantiateWithImports(c, tt.imports)
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
	c, err := Compile(mod)
	if err != nil {
		t.Fatal(err)
	}
	in, err := InstantiateWithImports(c, Imports{Globals: map[string]GlobalImport{"env.offset": {Type: wasm.I32, Bits: 9}}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got := string(in.LinearMemory()[9:11]); got != "OK" {
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
	c, err := Compile(mod)
	if err != nil {
		t.Fatal(err)
	}
	in, err := InstantiateWithImports(c, Imports{Globals: map[string]GlobalImport{"env.slot": {Type: wasm.I32, Bits: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	res, err := in.Invoke("call", I32(1))
	if err != nil {
		t.Fatal(err)
	}
	if got := res[0].AsI32(); got != 7 {
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
	c, err := Compile(mod)
	if err != nil {
		t.Fatal(err)
	}
	in, err := InstantiateWithImports(c, Imports{Globals: map[string]GlobalImport{"env.seed": {Type: wasm.I32, Bits: 77}}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if res, err := in.Invoke("imported"); err != nil || res[0].AsI32() != 77 {
		t.Fatalf("imported global function = %v, %v; want 77", res, err)
	}
	if res, err := in.Invoke("local"); err != nil || res[0].AsI32() != 77 {
		t.Fatalf("local initialized from import = %v, %v; want 77", res, err)
	}
	if got, err := in.Global("copied"); err != nil || got.AsI32() != 77 {
		t.Fatalf("copied exported global = %v, %v; want 77", got, err)
	}
}

func TestCompileRejectsLocalInitializerFromMutableImportedGlobal(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "seed", wasm.I32, true))),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, []byte{0x23, 0x00, 0x0b}))),
	)
	if _, err := Compile(mod); err == nil || !bytes.Contains([]byte(err.Error()), []byte("validate")) {
		t.Fatalf("Compile mutable imported global initializer error = %v, want validate error", err)
	}
}

func TestRunIntArgsCoerceFloatParamsNumerically(t *testing.T) {
	vals := valuesForIntArgs([]wasm.ValType{wasm.F32, wasm.F64}, []int32{3, 4})
	if len(vals) != 2 {
		t.Fatalf("valuesForIntArgs length = %d, want 2", len(vals))
	}
	if !valTypeEqual(vals[0].Type, wasm.F32) || math.Float32bits(vals[0].AsF32()) != math.Float32bits(3) {
		t.Fatalf("f32 coerced value = %v, want 3.0", vals[0])
	}
	if !valTypeEqual(vals[1].Type, wasm.F64) || math.Float64bits(vals[1].AsF64()) != math.Float64bits(4) {
		t.Fatalf("f64 coerced value = %v, want 4.0", vals[1])
	}
}

func TestInvokeRejectsArgumentTypeMismatch(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("id", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x0b}))),
	)
	c, err := Compile(mod)
	if err != nil {
		t.Fatal(err)
	}
	in, err := Instantiate(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	_, err = in.Invoke("id", I64(1))
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("id arg 0 has type i64, want i32")) {
		t.Fatalf("Invoke type mismatch error = %v, want i64/i32 mismatch", err)
	}
}

func TestRunValuesWithImportsReadsImportedGlobal(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "seed", wasm.I32, false))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("get", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0x00, 0x0b}))),
	)
	imports := Imports{Globals: map[string]GlobalImport{"env.seed": {Type: wasm.I32, Bits: 42}}}
	got, err := RunValuesWithImports(mod, imports, "get")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].AsI32() != 42 {
		t.Fatalf("RunValuesWithImports get = %v, want i32 42", got)
	}
	ints, err := RunWithImports(mod, imports, "get")
	if err != nil {
		t.Fatal(err)
	}
	if len(ints) != 1 || ints[0] != 42 {
		t.Fatalf("RunWithImports get = %v, want 42", ints)
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
	c, err := Compile(mod)
	if err != nil {
		t.Fatal(err)
	}
	shared := NewGlobal(I32(3), true)
	defer shared.Close()
	in, err := InstantiateWithImports(c, Imports{Globals: map[string]GlobalImport{"env.dup": {Global: shared}}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("set0", I32(11)); err != nil {
		t.Fatal(err)
	}
	if res, err := in.Invoke("get1"); err != nil || res[0].AsI32() != 11 {
		t.Fatalf("get1 after set0 = %v, %v; want aliased 11", res, err)
	}
	if got := shared.Value().AsI32(); got != 11 {
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
	c, err := Compile(mod)
	if err != nil {
		t.Fatal(err)
	}
	shared := NewGlobal(I32(10), true)
	defer shared.Close()
	imports := Imports{Globals: map[string]GlobalImport{"env.counter": {Global: shared}}}
	in, err := InstantiateWithImports(c, imports)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if err := shared.Set(I32(99)); err != nil {
		t.Fatal(err)
	}
	if got, err := in.Global("counter"); err != nil || got.AsI32() != 99 {
		t.Fatalf("Global after host-side object mutation = %v, %v; want shared 99", got, err)
	}
	if res, err := in.Invoke("get"); err != nil || res[0].AsI32() != 99 {
		t.Fatalf("wasm get after host-side object mutation = %v, %v; want 99", res, err)
	}
	if err := in.SetGlobal("counter", I32(15)); err != nil {
		t.Fatalf("SetGlobal imported mutable global: %v", err)
	}
	if got := shared.Value().AsI32(); got != 15 {
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
	c, err := Compile(mod)
	if err != nil {
		t.Fatal(err)
	}
	in, err := InstantiateWithImports(c, Imports{Globals: map[string]GlobalImport{"env.counter": {Type: wasm.I32, Mutable: true, Bits: 10}}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got, err := in.Global("counter"); err != nil || got.AsI32() != 10 {
		t.Fatalf("imported Global initial = %v, %v; want 10", got, err)
	}
	if res, err := in.Invoke("add", I32(5)); err != nil || res[0].AsI32() != 15 {
		t.Fatalf("add imported global = %v, %v; want 15", res, err)
	}
	if got, err := in.Global("counter"); err != nil || got.AsI32() != 15 {
		t.Fatalf("imported Global after wasm write = %v, %v; want 15", got, err)
	}
	if _, err := InstantiateWithImports(c, Imports{}); err == nil {
		t.Fatal("InstantiateWithImports missing global succeeded, want error")
	}
	if _, err := InstantiateWithImports(c, Imports{Globals: map[string]GlobalImport{"env.counter": {Type: wasm.I64, Mutable: true}}}); err == nil {
		t.Fatal("InstantiateWithImports type mismatch succeeded, want error")
	}
	if _, err := InstantiateWithImports(c, Imports{Globals: map[string]GlobalImport{"env.counter": {Type: wasm.I32}}}); err == nil {
		t.Fatal("InstantiateWithImports mutability mismatch succeeded, want error")
	}
}

func TestGlobalSlotBitsCanonicalize32BitValues(t *testing.T) {
	c := &Compiled{
		GlobalImports: []GlobalImportDef{{Module: "env", Name: "i", Type: wasm.I32}},
		Globals: []GlobalDef{
			{Type: wasm.I32},
			{Type: wasm.F32, Mutable: true, Bits: 0xffff00003f800000},
		},
		GlobalExports: map[string]int{"i": 0, "f": 1},
	}
	in, err := InstantiateWithImports(c, Imports{Globals: map[string]GlobalImport{"env.i": {Type: wasm.I32, Bits: 0xffff000012345678}}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got := readGlobalObject(in.globalCells[0], wasm.I32); got != 0x12345678 {
		t.Fatalf("imported i32 raw slot = %#x, want low 32 bits only", got)
	}
	if got := readGlobalObject(in.globalCells[1], wasm.F32); got != 0x3f800000 {
		t.Fatalf("local f32 raw slot = %#x, want low 32 bits only", got)
	}
	if err := in.SetGlobal("f", Value{Type: wasm.F32, Bits: 0xffff000040000000}); err != nil {
		t.Fatalf("SetGlobal f32: %v", err)
	}
	if got := readGlobalObject(in.globalCells[1], wasm.F32); got != 0x40000000 {
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
	c, err := Compile(mod)
	if err != nil {
		t.Fatal(err)
	}
	in, err := Instantiate(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got, err := in.Global("imm"); err != nil || !valTypeEqual(got.Type, wasm.I32) || got.AsI32() != 7 {
		t.Fatalf("Global imm = %v, %v; want i32 7", got, err)
	}
	if got, err := in.Global("mut"); err != nil || got.AsI32() != 41 {
		t.Fatalf("Global mut initial = %v, %v; want 41", got, err)
	}
	if err := in.SetGlobal("mut", I32(99)); err != nil {
		t.Fatalf("SetGlobal mut: %v", err)
	}
	if res, err := in.Invoke("get"); err != nil || res[0].AsI32() != 99 {
		t.Fatalf("wasm get after host write = %v, %v; want 99", res, err)
	}
	if _, err := in.Invoke("set", I32(123)); err != nil {
		t.Fatalf("wasm set: %v", err)
	}
	if got, err := in.Global("mut"); err != nil || got.AsI32() != 123 {
		t.Fatalf("Global mut after wasm write = %v, %v; want 123", got, err)
	}
	if err := in.SetGlobal("imm", I32(1)); err == nil {
		t.Fatal("SetGlobal immutable succeeded, want error")
	}
	if err := in.SetGlobal("mut", I64(1)); err == nil {
		t.Fatal("SetGlobal type mismatch succeeded, want error")
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
	c, err := Compile(mod)
	if err != nil {
		t.Fatal(err)
	}
	in, err := Instantiate(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if res, err := in.Invoke("mix", I32(10), I32(1)); err != nil || res[0].AsI32() != 20 {
		t.Fatalf("mix then branch = %v, %v; want 20", res, err)
	}
	if res, err := in.Invoke("mix", I32(5), I32(0)); err != nil || res[0].AsI32() != 16 {
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
	c, err := Compile(mod)
	if err != nil {
		t.Fatal(err)
	}
	in, err := Instantiate(c, nil)
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
		res, err := RunValues(mod, "get")
		if err != nil || res[0].AsI32() != 42 {
			t.Fatalf("get immutable i32 = %v, %v; want 42", res, err)
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
		c, err := Compile(mod)
		if err != nil {
			t.Fatal(err)
		}
		in, err := Instantiate(c, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		for _, tc := range []struct{ delta, want int32 }{{3, 3}, {4, 7}} {
			res, err := in.Invoke("add", I32(tc.delta))
			if err != nil || res[0].AsI32() != tc.want {
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
		res, err := RunValues(mod, "get")
		if err != nil || res[0].AsI64() != 0x0102030405060708 {
			t.Fatalf("get i64 = %v, %v; want %#x", res, err, int64(0x0102030405060708))
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
		res, err := RunValues(mod, "get")
		if err != nil || math.Float32bits(res[0].AsF32()) != math.Float32bits(1.25) {
			t.Fatalf("get f32 = %v, %v; want 1.25", res, err)
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
		res, err := RunValues(mod, "get")
		if err != nil || math.Float64bits(res[0].AsF64()) != math.Float64bits(2.5) {
			t.Fatalf("get f64 = %v, %v; want 2.5", res, err)
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
		c, err := Compile(mod)
		if err != nil {
			t.Fatal(err)
		}
		in, err := Instantiate(c, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		if got, err := in.Global("imm"); err != nil || got.AsI32() != 7 {
			t.Fatalf("Global imm = %v, %v; want 7", got, err)
		}
		if err := in.SetGlobal("mut", I32(9)); err != nil {
			t.Fatalf("SetGlobal mut: %v", err)
		}
		if got, err := in.Global("mut"); err != nil || got.AsI32() != 9 {
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
	if res, err := RunValues(mod, "add", I32(5)); err != nil || res[0].AsI32() != 15 {
		t.Fatalf("RunValues add global = %v, %v; want 15", res, err)
	}
	c, err := Compile(mod)
	if err != nil {
		t.Fatal(err)
	}
	in1, err := Instantiate(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer in1.Close()
	in2, err := Instantiate(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer in2.Close()
	if res, err := in1.Invoke("add", I32(1)); err != nil || res[0].AsI32() != 11 {
		t.Fatalf("in1 first add = %v, %v; want 11", res, err)
	}
	if res, err := in1.Invoke("add", I32(2)); err != nil || res[0].AsI32() != 13 {
		t.Fatalf("in1 second add = %v, %v; want persistent 13", res, err)
	}
	if got, err := in1.Global("counter"); err != nil || got.AsI32() != 13 {
		t.Fatalf("in1 Global counter = %v, %v; want 13", got, err)
	}
	if got, err := in2.Global("counter"); err != nil || got.AsI32() != 10 {
		t.Fatalf("in2 Global counter = %v, %v; want independent 10", got, err)
	}
	if err := in2.SetGlobal("counter", I32(20)); err != nil {
		t.Fatalf("in2 SetGlobal: %v", err)
	}
	if got, err := in1.Global("counter"); err != nil || got.AsI32() != 13 {
		t.Fatalf("in1 Global after in2 SetGlobal = %v, %v; want 13", got, err)
	}
	if res, err := in2.Invoke("add", I32(1)); err != nil || res[0].AsI32() != 21 {
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
	c, err := Compile(mod)
	if err != nil {
		t.Fatal(err)
	}
	in1, err := Instantiate(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer in1.Close()
	in2, err := Instantiate(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer in2.Close()
	if res, err := in1.Invoke("add", I32(5)); err != nil || res[0].AsI32() != 5 {
		t.Fatalf("in1 add = %v, %v", res, err)
	}
	if res, err := in2.Invoke("add", I32(7)); err != nil || res[0].AsI32() != 7 {
		t.Fatalf("in2 add = %v, %v", res, err)
	}
	if res, err := in1.Invoke("add", I32(0)); err != nil || res[0].AsI32() != 5 {
		t.Fatalf("in1 persisted = %v, %v", res, err)
	}
}

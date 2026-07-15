//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func boundedTable64Module(max uint64) []byte {
	table := []byte{0x70, 0x05}
	table = append(table, uleb64(2)...)
	table = append(table, uleb64(max)...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64}, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(3), wasmtest.ULEB(4))),
		wasmtest.Section(4, wasmtest.Vec(table)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("size", 0, 0),
			wasmtest.ExportEntry("clear", 0, 1),
			wasmtest.ExportEntry("is_null", 0, 2),
			wasmtest.ExportEntry("grow", 0, 3),
			wasmtest.ExportEntry("fill", 0, 4),
			wasmtest.ExportEntry("table", 1, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0xfc, 0x10, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0xd0, 0x70, 0x26, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x25, 0x00, 0xd1, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0x70, 0x20, 0x00, 0xfc, 0x0f, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0xd2, 0x00, 0x20, 0x01, 0xfc, 0x11, 0x00, 0x0b}),
		)),
	)
}

func table32GetSetGrowSizeFillModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(2), wasmtest.ULEB(3))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x02, 0x04})),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0xfc, 0x10, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0xd0, 0x70, 0x26, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x25, 0x00, 0xd1, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0x70, 0x20, 0x00, 0xfc, 0x0f, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0xd0, 0x70, 0x20, 0x01, 0xfc, 0x11, 0x00, 0x0b}),
		)),
	)
}

func table64WithInit(min, max uint64, expr []byte) []byte {
	out := []byte{0x40, 0x00, 0x70, 0x05} // table initializer, funcref, i64 min+max limits
	out = append(out, uleb64(min)...)
	out = append(out, uleb64(max)...)
	out = append(out, expr...)
	return append(out, 0x0b)
}

func table64ActiveElemExpr(offset []byte, exprs ...[]byte) []byte {
	out := []byte{0x04} // active table 0, funcref expression payloads
	out = append(out, offset...)
	out = append(out, 0x0b)
	return append(out, tableTestExprVec(exprs...)...)
}

func table64InitializerAndElementModule(offset []byte) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec(table64WithInit(2, 4, tableTestRefFuncExpr(0)))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("is_null", 0, 1))),
		wasmtest.Section(9, wasmtest.Vec(table64ActiveElemExpr(offset, tableTestRefNullFuncExpr()))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x4d, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x25, 0x00, 0xd1, 0x0b}),
		)),
	)
}

func table64CallIndirectModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x04, 0x03})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 2))),
		wasmtest.Section(9, wasmtest.Vec(table64ActiveElemExpr(
			[]byte{0x42, 0x00}, tableTestRefFuncExpr(0), tableTestRefNullFuncExpr(), tableTestRefFuncExpr(1),
		))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x2a, 0x0b}),
			wasmtest.Code([]byte{0x42, 0x58, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x11, 0x00, 0x00, 0x0b}),
		)),
	)
}

func table32CallIndirectModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
		wasmtest.Section(9, wasmtest.Vec([]byte{0x00, 0x41, 0x00, 0x0b, 0x01, 0x00})),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x2a, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x11, 0x00, 0x00, 0x0b}),
		)),
	)
}

func table64LifecycleModule(max *uint64) []byte {
	table := []byte{0x70, 0x04}
	table = append(table, uleb64(2)...)
	if max != nil {
		table[1] = 0x05
		table = append(table, uleb64(*max)...)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec(table)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("size", 0, 0),
			wasmtest.ExportEntry("grow", 0, 1),
			wasmtest.ExportEntry("table", 1, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0xfc, 0x10, 0x00, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0x70, 0x20, 0x00, 0xfc, 0x0f, 0x00, 0x0b}),
		)),
	)
}

func table64ImportLifecycleModule(min uint64, max *uint64) []byte {
	limits := []byte{0x04}
	limits = append(limits, uleb64(min)...)
	if max != nil {
		limits[0] = 0x05
		limits = append(limits, uleb64(*max)...)
	}
	imported := append(wasmtest.Name("env"), wasmtest.Name("table")...)
	imported = append(imported, byte(wasm.ExternTable), 0x70)
	imported = append(imported, limits...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}),
		)),
		wasmtest.Section(2, wasmtest.Vec(imported)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("size", 0, 0),
			wasmtest.ExportEntry("grow", 0, 1),
			wasmtest.ExportEntry("table", 1, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0xfc, 0x10, 0x00, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0x70, 0x20, 0x00, 0xfc, 0x0f, 0x00, 0x0b}),
		)),
	)
}

func table64CopyModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64, wasm.I64}, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 0x04, 0x04})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("is_null", 0, 1),
			wasmtest.ExportEntry("copy", 0, 2),
		)),
		wasmtest.Section(9, wasmtest.Vec(table64ActiveElemExpr(
			[]byte{0x42, 0x00}, tableTestRefFuncExpr(0), tableTestRefNullFuncExpr(), tableTestRefNullFuncExpr(), tableTestRefFuncExpr(0),
		))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x2a, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x25, 0x00, 0xd1, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x0e, 0x00, 0x00, 0x0b}),
		)),
	)
}

func tableInitDropModule(addr64 bool) []byte {
	addrType := wasm.I32
	limits := []byte{0x70, 0x01, 0x04, 0x04}
	if addr64 {
		addrType = wasm.I64
		limits[1] = 0x05
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{addrType}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{addrType, wasm.I32, wasm.I32}, nil),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(3), wasmtest.ULEB(2), wasmtest.ULEB(3))),
		wasmtest.Section(4, wasmtest.Vec(limits)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("is_null", 0, 1),
			wasmtest.ExportEntry("init", 0, 2),
			wasmtest.ExportEntry("drop", 0, 3),
			wasmtest.ExportEntry("init_decl", 0, 4),
			wasmtest.ExportEntry("drop_decl", 0, 5),
		)),
		wasmtest.Section(9, wasmtest.Vec(
			tableTestPassiveElemExpr(tableTestRefFuncExpr(0), tableTestRefNullFuncExpr(), tableTestRefFuncExpr(0)),
			tableTestDeclarativeElem(0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x2a, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x25, 0x00, 0xd1, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x0c, 0x00, 0x00, 0x0b}),
			wasmtest.Code([]byte{0xfc, 0x0d, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x0c, 0x01, 0x00, 0x0b}),
			wasmtest.Code([]byte{0xfc, 0x0d, 0x01, 0x0b}),
		)),
	)
}

func table32CopyModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x04, 0x04})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x0e, 0x00, 0x00, 0x0b}))),
	)
}

func compileStagedTable64(data []byte) (*Compiled, error) {
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.Table64 = true
	return compileWithFrontendFeatures(cfg, data, features)
}

func TestStagedTable64LocalGetSetSizeAndProductRoundTrip(t *testing.T) {
	module := boundedTable64Module(4)
	if _, err := Compile(nil, module); err == nil || !strings.Contains(err.Error(), "table64") {
		t.Fatalf("public table64 compile error = %v", err)
	}
	compiled, err := compileStagedTable64(module)
	if err != nil {
		t.Fatalf("compile staged table64: %v", err)
	}
	defer compiled.Close()
	if !compiled.requiredFeatures.IsEnabled(CoreFeatureTable64) {
		t.Fatalf("table64 required features = %s", compiled.requiredFeatures)
	}
	if !compiled.HasTable || !compiled.TableAddr64 || compiled.TableSize != 2 || compiled.TableMax != 4 || !compiled.TableHasMax {
		t.Fatalf("compiled table64 shape = present %v addr64 %v size/max %d/%d hasMax %v", compiled.HasTable, compiled.TableAddr64, compiled.TableSize, compiled.TableMax, compiled.TableHasMax)
	}
	meta := (&Module{c: compiled}).Metadata()
	if len(meta.Tables) != 1 || !meta.Tables[0].Addr64 || meta.Tables[0].Min != 2 || meta.Tables[0].Max != 4 || !meta.Tables[0].HasMax || !reflect.DeepEqual(meta.Tables[0].Exports, []string{"table"}) {
		t.Fatalf("table64 metadata = %#v", meta.Tables)
	}
	blob, err := compiled.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal table64: %v", err)
	}
	if blob[4] != 26 {
		t.Fatalf("table64 codec version = %d, want 26", blob[4])
	}
	var public Compiled
	if err := public.UnmarshalBinary(blob); err == nil || !strings.Contains(err.Error(), "unknown required feature bits") {
		t.Fatalf("public table64 codec load error = %v", err)
	}
	var loaded Compiled
	if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
		t.Fatalf("decode table64 metadata: %v", err)
	}
	defer loaded.Close()
	loaded.stagedTable64 = true // private execution proof; codec never serializes admission
	loaded.hasTableExportMetadata = true
	if !loaded.requiredFeatures.IsEnabled(CoreFeatureTable64) || !loaded.TableAddr64 || !reflect.DeepEqual((&Module{c: &loaded}).Metadata().Tables, meta.Tables) {
		t.Fatalf("table64 codec metadata = %#v, want %#v", (&Module{c: &loaded}).Metadata().Tables, meta.Tables)
	}
	if _, err := Capture(compiled, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "tables cannot be snapshotted") {
		t.Fatalf("table64 snapshot error = %v", err)
	}

	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate table64: %v", err)
	}
	defer in.Close()
	if got, err := in.Invoke("size"); err != nil || len(got) != 1 || got[0] != 2 {
		t.Fatalf("table64.size = %v, err=%v", got, err)
	}
	for _, index := range []uint64{0, 1} {
		if got, err := in.Invoke("is_null", index); err != nil || len(got) != 1 || got[0] != 1 {
			t.Fatalf("table64.get(%d) null = %v, err=%v", index, got, err)
		}
		if _, err := in.Invoke("clear", index); err != nil {
			t.Fatalf("table64.set(%d): %v", index, err)
		}
	}
	for _, index := range []uint64{2, 1 << 32, ^uint64(0)} {
		if _, err := in.Invoke("is_null", index); err == nil || !strings.Contains(err.Error(), "indirect call out of bounds") {
			t.Fatalf("table64.get(%d) error = %v", index, err)
		}
		if _, err := in.Invoke("clear", index); err == nil || !strings.Contains(err.Error(), "indirect call out of bounds") {
			t.Fatalf("table64.set(%d) error = %v", index, err)
		}
	}
	if got, err := in.Invoke("is_null", 1); err != nil || got[0] != 1 {
		t.Fatalf("table64 state changed after traps = %v, err=%v", got, err)
	}
	if _, err := in.Invoke("fill", 0, 2); err != nil {
		t.Fatalf("table64.fill full range: %v", err)
	}
	for _, index := range []uint64{0, 1} {
		if got, err := in.Invoke("is_null", index); err != nil || got[0] != 0 {
			t.Fatalf("table64.fill entry %d null = %v, err=%v", index, got, err)
		}
	}
	if _, err := in.Invoke("clear", 1); err != nil {
		t.Fatalf("clear table64 fill sentinel: %v", err)
	}
	if _, err := in.Invoke("fill", 2, 0); err != nil {
		t.Fatalf("zero-length table64.fill at boundary: %v", err)
	}
	for _, args := range [][2]uint64{{1, 2}, {1 << 32, 0}, {0, 1 << 32}, {^uint64(0), 2}} {
		if _, err := in.Invoke("fill", args[0], args[1]); err == nil || !strings.Contains(err.Error(), "indirect call out of bounds") {
			t.Fatalf("table64.fill(%d,%d) error = %v", args[0], args[1], err)
		}
		if got, err := in.Invoke("is_null", 1); err != nil || got[0] != 1 {
			t.Fatalf("trapping table64.fill changed sentinel = %v, err=%v", got, err)
		}
	}
	if got, err := in.Invoke("is_null", 0); err != nil || got[0] != 0 {
		t.Fatalf("trapping table64.fill changed prior entry = %v, err=%v", got, err)
	}
	if _, err := in.Invoke("clear", 0); err != nil {
		t.Fatalf("clear table64 fill entry: %v", err)
	}
	if got, err := in.Invoke("grow", 1); err != nil || len(got) != 1 || got[0] != 2 {
		t.Fatalf("table64.grow(1) = %v, err=%v, want [2]", got, err)
	}
	if got, err := in.Invoke("size"); err != nil || got[0] != 3 {
		t.Fatalf("table64 size after grow = %v, err=%v", got, err)
	}
	if got, err := in.Invoke("is_null", 2); err != nil || got[0] != 1 {
		t.Fatalf("grown table64 entry = %v, err=%v", got, err)
	}
	if got, err := in.Invoke("grow", 0); err != nil || got[0] != 3 {
		t.Fatalf("table64.grow(0) = %v, err=%v", got, err)
	}
	if got, err := in.Invoke("grow", 1); err != nil || got[0] != 3 {
		t.Fatalf("table64 grow to maximum = %v, err=%v", got, err)
	}
	for _, delta := range []uint64{1, 1 << 32, ^uint64(0)} {
		if got, err := in.Invoke("grow", delta); err != nil || len(got) != 1 || got[0] != ^uint64(0) {
			t.Fatalf("table64.grow(%d) = %v, err=%v, want [-1]", delta, got, err)
		}
		if got, err := in.Invoke("size"); err != nil || got[0] != 4 {
			t.Fatalf("failed table64 grow changed size = %v, err=%v", got, err)
		}
	}

	loadedIn, err := instantiateCore(&loaded, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate codec-reloaded table64: %v", err)
	}
	defer loadedIn.Close()
	if _, err := loadedIn.Invoke("fill", 0, 2); err != nil {
		t.Fatalf("codec-reloaded table64.fill: %v", err)
	}
	if got, err := loadedIn.Invoke("is_null", 1); err != nil || got[0] != 0 {
		t.Fatalf("codec-reloaded table64.fill entry = %v, err=%v", got, err)
	}
	if got, err := loadedIn.Invoke("grow", 2); err != nil || len(got) != 1 || got[0] != 2 {
		t.Fatalf("codec-reloaded table64.grow = %v, err=%v", got, err)
	}
	if got, err := loadedIn.Invoke("grow", 1<<32); err != nil || len(got) != 1 || got[0] != ^uint64(0) {
		t.Fatalf("codec-reloaded high-delta table64.grow = %v, err=%v", got, err)
	}
}

func TestStagedTable64InitializerAndI64ActiveElementRoundTrip(t *testing.T) {
	module := table64InitializerAndElementModule([]byte{0x42, 0x01})
	compiled, err := compileStagedTable64(module)
	if err != nil {
		t.Fatalf("compile table64 initializer/element: %v", err)
	}
	defer compiled.Close()
	if !compiled.HasTableInitFunc || compiled.TableInitFunc != 0 || len(compiled.Elems) != 1 || len(compiled.Elems[0].Offset.Expr) == 0 {
		t.Fatalf("table64 initializer/element metadata = init %v/%d elems %#v", compiled.HasTableInitFunc, compiled.TableInitFunc, compiled.Elems)
	}
	blob, err := compiled.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal table64 initializer/element: %v", err)
	}
	var loaded Compiled
	if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
		t.Fatalf("reload table64 initializer/element: %v", err)
	}
	defer loaded.Close()
	loaded.stagedTable64 = true

	for name, c := range map[string]*Compiled{"source": compiled, "codec": &loaded} {
		in, err := instantiateCore(c, InstantiateOptions{})
		if err != nil {
			t.Fatalf("instantiate %s table64 initializer/element: %v", name, err)
		}
		if got, err := in.Invoke("is_null", 0); err != nil || len(got) != 1 || got[0] != 0 {
			_ = in.Close()
			t.Fatalf("%s table initializer entry = %v, err=%v", name, got, err)
		}
		if got, err := in.Invoke("is_null", 1); err != nil || len(got) != 1 || got[0] != 1 {
			_ = in.Close()
			t.Fatalf("%s active element override = %v, err=%v", name, got, err)
		}
		if err := in.Close(); err != nil {
			t.Fatalf("close %s table64 instance: %v", name, err)
		}
	}

	oob, err := compileStagedTable64(table64InitializerAndElementModule([]byte{0x42, 0x7f}))
	if err != nil {
		t.Fatalf("compile high-offset table64 element: %v", err)
	}
	defer oob.Close()
	if in, err := instantiateCore(oob, InstantiateOptions{}); err == nil || in != nil || !strings.Contains(err.Error(), "18446744073709551615") {
		t.Fatalf("high-offset table64 element instantiate = %v, %v", in, err)
	}
}

func TestStagedTable64CallIndirectFullWidthAndCodecRoundTrip(t *testing.T) {
	module := table64CallIndirectModule()
	compiled, err := compileStagedTable64(module)
	if err != nil {
		t.Fatalf("compile table64 call_indirect: %v", err)
	}
	defer compiled.Close()
	if !compiled.TableAddr64 || compiled.TableSize != 3 || compiled.TableMax != 3 || compiled.TableHasMax || len(compiled.Elems) != 1 {
		t.Fatalf("table64 call_indirect metadata = addr64 %v size/max %d/%d hasMax %v elems %d", compiled.TableAddr64, compiled.TableSize, compiled.TableMax, compiled.TableHasMax, len(compiled.Elems))
	}
	blob, err := compiled.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal table64 call_indirect: %v", err)
	}
	var loaded Compiled
	if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
		t.Fatalf("reload table64 call_indirect: %v", err)
	}
	defer loaded.Close()
	loaded.stagedTable64 = true
	for name, c := range map[string]*Compiled{"source": compiled, "codec": &loaded} {
		in, err := instantiateCore(c, InstantiateOptions{})
		if err != nil {
			t.Fatalf("instantiate %s table64 call_indirect: %v", name, err)
		}
		if got, err := in.Invoke("call", 0); err != nil || len(got) != 1 || AsI32(got[0]) != 42 {
			in.Close()
			t.Fatalf("%s table64 call_indirect result = %v, err=%v", name, got, err)
		}
		for _, index := range []uint64{1, 3, 1 << 32, ^uint64(0)} {
			if _, err := in.Invoke("call", index); err == nil || !strings.Contains(err.Error(), "indirect call out of bounds") {
				in.Close()
				t.Fatalf("%s table64 call_indirect(%d) = %v, want null/bounds trap", name, index, err)
			}
		}
		if _, err := in.Invoke("call", 2); err == nil || !strings.Contains(err.Error(), "signature") {
			in.Close()
			t.Fatalf("%s table64 call_indirect wrong signature = %v", name, err)
		}
		if got, err := in.Invoke("call", 0); err != nil || AsI32(got[0]) != 42 {
			in.Close()
			t.Fatalf("%s table64 call_indirect after traps = %v, err=%v", name, got, err)
		}
		if err := in.Close(); err != nil {
			t.Fatalf("close %s table64 call_indirect: %v", name, err)
		}
	}
}

func TestStagedTable64CopyFullWidthOverlapAtomicityAndCodecRoundTrip(t *testing.T) {
	module := table64CopyModule()
	compiled, err := compileStagedTable64(module)
	if err != nil {
		t.Fatalf("compile table64.copy: %v", err)
	}
	defer compiled.Close()
	blob, err := compiled.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal table64.copy: %v", err)
	}
	var loaded Compiled
	if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
		t.Fatalf("reload table64.copy: %v", err)
	}
	defer loaded.Close()
	loaded.stagedTable64 = true

	for name, c := range map[string]*Compiled{"source": compiled, "codec": &loaded} {
		in, err := instantiateCore(c, InstantiateOptions{})
		if err != nil {
			t.Fatalf("instantiate %s table64.copy: %v", name, err)
		}
		state := func() [4]uint64 {
			var out [4]uint64
			for i := range out {
				got, err := in.Invoke("is_null", uint64(i))
				if err != nil || len(got) != 1 {
					in.Close()
					t.Fatalf("%s table64.copy state[%d] = %v, err=%v", name, i, got, err)
				}
				out[i] = got[0]
			}
			return out
		}
		if got := state(); got != [4]uint64{0, 1, 1, 0} {
			in.Close()
			t.Fatalf("%s initial table64.copy state = %v", name, got)
		}
		if _, err := in.Invoke("copy", 1, 0, 3); err != nil {
			in.Close()
			t.Fatalf("%s overlapping forward table64.copy: %v", name, err)
		}
		if got := state(); got != [4]uint64{0, 0, 1, 1} {
			in.Close()
			t.Fatalf("%s forward table64.copy state = %v", name, got)
		}
		if _, err := in.Invoke("copy", 0, 1, 3); err != nil {
			in.Close()
			t.Fatalf("%s overlapping backward table64.copy: %v", name, err)
		}
		if got := state(); got != [4]uint64{0, 1, 1, 1} {
			in.Close()
			t.Fatalf("%s backward table64.copy state = %v", name, got)
		}
		if _, err := in.Invoke("copy", 4, 4, 0); err != nil {
			in.Close()
			t.Fatalf("%s zero-length boundary table64.copy: %v", name, err)
		}
		before := state()
		for _, args := range [][3]uint64{
			{^uint64(0), 0, 2}, {0, ^uint64(0), 2}, {0, 0, 5},
			{1 << 32, 0, 0}, {0, 1 << 32, 0}, {0, 0, 1 << 32},
		} {
			if _, err := in.Invoke("copy", args[0], args[1], args[2]); err == nil || !strings.Contains(err.Error(), "indirect call out of bounds") {
				in.Close()
				t.Fatalf("%s table64.copy(%d,%d,%d) = %v", name, args[0], args[1], args[2], err)
			}
			if got := state(); got != before {
				in.Close()
				t.Fatalf("%s trapping table64.copy changed state: got %v want %v", name, got, before)
			}
		}
		if err := in.Close(); err != nil {
			t.Fatalf("close %s table64.copy: %v", name, err)
		}
	}

	ordinary, err := Compile(nil, table32CopyModule())
	if err != nil {
		t.Fatal(err)
	}
	defer ordinary.Close()
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.Table64 = true
	staged, err := compileWithFrontendFeatures(cfg, table32CopyModule(), features)
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Close()
	if !bytes.Equal(ordinary.Code, staged.Code) {
		t.Fatal("enabling staged table64.copy changed table32 code bytes")
	}
}

func TestStagedTable64PassiveInitDropFullWidthAtomicityAndCodecRoundTrip(t *testing.T) {
	module := tableInitDropModule(true)
	compiled, err := compileStagedTable64(module)
	if err != nil {
		t.Fatalf("compile table64.init/drop: %v", err)
	}
	defer compiled.Close()
	if len(compiled.passiveElems) != 2 || len(compiled.passiveElems[0].Values) != 3 || len(compiled.passiveElems[1].Values) != 0 {
		t.Fatalf("table64 passive/declarative metadata = %#v", compiled.passiveElems)
	}
	blob, err := compiled.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal table64.init/drop: %v", err)
	}
	var loaded Compiled
	if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
		t.Fatalf("reload table64.init/drop: %v", err)
	}
	defer loaded.Close()
	loaded.stagedTable64 = true
	if len(loaded.passiveElems) != 2 || len(loaded.passiveElems[0].Values) != 3 || len(loaded.passiveElems[1].Values) != 0 {
		t.Fatalf("reloaded table64 passive/declarative metadata = %#v", loaded.passiveElems)
	}

	for name, c := range map[string]*Compiled{"source": compiled, "codec": &loaded} {
		in, err := instantiateCore(c, InstantiateOptions{})
		if err != nil {
			t.Fatalf("instantiate %s table64.init/drop: %v", name, err)
		}
		state := func() [4]uint64 {
			var out [4]uint64
			for i := range out {
				got, err := in.Invoke("is_null", uint64(i))
				if err != nil || len(got) != 1 {
					in.Close()
					t.Fatalf("%s table64.init state[%d] = %v, err=%v", name, i, got, err)
				}
				out[i] = got[0]
			}
			return out
		}
		if got := state(); got != [4]uint64{1, 1, 1, 1} {
			in.Close()
			t.Fatalf("%s initial table64.init state = %v", name, got)
		}
		if _, err := in.Invoke("init", 1, 0, 3); err != nil {
			in.Close()
			t.Fatalf("%s table64.init: %v", name, err)
		}
		if got := state(); got != [4]uint64{1, 0, 1, 0} {
			in.Close()
			t.Fatalf("%s initialized table64 state = %v", name, got)
		}
		if _, err := in.Invoke("init", 4, 3, 0); err != nil {
			in.Close()
			t.Fatalf("%s zero-length table64.init boundary: %v", name, err)
		}
		before := state()
		for _, args := range [][3]uint64{
			{^uint64(0), 0, 2}, {3, 0, 2}, {0, 2, 2}, {0, uint64(^uint32(0)), 2}, {1 << 32, 0, 0},
		} {
			if _, err := in.Invoke("init", args[0], args[1], args[2]); err == nil || !strings.Contains(err.Error(), "out of bounds") {
				in.Close()
				t.Fatalf("%s table64.init(%d,%d,%d) = %v", name, args[0], args[1], args[2], err)
			}
			if got := state(); got != before {
				in.Close()
				t.Fatalf("%s trapping table64.init changed state: got %v want %v", name, got, before)
			}
		}
		if _, err := in.Invoke("init_decl", 4, 0, 0); err != nil {
			in.Close()
			t.Fatalf("%s zero-length declarative table64.init: %v", name, err)
		}
		if _, err := in.Invoke("init_decl", 0, 0, 1); err == nil {
			in.Close()
			t.Fatalf("%s nonzero declarative table64.init succeeded", name)
		}
		if _, err := in.Invoke("drop_decl"); err != nil {
			in.Close()
			t.Fatalf("%s declarative elem.drop: %v", name, err)
		}
		if _, err := in.Invoke("drop"); err != nil {
			in.Close()
			t.Fatalf("%s table64 elem.drop: %v", name, err)
		}
		if _, err := in.Invoke("drop"); err != nil {
			in.Close()
			t.Fatalf("%s repeated table64 elem.drop: %v", name, err)
		}
		if _, err := in.Invoke("init", 4, 0, 0); err != nil {
			in.Close()
			t.Fatalf("%s zero-length table64.init after drop: %v", name, err)
		}
		if _, err := in.Invoke("init", 0, 0, 1); err == nil {
			in.Close()
			t.Fatalf("%s nonzero table64.init after drop succeeded", name)
		}
		if got := state(); got != before {
			in.Close()
			t.Fatalf("%s dropped/trapping table64.init changed state: got %v want %v", name, got, before)
		}
		if err := in.Close(); err != nil {
			t.Fatalf("close %s table64.init/drop: %v", name, err)
		}
	}

	ordinary, err := Compile(nil, tableInitDropModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer ordinary.Close()
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.Table64 = true
	staged, err := compileWithFrontendFeatures(cfg, tableInitDropModule(false), features)
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Close()
	if !bytes.Equal(ordinary.Code, staged.Code) {
		t.Fatal("enabling staged table64.init/drop changed table32 code bytes")
	}
}

func TestStagedTable64InstanceExportImportLifecycle(t *testing.T) {
	max4 := uint64(4)
	ownerCompiled, err := compileStagedTable64(table64LifecycleModule(&max4))
	if err != nil {
		t.Fatalf("compile bounded table64 owner: %v", err)
	}
	defer ownerCompiled.Close()
	owner, err := instantiateCore(ownerCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate bounded table64 owner: %v", err)
	}
	table, err := owner.ExportedTable("table")
	if err != nil {
		t.Fatalf("export bounded table64: %v", err)
	}

	consumerCompiled, err := compileStagedTable64(table64ImportLifecycleModule(2, &max4))
	if err != nil {
		t.Fatalf("compile bounded table64 consumer: %v", err)
	}
	defer consumerCompiled.Close()
	meta := (&Module{c: consumerCompiled}).Metadata()
	if len(meta.Tables) != 1 || meta.Tables[0].ImportModule != "env" || meta.Tables[0].ImportName != "table" || !meta.Tables[0].Addr64 || meta.Tables[0].Min != 2 || meta.Tables[0].Max != 4 || !meta.Tables[0].HasMax || !reflect.DeepEqual(meta.Tables[0].Exports, []string{"table"}) {
		t.Fatalf("table64 import metadata = %#v", meta.Tables)
	}
	rt := &Runtime{imports: Imports{}}
	imports := rt.buildModule(consumerCompiled).Imports()
	if len(imports) != 1 || imports[0].Kind != ImportTable || !imports[0].Addr64 || imports[0].Min != 2 || imports[0].Max != 4 || !imports[0].HasMax {
		t.Fatalf("table64 import inspection = %#v", imports)
	}
	if err := applyPolicy(&Module{c: consumerCompiled}, Policy{MaxTableEntries: 2}); err != nil {
		t.Fatalf("table64 import exact policy: %v", err)
	}
	if err := applyPolicy(&Module{c: consumerCompiled}, Policy{MaxTableEntries: 1}); err == nil {
		t.Fatal("table64 import minimum above policy limit was accepted")
	}
	blob, err := consumerCompiled.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal table64 import: %v", err)
	}
	var loaded Compiled
	if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
		t.Fatalf("reload table64 import: %v", err)
	}
	defer loaded.Close()
	loaded.stagedTable64 = true
	loaded.hasTableExportMetadata = true
	if !reflect.DeepEqual((&Module{c: &loaded}).Metadata().Tables, meta.Tables) {
		t.Fatalf("table64 import codec metadata = %#v, want %#v", (&Module{c: &loaded}).Metadata().Tables, meta.Tables)
	}
	if _, err := Capture(consumerCompiled, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "tables cannot be snapshotted") {
		t.Fatalf("imported table64 snapshot = %v", err)
	}

	consumer, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"env.table": table}})
	if err != nil {
		t.Fatalf("instantiate bounded table64 consumer: %v", err)
	}
	if got, err := consumer.Invoke("size"); err != nil || len(got) != 1 || got[0] != 2 {
		t.Fatalf("imported table64.size = %v, err=%v", got, err)
	}
	if got, err := consumer.Invoke("grow", 1); err != nil || len(got) != 1 || got[0] != 2 {
		t.Fatalf("imported table64.grow = %v, err=%v", got, err)
	}
	if got, err := owner.Invoke("size"); err != nil || got[0] != 3 || table.Size() != 3 {
		t.Fatalf("table64 grow visibility owner=%v handle=%d err=%v", got, table.Size(), err)
	}
	reexported, err := consumer.ExportedTable("table")
	if err != nil {
		t.Fatalf("re-export imported table64: %v", err)
	}
	loadedIn, err := instantiateCore(&loaded, InstantiateOptions{Imports: Imports{"env.table": reexported}})
	if err != nil {
		t.Fatalf("instantiate codec-reloaded table64 consumer: %v", err)
	}
	if got, err := loadedIn.Invoke("size"); err != nil || got[0] != 3 {
		t.Fatalf("codec/re-export table64 size = %v, err=%v", got, err)
	}
	if err := owner.Close(); err != nil {
		t.Fatalf("logical close table64 producer: %v", err)
	}
	owner.lifeMu.Lock()
	closed := owner.resourcesClosed
	owner.lifeMu.Unlock()
	if closed {
		t.Fatal("table64 producer resources closed while consumers retained export")
	}
	if got, err := consumer.Invoke("size"); err != nil || got[0] != 3 {
		t.Fatalf("retained table64 import after producer close = %v, err=%v", got, err)
	}
	if got, err := loadedIn.Invoke("size"); err != nil || got[0] != 3 {
		t.Fatalf("retained codec/re-export table64 after producer close = %v, err=%v", got, err)
	}
	if err := loadedIn.Close(); err != nil {
		t.Fatalf("close codec-reloaded table64 consumer: %v", err)
	}
	if err := consumer.Close(); err != nil {
		t.Fatalf("close table64 consumer: %v", err)
	}
	owner.lifeMu.Lock()
	closed = owner.resourcesClosed
	owner.lifeMu.Unlock()
	if !closed {
		t.Fatal("table64 producer resources remained after final consumer close")
	}
}

func TestStagedTable64ImportLimitCompatibilityAndRollback(t *testing.T) {
	instantiateOwner := func(t *testing.T, max *uint64) (*Compiled, *Instance, *Table) {
		t.Helper()
		compiled, err := compileStagedTable64(table64LifecycleModule(max))
		if err != nil {
			t.Fatal(err)
		}
		owner, err := instantiateCore(compiled, InstantiateOptions{})
		if err != nil {
			compiled.Close()
			t.Fatal(err)
		}
		table, err := owner.ExportedTable("table")
		if err != nil {
			owner.Close()
			compiled.Close()
			t.Fatal(err)
		}
		return compiled, owner, table
	}
	max4, max5, max3 := uint64(4), uint64(5), uint64(3)
	boundedCompiled, boundedOwner, bounded := instantiateOwner(t, &max4)
	defer boundedCompiled.Close()
	defer boundedOwner.Close()
	for name, module := range map[string][]byte{
		"no maximum import accepts bounded provider": table64ImportLifecycleModule(1, nil),
		"wider maximum accepts bounded provider":     table64ImportLifecycleModule(1, &max5),
	} {
		c, err := compileStagedTable64(module)
		if err != nil {
			t.Fatalf("compile %s: %v", name, err)
		}
		in, err := instantiateCore(c, InstantiateOptions{Imports: Imports{"env.table": bounded}})
		if err != nil {
			c.Close()
			t.Fatalf("%s: %v", name, err)
		}
		in.Close()
		c.Close()
	}
	for name, module := range map[string][]byte{
		"minimum": table64ImportLifecycleModule(3, &max4),
		"maximum": table64ImportLifecycleModule(1, &max3),
	} {
		c, err := compileStagedTable64(module)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := instantiateCore(c, InstantiateOptions{Imports: Imports{"env.table": bounded}}); err == nil {
			c.Close()
			t.Fatalf("%s mismatch was accepted", name)
		}
		if boundedOwner.hasResourceRoots() {
			c.Close()
			t.Fatalf("%s mismatch retained table64 producer", name)
		}
		c.Close()
	}

	unboundedCompiled, unboundedOwner, unbounded := instantiateOwner(t, nil)
	defer unboundedCompiled.Close()
	defer unboundedOwner.Close()
	if unboundedCompiled.TableHasMax || unboundedCompiled.TableMax != int(frontend.StagedTable64Max()) {
		t.Fatalf("no-max exported table64 runtime reservation = max %d hasMax %v", unboundedCompiled.TableMax, unboundedCompiled.TableHasMax)
	}
	noMaxConsumer, err := compileStagedTable64(table64ImportLifecycleModule(1, nil))
	if err != nil {
		t.Fatal(err)
	}
	noMaxIn, err := instantiateCore(noMaxConsumer, InstantiateOptions{Imports: Imports{"env.table": unbounded}})
	if err != nil {
		noMaxConsumer.Close()
		t.Fatalf("no-max table64 import: %v", err)
	}
	reexported, err := noMaxIn.ExportedTable("table")
	if err != nil {
		noMaxIn.Close()
		noMaxConsumer.Close()
		t.Fatal(err)
	}
	boundedImport, err := compileStagedTable64(table64ImportLifecycleModule(1, &max5))
	if err != nil {
		noMaxIn.Close()
		noMaxConsumer.Close()
		t.Fatal(err)
	}
	for name, provider := range map[string]*Table{"owner": unbounded, "re-export": reexported} {
		if _, err := instantiateCore(boundedImport, InstantiateOptions{Imports: Imports{"env.table": provider}}); err == nil || !strings.Contains(err.Error(), "no declared maximum") {
			boundedImport.Close()
			noMaxIn.Close()
			noMaxConsumer.Close()
			t.Fatalf("bounded import of %s no-max table64 provider = %v", name, err)
		}
	}
	boundedImport.Close()
	noMaxIn.Close()
	noMaxConsumer.Close()

	host, err := NewTable(2, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	consumer, err := compileStagedTable64(table64ImportLifecycleModule(1, &max4))
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	if _, err := instantiateCore(consumer, InstantiateOptions{Imports: Imports{"env.table": host}}); err == nil || !strings.Contains(err.Error(), "provider is table32, import requires table64") {
		t.Fatalf("host table32 into table64 import = %v", err)
	}
}

func TestStagedTable64GatesAndTable32CodeStability(t *testing.T) {
	unboundedGrow := table64LifecycleModule(nil)
	unbounded, err := compileStagedTable64(unboundedGrow)
	if err != nil {
		t.Fatalf("bounded-reservation no-max table64: %v", err)
	}
	if unbounded.TableHasMax || unbounded.TableMax != int(frontend.StagedTable64Max()) {
		unbounded.Close()
		t.Fatalf("no-max table64 runtime shape = max %d hasMax %v", unbounded.TableMax, unbounded.TableHasMax)
	}
	unbounded.Close()
	if _, err := compileStagedTable64(boundedTable64Module(16385)); err == nil || !strings.Contains(err.Error(), "16384") {
		t.Fatalf("oversized table64 error = %v", err)
	}
	imported, err := compileStagedTable64(table64ImportLifecycleModule(1, nil))
	if err != nil {
		t.Fatalf("bounded table64 import compile: %v", err)
	}
	imported.Close()
	importedCopyEntry := append(wasmtest.Name("env"), wasmtest.Name("table")...)
	importedCopyEntry = append(importedCopyEntry, byte(wasm.ExternTable), 0x70, 0x05, 0x01, 0x04)
	importedCopy := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64, wasm.I64}, nil))),
		wasmtest.Section(2, wasmtest.Vec(importedCopyEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x0e, 0x00, 0x00, 0x0b}))),
	)
	if _, err := compileStagedTable64(importedCopy); err == nil || !strings.Contains(err.Error(), "imported table64") {
		t.Fatalf("imported table64.copy gate = %v", err)
	}
	producerCompiled, err := compileStagedTable64(boundedTable64Module(4))
	if err != nil {
		t.Fatal(err)
	}
	defer producerCompiled.Close()
	producer, err := instantiateCore(producerCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	table64, err := producer.ExportedTable("table")
	if err != nil {
		t.Fatal(err)
	}
	memory32Import := append(wasmtest.Name("env"), wasmtest.Name("table")...)
	memory32Import = append(memory32Import, byte(wasm.ExternTable), 0x70, 0x01, 0x02, 0x04)
	memory32Consumer := MustCompile(wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(memory32Import))))
	defer memory32Consumer.Close()
	if _, err := instantiateCore(memory32Consumer, InstantiateOptions{Imports: Imports{"env.table": table64}}); err == nil || !strings.Contains(err.Error(), "provider is table64, import requires table32") {
		t.Fatalf("table64 provider into table32 import = %v", err)
	}
	table := []byte{0x70, 0x05, 0x01, 0x02}
	if _, err := compileStagedTable64(wasmtest.Module(wasmtest.Section(4, wasmtest.Vec(table, table)))); err == nil || !strings.Contains(err.Error(), "exactly one local or imported table") {
		t.Fatalf("multiple table64 error = %v", err)
	}
	externref := []byte{0x6f, 0x05, 0x01, 0x02}
	if _, err := compileStagedTable64(wasmtest.Module(wasmtest.Section(4, wasmtest.Vec(externref)))); err == nil || !strings.Contains(err.Error(), "funcref") {
		t.Fatalf("externref table64 error = %v", err)
	}
	passive := tableInitDropModule(true)
	passiveCompiled, err := compileStagedTable64(passive)
	if err != nil {
		t.Fatalf("sole-local passive table64 element lifecycle: %v", err)
	}
	passiveCompiled.Close()
	importedInitEntry := append(wasmtest.Name("env"), wasmtest.Name("table")...)
	importedInitEntry = append(importedInitEntry, byte(wasm.ExternTable), 0x70, 0x05, 0x01, 0x04)
	importedInit := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I32, wasm.I32}, nil))),
		wasmtest.Section(2, wasmtest.Vec(importedInitEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(9, wasmtest.Vec(tableTestPassiveElem())),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x0c, 0x00, 0x00, 0x0b}))),
	)
	if _, err := compileStagedTable64(importedInit); err == nil || !strings.Contains(err.Error(), "imported table64") {
		t.Fatalf("imported table64.init gate = %v", err)
	}
	cfg := NewRuntimeConfig()
	cfg.boundsChecks = BoundsChecksSignalsBased
	features := cfg.frontendFeatures()
	features.Table64 = true
	if _, err := compileWithFrontendFeatures(cfg, boundedTable64Module(4), features); err == nil || !strings.Contains(err.Error(), "signals-based") {
		t.Fatalf("guard table64 error = %v", err)
	}

	ordinary := table32GetSetGrowSizeFillModule()
	base, err := Compile(nil, ordinary)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	stageCfg := NewRuntimeConfig()
	stageFeatures := stageCfg.frontendFeatures()
	stageFeatures.Table64 = true
	staged, err := compileWithFrontendFeatures(stageCfg, ordinary, stageFeatures)
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Close()
	if !bytes.Equal(base.Code, staged.Code) {
		t.Fatal("enabling staged table64 changed table32 code bytes")
	}
	ordinaryIndirect := table32CallIndirectModule()
	baseIndirect, err := Compile(nil, ordinaryIndirect)
	if err != nil {
		t.Fatal(err)
	}
	defer baseIndirect.Close()
	stagedIndirect, err := compileWithFrontendFeatures(stageCfg, ordinaryIndirect, stageFeatures)
	if err != nil {
		t.Fatal(err)
	}
	defer stagedIndirect.Close()
	if !bytes.Equal(baseIndirect.Code, stagedIndirect.Code) {
		t.Fatal("enabling staged table64 changed table32 call_indirect code bytes")
	}
}

func BenchmarkStagedTable64InitZero(b *testing.B) {
	compiled, err := compileStagedTable64(tableInitDropModule(true))
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("init", 4, 3, 0); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStagedTable64CopyZero(b *testing.B) {
	compiled, err := compileStagedTable64(table64CopyModule())
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("copy", 4, 4, 0); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStagedTable64ImportedSize(b *testing.B) {
	max4 := uint64(4)
	ownerCompiled, err := compileStagedTable64(table64LifecycleModule(&max4))
	if err != nil {
		b.Fatal(err)
	}
	defer ownerCompiled.Close()
	owner, err := instantiateCore(ownerCompiled, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer owner.Close()
	table, err := owner.ExportedTable("table")
	if err != nil {
		b.Fatal(err)
	}
	consumerCompiled, err := compileStagedTable64(table64ImportLifecycleModule(2, &max4))
	if err != nil {
		b.Fatal(err)
	}
	defer consumerCompiled.Close()
	consumer, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"env.table": table}})
	if err != nil {
		b.Fatal(err)
	}
	defer consumer.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got, err := consumer.Invoke("size"); err != nil || len(got) != 1 || got[0] != 2 {
			b.Fatalf("imported table64.size = %v, err=%v", got, err)
		}
	}
}

func BenchmarkStagedTable64CallIndirect(b *testing.B) {
	compiled, err := compileStagedTable64(table64CallIndirectModule())
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got, err := in.Invoke("call", 0); err != nil || len(got) != 1 || AsI32(got[0]) != 42 {
			b.Fatalf("table64 call_indirect = %v, err=%v", got, err)
		}
	}
}

func BenchmarkStagedTable64InitializedGet(b *testing.B) {
	compiled, err := compileStagedTable64(table64InitializerAndElementModule([]byte{0x42, 0x01}))
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got, err := in.Invoke("is_null", 0); err != nil || len(got) != 1 || got[0] != 0 {
			b.Fatalf("initialized table64 get = %v, err=%v", got, err)
		}
	}
}

func BenchmarkStagedTable64FillZero(b *testing.B) {
	compiled, err := compileStagedTable64(boundedTable64Module(4))
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("fill", 2, 0); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStagedTable64GrowZero(b *testing.B) {
	compiled, err := compileStagedTable64(boundedTable64Module(4))
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got, err := in.Invoke("grow", 0); err != nil || len(got) != 1 || got[0] != 2 {
			b.Fatalf("table64.grow(0) = %v, err=%v", got, err)
		}
	}
}

func BenchmarkStagedTable64Size(b *testing.B) {
	compiled, err := compileStagedTable64(boundedTable64Module(4))
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("size"); err != nil {
			b.Fatal(err)
		}
	}
}

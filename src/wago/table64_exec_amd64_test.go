//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

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

func TestStagedTable64GatesAndTable32CodeStability(t *testing.T) {
	unboundedGrow := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x04, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd0, 0x70, 0x20, 0x00, 0xfc, 0x0f, 0x00, 0x0b}))),
	)
	if _, err := compileStagedTable64(unboundedGrow); err == nil || !strings.Contains(err.Error(), "private and non-growing") {
		t.Fatalf("unbounded growing table64 error = %v", err)
	}
	if _, err := compileStagedTable64(boundedTable64Module(16385)); err == nil || !strings.Contains(err.Error(), "16384") {
		t.Fatalf("oversized table64 error = %v", err)
	}
	imported := append(wasmtest.Name("env"), wasmtest.Name("table")...)
	imported = append(imported, byte(wasm.ExternTable), 0x70, 0x05, 0x01, 0x02)
	if _, err := compileStagedTable64(wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(imported)))); err == nil || !strings.Contains(err.Error(), "exactly one local table") {
		t.Fatalf("imported table64 error = %v", err)
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
	if _, err := compileStagedTable64(wasmtest.Module(wasmtest.Section(4, wasmtest.Vec(table, table)))); err == nil || !strings.Contains(err.Error(), "exactly one local table") {
		t.Fatalf("multiple table64 error = %v", err)
	}
	externref := []byte{0x6f, 0x05, 0x01, 0x02}
	if _, err := compileStagedTable64(wasmtest.Module(wasmtest.Section(4, wasmtest.Vec(externref)))); err == nil || !strings.Contains(err.Error(), "funcref") {
		t.Fatalf("externref table64 error = %v", err)
	}
	passive := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 0x01, 0x02})),
		wasmtest.Section(9, wasmtest.Vec(tableTestPassiveElem(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))),
	)
	if _, err := compileStagedTable64(passive); err == nil || !strings.Contains(err.Error(), "only active element segments") {
		t.Fatalf("passive table64 element error = %v", err)
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

//go:build linux && amd64 && !tinygo

package wago

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func TestLocalExternrefTablesExecuteAcrossHeterogeneousIndexes(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	mod, err := rt.Compile(watToWasm(t, `(module
		(type $ret-i32 (func (result i32)))
		(type $get-ref (func (param i32) (result externref)))
		(type $set-ref (func (param i32 externref)))
		(type $grow-ref (func (param externref i32) (result i32)))
		(type $fill-ref (func (param i32 externref i32)))
		(table $ext0 3 3 externref)
		(table $fun1 1 1 funcref)
		(table $ext2 1 externref)
		(elem (table $fun1) (i32.const 0) func $dummy)
		(func $dummy)
		(func (export "get0") (type $get-ref) (table.get $ext0 (local.get 0)))
		(func (export "set0") (type $set-ref) (table.set $ext0 (local.get 0) (local.get 1)))
		(func (export "size0") (type $ret-i32) (table.size $ext0))
		(func (export "grow0") (type $grow-ref) (table.grow $ext0 (local.get 0) (local.get 1)))
		(func (export "fill0") (type $fill-ref) (table.fill $ext0 (local.get 0) (local.get 1) (local.get 2)))
		(func (export "get2") (type $get-ref) (table.get $ext2 (local.get 0)))
		(func (export "set2") (type $set-ref) (table.set $ext2 (local.get 0) (local.get 1)))
		(func (export "size2") (type $ret-i32) (table.size $ext2))
		(func (export "grow2") (type $grow-ref) (table.grow $ext2 (local.get 0) (local.get 1)))
		(func (export "fill2") (type $fill-ref) (table.fill $ext2 (local.get 0) (local.get 1) (local.get 2)))
		(func (export "fun1-null") (result i32) (ref.is_null (table.get $fun1 (i32.const 0))))
	)`))
	if err != nil {
		t.Fatalf("Compile heterogeneous externref tables: %v", err)
	}
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatalf("Instantiate heterogeneous externref tables: %v", err)
	}
	defer in.Close()

	callRef := func(name string, index int32) ExternRef {
		t.Helper()
		out, err := in.Call(context.Background(), name, ValueI32(index))
		if err != nil || len(out) != 1 || out[0].Type() != ValExternRef {
			t.Fatalf("Call %s(%d) = %v, %v; want one externref", name, index, out, err)
		}
		return out[0].ExternRef()
	}
	for _, tc := range []struct {
		name  string
		index int32
	}{
		{"get0", 0}, {"get0", 2}, {"get2", 0},
	} {
		if got := callRef(tc.name, tc.index); !got.IsNull() {
			t.Fatalf("%s(%d) initial value = %v, want null", tc.name, tc.index, got)
		}
	}
	if got := tableTestCallI32(t, in, "size0"); got != 3 {
		t.Fatalf("size0 = %d, want 3", got)
	}
	if got := tableTestCallI32(t, in, "size2"); got != 1 {
		t.Fatalf("size2 = %d, want 1", got)
	}
	if got := tableTestCallI32(t, in, "fun1-null"); got != 0 {
		t.Fatalf("fun1-null = %d, want initialized non-null funcref", got)
	}

	refA := issueExternref(t, rt, "table-a")
	refB := issueExternref(t, rt, "table-b")
	if _, err := in.Call(context.Background(), "set0", ValueI32(1), ValueExternRef(refA)); err != nil {
		t.Fatalf("set0: %v", err)
	}
	if got := callRef("get0", 1); got != refA {
		t.Fatalf("get0(1) = %v, want same-store identity %v", got, refA)
	}
	if _, err := in.Call(context.Background(), "set2", ValueI32(0), ValueExternRef(refA)); err != nil {
		t.Fatalf("set2: %v", err)
	}
	if got := callRef("get2", 0); got != refA {
		t.Fatalf("get2(0) = %v, want same-store identity %v", got, refA)
	}
	out, err := in.Call(context.Background(), "grow2", ValueExternRef(refB), ValueI32(2))
	if err != nil || len(out) != 1 || out[0].I32() != 1 {
		t.Fatalf("grow2(refB, 2) = %v, %v; want old size 1", out, err)
	}
	if got := tableTestCallI32(t, in, "size2"); got != 3 {
		t.Fatalf("size2 after grow = %d, want 3", got)
	}
	for _, index := range []int32{1, 2} {
		if got := callRef("get2", index); got != refB {
			t.Fatalf("get2(%d) after grow = %v, want %v", index, got, refB)
		}
	}
	if _, err := in.Call(context.Background(), "fill0", ValueI32(0), ValueExternRef(refB), ValueI32(2)); err != nil {
		t.Fatalf("fill0: %v", err)
	}
	for _, index := range []int32{0, 1} {
		if got := callRef("get0", index); got != refB {
			t.Fatalf("get0(%d) after fill = %v, want %v", index, got, refB)
		}
	}
	if _, err := in.Call(context.Background(), "fill0", ValueI32(3), ValueExternRef(refA), ValueI32(0)); err != nil {
		t.Fatalf("zero-length fill at end: %v", err)
	}
	out, err = in.Call(context.Background(), "grow0", ValueExternRef(refA), ValueI32(0))
	if err != nil || len(out) != 1 || out[0].I32() != 3 {
		t.Fatalf("zero grow0 = %v, %v; want old size 3", out, err)
	}
	out, err = in.Call(context.Background(), "grow0", ValueExternRef(refA), ValueI32(1))
	if err != nil || len(out) != 1 || out[0].I32() != -1 {
		t.Fatalf("over-max grow0 = %v, %v; want -1", out, err)
	}

	for _, tc := range []struct {
		name string
		args []Value
	}{
		{"get0", []Value{ValueI32(3)}},
		{"get2", []Value{ValueI32(-1)}},
		{"set0", []Value{ValueI32(3), ValueExternRef(NullExternRef())}},
		{"fill2", []Value{ValueI32(2), ValueExternRef(refA), ValueI32(2)}},
	} {
		if _, err := in.Call(context.Background(), tc.name, tc.args...); err == nil {
			t.Fatalf("%s%v unexpectedly succeeded", tc.name, tc.args)
		}
	}
	if got := callRef("get2", 2); got != refB {
		t.Fatalf("trapped fill mutated table: get2(2) = %v, want %v", got, refB)
	}

	if got, want := len(in.tableDescriptor(0)), 8+3*8; got != want {
		t.Fatalf("externref table 0 descriptor = %d bytes, want %d", got, want)
	}
	if got, want := len(in.tableDescriptor(1)), 8+coreruntime.TableEntryBytes; got != want {
		t.Fatalf("funcref table 1 descriptor = %d bytes, want %d", got, want)
	}
	if got, want := len(in.tableDescriptor(2)), 8+1024*8; got != want {
		t.Fatalf("growth-capable externref table 2 descriptor = %d bytes, want bounded %d", got, want)
	}
}

func TestExternrefOnlyTableUsesEightByteEntriesWithoutFuncrefArena(t *testing.T) {
	c, err := Compile(nil, watToWasm(t, `(module
		(table 2 4 externref)
		(func (export "get") (param i32) (result externref) (table.get 0 (local.get 0)))
	)`))
	if err != nil {
		t.Fatalf("Compile externref-only table: %v", err)
	}
	defer c.Close()
	in, err := Instantiate(c)
	if err != nil {
		t.Fatalf("Instantiate externref-only table: %v", err)
	}
	defer in.Close()
	if got := len(in.funcRefDescs); got != 0 {
		t.Fatalf("externref-only table allocated %d funcref descriptor bytes, want zero", got)
	}
	if got, want := len(in.tableDescriptor(0)), 8+4*8; got != want {
		t.Fatalf("externref-only descriptor = %d bytes, want %d", got, want)
	}
}

func TestExternrefTableStructFootprintsRemainBounded(t *testing.T) {
	if got := unsafe.Sizeof(Compiled{}); got != 632 {
		t.Fatalf("Compiled size = %d, want 632 bytes", got)
	}
	if got := unsafe.Sizeof(tableDef{}); got != 40 {
		t.Fatalf("tableDef size = %d, want 40 bytes", got)
	}
	if got := unsafe.Sizeof(Instance{}); got != 776 {
		t.Fatalf("Instance size = %d, want 776 bytes", got)
	}
	if got := unsafe.Sizeof(Table{}); got != 64 {
		t.Fatalf("Table size = %d, want 64 bytes", got)
	}
}

func TestLocalExternrefTablesRespectFeatureStoreAndPersistenceBoundaries(t *testing.T) {
	wasmBytes := watToWasm(t, `(module
		(table 1 2 externref)
		(func (export "get") (param i32) (result externref) (table.get 0 (local.get 0)))
		(func (export "set") (param i32 externref) (table.set 0 (local.get 0) (local.get 1)))
	)`)
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureReferenceTypes, false)
	if _, err := Compile(cfg, wasmBytes); err == nil || !strings.Contains(err.Error(), "reference-types disabled") {
		t.Fatalf("Compile with reference types disabled error = %v, want feature gate", err)
	}

	compiled, err := Compile(nil, wasmBytes)
	if err != nil {
		t.Fatalf("Compile persistence fixture: %v", err)
	}
	defer compiled.Close()
	_ = roundTripCompiled(t, compiled)
	if _, err := Capture(compiled, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "tables") {
		t.Fatalf("Capture error = %v, want table snapshot rejection", err)
	}

	rtA, rtB := NewRuntime(), NewRuntime()
	foreign := issueExternref(t, rtA, "foreign-table")
	modB, err := rtB.Compile(wasmBytes)
	if err != nil {
		t.Fatalf("Runtime B Compile: %v", err)
	}
	inB, err := rtB.Instantiate(context.Background(), modB)
	if err != nil {
		t.Fatalf("Runtime B Instantiate: %v", err)
	}
	for name, ref := range map[string]ExternRef{
		"cross-runtime": foreign,
		"forged":        ValueOf(ValExternRef, ValueExternRef(foreign).Bits()^0x9e3779b97f4a7c15).ExternRef(),
	} {
		if _, err := inB.Call(context.Background(), "set", ValueI32(0), ValueExternRef(ref)); err == nil || !strings.Contains(err.Error(), "invalid externref token") {
			t.Fatalf("%s table.set error = %v, want invalid token before storage", name, err)
		}
	}
	out, err := inB.Call(context.Background(), "get", ValueI32(0))
	if err != nil || len(out) != 1 || !out[0].ExternRef().IsNull() {
		t.Fatalf("table after rejected stores = %v, %v; want null", out, err)
	}
	if err := rtA.Close(); err != nil {
		t.Fatalf("Runtime A Close: %v", err)
	}
	if _, ok := rtA.ExternRefValue(foreign); ok {
		t.Fatal("closed producer runtime retained cross-store externref")
	}
	local := issueExternref(t, rtB, "local-table-root")
	if _, err := inB.Call(context.Background(), "set", ValueI32(0), ValueExternRef(local)); err != nil {
		t.Fatalf("store local table root: %v", err)
	}
	if err := rtB.Close(); err != nil {
		t.Fatalf("Runtime B Close with live instance: %v", err)
	}
	if value, ok := inB.ExternRefValue(local); !ok || value != "local-table-root" {
		t.Fatalf("live table root after Runtime.Close = %#v, %v", value, ok)
	}
	if err := inB.Close(); err != nil {
		t.Fatalf("Runtime B instance Close: %v", err)
	}
	if _, ok := rtB.ExternRefValue(local); ok {
		t.Fatal("last instance close retained externref table root")
	}
}

func TestRelease2ExternrefTableSourceGuard(t *testing.T) {
	sites := map[string][]string{
		"ref_is_null.wast": {
			`(table $t1 2 funcref)`,
			`(table $t2 2 externref)`,
			`(table.set $t2 (i32.const 1) (local.get $r))`,
			`(call $f2 (table.get $t2 (local.get $x)))`,
		},
		"table_get.wast": {
			`(table $t2 2 externref)`,
			`(table $t3 3 funcref)`,
			`(table.get (local.get $i))`,
			`(assert_return (invoke "get-externref" (i32.const 1)) (ref.extern 1))`,
		},
		"table_set.wast": {
			`(table $t2 1 externref)`,
			`(table $t3 2 funcref)`,
			`(table.set (local.get $i) (local.get $r))`,
			`(assert_return (invoke "set-externref" (i32.const 0) (ref.null extern)))`,
		},
		"table_size.wast": {
			`(table $t0 0 externref)`,
			`(table $t1 1 externref)`,
			`(table $t2 0 2 externref)`,
			`(table $t3 3 8 externref)`,
			`(func (export "size-t3") (result i32) (table.size $t3))`,
		},
		"table_grow.wast": {
			`(func (export "grow") (param $sz i32) (param $init externref) (result i32)`,
			`(table.grow $t (local.get $init) (local.get $sz))`,
			`(assert_return (invoke "grow-abbrev" (i32.const 4) (ref.extern 3)) (i32.const 1))`,
			`(assert_return (invoke "grow" (i32.const 800)) (i32.const 3))`,
		},
		"table_fill.wast": {
			`(table $t 10 externref)`,
			`(table.fill $t (local.get $i) (local.get $r) (local.get $n))`,
			`(table.fill (local.get $i) (local.get $r) (local.get $n))`,
			`(invoke "fill" (i32.const 8) (ref.extern 6) (i32.const 3))`,
		},
	}
	for file, snippets := range sites {
		raw, err := os.ReadFile(filepath.Clean("../../tests/spec-v2/test/core/" + file))
		if err != nil {
			t.Skipf("Release 2 %s unavailable: %v", file, err)
		}
		for _, snippet := range snippets {
			if !strings.Contains(string(raw), snippet) {
				t.Fatalf("%s no longer contains pinned externref-table site %q", file, snippet)
			}
		}
	}
}

//go:build linux && amd64 && !tinygo && !wago_target_tinygo && !wago_guardpage

package wago

import (
	"context"
	"encoding/binary"
	"testing"
)

func TestExternStoreOrdinaryPublicBulkGlobalInit(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	c, e := rt.Compile(watToWasm(t, `(module
 (table (export "t") 5 7 externref)
 (global $g (mut externref) (ref.null extern))
 (elem $e externref (ref.null extern) (ref.null extern))
 (func (export "set") (param i32 externref) (table.set (local.get 0) (local.get 1)))
 (func (export "copy") (param i32 i32 i32) (table.copy (local.get 0) (local.get 1) (local.get 2)))
 (func (export "fill") (param i32 externref i32) (table.fill (local.get 0) (local.get 1) (local.get 2)))
 (func (export "init") (param i32 i32 i32) (table.init $e (local.get 0) (local.get 1) (local.get 2)))
 (func (export "drop") (elem.drop $e))
 (func (export "grow") (param externref i32) (result i32) (table.grow (local.get 0) (local.get 1)))
 (func (export "global") (param externref) (result externref) (global.set $g (local.get 0)) (global.get $g))
 )`))
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	in, e := rt.Instantiate(context.Background(), c)
	if e != nil {
		t.Fatal(e)
	}
	defer in.Close()
	refs := []ExternRef{issueExternref(t, rt, "A"), issueExternref(t, rt, "B"), issueExternref(t, rt, "C"), issueExternref(t, rt, "D"), issueExternref(t, rt, "E")}
	call := func(n string, a ...Value) []Value {
		t.Helper()
		v, e := in.InvokeValues(context.Background(), n, a...)
		if e != nil {
			t.Fatal(n, e)
		}
		return v
	}
	desc, _ := in.conversionTableStorage(0)
	check := func(want ...uint64) {
		t.Helper()
		for i, w := range want {
			if got := binary.LittleEndian.Uint64(desc[8+i*8:]); got != w {
				t.Fatalf("slot %d: %x want %x", i, got, w)
			}
		}
	}
	seed := func() {
		for i, r := range refs {
			call("set", ValueI32(int32(i)), ValueExternRef(r))
		}
	}
	// Public SetValue creates an empty physical container even without conversions.
	table, e := in.ExportedTable("t")
	if e != nil {
		t.Fatal(e)
	}
	if e = table.SetValue(0, ValueExternRef(refs[0])); e != nil {
		t.Fatal(e)
	}
	seed()
	call("copy", ValueI32(1), ValueI32(0), ValueI32(4))
	check(refs[0].token, refs[0].token, refs[1].token, refs[2].token, refs[3].token)
	seed()
	call("copy", ValueI32(0), ValueI32(1), ValueI32(4))
	check(refs[1].token, refs[2].token, refs[3].token, refs[4].token, refs[4].token)
	call("init", ValueI32(1), ValueI32(0), ValueI32(2))
	check(refs[1].token, 0, 0, refs[4].token, refs[4].token)
	call("drop")
	call("init", ValueI32(5), ValueI32(0), ValueI32(0))
	if _, e = in.InvokeValues(context.Background(), "init", ValueI32(0), ValueI32(0), ValueI32(1)); e == nil {
		t.Fatal("dropped nonzero init accepted")
	}
	if _, e = in.InvokeValues(context.Background(), "fill", ValueI32(4), ValueExternRef(refs[0]), ValueI32(2)); e == nil {
		t.Fatal("OOB fill accepted")
	}
	check(refs[1].token, 0, 0, refs[4].token, refs[4].token)
	if v := call("grow", ValueExternRef(refs[0]), ValueI32(3)); uint32(v[0].Bits()) != ^uint32(0) {
		t.Fatal("grow must fail")
	}
	if v := call("grow", ValueExternRef(refs[0]), ValueI32(2)); v[0].Bits() != 5 {
		t.Fatal("grow result", v)
	}
	check(refs[1].token, 0, 0, refs[4].token, refs[4].token, refs[0].token, refs[0].token)
	if v := call("global", ValueExternRef(refs[2])); v[0].Bits() != refs[2].token {
		t.Fatal("global identity")
	}
}

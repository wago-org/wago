//go:build linux && amd64 && !tinygo && !wago_target_tinygo && !wago_guardpage

package wago

import (
	"context"
	"testing"
)

func TestPublicInactiveSettersPreservePhysicalAliases(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	c, e := rt.Compile(watToWasm(t, `(module (table (export "t") 1 externref) (global (export "g") (mut externref) (ref.null extern)))`))
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	in, e := rt.Instantiate(context.Background(), c)
	if e != nil {
		t.Fatal(e)
	}
	defer in.Close()
	table, e := in.ExportedTable("t")
	if e != nil {
		t.Fatal(e)
	}
	g, e := in.ExportedGlobalObject("g")
	if e != nil {
		t.Fatal(e)
	}
	ac, e := rt.Compile(watToWasm(t, `(module (import "env" "t" (table 1 externref)) (import "env" "g" (global (mut externref))) (export "t" (table 0)) (export "g" (global 0)))`))
	if e != nil {
		t.Fatal(e)
	}
	defer ac.Close()
	alias, e := rt.Instantiate(context.Background(), ac, WithImports(NewImports().Table("env", "t", table).Global("env", "g", g)))
	if e != nil {
		t.Fatal(e)
	}
	defer alias.Close()
	at, e := alias.ExportedTable("t")
	if e != nil {
		t.Fatal(e)
	}
	ag, e := alias.ExportedGlobalObject("g")
	if e != nil {
		t.Fatal(e)
	}
	if at != table || ag != g {
		t.Fatal("reexport lost physical authority")
	}
	ref := issueExternref(t, rt, "live")
	if e = at.SetValue(0, ValueExternRef(ref)); e != nil {
		t.Fatal(e)
	}
	if e = ag.SetValue(ValueExternRef(ref)); e != nil {
		t.Fatal(e)
	}
	stale := issueExternref(t, rt, "released")
	if !rt.ReleaseExternRef(stale) {
		t.Fatal("release failed")
	}
	if e = at.SetValue(0, ValueExternRef(stale)); e == nil {
		t.Fatal("released table token accepted")
	}
	if e = ag.SetValue(ValueExternRef(stale)); e == nil {
		t.Fatal("released global token accepted")
	}
	if e = in.Close(); e != nil {
		t.Fatal(e)
	}
	if e = at.SetValue(0, ValueExternRef(ref)); e == nil {
		t.Fatal("closed physical table owner accepted")
	}
	if e = ag.SetValue(ValueExternRef(ref)); e == nil {
		t.Fatal("closed physical global owner accepted")
	}
	t.Log("reexport uses original authority; closed owner rejects while importer retains resources")
}

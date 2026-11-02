//go:build (!(linux || (darwin && arm64)) || tinygo || wago_target_tinygo) && !riscv64

package wago

import (
	"encoding/binary"
	"strings"
	"testing"
)

// Metadata-only fallback test: never construct a managed object on an
// unsupported target or infer lifetime safety from a successful public write.
func TestUnsupportedConversionPublicGlobalRejectsOpaqueBeforeDirectory(t *testing.T) {
	store := newReferenceStore(false)
	const public = uint64(0x1234567800000042)
	const internal = uint64(0x8765432100000042)
	conversion := &gcExternConversionState{store: store, entries: []gcExternConversionEntry{{kind: gcExternConversionData, publicExternWord: public, externWord: internal}}}
	in := &Instance{refStore: store}
	state := &instancePluginState{}
	state.gcRefTestTable.Store(&gcRefTestTableState{Conversion: conversion})
	in.pluginState.Store(state)
	cell := make([]byte, 8)
	binary.LittleEndian.PutUint64(cell, 99)
	g := &Global{Type: ValExternRef, Mutable: true, cell: cell, owner: &globalOwner{instance: in, store: store, typ: ValExternRef, mutable: true}}
	if _, ok := store.resolveExternref(public); ok {
		t.Fatal("fixture unexpectedly has ordinary directory identity")
	}
	handled, err := setConversionGlobalPublic(nil, g, Value{typ: ValExternRef, bits: public})
	if !handled || err == nil || !strings.Contains(err.Error(), "unsupported on this target") {
		t.Fatalf("opaque public ingress: handled=%t err=%v", handled, err)
	}
	if got := binary.LittleEndian.Uint64(cell); got != 99 {
		t.Fatalf("rejected publication changed cell to %d", got)
	}
	// SetGlobalValue authenticates in the accessing instance, which can differ
	// from the imported physical owner used by Global.SetValue.
	g.owner.instance = &Instance{refStore: store}
	if handled, err := setConversionGlobalPublic(in, g, Value{typ: ValExternRef, bits: public}); !handled || err == nil {
		t.Fatalf("access-instance conversion not rejected: handled=%t err=%v", handled, err)
	}
	if got := binary.LittleEndian.Uint64(cell); got != 99 {
		t.Fatal("rejected imported publication changed the cell")
	}
	g.owner.instance = in
	if err := unsupportedConversionWord(store, internal); err == nil {
		t.Fatal("internal opaque identity accepted")
	}
	token, err := store.issueExternref("ordinary")
	if err != nil {
		t.Fatal(err)
	}
	for _, word := range []uint64{0, token} {
		handled, err = setConversionGlobalPublic(nil, g, Value{typ: ValExternRef, bits: word})
		if handled || err != nil {
			t.Fatalf("ordinary/null ingress was not left to normal validation: %t %v", handled, err)
		}
		if err := unsupportedConversionWord(store, word); err != nil {
			t.Fatal(err)
		}
	}
	if handled, err := setConversionGlobalPublic(nil, g, Value{typ: ValExternRef, bits: public + 1}); handled || err != nil {
		t.Fatalf("invalid token no longer reaches ordinary invalid-token validation: %t %v", handled, err)
	}
	store.releaseExternref(token)
}

func TestUnsupportedConversionGlobalValidationOrdering(t *testing.T) {
	store := newReferenceStore(false)
	other := newReferenceStore(false)
	ordinary, err := store.issueExternref("ordinary")
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := other.issueExternref("foreign")
	if err != nil {
		t.Fatal(err)
	}
	defer store.releaseExternref(ordinary)
	defer other.releaseExternref(foreign)
	const opaque = uint64(0x1234567800000042)
	for _, tc := range []struct {
		name              string
		bits              uint64
		immutable, closed bool
		want              string
	}{
		{name: "ordinary", bits: ordinary}, {name: "null"},
		{name: "invalid", bits: opaque, want: "invalid externref token"},
		{name: "cross-store", bits: foreign, want: "invalid externref token"},
		{name: "immutable-before-invalid", bits: opaque, immutable: true, want: "immutable"},
		{name: "immutable-before-null", immutable: true, want: "immutable"},
		{name: "closed-before-invalid", bits: opaque, closed: true, want: "metadata is invalid"},
		{name: "closed-before-null", closed: true, want: "metadata is invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cell := make([]byte, 8)
			binary.LittleEndian.PutUint64(cell, 99)
			g := &Global{Type: ValExternRef, Mutable: !tc.immutable, cell: cell, owner: &globalOwner{store: store, typ: ValExternRef, mutable: !tc.immutable, closed: tc.closed}}
			err := g.SetValue(Value{typ: ValExternRef, bits: tc.bits})
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				if binary.LittleEndian.Uint64(cell) != tc.bits {
					t.Fatal("ordinary/null write missing")
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("error = %v, want %q", err, tc.want)
				}
				if binary.LittleEndian.Uint64(cell) != 99 {
					t.Fatal("rejection changed storage")
				}
			}
		})
	}
	// A known opaque entry must not supersede immutable or closed diagnostics.
	in := &Instance{refStore: store}
	plugin := &instancePluginState{}
	plugin.gcRefTestTable.Store(&gcRefTestTableState{Conversion: &gcExternConversionState{store: store, entries: []gcExternConversionEntry{{kind: gcExternConversionData, publicExternWord: opaque}}}})
	in.pluginState.Store(plugin)
	g := &Global{Type: ValExternRef, Mutable: false, cell: make([]byte, 8), owner: &globalOwner{instance: in, store: store, typ: ValExternRef, mutable: false}}
	if handled, err := setConversionGlobalPublic(nil, g, Value{typ: ValExternRef, bits: opaque}); handled || err != nil {
		t.Fatalf("immutable opaque precedence: %t %v", handled, err)
	}
	// Exercise the public closed-instance error without entering a fake engine.
	g.Mutable = true
	g.owner.mutable = true
	in.invocationState.Store(instanceInvocationClosed)
	if err := g.SetValue(Value{typ: ValExternRef, bits: opaque}); err == nil || !strings.Contains(err.Error(), "owner instance is closed") {
		t.Fatalf("closed opaque precedence: %v", err)
	}
	if handled, err := setConversionGlobalPublic(in, g, Value{typ: ValExternRef, bits: opaque}); handled || err != nil {
		t.Fatalf("closed accessing-instance precedence: %t %v", handled, err)
	}
}

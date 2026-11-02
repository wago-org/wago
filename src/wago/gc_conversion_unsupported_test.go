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

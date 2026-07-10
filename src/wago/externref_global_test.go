package wago

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestNullableLocalExternrefGlobals(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	mod, err := rt.Compile(nullableLocalExternrefGlobalsModule())
	if err != nil {
		t.Fatalf("Compile nullable local externref globals: %v", err)
	}
	for _, name := range []string{"immutable", "mutable"} {
		g, ok := mod.c.ExportedGlobal(name)
		if !ok || g.Type != ValExternRef || (name == "mutable") != g.Mutable {
			t.Fatalf("ExportedGlobal(%q) = %+v, %v", name, g, ok)
		}
	}
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatalf("Instantiate nullable local externref globals: %v", err)
	}
	defer in.Close()

	for _, name := range []string{"get_immutable", "get_mutable"} {
		out, err := in.Call(context.Background(), name)
		if err != nil || len(out) != 1 || out[0].Type() != ValExternRef || !out[0].ExternRef().IsNull() {
			t.Fatalf("Call %s = %v, %v; want one null externref", name, out, err)
		}
	}
	ref := issueExternref(t, in, "local-global")
	out, err := in.Call(context.Background(), "set_and_get", ValueExternRef(ref))
	if err != nil || len(out) != 1 || out[0].ExternRef() != ref {
		t.Fatalf("set_and_get(ref) = %v, %v; want stable externref", out, err)
	}
	if err := in.SetGlobalValue("mutable", ValueExternRef(ref)); err != nil {
		t.Fatalf("SetGlobalValue(mutable, ref): %v", err)
	}
	got, err := in.GlobalValue("mutable")
	if err != nil || got.Type() != ValExternRef || got.ExternRef() != ref {
		t.Fatalf("GlobalValue(mutable) = %v, %v; want stable externref", got, err)
	}
	if _, err := in.Global("mutable"); err == nil || !strings.Contains(err.Error(), "reference") {
		t.Fatalf("Global(mutable) error = %v, want raw reference-global rejection", err)
	}
	if err := in.SetGlobal("mutable", ValueExternRef(ref).Bits()); err == nil || !strings.Contains(err.Error(), "reference") {
		t.Fatalf("SetGlobal(mutable, token) error = %v, want raw reference-global rejection", err)
	}
	raw, err := in.ExportedGlobalObject("mutable")
	if err != nil {
		t.Fatalf("ExportedGlobalObject(mutable): %v", err)
	}
	if bits := raw.Get(); bits != 0 {
		t.Fatalf("reference Global.Get() = %#x, want fail-closed zero", bits)
	}
	if err := raw.Set(ValueExternRef(ref).Bits()); err == nil || !strings.Contains(err.Error(), "reference type") {
		t.Fatalf("reference Global.Set(token) error = %v, want typed-accessor rejection", err)
	}
	for i, g := range in.globalCells {
		if len(g.cell) != 8 {
			t.Fatalf("externref global %d cell = %d bytes, want 8", i, len(g.cell))
		}
	}
}

func TestLocalExternrefGlobalsRespectFeatureStoreAndLifetimeBoundaries(t *testing.T) {
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureReferenceTypes, false)
	if _, err := Compile(cfg, nullableLocalExternrefGlobalsModule()); err == nil || !strings.Contains(err.Error(), "reference-types disabled") {
		t.Fatalf("Compile with reference types disabled error = %v, want reference-types gate", err)
	}
	imported := wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "ref", wasm.ExternRef, false))))
	if _, err := Compile(nil, imported); err == nil || !strings.Contains(err.Error(), "imported global type externref") {
		t.Fatalf("Compile imported externref global error = %v, want shared-owner rejection", err)
	}

	compiled, err := Compile(nil, nullableLocalExternrefGlobalsModule())
	if err != nil {
		t.Fatalf("Compile globals: %v", err)
	}
	defer compiled.Close()
	rtA, rtB := NewRuntime(), NewRuntime()
	defer rtB.Close()
	foreign := issueExternref(t, rtA, "foreign")
	in, err := instantiateCore(compiled, InstantiateOptions{store: rtB.refStore})
	if err != nil {
		t.Fatalf("Instantiate consumer: %v", err)
	}
	defer in.Close()
	for name, ref := range map[string]ExternRef{
		"cross-runtime": foreign,
		"forged":        ValueOf(ValExternRef, ValueExternRef(foreign).Bits()^0x9e3779b97f4a7c15).ExternRef(),
	} {
		t.Run(name, func(t *testing.T) {
			if err := in.SetGlobalValue("mutable", ValueExternRef(ref)); err == nil || !strings.Contains(err.Error(), "invalid externref token") {
				t.Fatalf("SetGlobalValue = %v, want invalid externref token", err)
			}
			got, err := in.GlobalValue("mutable")
			if err != nil || !got.ExternRef().IsNull() {
				t.Fatalf("global after rejected store = %v, %v; want null", got, err)
			}
		})
	}
	if err := rtA.Close(); err != nil {
		t.Fatalf("Runtime.Close producer: %v", err)
	}

	rt := NewRuntime()
	mod, err := rt.Compile(nullableLocalExternrefGlobalsModule())
	if err != nil {
		t.Fatalf("Runtime Compile: %v", err)
	}
	live, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatalf("Runtime Instantiate: %v", err)
	}
	root := issueExternref(t, rt, "retained")
	if err := live.SetGlobalValue("mutable", ValueExternRef(root)); err != nil {
		t.Fatalf("store retained root: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Runtime.Close with live instance: %v", err)
	}
	if value, ok := live.ExternRefValue(root); !ok || value != "retained" {
		t.Fatalf("live global root after Runtime.Close = %#v, %v", value, ok)
	}
	if err := live.Close(); err != nil {
		t.Fatalf("close live instance: %v", err)
	}
	if _, ok := rt.ExternRefValue(root); ok {
		t.Fatal("last instance close retained runtime externref root")
	}

	private, err := Instantiate(compiled)
	if err != nil {
		t.Fatalf("Instantiate private: %v", err)
	}
	privateRef := issueExternref(t, private, "private")
	if err := private.SetGlobalValue("mutable", ValueExternRef(privateRef)); err != nil {
		t.Fatalf("private SetGlobalValue: %v", err)
	}
	if err := private.Close(); err != nil {
		t.Fatalf("private Close: %v", err)
	}
	if _, ok := private.ExternRefValue(privateRef); ok {
		t.Fatal("closed private instance retained externref root")
	}
}

func TestLocalExternrefGlobalsRemainOutOfSerializedState(t *testing.T) {
	c, err := Compile(nil, nullableLocalExternrefGlobalsModule())
	if err != nil {
		t.Fatalf("Compile nullable local externref globals: %v", err)
	}
	defer c.Close()
	if _, err := c.MarshalBinary(); err == nil || !strings.Contains(err.Error(), "reference global metadata") {
		t.Fatalf("MarshalBinary error = %v, want reference-global rejection", err)
	}
	if _, err := Capture(c, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "reference global metadata") {
		t.Fatalf("Capture error = %v, want reference-global rejection", err)
	}
	if _, err := Instantiate(&Compiled{Globals: []GlobalDef{{Type: ValExternRef, Bits: 1}}}); err == nil || !strings.Contains(err.Error(), "non-null externref global initializer") {
		t.Fatalf("Instantiate forged externref metadata error = %v", err)
	}
}

func TestRelease2ExternrefGlobalSourceGuard(t *testing.T) {
	sites := []struct {
		file string
		text []string
	}{
		{"global.wast", []string{
			`(global $r externref (ref.null extern))`,
			`(global $mr (mut externref) (ref.null extern))`,
			`(func (export "get-r") (result externref) (global.get $r))`,
			`(func (export "get-mr") (result externref) (global.get $mr))`,
			`(func (export "set-mr") (param externref) (global.set $mr (local.get 0)))`,
			`(assert_return (invoke "get-r") (ref.null extern))`,
			`(assert_return (invoke "get-mr") (ref.null extern))`,
			`(assert_return (invoke "set-mr" (ref.extern 10)))`,
			`(assert_return (invoke "get-mr") (ref.extern 10))`,
		}},
		{"ref_null.wast", []string{`(global externref (ref.null extern))`}},
		{"linking.wast", []string{
			`(global (export "g-const-extern") externref (ref.null extern))`,
			`(global (export "g-var-extern") (mut externref) (ref.null extern))`,
		}},
	}
	for _, site := range sites {
		raw, err := os.ReadFile(filepath.Join("../../tests/spec-v2/test/core", site.file))
		if err != nil {
			t.Skipf("Release 2 fixture unavailable: %v", err)
		}
		for _, text := range site.text {
			if !strings.Contains(string(raw), text) {
				t.Fatalf("%s no longer contains pinned externref-global site %q", site.file, text)
			}
		}
	}
}

func nullableLocalExternrefGlobalsModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.ExternRef}),
			wasmtest.FuncType([]wasm.ValType{wasm.ExternRef}, []wasm.ValType{wasm.ExternRef}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(6, wasmtest.Vec(
			wasmtest.GlobalEntry(wasm.ExternRef, false, []byte{0xd0, 0x6f, 0x0b}),
			wasmtest.GlobalEntry(wasm.ExternRef, true, []byte{0xd0, 0x6f, 0x0b}),
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("get_immutable", 0, 0),
			wasmtest.ExportEntry("get_mutable", 0, 1),
			wasmtest.ExportEntry("set_and_get", 0, 2),
			wasmtest.ExportEntry("immutable", 3, 0),
			wasmtest.ExportEntry("mutable", 3, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x23, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x23, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x24, 0x01, 0x23, 0x01, 0x0b}),
		)),
	)
}

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

func TestRelease2NullableLocalFuncrefGlobals(t *testing.T) {
	c, err := Compile(nil, nullableLocalFuncrefGlobalsModule())
	if err != nil {
		t.Fatalf("Compile nullable local funcref globals: %v", err)
	}
	defer c.Close()

	for _, name := range []string{"immutable", "mutable"} {
		g, ok := c.ExportedGlobal(name)
		if !ok || g.Type != ValFuncRef || (name == "mutable") != g.Mutable {
			t.Fatalf("ExportedGlobal(%q) = %+v, %v", name, g, ok)
		}
	}

	in, err := Instantiate(c)
	if err != nil {
		t.Fatalf("Instantiate nullable local funcref globals: %v", err)
	}
	defer in.Close()

	for _, name := range []string{"get_immutable", "get_mutable"} {
		out, err := in.Call(context.Background(), name)
		if err != nil {
			t.Fatalf("Call %s: %v", name, err)
		}
		if len(out) != 1 || out[0].Type() != ValFuncRef || !out[0].FuncRef().IsNull() {
			t.Fatalf("Call %s = %v, want one null funcref", name, out)
		}
	}

	out, err := in.Call(context.Background(), "set_and_get", ValueFuncRef(NullFuncRef()))
	if err != nil {
		t.Fatalf("Call set_and_get(null): %v", err)
	}
	if len(out) != 1 || out[0].Type() != ValFuncRef || !out[0].FuncRef().IsNull() {
		t.Fatalf("Call set_and_get(null) = %v, want one null funcref", out)
	}

	v, err := in.GlobalValue("mutable")
	if err != nil || v.Type() != ValFuncRef || !v.FuncRef().IsNull() {
		t.Fatalf("GlobalValue(mutable) = %v, %v; want null funcref", v, err)
	}
	if err := in.SetGlobalValue("mutable", ValueFuncRef(NullFuncRef())); err != nil {
		t.Fatalf("SetGlobalValue(mutable, null): %v", err)
	}
	if _, err := in.Global("mutable"); err == nil || !strings.Contains(err.Error(), "reference") {
		t.Fatalf("Global(mutable) error = %v, want raw reference-global rejection", err)
	}
	if err := in.SetGlobal("mutable", 1); err == nil || !strings.Contains(err.Error(), "reference") {
		t.Fatalf("SetGlobal(mutable, forged) error = %v, want raw reference-global rejection", err)
	}
	if err := in.SetGlobalValue("mutable", ValueOf(ValFuncRef, 1)); err == nil || !strings.Contains(err.Error(), "invalid funcref token") {
		t.Fatalf("SetGlobalValue(mutable, forged) error = %v, want token rejection", err)
	}
}

func TestNullableLocalFuncrefGlobalsRespectFeatureAndOwnershipBoundaries(t *testing.T) {
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureReferenceTypes, false)
	if _, err := Compile(cfg, nullableLocalFuncrefGlobalsModule()); err == nil || !strings.Contains(err.Error(), "reference-types disabled") {
		t.Fatalf("Compile with reference types disabled error = %v, want reference-types gate", err)
	}

	imported := wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "ref", wasm.FuncRef, false))))
	if _, err := Compile(nil, imported); err == nil || !strings.Contains(err.Error(), "imported global type funcref") {
		t.Fatalf("Compile imported funcref global error = %v, want explicit ownership-boundary rejection", err)
	}
}

func TestNullableLocalFuncrefGlobalsRemainOutOfSerializedState(t *testing.T) {
	c, err := Compile(nil, nullableLocalFuncrefGlobalsModule())
	if err != nil {
		t.Fatalf("Compile nullable local funcref globals: %v", err)
	}
	defer c.Close()

	if _, err := c.MarshalBinary(); err == nil || !strings.Contains(err.Error(), "reference global metadata") {
		t.Fatalf("MarshalBinary error = %v, want reference-global rejection", err)
	}
	if _, err := Capture(c, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "reference global metadata") {
		t.Fatalf("Capture error = %v, want reference-global rejection", err)
	}
}

func TestRelease2NullableFuncrefGlobalSourceGuard(t *testing.T) {
	path := filepath.Clean("../../tests/spec-v2/test/core/linking.wast")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("Release 2 linking fixture unavailable: %v", err)
	}
	text := string(b)
	for _, declaration := range []string{
		`(global (export "g-const-func") funcref (ref.null func))`,
		`(global (export "g-var-func") (mut funcref) (ref.null func))`,
	} {
		if !strings.Contains(text, declaration) {
			t.Fatalf("Release 2 linking fixture no longer contains %q", declaration)
		}
	}
}

// nullableLocalFuncrefGlobalsModule isolates the funcref half of the official
// Release 2 linking.wast lines 97-98 declarations and adds function-level
// global.get/global.set execution. Imported globals, externref globals, and
// non-null ref.func initializers remain separate ownership slices.
func nullableLocalFuncrefGlobalsModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.FuncRef}),
			wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, []wasm.ValType{wasm.FuncRef}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(6, wasmtest.Vec(
			wasmtest.GlobalEntry(wasm.FuncRef, false, []byte{0xd0, 0x70, 0x0b}),
			wasmtest.GlobalEntry(wasm.FuncRef, true, []byte{0xd0, 0x70, 0x0b}),
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

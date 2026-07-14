package wago

import (
	"context"
	"errors"
	"testing"

	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestInstanceCallTyped(t *testing.T) {
	rt := NewRuntime()
	if err := rt.Use(tripleExt{}); err != nil {
		t.Fatalf("use: %v", err)
	}
	mod := callsEnvF(t, rt)
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()

	out, err := in.Call(context.Background(), "g", ValueI32(7))
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if len(out) != 1 || out[0].Type() != ValI32 || out[0].I32() != 21 {
		t.Fatalf("g(7) = %v, want i32(21)", out)
	}

	// Wrong arg type is rejected.
	if _, err := in.Call(context.Background(), "g", ValueI64(7)); err == nil {
		t.Fatal("expected type-mismatch error for i64 arg to i32 param")
	}
	// Wrong arg count is rejected.
	if _, err := in.Call(context.Background(), "g"); err == nil {
		t.Fatal("expected arity error")
	}
}

func TestInstanceCallCanceledContext(t *testing.T) {
	rt := NewRuntime()
	if err := rt.Use(tripleExt{}); err != nil {
		t.Fatalf("use: %v", err)
	}
	in, err := rt.Instantiate(context.Background(), callsEnvF(t, rt))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := in.Call(ctx, "g", ValueI32(1)); !errors.Is(err, context.Canceled) {
		t.Fatalf("Call with canceled ctx = %v, want context.Canceled", err)
	}
}

func TestRuntimeModuleBindsCompiledArtifact(t *testing.T) {
	if _, err := (*Runtime)(nil).Module(nil); err == nil {
		t.Fatal("nil runtime and artifact accepted")
	}
	rt := NewRuntime()
	if _, err := rt.Module(nil); err == nil {
		t.Fatal("nil artifact accepted")
	}
	c, err := Compile(nil, wasmtest.Module())
	if err != nil {
		t.Fatal(err)
	}
	if mod, err := rt.Module(c); err != nil || mod == nil {
		t.Fatalf("Module = %p, %v", mod, err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Module(c); err == nil {
		t.Fatal("closed runtime accepted compiled artifact")
	}
}

func TestModuleInspection(t *testing.T) {
	rt := NewRuntime()
	if err := rt.Use(tripleExt{}); err != nil {
		t.Fatalf("use: %v", err)
	}
	mod := callsEnvF(t, rt)

	imps := mod.Imports()
	if len(imps) != 1 {
		t.Fatalf("Imports() = %+v, want 1", imps)
	}
	got := imps[0]
	if got.Module != "env" || got.Name != "f" || got.Kind != ImportFunc {
		t.Fatalf("import spec = %+v", got)
	}
	if !got.Provided {
		t.Fatal("import should be marked Provided")
	}
	if !got.HasCapability || got.Capability != CapMetricsWrite {
		t.Fatalf("import capability = %v (%v)", got.Capability, got.HasCapability)
	}
	if len(got.Params) != 1 || got.Params[0] != ValI32 || len(got.Results) != 1 || got.Results[0] != ValI32 {
		t.Fatalf("import signature = %v -> %v", got.Params, got.Results)
	}

	if caps := mod.RequiredCapabilities(); len(caps) != 1 || caps[0] != CapMetricsWrite {
		t.Fatalf("RequiredCapabilities() = %v", caps)
	}
	meta := mod.Metadata()
	if meta.FuncImportCount != 1 || len(meta.ExportedFuncs) != 1 || meta.ExportedFuncs[0] != "g" {
		t.Fatalf("metadata = %+v", meta)
	}
}

func TestInstantiateMissingImportHint(t *testing.T) {
	rt := NewRuntime() // no extension provides env.f
	mod := callsEnvF(t, rt)
	_, err := rt.Instantiate(context.Background(), mod)
	if !errors.Is(err, ErrMissingImport) {
		t.Fatalf("missing-import error = %v, want ErrMissingImport", err)
	}
}

// TestInstanceGlobalValueTyped exercises typed global get/set through an exported
// mutable i64 global.
func TestInstanceGlobalValueTyped(t *testing.T) {
	rt := NewRuntime()
	// module: one exported mutable i64 global = 5.
	// globaltype i64(0x7e) mutable(0x01); init: i64.const 5 (0x42 0x05), end (0x0b).
	glob := []byte{0x7e, 0x01, 0x42, 0x05, 0x0b}
	mod := wasmtest.Module(
		wasmtest.Section(6, wasmtest.Vec(glob)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("g", 3, 0))), // export kind 3 = global, index 0
	)
	m, err := rt.Compile(mod)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	in, err := rt.Instantiate(context.Background(), m)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()

	v, err := in.GlobalValue("g")
	if err != nil {
		t.Fatalf("GlobalValue: %v", err)
	}
	if v.Type() != ValI64 || v.I64() != 5 {
		t.Fatalf("global g = %v, want i64(5)", v)
	}
	if err := in.SetGlobalValue("g", ValueI64(42)); err != nil {
		t.Fatalf("SetGlobalValue: %v", err)
	}
	if v, _ := in.GlobalValue("g"); v.I64() != 42 {
		t.Fatalf("after set, g = %v, want i64(42)", v)
	}
	// Type mismatch is rejected.
	if err := in.SetGlobalValue("g", ValueI32(1)); err == nil {
		t.Fatal("expected type-mismatch error setting i64 global with i32")
	}
}

package wago

import (
	"context"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
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

func TestConstExpressionByteFormsAndInitializers(t *testing.T) {
	for _, tc := range []struct {
		body []byte
		want wasm.ValType
		bits uint64
	}{
		{[]byte{0x41, 0x7f, 0x0b}, wasm.I32, 0xffffffff},
		{[]byte{0x42, 0x7f, 0x0b}, wasm.I64, ^uint64(0)},
		{[]byte{0x43, 1, 2, 3, 4, 0x0b}, wasm.F32, 0x04030201},
		{[]byte{0x44, 1, 2, 3, 4, 5, 6, 7, 8, 0x0b}, wasm.F64, 0x0807060504030201},
		{[]byte{0xd0, 0x70, 0x0b}, wasm.FuncRef, 0},
		{[]byte{0xd0, 0x6f, 0x0b}, wasm.ExternRef, 0},
		{[]byte{0xd2, 0x03, 0x0b}, wasm.FuncRef, 0},
	} {
		res, err := evalConstExprBytes(tc.body, tc.want)
		if err != nil || res.bits != tc.bits {
			t.Fatalf("eval %x = %#v, %v", tc.body, res, err)
		}
	}
	v128 := append([]byte{0xfd, 0x0c}, append([]byte{1}, append(make([]byte, 15), 0x0b)...)...)
	if res, err := evalConstExprBytes(v128, wasm.V128); err != nil || res.v128[0] != 1 {
		t.Fatalf("v128 eval = %#v, %v", res, err)
	}
	for _, tc := range []struct {
		body []byte
		want wasm.ValType
	}{
		{[]byte{0xd0, 0x7f, 0x0b}, wasm.FuncRef},
		{[]byte{0xfd, 0x00, 0x0b}, wasm.V128},
		{[]byte{0x41, 0x00}, wasm.I32},
		{[]byte{0x41, 0x00, 0x00}, wasm.I32},
		{[]byte{0x41, 0x00, 0x0b, 0x00}, wasm.I32},
		{[]byte{0x41, 0x00, 0x0b}, wasm.I64},
	} {
		if _, err := evalConstExpr(tc.body, tc.want); err == nil {
			t.Fatalf("invalid const expression accepted: %x", tc.body)
		}
	}
	init := constExprInit{Bits: 7, GlobalIndex: 2, FuncIndex: 3}
	var g GlobalDef
	applyGlobalInit(&g, init)
	if g.Bits != 7 || !g.HasInitGlobal || g.InitGlobal != 2 || !g.HasInitFunc || g.InitFunc != 3 {
		t.Fatalf("global initializer = %#v", g)
	}
	var o OffsetInit
	applyOffsetInit(&o, init)
	if o.Base != 7 || !o.HasGlobal || o.Global != 2 {
		t.Fatalf("offset initializer = %#v", o)
	}
}

func TestConstExpressionModuleGlobalAndInstructionForms(t *testing.T) {
	m := &wasm.Module{Imports: []wasm.Import{{Type: wasm.ExternType{
		Kind:   wasm.ExternGlobal,
		Global: wasm.GlobalType{Type: wasm.I64},
	}}}}
	res, err := evalConstExprBytesWithModule([]byte{0x23, 0x00, 0x0b}, wasm.I64, m)
	if err != nil || res.GlobalIndex != 0 {
		t.Fatalf("imported global expression = %#v, %v", res, err)
	}
	for _, bad := range []*wasm.Module{
		nil,
		{Imports: []wasm.Import{{Type: wasm.ExternType{Kind: wasm.ExternGlobal, Global: wasm.GlobalType{Type: wasm.I64, Mutable: true}}}}},
		{Globals: []wasm.Global{{Type: wasm.GlobalType{Type: wasm.I64}}}},
	} {
		if _, err := evalConstExprBytesWithModule([]byte{0x23, 0x00, 0x0b}, wasm.I64, bad); err == nil {
			t.Fatal("invalid global.get constant expression accepted")
		}
	}
	for _, tc := range []struct {
		in   wasm.Instruction
		want wasm.ValType
		bits uint64
	}{
		{wasm.Instruction{Kind: wasm.InstrI32Const, I32: -2}, wasm.I32, 0xfffffffe},
		{wasm.Instruction{Kind: wasm.InstrI64Const, I64: -3}, wasm.I64, ^uint64(2)},
		{wasm.Instruction{Kind: wasm.InstrF32Const, F32Bits: 7}, wasm.F32, 7},
		{wasm.Instruction{Kind: wasm.InstrF64Const, F64Bits: 8}, wasm.F64, 8},
		{wasm.Instruction{Kind: wasm.InstrRefFunc, Index: 4}, wasm.FuncRef, 0},
		{wasm.Instruction{Kind: wasm.InstrV128Const}, wasm.V128, 0},
	} {
		got, err := evalConstExprWithModule(wasm.Expr{Instrs: []wasm.Instruction{tc.in}}, tc.want, m)
		if err != nil || got.bits != tc.bits {
			t.Fatalf("instruction expression %#v = %#v, %v", tc.in, got, err)
		}
	}
	got, err := evalConstExprWithModule(wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrGlobalGet}}}, wasm.I64, m)
	if err != nil || got.GlobalIndex != 0 {
		t.Fatalf("instruction global.get = %#v, %v", got, err)
	}
	for _, expr := range []wasm.Expr{{}, {Instrs: []wasm.Instruction{{Kind: wasm.InstrI32Const}, {Kind: wasm.InstrI32Const}}}, {Instrs: []wasm.Instruction{{Kind: wasm.InstrGlobalGet}}}} {
		if _, err := evalConstExprWithModule(expr, wasm.I32, nil); err == nil {
			t.Fatalf("invalid instruction expression accepted: %#v", expr)
		}
	}
}

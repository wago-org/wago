package wago

import (
	"testing"
)

// TestFacadeForwards exercises the generated public facade.  The implementation
// package has exhaustive behavioural tests; this test ensures each exported
// forwarding helper remains wired to it when the facade is regenerated.
func TestFacadeForwards(t *testing.T) {
	const wasm = "\x00asm\x01\x00\x00\x00"

	if AsF32(F32(1.5)) != 1.5 || AsF64(F64(2.5)) != 2.5 || AsI32(I32(-3)) != -3 || AsI64(I64(-4)) != -4 {
		t.Fatal("scalar facade conversion mismatch")
	}
	_ = CapabilityDocs("test")
	_ = DirsFor("test")
	_ = GuardPageSupported()

	c, err := Compile([]byte(wasm))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	defer c.Close()
	if _, err := CompileWithConfig(NewRuntimeConfig(), []byte(wasm)); err != nil {
		t.Fatalf("CompileWithConfig: %v", err)
	}
	if MustCompile([]byte(wasm)) == nil {
		t.Fatal("MustCompile returned nil")
	}
	if _, err := Instantiate(c); err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	encoded, err := c.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	if !IsCompiled(encoded) {
		t.Fatal("IsCompiled false for encoded module")
	}
	if _, err := Load(encoded); err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, g := range []*Global{
		NewGlobalF32(1, true), NewGlobalF64(1, true), NewGlobalI32(1, true), NewGlobalI64(1, true), NewGlobalV128(V128{}, true),
	} {
		if err := g.Close(); err != nil {
			t.Fatalf("close global: %v", err)
		}
	}
	if m, err := NewMemory(0, 1); err != nil {
		t.Fatalf("NewMemory: %v", err)
	} else {
		_ = m.Close()
	}
	if m, err := NewSharedMemory(0, 1); err != nil {
		t.Fatalf("NewSharedMemory: %v", err)
	} else {
		_ = m.Close()
	}
	if table, err := NewTable(0, 1); err != nil {
		t.Fatalf("NewTable: %v", err)
	} else {
		_ = table.Close()
	}
	_ = NewHandleTable()
	_ = NullExternRef()
	_ = NullFuncRef()
	_ = OptKnobs()
	_ = SupportedFeatures()
	_ = ValueExternRef(NullExternRef())
	_ = ValueFuncRef(NullFuncRef())
	_ = ValueF32(1)
	_ = ValueF64(1)
	_ = ValueI32(1)
	_ = ValueI64(1)
	_ = ValueOf(ValI32, I32(1))

	rt := NewRuntime(WithImportOverridePolicy(NoPluginOverrides), WithRuntimeConfig(NewRuntimeConfig()), WithGuestArguments([]string{"facade"}))
	if err := rt.Close(); err != nil {
		t.Fatalf("Runtime.Close: %v", err)
	}
	_ = WithImports(Imports{})
	_ = WithPolicy(Policy{})
	_ = WithGC(GCConfig{})
	_ = IsGuardPageUnavailable(nil)
	_, _ = InspectPluginPlan(PluginSet{})
	_ = ValidatePluginSet(PluginSet{})
	_ = SetOptKnob("missing", true)
	def := PluginDefinition{ID: "example.com/facade/plugin", Version: "1.0.0", Provenance: PluginProvenance{Repository: "https://example.com/facade", License: "MIT"}}
	if digest, err := DefinitionDigest(def); err != nil || digest == "" {
		t.Fatalf("DefinitionDigest=%q,%v", digest, err)
	}
}

package wago

import (
	"path/filepath"
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
	SetGuestArgs([]string{"facade"})
	if len(GuestArgs()) != 1 {
		t.Fatal("GuestArgs did not round-trip")
	}

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
	snap, err := Capture(c, SnapshotOptions{})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	snapshotBytes, err := snap.MarshalBinary()
	if err != nil {
		t.Fatalf("snapshot MarshalBinary: %v", err)
	}
	if !IsSnapshot(snapshotBytes) {
		t.Fatal("IsSnapshot false for encoded snapshot")
	}
	if _, err := LoadSnapshot(snapshotBytes); err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	path := filepath.Join(t.TempDir(), "module.wagos")
	if err := snap.WriteFile(path); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := ReadSnapshotFile(path); err != nil {
		t.Fatalf("ReadSnapshotFile: %v", err)
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

	rt := NewRuntime(WithImportOverridePolicy(NoExtensionOverrides), WithRuntimeConfig(NewRuntimeConfig()))
	if err := rt.Close(); err != nil {
		t.Fatalf("Runtime.Close: %v", err)
	}
	reg := &Registry{}
	if err := ProvideService(reg, "facade", 1); err != nil {
		t.Fatalf("ProvideService: %v", err)
	}
	if _, err := RequireService(reg, "facade"); err != nil {
		t.Fatalf("RequireService: %v", err)
	}
	_ = WithPluginGrants()
	_ = WithImports(Imports{})
	_ = WithPolicy(Policy{})
	_ = WithGC(GCConfig{})
	_ = IsGuardPageUnavailable(nil)
	_, _ = InspectPluginPlan(nil)
	_ = SetOptKnob("missing", true)

	const extensionName = "wago-test/facade"
	RegisterExtension(extensionName, func() Extension { return nil })
	if _, ok := NewExtension(extensionName); !ok {
		t.Fatal("registered extension was not found")
	}
	if len(RegisteredPluginNames()) == 0 {
		t.Fatal("RegisteredPluginNames empty")
	}
}

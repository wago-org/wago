package wago

import (
	"slices"
	"testing"
)

func init() {
	RegisterExtension("test.triple.plugin", func() Extension { return tripleExt{} })
}

func TestPluginRegistry(t *testing.T) {
	found := false
	for _, n := range RegisteredPluginNames() {
		if n == "test.triple.plugin" {
			found = true
		}
	}
	if !found {
		t.Fatalf("registered names %v missing test.triple.plugin", RegisteredPluginNames())
	}

	ext, ok := NewExtension("test.triple.plugin")
	if !ok || ext.Info().ID != "test.triple" {
		t.Fatalf("NewExtension = %v, %v", ext, ok)
	}
	if _, ok := NewExtension("does.not.exist"); ok {
		t.Fatal("NewExtension resolved an unknown plugin")
	}
}

func TestProvidedImports(t *testing.T) {
	rt := NewRuntime()
	if err := rt.Use(tripleExt{}); err != nil { // provides env.f(i32)->i32, cap MetricsWrite
		t.Fatalf("use: %v", err)
	}
	specs := rt.ProvidedImports()
	if len(specs) != 1 {
		t.Fatalf("ProvidedImports = %+v, want 1", specs)
	}
	s := specs[0]
	if s.Key() != "env.f" || s.Kind != ImportFunc || !s.Provided {
		t.Fatalf("spec = %+v", s)
	}
	if len(s.Params) != 1 || s.Params[0] != ValI32 || len(s.Results) != 1 || s.Results[0] != ValI32 {
		t.Fatalf("spec signature = %v -> %v", s.Params, s.Results)
	}
	if !s.HasCapability || s.Capability != CapMetricsWrite {
		t.Fatalf("spec capability = %v (%v)", s.Capability, s.HasCapability)
	}
}

func TestUsePluginAndHostImports(t *testing.T) {
	rt := NewRuntime()
	if err := rt.UsePlugin("test.triple.plugin"); err != nil {
		t.Fatalf("UsePlugin: %v", err)
	}
	if err := rt.UsePlugin("nope"); err == nil {
		t.Fatal("expected error for unknown plugin")
	}
	hi := rt.HostImports()
	if _, ok := hi["env.f"]; !ok {
		t.Fatalf("HostImports missing env.f: %v keys present", len(hi))
	}
	// The returned map is a copy: mutating it does not affect the runtime.
	hi["env.f"] = nil
	if rt.HostImports()["env.f"] == nil {
		t.Fatal("HostImports returned a live reference, not a copy")
	}
}

func TestHostEnvironmentGuestArgsHelpers(t *testing.T) {
	before := GuestArgs()
	t.Cleanup(func() { SetGuestArgs(before) })
	SetGuestArgs([]string{"module.wasm", "one", "two"})
	if got := (&HostEnvironment{}).GuestArgs(); !slices.Equal(got, []string{"module.wasm", "one", "two"}) {
		t.Fatalf("HostEnvironment.GuestArgs = %v", got)
	}
}

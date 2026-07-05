package wago

import "testing"

// Register a test plugin once for the whole package test binary (init runs once,
// so this is safe under -count=N).
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

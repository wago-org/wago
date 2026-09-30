package wago

import (
	"strings"
	"testing"
)

func TestImportsFlatRegistrationPreservesExactIdentity(t *testing.T) {
	imports := NewImports()
	imports.HostFunc("env", "step", func(x int32) int32 { return x + 1 })
	imports.HostFunc("foo", "step", func(x int32) int32 { return x * 2 })
	imports.HostFunc("a.b", "c", func() {})
	imports.HostFunc("a", "b.c", func() {})
	bindings, err := imports.snapshot()
	if err != nil {
		t.Fatal(err)
	}
	for _, identity := range []importBindingKey{{module: "env", name: "step"}, {module: "foo", name: "step"}, {module: "a.b", name: "c"}, {module: "a", name: "b.c"}} {
		key := importBindingMapKey(identity.module, identity.name)
		module, name, split := splitImportBindingMapKey(key)
		if _, ok := bindings[key]; !ok || !split || module != identity.module || name != identity.name {
			t.Fatalf("binding %q = %v, split=(%q, %q, %v); want exact identity %+v", key, bindings[key], module, name, split, identity)
		}
	}
}

func TestImportsDeferredDeclarationValidation(t *testing.T) {
	tests := []struct {
		name string
		add  func(*Imports)
		want string
	}{
		{"duplicate", func(im *Imports) {
			im.HostFunc("env", "f", func() {})
			im.HostFunc("env", "f", func() {})
		}, "duplicate import \"env\".\"f\""},
		{"nil", func(im *Imports) { im.HostFunc("env", "f", nil) }, "host callback is nil"},
		{"typed nil", func(im *Imports) {
			var fn func(int32) int32
			im.HostFunc("env", "f", fn)
		}, "host callback is nil"},
		{"typed nil HostCall", func(im *Imports) {
			var fn HostCallFunc
			im.HostFunc("env", "f", fn)
		}, "host callback is nil"},
		{"nil deferred event", func(im *Imports) {
			var fn I32HostEvent
			im.I32Event("env", "f", fn)
		}, "deferred host callback is nil"},
		{"unsupported", func(im *Imports) { im.HostFunc("env", "f", func(string) {}) }, "unsupported host callback"},
		{"mismatched inferred signature", func(im *Imports) {
			im.HostFunc("env", "f", func(int32) int32 { return 0 }).Params(ValI64).Results(ValI32)
		}, "does not match callback signature"},
		{"legacy raw slots", func(im *Imports) {
			im.HostFunc("env", "f", func(HostModule, []uint64, []uint64) {})
		}, "unsupported host callback"},
		{"legacy caller raw slots", func(im *Imports) {
			im.HostFunc("env", "f", func(Caller, []uint64, []uint64) {})
		}, "unsupported host callback"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			imports := NewImports()
			test.add(imports)
			if _, err := imports.snapshot(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("snapshot error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestImportsSealSnapshotsBindings(t *testing.T) {
	imports := NewImports()
	imports.HostFunc("env", "f", func() {})
	first, err := imports.snapshot()
	if err != nil {
		t.Fatal(err)
	}
	second, err := imports.snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("snapshot sizes = %d, %d; want 1, 1", len(first), len(second))
	}
	imports.HostFunc("env", "late", func() {})
	if _, err := imports.snapshot(); err == nil || !strings.Contains(err.Error(), "collection is sealed") {
		t.Fatalf("mutation after sealing error = %v", err)
	}
	if _, ok := second[importBindingMapKey("env", "late")]; ok {
		t.Fatal("late mutation changed an already-bound snapshot")
	}
}

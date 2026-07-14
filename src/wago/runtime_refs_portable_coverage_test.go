package wago

import (
	"errors"
	"testing"
)

func TestRuntimeReferenceAndErrorPortableSurface(t *testing.T) {
	if _, err := (*Runtime)(nil).NewExternRef("x"); err == nil {
		t.Fatal("nil runtime accepted an externref")
	}
	if _, err := (*Runtime)(nil).NewExternRefTable(0, 1); err == nil {
		t.Fatal("nil runtime accepted an externref table")
	}
	rt := NewRuntime()
	ref, err := rt.NewExternRef("value")
	if err != nil {
		t.Fatalf("NewExternRef: %v", err)
	}
	if got, ok := rt.ExternRefValue(ref); !ok || got != "value" {
		t.Fatalf("ExternRefValue = %#v, %v", got, ok)
	}
	if got, ok := rt.ExternRefValue(NullExternRef()); !ok || got != nil {
		t.Fatalf("null ExternRefValue = %#v, %v", got, ok)
	}
	if _, ok := (*Runtime)(nil).ExternRefValue(ref); ok {
		t.Fatal("nil runtime resolved an externref")
	}
	if _, err := (*Instance)(nil).NewExternRef("x"); err == nil {
		t.Fatal("nil instance accepted an externref")
	}
	private := &Instance{}
	privateRef, err := private.NewExternRef("private")
	if err != nil {
		t.Fatalf("private NewExternRef: %v", err)
	}
	if got, ok := private.ExternRefValue(privateRef); !ok || got != "private" || !private.validExternrefToken(privateRef.token) {
		t.Fatalf("private externref = %#v, %v", got, ok)
	}
	if _, ok := (&Instance{}).ExternRefValue(privateRef); ok {
		t.Fatal("another private instance resolved an externref")
	}
	private.closed = true
	if _, err := private.NewExternRef("after-close"); err == nil {
		t.Fatal("closed private instance accepted an externref")
	}
	if private.validExternrefToken(privateRef.token ^ 1) {
		t.Fatal("forged externref token was accepted")
	}
	table, err := rt.NewExternRefTable(2, 4)
	if err != nil {
		t.Fatalf("NewExternRefTable: %v", err)
	}
	if table.Size() != 2 || (*Table)(nil).Size() != 0 {
		t.Fatalf("table size = %d", table.Size())
	}
	if err := table.Close(); err != nil {
		t.Fatalf("table Close: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("runtime Close: %v", err)
	}
	if _, err := rt.NewExternRefTable(0, 1); err == nil {
		t.Fatal("closed runtime accepted an externref table")
	}

	globalRT := NewRuntime()
	globalRef, err := globalRT.NewExternRef("global")
	if err != nil {
		t.Fatal(err)
	}
	global, err := globalRT.NewExternRefGlobal(globalRef, true)
	if err != nil {
		t.Fatalf("NewExternRefGlobal: %v", err)
	}
	if got, err := global.GetValue(); err != nil || got != ValueExternRef(globalRef) {
		t.Fatalf("externref global value = %v, %v", got, err)
	}
	if err := global.SetValue(ValueExternRef(NullExternRef())); err != nil {
		t.Fatalf("SetValue(null): %v", err)
	}
	if got, err := global.GetValue(); err != nil || !got.ExternRef().IsNull() {
		t.Fatalf("externref global null value = %v, %v", got, err)
	}
	if err := global.SetValue(ValueI32(1)); err == nil {
		t.Fatal("externref global accepted wrong value type")
	}
	foreignRT := NewRuntime()
	foreignRef, err := foreignRT.NewExternRef("foreign")
	if err != nil {
		t.Fatal(err)
	}
	if err := global.SetValue(ValueExternRef(foreignRef)); err == nil {
		t.Fatal("externref global accepted foreign token")
	}
	if err := foreignRT.Close(); err != nil {
		t.Fatal(err)
	}
	if err := global.Close(); err != nil {
		t.Fatal(err)
	}
	if err := globalRT.Close(); err != nil {
		t.Fatal(err)
	}

	pluginErr := &PluginError{Plugin: "test", Phase: PluginPhaseRegister, Capability: PluginHostImports, Path: "x", Err: ErrMissingImport}
	if !errors.Is(pluginErr, ErrMissingImport) || pluginErr.Error() != "wago plugin test: register capability host.imports at x: wago: missing import" {
		t.Fatalf("PluginError = %q", pluginErr)
	}
	extErr := &ExtensionError{Extension: "test", Operation: "use", Err: ErrPermissionDenied}
	if !errors.Is(extErr, ErrPermissionDenied) || extErr.Error() != "wago extension test: use: wago: permission denied" {
		t.Fatalf("ExtensionError = %q", extErr)
	}
	for _, cap := range []PluginCapability{PluginHostImports, PluginHostEnvironment, PluginCompileHooks, PluginInstanceHooks, PluginInvokeHooks, PluginRuntimeHooks, PluginManagedInstances} {
		if !validPluginCapability(cap) {
			t.Errorf("validPluginCapability(%q) = false", cap)
		}
	}
	if validPluginCapability("unknown") {
		t.Fatal("unknown plugin capability accepted")
	}
	for _, tc := range []struct {
		kind ImportKind
		want string
	}{{ImportFunc, "func"}, {ImportGlobal, "global"}, {ImportMemory, "memory"}, {ImportTable, "table"}, {ImportKind(99), "func"}} {
		if got := tc.kind.String(); got != tc.want {
			t.Errorf("ImportKind(%d).String() = %q", tc.kind, got)
		}
	}
	if err := (&Module{}).Close(); err != nil {
		t.Fatalf("Module.Close: %v", err)
	}
	if _, ok := (*Runtime)(nil).Extension("anything"); ok {
		t.Fatal("nil runtime found an extension")
	}
	if _, ok := NewRuntime().Extension("anything"); ok {
		t.Fatal("empty runtime found an extension")
	}
	moduleRT := NewRuntime()
	mod, err := moduleRT.Compile([]byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0})
	if err != nil {
		t.Fatalf("Runtime.Compile: %v", err)
	}
	if meta := mod.Metadata(); len(meta.Functions) != 0 || len(meta.Globals) != 0 || len(meta.Tables) != 0 {
		t.Fatalf("empty Module.Metadata = %+v", meta)
	}
	if err := moduleRT.Close(); err != nil {
		t.Fatalf("module runtime Close: %v", err)
	}
}

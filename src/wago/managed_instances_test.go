package wago

import (
	"context"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

type managedTestExtension struct{ manager *InstanceManager }

func (*managedTestExtension) Info() ExtensionInfo {
	return ExtensionInfo{ID: "test.managed", RequiresCapabilities: []PluginCapability{PluginManagedInstances}}
}

func (e *managedTestExtension) Register(reg *Registry) error {
	var err error
	e.manager, err = reg.ManagedInstances()
	return err
}

func TestManagedInstancesAreRuntimeOwned(t *testing.T) {
	ext := &managedTestExtension{}
	rt := NewRuntime()
	if err := rt.Use(ext); err != nil {
		t.Fatalf("Use: %v", err)
	}
	mod, err := rt.Compile(wasmtest.Module())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	owned, err := ext.manager.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	if owned.Instance() == nil {
		t.Fatal("managed instance is nil")
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Runtime.Close: %v", err)
	}
	if owned.Instance() != nil {
		t.Fatal("runtime close retained managed instance")
	}
}

func TestManagedInstancesRequireManifestGrant(t *testing.T) {
	ext := &managedTestExtension{}
	err := NewRuntime().Use(ext, WithPluginGrants())
	if err == nil {
		t.Fatal("strict Use accepted ungranted instance manager")
	}
}

func TestManagedCallerAndWatcherDuringHostCall(t *testing.T) {
	rt := NewRuntime()
	m := newPendingInstanceManager("test.managed", CapabilityBudget{})
	m.activate(rt)
	in := &Instance{rt: rt}
	owned := &ManagedInstance{manager: m, value: in}
	m.byInstance[in] = owned

	h := in.beginHostCallScope()
	got, err := m.ManagedCaller(h)
	if err != nil || got != owned {
		t.Fatalf("ManagedCaller = %p, %v; want %p, nil", got, err, owned)
	}
	wake, cancel, err := m.WatchCaller(h)
	if err != nil {
		t.Fatalf("WatchCaller: %v", err)
	}
	in.pluginState.Load().hostScope.end(h.generation)
	select {
	case <-wake:
	default:
		t.Fatal("watcher was not signaled after the host call ended")
	}
	cancel()
	if _, err := m.ManagedCaller(h); err == nil {
		t.Fatal("expired host caller retained managed authority")
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Runtime.Close: %v", err)
	}
}

func TestManagedForkImportCopyValidation(t *testing.T) {
	fn := HostFunc(func(HostModule, []uint64, []uint64) {})
	parent := &Instance{c: &Compiled{
		Imports:       []string{"env.fn"},
		GlobalImports: []GlobalImportDef{{Module: "env", Name: "g"}},
		memoryImport:  "env.mem",
		tableImport:   "env.table",
	}, imports: Imports{
		"env.fn":    fn,
		"env.g":     GlobalImport{Type: ValI32},
		"env.mem":   fn,
		"env.table": fn,
	}}
	got, err := managedForkImports(parent)
	if err != nil || len(got) != 4 || got["env.fn"] == nil || got["env.g"] == nil {
		t.Fatalf("managedForkImports = %#v, %v", got, err)
	}
	for _, tc := range []struct {
		name string
		mut  func(*Instance)
	}{
		{"missing", func(in *Instance) { delete(in.imports, "env.fn") }},
		{"unsafe", func(in *Instance) { in.imports["env.fn"] = 3 }},
		{"borrowed global", func(in *Instance) { in.imports["env.g"] = GlobalImport{Global: &Global{}} }},
		{"unsafe memory", func(in *Instance) { in.imports["env.mem"] = &Global{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clone := &Instance{c: parent.c, imports: make(Imports, len(parent.imports))}
			for k, v := range parent.imports {
				clone.imports[k] = v
			}
			tc.mut(clone)
			if _, err := managedForkImports(clone); err == nil {
				t.Fatal("unsafe fork imports accepted")
			}
		})
	}
}

func TestManagedVoidTableDispatch(t *testing.T) {
	ext := &managedTestExtension{}
	rt := NewRuntime()
	if err := rt.Use(ext); err != nil {
		t.Fatalf("Use: %v", err)
	}
	mod, err := rt.Compile(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})), // funcref table, min 1
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))),
	))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	owned, err := ext.manager.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	if err := owned.ValidateVoidTableEntry(0); err != nil {
		t.Fatalf("ValidateVoidTableEntry(0): %v", err)
	}
	if err := owned.ValidateVoidTableEntry(1); err == nil {
		t.Fatal("out-of-range table entry accepted")
	}
	if err := owned.InvokeVoidTable(context.Background(), 0); err != nil {
		t.Fatalf("InvokeVoidTable: %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := owned.InvokeVoidTable(canceled, 0); err != context.Canceled {
		t.Fatalf("canceled InvokeVoidTable = %v, want context.Canceled", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Runtime.Close: %v", err)
	}
}

func TestManagedVoidTableValidationRejectsNullAndWrongSignature(t *testing.T) {
	instantiate := func(t *testing.T, src []byte) (*Runtime, *ManagedInstance) {
		t.Helper()
		ext := &managedTestExtension{}
		rt := NewRuntime()
		if err := rt.Use(ext); err != nil {
			t.Fatal(err)
		}
		mod, err := rt.Compile(src)
		if err != nil {
			t.Fatal(err)
		}
		owned, err := ext.manager.Instantiate(context.Background(), mod)
		if err != nil {
			t.Fatal(err)
		}
		return rt, owned
	}
	t.Run("null", func(t *testing.T) {
		rt, owned := instantiate(t, wasmtest.Module(wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01}))))
		defer rt.Close()
		if err := owned.ValidateVoidTableEntry(0); err == nil {
			t.Fatal("null table entry accepted")
		}
	})
	t.Run("wrong signature", func(t *testing.T) {
		rt, owned := instantiate(t, wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
			wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x00, 0x0b}))),
		))
		defer rt.Close()
		if err := owned.ValidateVoidTableEntry(0); err == nil {
			t.Fatal("non-void table entry accepted")
		}
	})
}

package wago

import (
	"context"
	"testing"

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

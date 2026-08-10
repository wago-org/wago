package component_test

import (
	"context"
	_ "embed"
	"errors"
	"testing"

	"github.com/wago-org/wago/plugin"
	"github.com/wago-org/wago/src/component"
	"github.com/wago-org/wago/src/wago"
)

// This fixture is a genuine Component Model binary, not a core Wasm module.
// It exercises nested instance exports and Canonical ABI lift/lower on Wago.
//
//go:embed testdata/adder.wasm
var adderWasm []byte

type componentServiceConsumer struct {
	ref *plugin.Ref[component.Service]
}

func (*componentServiceConsumer) Info() wago.ExtensionInfo {
	return wago.ExtensionInfo{ID: "test.component-consumer"}
}
func (e *componentServiceConsumer) Register(reg *wago.Registry) (err error) {
	e.ref, err = plugin.Require(reg, component.RuntimeService)
	return err
}

func TestInstantiateAdder(t *testing.T) {
	ctx := context.Background()
	r := wago.NewRuntime()
	defer r.Close()
	components, err := component.Enable(r)
	if err != nil {
		t.Fatalf("Enable: %v", err)
	}

	inst, err := components.Instantiate(ctx, adderWasm)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer inst.Close(ctx)

	got, err := inst.CallExport(ctx, "component:adder/calc", "add", uint32(2), uint32(3))
	if err != nil {
		t.Fatalf("CallExport add: %v", err)
	}
	if len(got) != 1 || got[0] != uint32(5) {
		t.Fatalf("add(2, 3) = %#v, want [5]", got)
	}
}

func TestCompileCache(t *testing.T) {
	ctx := context.Background()
	r := wago.NewRuntime()
	defer r.Close()
	components, err := component.Enable(r)
	if err != nil {
		t.Fatalf("Enable: %v", err)
	}
	cache := component.NewCompileCache()
	defer cache.Close(ctx)

	for i := 0; i < 2; i++ {
		inst, err := components.Instantiate(ctx, adderWasm, component.WithCompileCache(cache))
		if err != nil {
			t.Fatalf("Instantiate #%d: %v", i, err)
		}
		got, err := inst.CallExport(ctx, "component:adder/calc", "add", uint32(10), uint32(20))
		if err != nil {
			t.Fatalf("Call #%d: %v", i, err)
		}
		if len(got) != 1 || got[0] != uint32(30) {
			t.Fatalf("add(10, 20) = %#v, want [30]", got)
		}
		if err := inst.Close(ctx); err != nil {
			t.Fatalf("Close #%d: %v", i, err)
		}
	}
}

func TestPluginFailsClosedWithoutCapabilityOrInstallation(t *testing.T) {
	ctx := context.Background()
	r := wago.NewRuntime()
	defer r.Close()

	if _, err := component.Instantiate(ctx, r, adderWasm); err == nil {
		t.Fatal("Instantiate without the component plugin succeeded")
	}
	if err := r.Use(component.NewExtension(), wago.WithPluginGrants()); !errors.Is(err, wago.ErrPermissionDenied) {
		t.Fatalf("Use without runtime.core grant = %v, want permission denied", err)
	}
}

func TestPluginRuntimeAccessIsRevokedOnClose(t *testing.T) {
	r := wago.NewRuntime()
	components, err := component.Enable(r)
	if err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := components.Instantiate(context.Background(), adderWasm); !errors.Is(err, wago.ErrPermissionDenied) {
		t.Fatalf("Instantiate after Close = %v, want permission denied", err)
	}
}

func TestPluginProvidesVersionedRuntimeService(t *testing.T) {
	if component.PluginID != "wago-org/component-model" {
		t.Fatalf("PluginID = %q", component.PluginID)
	}
	r := wago.NewRuntime()
	defer r.Close()
	components, err := component.Enable(r)
	if err != nil {
		t.Fatalf("Enable: %v", err)
	}
	consumer := &componentServiceConsumer{}
	if err := r.Use(consumer); err != nil {
		t.Fatalf("Use consumer: %v", err)
	}
	service, err := consumer.ref.Get()
	if err != nil || service != components {
		t.Fatalf("component service = %#v, %v; want %#v", service, err, components)
	}
}

package workers_test

import (
	"errors"
	"testing"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/workers"
)

func TestPluginActivatesWorkerService(t *testing.T) {
	p := workers.New()
	rt := wago.NewRuntime()
	if err := rt.Use(p); err != nil {
		t.Fatalf("Use: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	if p.Service() == nil {
		t.Fatal("Service returned nil after registration")
	}
	if err := p.Service().Kill(1); !errors.Is(err, wago.ErrWorkerNotFound) {
		t.Fatalf("Kill on active empty service = %v, want ErrWorkerNotFound", err)
	}
}

func TestPluginIsRegisteredByName(t *testing.T) {
	ext, ok := wago.NewExtension(workers.PluginName)
	if !ok {
		t.Fatal("workers plugin is not registered")
	}
	if got := ext.Info().ID; got != "wago.workers" {
		t.Fatalf("plugin ID = %q, want wago.workers", got)
	}
}

func TestUsePluginServiceCanBeRetrieved(t *testing.T) {
	rt := wago.NewRuntime()
	if err := rt.UsePlugin(workers.PluginName); err != nil {
		t.Fatalf("UsePlugin: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	ext, ok := rt.Extension("wago.workers")
	if !ok {
		t.Fatal("Extension did not return the selected workers plugin")
	}
	p, ok := ext.(*workers.Plugin)
	if !ok || p.Service() == nil {
		t.Fatalf("Extension returned %T with service %v", ext, ok && p.Service() != nil)
	}
}

func TestFreshPluginStartsInactive(t *testing.T) {
	p := workers.New()
	if p.Service() != nil {
		t.Fatal("fresh plugin unexpectedly has a service")
	}
}

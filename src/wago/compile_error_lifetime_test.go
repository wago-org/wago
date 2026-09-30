package wago

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestCompileErrorObserverRetainsPluginLifetime(t *testing.T) {
	for _, panicTransform := range []bool{false, true} {
		name := "error"
		if panicTransform {
			name = "panic"
		}
		t.Run(name, func(t *testing.T) {
			observerEntered := make(chan error, 1)
			observerRelease := make(chan struct{})
			var releaseOnce sync.Once
			releaseObserver := func() { releaseOnce.Do(func() { close(observerRelease) }) }
			stopped := make(chan struct{})
			transformErr := errors.New("source transform failed")
			def := testDefinition("example.com/compile/error-lifetime")
			def.Authorities = []AuthorityRequest{
				{Name: AuthorityModuleSourceTransform, Mode: AuthorityRequired, Reason: "transform source"},
				{Name: AuthorityModuleCompileObserve, Mode: AuthorityRequired, Reason: "observe errors"},
			}
			provider := PluginProvider{Definition: def, New: func() Plugin {
				return pluginFunc(func(reg *Registrar) error {
					transform, err := reg.ModuleSourceTransformer()
					if err != nil {
						return err
					}
					if err := transform.Transform(func(ModuleSourceContext, []byte) ([]byte, error) {
						if panicTransform {
							panic(transformErr)
						}
						return nil, transformErr
					}); err != nil {
						return err
					}
					observer, err := reg.ModuleCompileObserver()
					if err != nil {
						return err
					}
					if err := observer.OnError(func(event ModuleCompileErrorEvent) {
						observerEntered <- event.Err
						<-observerRelease
					}); err != nil {
						return err
					}
					return reg.Lifecycle(PluginLifecycle{Stop: func(context.Context) error { close(stopped); return nil }})
				})
			}}
			rt := NewRuntime()
			defer rt.Close()
			if err := rt.LoadPlugins(context.Background(), testSet(t, provider)); err != nil {
				t.Fatal(err)
			}
			compileDone := make(chan error, 1)
			go func() { _, err := rt.Compile(wasmtest.Module()); compileDone <- err }()
			// Always unblock the callback before any cleanup can wait for shutdown.
			defer releaseObserver()
			select {
			case err := <-observerEntered:
				want := transformErr
				if !errors.Is(err, want) {
					t.Fatalf("observer error = %v, want %v", err, want)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("compile-error observer did not start")
			}
			if err := rt.Close(); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
			defer cancel()
			if err := rt.WaitClosed(ctx); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("shutdown completed before error observer returned: %v", err)
			}
			select {
			case <-stopped:
				t.Fatal("plugin stopped while error observer was active")
			default:
			}
			releaseObserver()
			select {
			case err := <-compileDone:
				want := transformErr
				if !errors.Is(err, want) {
					t.Fatalf("compile error = %v, want %v", err, want)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("compile did not complete")
			}
			waitCtx, waitCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer waitCancel()
			if err := rt.WaitClosed(waitCtx); err != nil {
				t.Fatal(err)
			}
			select {
			case <-stopped:
			default:
				t.Fatal("plugin shutdown did not run")
			}
		})
	}
}

func BenchmarkPrepareCompileTransformError(b *testing.B) {
	rt := NewRuntime()
	defer rt.Close()
	errTransform := errors.New("source transform failed")
	rt.storeHooks(&hookRegistry{
		beforeCompile:  []func(ModuleSourceContext, []byte) ([]byte, error){func(ModuleSourceContext, []byte) ([]byte, error) { return nil, errTransform }},
		onCompileError: []func(ModuleCompileErrorEvent){func(ModuleCompileErrorEvent) {}},
	})
	source := wasmtest.Module()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := rt.PrepareCompile(source); !errors.Is(err, errTransform) {
			b.Fatalf("error = %v", err)
		}
	}
}

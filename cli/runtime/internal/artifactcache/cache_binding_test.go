package artifactcache

import (
	"errors"
	"strings"
	"testing"

	"github.com/wago-org/wago"
)

type rejectingCompileExtension struct {
	err   error
	calls *int
}

func (e rejectingCompileExtension) Info() wago.ExtensionInfo {
	return wago.ExtensionInfo{ID: "test.reject-cache-bind"}
}

func (e rejectingCompileExtension) Register(reg *wago.Registry) error {
	hooks, err := reg.ModuleCompiler()
	if err != nil {
		return err
	}
	hooks.After(func(*wago.CompileContext, *wago.Module) error {
		(*e.calls)++
		return e.err
	})
	return nil
}

func TestLoadOrCompilePropagatesCachedModuleBindingFailureOnce(t *testing.T) {
	source := constantModule()
	config := wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit)
	cache := Cache{Dir: t.TempDir(), Identity: []byte("runtime-a")}
	producer := wago.NewRuntime(wago.WithRuntimeConfig(config))
	module, err := cache.LoadOrCompile(source, config, producer)
	if err != nil {
		t.Fatal(err)
	}
	if err := module.Compiled().Close(); err != nil {
		t.Fatal(err)
	}
	if err := producer.Close(); err != nil {
		t.Fatal(err)
	}

	rejected := errors.New("cached artifact rejected")
	var calls int
	consumer := wago.NewRuntime(wago.WithRuntimeConfig(config))
	if err := consumer.Use(rejectingCompileExtension{err: rejected, calls: &calls}, wago.WithPluginGrants(wago.PluginCompileHooks)); err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	oldClose := closeDecodedArtifact
	var closedArtifacts int
	closeDecodedArtifact = func(compiled *wago.Compiled) error {
		closedArtifacts++
		if err := compiled.Close(); err != nil {
			return err
		}
		if _, err := wago.Instantiate(compiled); err == nil {
			t.Error("rejected decoded artifact remained instantiable after cleanup")
		}
		return nil
	}
	t.Cleanup(func() { closeDecodedArtifact = oldClose })
	if _, err := cache.LoadOrCompile(source, config, consumer); !errors.Is(err, rejected) {
		t.Fatalf("LoadOrCompile error = %v, want cached binding rejection", err)
	}
	if calls != 1 {
		t.Fatalf("AfterCompile called %d times, want exactly once", calls)
	}
	if closedArtifacts != 1 {
		t.Fatalf("rejected decoded artifacts closed %d times, want exactly once", closedArtifacts)
	}
}

func TestLoadOrCompileRejectsCachedArtifactOnClosedRuntime(t *testing.T) {
	source := constantModule()
	config := wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit)
	cache := Cache{Dir: t.TempDir(), Identity: []byte("runtime-a")}
	producer := wago.NewRuntime(wago.WithRuntimeConfig(config))
	module, err := cache.LoadOrCompile(source, config, producer)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Compiled().Close()
	if err := producer.Close(); err != nil {
		t.Fatal(err)
	}
	closed := wago.NewRuntime(wago.WithRuntimeConfig(config))
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.LoadOrCompile(source, config, closed); err == nil || !strings.Contains(err.Error(), "closed runtime") {
		t.Fatalf("cached load into closed runtime = %v, want closed-runtime error", err)
	}
}

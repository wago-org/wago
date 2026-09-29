//go:build wago_codegenstats || wago_profile

package artifactcache

import (
	"github.com/wago-org/wago"
	"testing"
)

func TestLoadOrCompileBypassesArtifactsForCompileOnlyTelemetry(t *testing.T) {
	source := constantModule()
	base := wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit)
	cache := Cache{Dir: t.TempDir(), Identity: []byte("runtime-a")}
	seedRuntime := wago.NewRuntime(wago.WithRuntimeConfig(base))
	seed, err := cache.LoadOrCompile(source, base, seedRuntime)
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}
	if err := seedRuntime.Close(); err != nil {
		t.Fatal(err)
	}

	telemetry := base.WithGCCodeTelemetry(true)
	var compileCalls int
	rt := wago.NewRuntime(wago.WithRuntimeConfig(telemetry))
	loadCachePlugin(t, rt, "example.com/cache/telemetry", []wago.AuthorityRequest{{
		Name: wago.AuthorityModuleSourceTransform, Mode: wago.AuthorityRequired, Reason: "count fresh compiles",
	}}, func(reg *wago.Registrar) error {
		transformer, err := reg.ModuleSourceTransformer()
		if err != nil {
			return err
		}
		return transformer.Transform(func(wago.ModuleSourceContext, []byte) ([]byte, error) {
			compileCalls++
			return nil, nil
		})
	})
	module, err := cache.LoadOrCompile(source, telemetry, rt)
	if err != nil {
		t.Fatal(err)
	}
	if compileCalls != 1 {
		t.Fatalf("compile-only telemetry used a warm artifact; compile calls = %d", compileCalls)
	}
	if _, ok := module.Compiled().GCNativeCodeTelemetry(); !ok {
		t.Fatal("fresh telemetry compile did not retain requested attribution")
	}
	if err := module.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
}

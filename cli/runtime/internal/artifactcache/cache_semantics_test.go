//go:build !tinygo

package artifactcache

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/wago-org/wago"
)

func TestLoadOrCompileValidatesDestinationConfigBeforeWarmLookup(t *testing.T) {
	source := constantModule()
	cache := Cache{Dir: t.TempDir(), Identity: []byte("runtime-a")}
	valid := wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit)
	seedRuntime := wago.NewRuntime(wago.WithRuntimeConfig(valid))
	seed, err := cache.LoadOrCompile(source, valid, seedRuntime)
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}
	if err := seedRuntime.Close(); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name string
		cfg  *wago.RuntimeConfig
		want string
	}{
		{name: "negative workers", cfg: valid.WithFunctionWorkers(-1), want: "function workers must be non-negative"},
		{name: "unknown optimization", cfg: valid.WithOptimization("test-does-not-exist", true), want: "unknown"},
	} {
		t.Run(test.name, func(t *testing.T) {
			rt := wago.NewRuntime(wago.WithRuntimeConfig(test.cfg))
			defer rt.Close()
			if _, err := cache.LoadOrCompile(source, valid, rt); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("LoadOrCompile error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestLoadOrCompileValidatesUnsupportedBMI2BeforeWarmLookup(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("bmi2-rorx is amd64-only")
	}
	base := wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit).WithOptimization("bmi2-rorx", false)
	unsupported := base.WithOptimization("bmi2-rorx", true)
	if err := unsupported.Validate(); err == nil || !strings.Contains(err.Error(), "requires BMI2") {
		t.Skip("host supports BMI2")
	}
	cache := Cache{Dir: t.TempDir(), Identity: []byte("runtime-a")}
	seedRuntime := wago.NewRuntime(wago.WithRuntimeConfig(base))
	seed, err := cache.LoadOrCompile(constantModule(), base, seedRuntime)
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}
	if err := seedRuntime.Close(); err != nil {
		t.Fatal(err)
	}
	seedPath, _ := cache.path(constantModule(), base)
	warmPath, _ := cache.path(constantModule(), unsupported)
	artifact, err := os.ReadFile(seedPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(warmPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(warmPath, artifact, 0o644); err != nil {
		t.Fatal(err)
	}

	rt := wago.NewRuntime(wago.WithRuntimeConfig(unsupported))
	defer rt.Close()
	if _, err := cache.LoadOrCompile(constantModule(), base, rt); err == nil || !strings.Contains(err.Error(), "requires BMI2") {
		t.Fatalf("LoadOrCompile BMI2 error = %v", err)
	}
}

func TestLoadOrCompileTransformGenerationsCannotReuseWrongCode(t *testing.T) {
	input := constantModule()
	cfg := wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit)
	cache := Cache{Dir: t.TempDir(), Identity: []byte("runtime-a")}
	for _, value := range []byte{11, 22} {
		transformed := constantModule()
		transformed[len(transformed)-2] = value
		rt := wago.NewRuntime(wago.WithRuntimeConfig(cfg))
		loadCachePlugin(t, rt, "example.com/cache/transform/"+string(rune('a'+value)), []wago.AuthorityRequest{{
			Name: wago.AuthorityModuleSourceTransform, Mode: wago.AuthorityRequired, Reason: "replace source",
		}}, func(reg *wago.Registrar) error {
			transformer, err := reg.ModuleSourceTransformer()
			if err != nil {
				return err
			}
			return transformer.Transform(func(wago.ModuleSourceContext, []byte) ([]byte, error) {
				return append([]byte(nil), transformed...), nil
			})
		})
		mod, err := cache.LoadOrCompile(input, cfg, rt)
		if err != nil {
			t.Fatal(err)
		}
		in, err := rt.Instantiate(context.Background(), mod)
		if err != nil {
			t.Fatal(err)
		}
		got, err := in.Call(context.Background(), "answer")
		if err != nil || len(got) != 1 || got[0].I32() != int32(value) {
			t.Fatalf("transformed answer = %v, %v; want %d", got, err, value)
		}
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
		if err := mod.Close(); err != nil {
			t.Fatal(err)
		}
		if err := rt.Close(); err != nil {
			t.Fatal(err)
		}
	}
	matches, err := filepath.Glob(filepath.Join(cache.Dir, "*", "*.wago"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("transform generations published artifacts %v, err %v", matches, err)
	}
}

func TestLoadOrCompileWarmHitRunsCompileObserver(t *testing.T) {
	source := constantModule()
	cfg := wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit)
	cache := Cache{Dir: t.TempDir(), Identity: []byte("runtime-a")}
	seedRuntime := wago.NewRuntime(wago.WithRuntimeConfig(cfg))
	seed, err := cache.LoadOrCompile(source, cfg, seedRuntime)
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}
	if err := seedRuntime.Close(); err != nil {
		t.Fatal(err)
	}

	calls := 0
	rt := wago.NewRuntime(wago.WithRuntimeConfig(cfg))
	loadCachePlugin(t, rt, "example.com/cache/observer", []wago.AuthorityRequest{{
		Name: wago.AuthorityModuleCompileObserve, Mode: wago.AuthorityRequired, Reason: "observe warm adoption",
	}}, func(reg *wago.Registrar) error {
		observer, err := reg.ModuleCompileObserver()
		if err != nil {
			return err
		}
		return observer.Observe(func(event wago.ModuleCompiledEvent) {
			calls++
			if event.Compilation.IsZero() || event.SourceDigest.IsZero() {
				t.Error("warm compile event lost compilation/source identity")
			}
		})
	})
	mod, err := cache.LoadOrCompile(source, cfg, rt)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("compile observer calls = %d, want 1", calls)
	}
	if err := mod.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadOrCompileCustomInstructionsAreNonCacheable(t *testing.T) {
	source := constantModule()
	cfg := wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit)
	cache := Cache{Dir: t.TempDir(), Identity: []byte("runtime-a")}
	rt := wago.NewRuntime(wago.WithRuntimeConfig(cfg))
	loadCachePlugin(t, rt, "example.com/cache/instruction", []wago.AuthorityRequest{{
		Name: wago.AuthorityCompilerInstructionDefine, Mode: wago.AuthorityRequired, Reason: "define instruction", Scope: wago.AuthorityScope{Modules: []string{"test.cache"}},
	}}, func(reg *wago.Registrar) error {
		instructions, err := reg.CompilerInstructions()
		if err != nil {
			return err
		}
		return instructions.Define(wago.InstructionSpec{
			Module: "test.cache", Name: "identity", Input: []int32{32}, Output: []int32{32},
			Handler: func(_ wago.InstructionContext, args []wago.Bits) ([]wago.Bits, error) { return args, nil },
		})
	})
	for range 2 {
		mod, err := cache.LoadOrCompile(source, cfg, rt)
		if err != nil {
			t.Fatal(err)
		}
		if err := mod.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(cache.Dir, "*", "*.wago"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("custom-instruction generation published artifacts %v, err %v", matches, err)
	}
}

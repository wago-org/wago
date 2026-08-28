package artifactcache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/wago-org/wago"
)

func TestLoadOrCompileCachesAndRepairsArtifact(t *testing.T) {
	source := constantModule()
	config := wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit)
	cache := Cache{Dir: t.TempDir(), Identity: []byte("runtime-a")}
	rt := wago.NewRuntime(wago.WithRuntimeConfig(config))
	defer rt.Close()

	first, err := cache.LoadOrCompile(source, config, rt)
	if err != nil {
		t.Fatal(err)
	}
	if first.Compiled().Exports["answer"] != 0 {
		t.Fatalf("unexpected exports: %#v", first.Compiled().Exports)
	}
	path, ok := cache.path(source, config)
	if !ok {
		t.Fatal("cache key unavailable")
	}
	artifact, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("artifact was not cached: %v", err)
	}
	if !wago.IsCompiled(artifact) {
		t.Fatal("cached file is not a .wago artifact")
	}

	if err := os.WriteFile(path, []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.LoadOrCompile(source, config, rt); err != nil {
		t.Fatal(err)
	}
	repaired, err := os.ReadFile(path)
	if err != nil || !wago.IsCompiled(repaired) {
		t.Fatalf("corrupt cache was not repaired: compiled=%v err=%v", wago.IsCompiled(repaired), err)
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".wago-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary artifacts remain: %v (err %v)", matches, err)
	}
}

func TestCacheHitRetriesPrune(t *testing.T) {
	dir := t.TempDir()
	config := wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit)
	cache := Cache{Dir: dir, Identity: []byte("runtime-a")}
	rt := wago.NewRuntime(wago.WithRuntimeConfig(config))
	defer rt.Close()

	module, err := cache.LoadOrCompile(constantModule(), config, rt)
	if err != nil {
		t.Fatal(err)
	}
	if err := module.Close(); err != nil {
		t.Fatal(err)
	}
	path, ok := cache.path(constantModule(), config)
	if !ok {
		t.Fatal("cache key unavailable")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(dir, "old.wago")
	if err := os.WriteFile(old, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	when := time.Unix(1, 0)
	if err := os.Chtimes(old, when, when); err != nil {
		t.Fatal(err)
	}
	cache.MaxBytes = info.Size()
	module, err = cache.LoadOrCompile(constantModule(), config, rt)
	if err != nil {
		t.Fatal(err)
	}
	if err := module.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatalf("recent maintenance did not skip warm-hit scan: %v", err)
	}
	marker := filepath.Join(dir, cachePruneMarker)
	stale := time.Now().Add(-cachePruneInterval - time.Second)
	if err := os.Chtimes(marker, stale, stale); err != nil {
		t.Fatal(err)
	}
	module, err = cache.LoadOrCompile(constantModule(), config, rt)
	if err != nil {
		t.Fatal(err)
	}
	if err := module.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("stale overflow entry remains after cache hit: %v", err)
	}
}

func TestCachePruneBoundsTotalArtifacts(t *testing.T) {
	dir := t.TempDir()
	cache := Cache{Dir: dir, MaxBytes: 5}
	nestedOld := filepath.Join(dir, "legacy", "nested", "old.wago")
	if err := os.MkdirAll(filepath.Dir(nestedOld), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nestedOld, []byte("123"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(nestedOld, time.Unix(1, 0), time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"old.wago", "middle.wago", "new.wago"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("123"), 0o600); err != nil {
			t.Fatal(err)
		}
		when := time.Unix(int64(i+1), 0)
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatal(err)
		}
	}
	if err := cache.prune(); err != nil {
		t.Fatal(err)
	}
	entries, err := filepath.Glob(filepath.Join(dir, "*.wago"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || filepath.Base(entries[0]) != "new.wago" {
		t.Fatalf("remaining cache entries = %v, want newest only", entries)
	}
	if _, err := os.Stat(nestedOld); !os.IsNotExist(err) {
		t.Fatalf("nested overflow entry remains: %v", err)
	}
}

func TestCachePruneBoundsEntryIndex(t *testing.T) {
	dir := t.TempDir()
	old := time.Unix(1, 0)
	for i := 0; i < maxPruneEntries+1; i++ {
		path := filepath.Join(dir, fmt.Sprintf("%04d.wago", i))
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	newest := filepath.Join(dir, fmt.Sprintf("%04d.wago", maxPruneEntries))
	if err := os.Chtimes(newest, old.Add(time.Second), old.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := (Cache{Dir: dir}).prune(); err != nil {
		t.Fatal(err)
	}
	entries, err := filepath.Glob(filepath.Join(dir, "*.wago"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != maxPruneEntries {
		t.Fatalf("remaining cache entries = %d, want %d", len(entries), maxPruneEntries)
	}
	if _, err := os.Stat(newest); err != nil {
		t.Fatalf("newest overflow entry was not retained: %v", err)
	}
}

func TestLoadArtifactRejectsSymlinkAndOversizeEntry(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.wago")
	if err := os.WriteFile(target, []byte("not an artifact"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.wago")
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink unavailable: %v", err)
		}
		t.Fatal(err)
	}
	if compiled, hit := loadArtifact(link); hit || compiled != nil {
		t.Fatalf("symlink cache entry loaded: %v, %v", compiled, hit)
	}

	oversize := filepath.Join(dir, "oversize.wago")
	limits := wago.DefaultArtifactLimits()
	file, err := os.Create(oversize)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(oversize, limits.MaxCodeBytes+limits.MaxMetadataBytes+65); err != nil {
		t.Fatal(err)
	}
	if compiled, hit := loadArtifact(oversize); hit || compiled != nil {
		t.Fatalf("oversize cache entry loaded: %v, %v", compiled, hit)
	}
}

func TestLoadOpenedArtifactRejectsReplacementAndGrowth(t *testing.T) {
	source := constantModule()
	compiled, err := wago.Compile(wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit), source)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := compiled.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if err := compiled.Close(); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name        string
		skipWindows bool
		mutate      func(string, *os.File) error
	}{
		{name: "replacement", skipWindows: true, mutate: func(path string, _ *os.File) error {
			replacement := path + ".replacement"
			if err := os.WriteFile(replacement, artifact, 0o644); err != nil {
				return err
			}
			return os.Rename(replacement, path)
		}},
		{name: "growth", mutate: func(_ string, file *os.File) error {
			return os.Truncate(file.Name(), int64(len(artifact)+1))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.skipWindows && runtime.GOOS == "windows" {
				t.Skip("Windows does not replace an open cache entry by rename")
			}
			path := filepath.Join(t.TempDir(), "entry.wago")
			if err := os.WriteFile(path, artifact, 0o644); err != nil {
				t.Fatal(err)
			}
			file, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			opened, err := file.Stat()
			if err != nil {
				t.Fatal(err)
			}
			if err := test.mutate(path, file); err != nil {
				t.Fatal(err)
			}
			if loaded, hit := loadOpenedArtifact(path, file, opened); hit || loaded != nil {
				t.Fatalf("mutated cache entry loaded: %v, %v", loaded, hit)
			}
		})
	}
}

func TestLoadOrCompileReportsPublicationFailure(t *testing.T) {
	source := constantModule()
	config := wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit)
	cache := Cache{Dir: t.TempDir(), Identity: []byte("runtime-a")}
	var reported error
	cache.ReportError = func(err error) { reported = err }
	rt := wago.NewRuntime(wago.WithRuntimeConfig(config))
	defer rt.Close()

	injected := errors.New("injected cache publication failure")
	oldPublish := publishArtifact
	publishArtifact = func(string, *wago.Compiled) error { return injected }
	t.Cleanup(func() { publishArtifact = oldPublish })

	module, err := cache.LoadOrCompile(source, config, rt)
	if err != nil || module == nil {
		t.Fatalf("LoadOrCompile = %v, %v", module, err)
	}
	if !errors.Is(reported, injected) {
		t.Fatalf("reported error = %v", reported)
	}
}

func TestLoadOrCompileSkipsArtifactLargerThanCache(t *testing.T) {
	config := wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit)
	cache := Cache{Dir: t.TempDir(), Identity: []byte("runtime-a"), MaxBytes: 1}
	stale := filepath.Join(cache.Dir, "stale.wago")
	if err := os.WriteFile(stale, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	rt := wago.NewRuntime(wago.WithRuntimeConfig(config))
	defer rt.Close()

	calls := 0
	oldPublish := publishArtifact
	publishArtifact = func(string, *wago.Compiled) error {
		calls++
		return nil
	}
	t.Cleanup(func() { publishArtifact = oldPublish })

	module, err := cache.LoadOrCompile(constantModule(), config, rt)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	if calls != 0 {
		t.Fatalf("oversized artifact publication calls = %d, want 0", calls)
	}
	entries, err := filepath.Glob(filepath.Join(cache.Dir, "*.wago"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("oversized artifact cache entries = %v, %v; want none", entries, err)
	}
}

type cachePlugin func(*wago.Registrar) error

func (f cachePlugin) Register(reg *wago.Registrar) error { return f(reg) }

func loadCachePlugin(t testing.TB, rt *wago.Runtime, id string, authorities []wago.AuthorityRequest, register cachePlugin) {
	t.Helper()
	definition := wago.PluginDefinition{
		ID: id, Version: "1.0.0",
		Provenance:  wago.PluginProvenance{Repository: "https://example.com/" + id, License: "MIT"},
		Authorities: authorities,
	}
	digest, err := wago.DefinitionDigest(definition)
	if err != nil {
		t.Fatal(err)
	}
	selection := wago.PluginSelection{ID: id, DefinitionDigest: digest, Direct: true, Dependencies: map[string]string{}}
	for _, authority := range authorities {
		if authority.Mode == wago.AuthorityRequired {
			selection.Grants = append(selection.Grants, wago.AuthorityGrant{Name: authority.Name, Scope: authority.Scope})
		}
	}
	if err := rt.LoadPlugins(context.Background(), wago.PluginSet{
		Providers:  []wago.PluginProvider{{Definition: definition, New: func() wago.Plugin { return register }}},
		Selections: []wago.PluginSelection{selection},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCacheHitPropagatesAfterCompileErrorExactlyOnce(t *testing.T) {
	source := constantModule()
	config := wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit)
	cache := Cache{Dir: t.TempDir(), Identity: []byte("runtime-a")}
	seedRuntime := wago.NewRuntime(wago.WithRuntimeConfig(config))
	module, err := cache.LoadOrCompile(source, config, seedRuntime)
	if err != nil {
		t.Fatal(err)
	}
	if err := module.Close(); err != nil {
		t.Fatal(err)
	}
	if err := seedRuntime.Close(); err != nil {
		t.Fatal(err)
	}

	rejected := errors.New("cached artifact rejected")
	calls := 0
	rt := wago.NewRuntime(wago.WithRuntimeConfig(config))
	defer rt.Close()
	loadCachePlugin(t, rt, "example.com/cache/reject", []wago.AuthorityRequest{{
		Name: wago.AuthorityModuleCompileObserve, Mode: wago.AuthorityRequired, Reason: "reject adopted artifacts",
	}}, func(reg *wago.Registrar) error {
		observer, err := reg.ModuleCompileObserver()
		if err != nil {
			return err
		}
		return observer.Observe(func(wago.ModuleCompiledEvent) {
			calls++
			if calls == 1 {
				panic(rejected)
			}
		})
	})
	if _, err := cache.LoadOrCompile(source, config, rt); !errors.Is(err, rejected) {
		t.Fatalf("cache binding error = %v, want %v", err, rejected)
	}
	if calls != 1 {
		t.Fatalf("AfterCompile calls = %d, want 1", calls)
	}
}

func TestCacheHitPropagatesRuntimeBindingError(t *testing.T) {
	source := constantModule()
	config := wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit)
	cache := Cache{Dir: t.TempDir(), Identity: []byte("runtime-a")}
	seedRuntime := wago.NewRuntime(wago.WithRuntimeConfig(config))
	module, err := cache.LoadOrCompile(source, config, seedRuntime)
	if err != nil {
		t.Fatal(err)
	}
	if err := module.Close(); err != nil {
		t.Fatal(err)
	}
	if err := seedRuntime.Close(); err != nil {
		t.Fatal(err)
	}

	closedRuntime := wago.NewRuntime(wago.WithRuntimeConfig(config))
	if err := closedRuntime.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.LoadOrCompile(source, config, closedRuntime); err == nil || !strings.Contains(err.Error(), "closed runtime") {
		t.Fatalf("cache binding error = %v", err)
	}
}

func TestLoadOrCompileUsesDestinationRuntimeConfig(t *testing.T) {
	source := constantModule()
	cache := Cache{Dir: t.TempDir(), Identity: []byte("runtime-a")}
	runtimeConfig := wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit)
	callerConfig := runtimeConfig.WithMemoryLimitPages(runtimeConfig.MemoryLimitPages() - 1)
	callerPath, ok := cache.path(source, callerConfig)
	if !ok {
		t.Fatal("caller cache key unavailable")
	}
	runtimePath, ok := cache.path(source, runtimeConfig)
	if !ok || runtimePath == callerPath {
		t.Fatalf("runtime cache key = %q, caller key = %q", runtimePath, callerPath)
	}

	rt := wago.NewRuntime(wago.WithRuntimeConfig(runtimeConfig))
	module, err := cache.LoadOrCompile(source, callerConfig, rt)
	if err != nil {
		t.Fatal(err)
	}
	if err := module.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(runtimePath); err != nil {
		t.Fatalf("runtime-config artifact was not published: %v", err)
	}
	if _, err := os.Stat(callerPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("mismatched caller-config artifact exists: %v", err)
	}
}

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

func TestCacheKeyIncludesRuntimeAndCompilerConfiguration(t *testing.T) {
	if cacheKeyFormat != 2 {
		t.Fatalf("cache key format = %d, want objective-free version 2", cacheKeyFormat)
	}
	source := constantModule()
	dir := t.TempDir()
	base := wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit)
	featureOff := base.WithFeature(wago.CoreFeatureSIMD, false)
	knob := base.OptimizationInfos()[0]
	optimizationOff := base.WithOptimization(knob.Name, !knob.On)
	workers := base.WithFunctionWorkers(2)
	bounds := base.WithBoundsChecks(wago.BoundsChecksSignalsBased)
	deferredOff := base.WithDeferBoundsChecks(false)
	memoryLimit := base.WithMemoryLimitPages(base.MemoryLimitPages() - 1)

	basePath, ok := (Cache{Dir: dir, Identity: []byte("runtime-a")}).path(source, base)
	if !ok {
		t.Fatal("base cache key unavailable")
	}
	featurePath, _ := (Cache{Dir: dir, Identity: []byte("runtime-a")}).path(source, featureOff)
	optimizationPath, _ := (Cache{Dir: dir, Identity: []byte("runtime-a")}).path(source, optimizationOff)
	workersPath, _ := (Cache{Dir: dir, Identity: []byte("runtime-a")}).path(source, workers)
	boundsPath, _ := (Cache{Dir: dir, Identity: []byte("runtime-a")}).path(source, bounds)
	deferredPath, _ := (Cache{Dir: dir, Identity: []byte("runtime-a")}).path(source, deferredOff)
	memoryPath, _ := (Cache{Dir: dir, Identity: []byte("runtime-a")}).path(source, memoryLimit)
	runtimePath, _ := (Cache{Dir: dir, Identity: []byte("runtime-b")}).path(source, base)
	sourcePath, _ := (Cache{Dir: dir, Identity: []byte("runtime-a")}).path(append(source, 0), base)
	if basePath == featurePath {
		t.Fatal("feature configuration did not change artifact key")
	}
	if basePath == runtimePath {
		t.Fatal("runtime identity did not change artifact key")
	}
	if basePath == optimizationPath {
		t.Fatal("optimization selection did not change artifact key")
	}
	if basePath != workersPath {
		t.Fatal("function-worker scheduling policy changed artifact key")
	}
	if basePath == boundsPath {
		t.Fatal("bounds-check mode did not change artifact key")
	}
	if basePath == deferredPath {
		t.Fatal("deferred-bounds policy did not change artifact key")
	}
	if basePath == memoryPath {
		t.Fatal("memory limit did not change artifact key")
	}
	if basePath == sourcePath {
		t.Fatal("source bytes did not change artifact key")
	}
}

func TestBuildIdentityRequiresStableSourceIdentity(t *testing.T) {
	clean := &debug.BuildInfo{
		GoVersion: "go1.25.0",
		Path:      "github.com/wago-org/wago/cli/wago",
		Main:      debug.Module{Path: "github.com/wago-org/wago", Version: "(devel)"},
		Settings: []debug.BuildSetting{
			{Key: "GOARCH", Value: "amd64"},
			{Key: "vcs.revision", Value: "0123456789abcdef"},
			{Key: "vcs.modified", Value: "false"},
		},
	}
	first, ok := buildIdentity(clean)
	if !ok {
		t.Fatal("clean VCS build identity was rejected")
	}
	copyInfo := *clean
	copyInfo.Settings = append([]debug.BuildSetting(nil), clean.Settings...)
	second, ok := buildIdentity(&copyInfo)
	if !ok || first != second {
		t.Fatal("identical build metadata did not produce a stable identity")
	}
	copyInfo.Settings[1].Value = "fedcba9876543210"
	changed, ok := buildIdentity(&copyInfo)
	if !ok || first == changed {
		t.Fatal("source revision did not change build identity")
	}
	copyInfo.Settings[2].Value = "true"
	if _, ok := buildIdentity(&copyInfo); ok {
		t.Fatal("dirty VCS build produced a reusable identity")
	}
	if _, ok := buildIdentity(&debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}); ok {
		t.Fatal("unstamped development build produced a reusable identity")
	}
	missingCleanState := *clean
	missingCleanState.Settings = missingCleanState.Settings[:2]
	if _, ok := buildIdentity(&missingCleanState); ok {
		t.Fatal("development build without an explicit clean-tree state produced a reusable identity")
	}
}

func TestBuildIdentityAcceptsVersionedModule(t *testing.T) {
	info := &debug.BuildInfo{
		GoVersion: "go1.25.0",
		Main: debug.Module{
			Path:    "example.com/custom-wago",
			Version: "v1.2.3",
			Sum:     "h1:module-sum",
		},
	}
	if _, ok := buildIdentity(info); !ok {
		t.Fatal("versioned module identity was rejected")
	}
}

func TestBuildIdentityRejectsMutableDependencies(t *testing.T) {
	base := debug.BuildInfo{
		GoVersion: "go1.25.0",
		Path:      "github.com/wago-org/wago/cli/wago",
		Main:      debug.Module{Path: "github.com/wago-org/wago", Version: "(devel)"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "0123456789abcdef"},
			{Key: "vcs.modified", Value: "false"},
		},
	}
	immutable := debug.Module{Path: "example.com/plugin", Version: "v1.2.3", Sum: "h1:plugin-sum"}
	base.Deps = []*debug.Module{&immutable}
	if _, ok := buildIdentity(&base); !ok {
		t.Fatal("checksummed dependency was rejected")
	}

	tests := []struct {
		name string
		dep  *debug.Module
	}{
		{name: "nil record", dep: nil},
		{name: "missing checksum", dep: &debug.Module{Path: "example.com/plugin", Version: "v1.2.3"}},
		{name: "development version", dep: &debug.Module{Path: "example.com/plugin", Version: "(devel)", Sum: "h1:plugin-sum"}},
		{name: "filesystem replacement", dep: &debug.Module{
			Path: "example.com/plugin", Version: "v1.2.3", Sum: "h1:plugin-sum",
			Replace: &debug.Module{Path: "/work/plugin"},
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			info := base
			info.Deps = []*debug.Module{tc.dep}
			if _, ok := buildIdentity(&info); ok {
				t.Fatal("mutable dependency produced a reusable identity")
			}
		})
	}

	versionedReplacement := immutable
	versionedReplacement.Replace = &debug.Module{Path: "example.com/plugin-fork", Version: "v1.2.4", Sum: "h1:fork-sum"}
	base.Deps = []*debug.Module{&versionedReplacement}
	if _, ok := buildIdentity(&base); !ok {
		t.Fatal("checksummed module replacement was rejected")
	}
}

func BenchmarkCachePath(b *testing.B) {
	cache := Cache{Dir: b.TempDir(), Identity: []byte("benchmark-runtime")}
	config := wago.NewRuntimeConfig()
	source := constantModule()
	b.Run("compact", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, ok := cache.path(source, config); !ok {
				b.Fatal("cache path unavailable")
			}
		}
	})
	b.Run("legacy-json", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, ok := legacyCachePath(cache, source, config); !ok {
				b.Fatal("legacy cache path unavailable")
			}
		}
	})
}

func legacyCachePath(cache Cache, source []byte, config *wago.RuntimeConfig) (string, bool) {
	identity, ok := cache.runtimeIdentity()
	if !ok {
		return "", false
	}
	type knob struct {
		Name string `json:"name"`
		On   bool   `json:"on"`
	}
	type signature struct {
		Source             [sha256.Size]byte `json:"source"`
		Runtime            [sha256.Size]byte `json:"runtime"`
		GOOS               string            `json:"goos"`
		GOARCH             string            `json:"goarch"`
		Features           uint64            `json:"features"`
		BoundsChecks       string            `json:"boundsChecks"`
		DeferredBounds     bool              `json:"deferredBoundsChecks"`
		MaximumMemoryPages uint32            `json:"maximumMemoryPages"`
		OptimizationKnobs  []knob            `json:"optimizationKnobs"`
	}
	infos := config.OptimizationInfos()
	knobs := make([]knob, len(infos))
	for i := range infos {
		knobs[i] = knob{Name: infos[i].Name, On: infos[i].On}
	}
	encoded, err := json.Marshal(signature{
		Source:             sha256.Sum256(source),
		Runtime:            identity,
		GOOS:               runtime.GOOS,
		GOARCH:             runtime.GOARCH,
		Features:           uint64(config.CoreFeatures()),
		BoundsChecks:       config.BoundsChecks().String(),
		DeferredBounds:     config.DeferBoundsChecks(),
		MaximumMemoryPages: config.MemoryLimitPages(),
		OptimizationKnobs:  knobs,
	})
	if err != nil {
		return "", false
	}
	key := sha256.Sum256(encoded)
	text := hex.EncodeToString(key[:])
	return filepath.Join(cache.Dir, text[:2], text[2:]+".wago"), true
}

func constantModule() []byte {
	// (module (func (export "answer") (result i32) i32.const 7))
	return []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0,
		1, 5, 1, 0x60, 0, 1, 0x7f,
		3, 2, 1, 0,
		7, 10, 1, 6, 'a', 'n', 's', 'w', 'e', 'r', 0, 0,
		10, 6, 1, 4, 0, 0x41, 7, 0x0b}
}

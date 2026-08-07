package artifactcache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"testing"

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

func TestCacheKeyIncludesRuntimeAndCompilerConfiguration(t *testing.T) {
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
	if basePath == workersPath {
		t.Fatal("function-worker policy did not change artifact key")
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

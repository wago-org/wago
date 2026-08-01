package artifactcache

import (
	"os"
	"path/filepath"
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

	basePath, ok := (Cache{Dir: dir, Identity: []byte("runtime-a")}).path(source, base)
	if !ok {
		t.Fatal("base cache key unavailable")
	}
	featurePath, _ := (Cache{Dir: dir, Identity: []byte("runtime-a")}).path(source, featureOff)
	runtimePath, _ := (Cache{Dir: dir, Identity: []byte("runtime-b")}).path(source, base)
	if basePath == featurePath {
		t.Fatal("feature configuration did not change artifact key")
	}
	if basePath == runtimePath {
		t.Fatal("runtime identity did not change artifact key")
	}
}

func constantModule() []byte {
	// (module (func (export "answer") (result i32) i32.const 7))
	return []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0,
		1, 5, 1, 0x60, 0, 1, 0x7f,
		3, 2, 1, 0,
		7, 10, 1, 6, 'a', 'n', 's', 'w', 'e', 'r', 0, 0,
		10, 6, 1, 4, 0, 0x41, 7, 0x0b}
}

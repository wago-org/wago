//go:build !tinygo

package artifactcache

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/wago-org/wago"
)

func TestArtifactsAreDeterministicAcrossWorkerPolicies(t *testing.T) {
	policies := []int{0, 1, 2, 4, runtime.GOMAXPROCS(0) + 1}
	corpus := [][]byte{wagoEmptyModule(), constantModule(), memoryModule()}
	for sourceIndex, source := range corpus {
		var baseline []byte
		var baselineEntries []int
		for _, workers := range policies {
			config := wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit).WithFunctionWorkers(workers)
			compiled, err := wago.Compile(config, source)
			if err != nil {
				t.Fatalf("source %d workers %d: %v", sourceIndex, workers, err)
			}
			artifact, err := compiled.MarshalBinary()
			if err != nil {
				_ = compiled.Close()
				t.Fatalf("source %d workers %d marshal: %v", sourceIndex, workers, err)
			}
			entries := append([]int(nil), compiled.Entry...)
			if err := compiled.Close(); err != nil {
				t.Fatal(err)
			}
			if baseline == nil {
				baseline = artifact
				baselineEntries = entries
				continue
			}
			if !bytes.Equal(artifact, baseline) || !slices.Equal(entries, baselineEntries) {
				t.Fatalf("source %d workers %d changed serialized/native metadata", sourceIndex, workers)
			}
		}
	}
}

func TestLoadOrCompileReusesArtifactAcrossWorkerPolicies(t *testing.T) {
	source := constantModule()
	policies := []int{0, 1, 2, 4, runtime.GOMAXPROCS(0) + 1}
	cache := Cache{Dir: t.TempDir(), Identity: []byte("runtime-a")}
	base := wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit)
	for _, workers := range policies {
		rt := wago.NewRuntime(wago.WithRuntimeConfig(base.WithFunctionWorkers(workers)))
		module, err := cache.LoadOrCompile(source, base, rt)
		if err != nil {
			t.Fatalf("workers %d: %v", workers, err)
		}
		if err := module.Close(); err != nil {
			t.Fatalf("workers %d close: %v", workers, err)
		}
		if err := rt.Close(); err != nil {
			t.Fatalf("workers %d runtime close: %v", workers, err)
		}
	}
	matches, err := filepath.Glob(filepath.Join(cache.Dir, "*", "*.wago"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("worker-policy artifacts = %v, %v; want one shared artifact", matches, err)
	}
}

func TestLoadOrCompileReusesArtifactAcrossProcess(t *testing.T) {
	config := wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit)
	cache := Cache{Dir: t.TempDir(), Identity: []byte("cross-process")}
	rt := wago.NewRuntime(wago.WithRuntimeConfig(config))
	module, err := cache.LoadOrCompile(constantModule(), config, rt)
	if err != nil {
		t.Fatal(err)
	}
	if err := module.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestArtifactCacheSubprocessHelper$")
	cmd.Env = append(os.Environ(), "WAGO_ARTIFACT_CACHE_HELPER=1", "WAGO_ARTIFACT_CACHE_DIR="+cache.Dir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("subprocess warm cache reuse: %v\n%s", err, output)
	}
}

func TestArtifactCacheSubprocessHelper(t *testing.T) {
	if os.Getenv("WAGO_ARTIFACT_CACHE_HELPER") != "1" {
		return
	}
	config := wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit).WithFunctionWorkers(runtime.GOMAXPROCS(0) + 1)
	cache := Cache{Dir: os.Getenv("WAGO_ARTIFACT_CACHE_DIR"), Identity: []byte("cross-process")}
	path, ok := cache.path(constantModule(), config)
	if !ok {
		t.Fatal("subprocess cache key unavailable")
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("warm artifact unavailable: %v", err)
	}
	rt := wago.NewRuntime(wago.WithRuntimeConfig(config))
	module, err := cache.LoadOrCompile(constantModule(), config, rt)
	if err != nil {
		t.Fatal(err)
	}
	if err := module.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("warm subprocess rewrote artifact: before=%v after=%v", before, after)
	}
}

func wagoEmptyModule() []byte {
	return []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}
}

func memoryModule() []byte {
	// (module (memory 1 2))
	return []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0, 5, 4, 1, 1, 1, 2}
}

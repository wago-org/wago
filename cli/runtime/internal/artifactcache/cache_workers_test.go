package artifactcache

import (
	"bytes"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"testing"

	"github.com/wago-org/wago"
)

type countingCompileExtension struct {
	calls *int
}

func (e countingCompileExtension) Info() wago.ExtensionInfo {
	return wago.ExtensionInfo{ID: "test.count-cache-compile"}
}

func (e countingCompileExtension) Register(reg *wago.Registry) error {
	hooks, err := reg.ModuleCompiler()
	if err != nil {
		return err
	}
	hooks.Before(func(*wago.CompileContext, []byte) ([]byte, error) {
		(*e.calls)++
		return nil, nil
	})
	return nil
}

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
				compiled.Close()
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
	var compileCalls int
	base := wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit)
	rt := wago.NewRuntime(wago.WithRuntimeConfig(base))
	if err := rt.Use(countingCompileExtension{calls: &compileCalls}, wago.WithPluginGrants(wago.PluginCompileHooks)); err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	for _, workers := range policies {
		module, err := cache.LoadOrCompile(source, base.WithFunctionWorkers(workers), rt)
		if err != nil {
			t.Fatalf("workers %d: %v", workers, err)
		}
		defer module.Compiled().Close()
	}
	if compileCalls != 1 {
		t.Fatalf("source compiled %d times across worker policies, want one warm hit", compileCalls)
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
	defer module.Compiled().Close()
	defer rt.Close()

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
	rt := wago.NewRuntime(wago.WithRuntimeConfig(config))
	var compileCalls int
	if err := rt.Use(countingCompileExtension{calls: &compileCalls}, wago.WithPluginGrants(wago.PluginCompileHooks)); err != nil {
		t.Fatal(err)
	}
	module, err := cache.LoadOrCompile(constantModule(), config, rt)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Compiled().Close()
	defer rt.Close()
	if compileCalls != 0 {
		t.Fatalf("subprocess compiled source %d times, want warm artifact reuse", compileCalls)
	}
}

func wagoEmptyModule() []byte {
	return []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}
}

func memoryModule() []byte {
	// (module (memory 1 2))
	return []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0, 5, 4, 1, 1, 1, 2}
}

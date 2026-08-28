package run

import (
	"fmt"

	"os"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/ui"
	"github.com/wago-org/wago/cli/internal/wasmcall"
	"github.com/wago-org/wago/cli/runtime/internal/artifactcache"
)

func mustLoadModule(file string, config *wago.RuntimeConfig, runtime *wago.Runtime, cache artifactcache.Cache, allowNativeArtifact bool) *wago.Module {
	module, err := loadModule(file, config, runtime, cache, allowNativeArtifact)
	if err != nil {
		ui.Fatal("%v", err)
	}
	return module
}

func loadModule(file string, config *wago.RuntimeConfig, runtime *wago.Runtime, cache artifactcache.Cache, allowNativeArtifact bool) (*wago.Module, error) {
	source, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	if wago.IsCompiled(source) {
		if !allowNativeArtifact {
			return nil, fmt.Errorf("refusing native-code artifact %q; pass --allow-native-artifact only for a trusted .wago file", file)
		}
		compiled, err := wago.LoadTrustedArtifact(source)
		if err != nil {
			return nil, err
		}
		module, err := runtime.Module(compiled)
		if err != nil {
			_ = compiled.Close()
			return nil, err
		}
		return module, nil
	}
	module, err := cache.LoadOrCompile(source, config, runtime)
	if err != nil {
		return nil, err
	}
	return module, nil
}

func mustResolveExport(compiled *wago.Compiled, requested string) string {
	export, err := wasmcall.ResolveExport(compiled, requested)
	if err != nil {
		ui.Fatal("%v", err)
	}
	return export
}

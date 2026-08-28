package run

import (
	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/ui"
	"github.com/wago-org/wago/cli/internal/wasmcall"
	"github.com/wago-org/wago/cli/runtime/internal/artifactcache"
	"github.com/wago-org/wago/cli/runtime/internal/modulefile"
)

func mustLoadModule(file string, config *wago.RuntimeConfig, runtime *wago.Runtime, cache artifactcache.Cache) *wago.Module {
	source, err := modulefile.ReadSourceOrArtifact(file)
	if err != nil {
		ui.Fatal("%v", err)
	}
	if wago.IsCompiled(source) {
		compiled, err := wago.Load(source)
		if err != nil {
			ui.Fatal("%v", err)
		}
		module, err := runtime.Module(compiled)
		if err != nil {
			ui.Fatal("%v", err)
		}
		return module
	}
	module, err := cache.LoadOrCompile(source, config, runtime)
	if err != nil {
		ui.Fatal("%v", err)
	}
	return module
}

func mustResolveExport(compiled *wago.Compiled, requested string) string {
	export, err := wasmcall.ResolveExport(compiled, requested)
	if err != nil {
		ui.Fatal("%v", err)
	}
	return export
}

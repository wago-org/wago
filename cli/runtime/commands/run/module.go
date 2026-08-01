package run

import (
	"fmt"
	"os"
	"strings"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/ui"
	"github.com/wago-org/wago/cli/runtime/internal/artifactcache"
)

func mustLoadModule(file string, config *wago.RuntimeConfig, runtime *wago.Runtime, cache artifactcache.Cache) *wago.Module {
	source, err := os.ReadFile(file)
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
	names := compiled.ExportedFunctions()
	if requested != "" {
		if _, ok := compiled.Exports[requested]; !ok {
			ui.Fatal("no exported function %q (have: %s)", requested, strings.Join(names, ", "))
		}
		return requested
	}
	for _, candidate := range []string{"_start", "main"} {
		if _, ok := compiled.Exports[candidate]; ok {
			return candidate
		}
	}
	if len(names) == 1 {
		return names[0]
	}
	ui.Fatal("multiple exports; pass -e <name> (have: %s)", strings.Join(names, ", "))
	return ""
}

func autoHosts(compiled *wago.Compiled, trace bool, provided wago.Imports) wago.Imports {
	hosts := wago.Imports{}
	for _, name := range compiled.Imports {
		if _, ok := provided[name]; ok {
			continue
		}
		importName := name
		if trace {
			hosts[importName] = wago.HostFunc(func(_ wago.HostModule, params, _ []uint64) {
				var argument int32
				if len(params) > 0 {
					argument = wago.AsI32(params[0])
				}
				fmt.Printf("  %s %s(%d)\n", ui.Dim("host"), importName, argument)
			})
		} else {
			hosts[importName] = wago.HostFunc(func(wago.HostModule, []uint64, []uint64) {})
		}
	}
	return hosts
}

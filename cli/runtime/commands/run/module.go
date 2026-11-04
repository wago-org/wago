package run

import (
	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/ui"
	"github.com/wago-org/wago/cli/internal/wasmcall"
	"github.com/wago-org/wago/cli/runtime/internal/modulefile"
)

func loadModule(file string, runtime *wago.Runtime) (*wago.Module, error) {
	source, err := modulefile.Read(file)
	if err != nil {
		return nil, err
	}
	return runtime.Compile(source)
}

func mustResolveExport(compiled *wago.Compiled, requested string) string {
	export, err := wasmcall.ResolveExport(compiled, requested)
	if err != nil {
		ui.Fatal("%v", err)
	}
	return export
}

// Package module supports runtime-owned module inspection commands.
package module

import (
	"os"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/ui"
)

func Compile(file string) (*wago.Runtime, *wago.Module) {
	rt := wago.NewRuntime()
	for _, name := range wago.RegisteredPluginNames() {
		_ = rt.UsePlugin(name)
	}
	src, err := os.ReadFile(file)
	if err != nil {
		rt.Close()
		ui.Fatal("%v", err)
	}
	mod, err := rt.Compile(src)
	if err != nil {
		rt.Close()
		ui.Fatal("compile: %v", err)
	}
	return rt, mod
}

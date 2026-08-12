// Package module supports runtime-owned module inspection commands.
package module

import (
	"context"
	"os"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/ui"
	runtimeplugin "github.com/wago-org/wago/cli/runtime/internal/plugin"
)

func Compile(file string) (*wago.Runtime, *wago.Module) {
	rt := wago.NewRuntime()
	if set := runtimeplugin.PluginSet(); len(set.Selections) != 0 {
		if err := rt.LoadPlugins(context.Background(), set); err != nil {
			rt.Close()
			ui.Fatal("plugins: %v", err)
		}
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

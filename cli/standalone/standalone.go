// Package standalone runs a Wasm command embedded in a native executable.
package standalone

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/wasmcall"
)

// Options is the compile-time runtime configuration baked into an executable.
type Options struct {
	Invoke            string
	DeferBoundsChecks bool
	OptimizationKnobs map[string]bool
}

// Run executes source as a command and returns its process exit code. Plugins
// must already be linked into the executable through their register packages;
// pluginConfig selects and configures those providers without reading files at
// runtime.
func Run(source, pluginConfig []byte, options Options, args []string) int {
	if err := execute(source, pluginConfig, options, args); err != nil {
		var exit *wago.ExitError
		if errors.As(err, &exit) {
			return int(exit.Code)
		}
		name := "program"
		if len(args) != 0 {
			name = filepath.Base(args[0])
		}
		fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
		return 1
	}
	return 0
}

func execute(source, pluginConfig []byte, options Options, args []string) error {
	var plugins []wago.PluginConfig
	if len(pluginConfig) != 0 {
		if err := json.Unmarshal(pluginConfig, &plugins); err != nil {
			return fmt.Errorf("embedded plugin configuration: %w", err)
		}
	}
	wago.SetGuestArgs(args)
	for name, enabled := range options.OptimizationKnobs {
		if !wago.SetOptKnob(name, enabled) {
			return fmt.Errorf("unknown optimization %q", name)
		}
	}
	config := wago.NewRuntimeConfig().WithDeferBoundsChecks(options.DeferBoundsChecks)
	runtime := wago.NewRuntime(wago.WithRuntimeConfig(config))
	defer runtime.Close()
	if err := runtime.LoadPlugins(plugins); err != nil {
		return err
	}
	module, err := runtime.Compile(source)
	if err != nil {
		return err
	}
	invoke := options.Invoke
	explicitInvoke := invoke != ""
	if !explicitInvoke {
		invoke = "_start"
	}
	if invoke == "_start" {
		if _, ok := module.Compiled().Exports[invoke]; !ok {
			return errors.New("module does not export _start")
		}
	} else if _, ok := module.Compiled().Exports[invoke]; !ok {
		return fmt.Errorf("no exported function %q", invoke)
	}
	params, results, err := module.Compiled().Signature(invoke)
	if err != nil {
		return err
	}
	values := []uint64(nil)
	if explicitInvoke {
		callArgs := args
		if len(callArgs) != 0 {
			callArgs = callArgs[1:]
		}
		values, err = wasmcall.ParseArgs(callArgs, params)
		if err != nil {
			return err
		}
	}
	instance, err := runtime.Instantiate(context.Background(), module)
	if err != nil {
		return err
	}
	defer instance.Close()
	result, err := instance.Invoke(invoke, values...)
	if err != nil {
		return err
	}
	if explicitInvoke {
		fmt.Println(wasmcall.Format(invoke, values, result, params, results))
	}
	return nil
}

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
)

// Run executes source as a command and returns its process exit code. Plugins
// must already be linked into the executable through their register packages;
// pluginConfig selects and configures those providers without reading files at
// runtime.
func Run(source, pluginConfig []byte, args []string) int {
	if err := execute(source, pluginConfig, args); err != nil {
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

func execute(source, pluginConfig []byte, args []string) error {
	var plugins []wago.PluginConfig
	if len(pluginConfig) != 0 {
		if err := json.Unmarshal(pluginConfig, &plugins); err != nil {
			return fmt.Errorf("embedded plugin configuration: %w", err)
		}
	}
	wago.SetGuestArgs(args)
	runtime := wago.NewRuntime()
	defer runtime.Close()
	if err := runtime.LoadPlugins(plugins); err != nil {
		return err
	}
	module, err := runtime.Compile(source)
	if err != nil {
		return err
	}
	if _, ok := module.Compiled().Exports["_start"]; !ok {
		return errors.New("module does not export _start")
	}
	instance, err := runtime.Instantiate(context.Background(), module)
	if err != nil {
		return err
	}
	defer instance.Close()
	_, err = instance.Invoke("_start")
	return err
}

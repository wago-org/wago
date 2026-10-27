package standalone

import "github.com/wago-org/wago"

// Run executes source as a command and returns its process exit code. Plugins
// are handed in as one explicit, reviewed PluginSet.
func Run(source []byte, plugins wago.PluginSet, options Options, args []string) int {
	if err := execute(source, plugins, options, args); err != nil {
		return reportError(err, args)
	}
	return 0
}

func execute(source []byte, plugins wago.PluginSet, options Options, args []string) (err error) {
	runtime, err := loadRuntime(plugins, options, args)
	if err != nil {
		return err
	}
	defer finishRuntime(runtime, &err)
	module, err := runtime.Compile(source)
	if err != nil {
		return err
	}
	return executeModule(runtime, module, options, args)
}

// Validate checks the source and plugin configuration without executing the guest.
// Standalone builds use this before linking; native code is never persisted.
func Validate(source []byte, plugins wago.PluginSet, options Options) (err error) {
	runtime, err := loadRuntime(plugins, options, nil)
	if err != nil {
		return err
	}
	defer finishRuntime(runtime, &err)
	module, err := runtime.Compile(source)
	if err != nil {
		return err
	}
	defer finishModule(module, &err)
	return nil
}

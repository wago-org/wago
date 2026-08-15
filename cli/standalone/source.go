//go:build !wago_precompiled

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

func execute(source []byte, plugins wago.PluginSet, options Options, args []string) error {
	runtime, err := loadRuntime(plugins, options, args)
	if err != nil {
		return err
	}
	defer runtime.Close()
	module, err := runtime.Compile(source)
	if err != nil {
		return err
	}
	return executeModule(runtime, module, options, args)
}

// CompileArtifact applies the selected plugins and compilation options once and
// returns the target-specific artifact embedded by standalone builds.
func CompileArtifact(source []byte, plugins wago.PluginSet, options Options) ([]byte, error) {
	runtime, err := loadRuntime(plugins, options, nil)
	if err != nil {
		return nil, err
	}
	defer runtime.Close()
	module, err := runtime.Compile(source)
	if err != nil {
		return nil, err
	}
	defer module.Close()
	return module.Compiled().MarshalBinary()
}

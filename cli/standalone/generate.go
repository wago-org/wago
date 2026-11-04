//go:build wago_standalone_generator && !wago_standalone

package standalone

import (
	"fmt"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/internal/standalonegen"
)

// GenerateModule compiles source and emits its native-code/metadata Go factory
// for the standalone linker. No Wasm or native-module decoder is needed at run time.
func GenerateModule(source []byte, plugins wago.PluginSet, options Options) (generated map[string][]byte, err error) {
	runtime, err := loadRuntime(plugins, options, nil)
	if err != nil {
		return nil, err
	}
	defer finishRuntime(runtime, &err)
	module, err := runtime.Compile(source)
	if err != nil {
		return nil, err
	}
	defer finishModule(module, &err)
	main, err := standalonegen.Generate(module.Compiled(), "StandaloneCompiled")
	if err != nil {
		return nil, err
	}
	if standalonegen.Dispatcher == nil {
		return nil, fmt.Errorf("standalone generation requires the build-helper profile")
	}
	dispatcher, err := standalonegen.Dispatcher()
	if err != nil {
		return nil, err
	}
	defer dispatcher.(interface{ Close() error }).Close()
	helper, err := standalonegen.Generate(dispatcher, "standaloneManagedDispatcher")
	if err != nil {
		return nil, err
	}
	helper = append(helper, []byte("\nfunc init() { standaloneDispatcherFactory = standaloneManagedDispatcher }\n")...)
	return map[string][]byte{"standalone_module.go": main, "standalone_dispatcher.go": helper}, nil
}

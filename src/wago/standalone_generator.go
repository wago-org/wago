//go:build wago_standalone_generator && !wago_standalone

package wago

import "github.com/wago-org/wago/internal/standalonegen"

// The build helper also compiles the runtime's managed-call trampoline. Final
// executables link that factory instead of compiling an internal Wasm module.
func init() {
	standalonegen.Dispatcher = func() (any, error) { return Compile(nil, managedVoidDispatcherWasm) }
}

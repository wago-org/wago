//go:build !wago_standalone

package wago

func compileManagedVoidDispatcher(cfg *RuntimeConfig) (*Compiled, error) {
	return Compile(cfg, managedVoidDispatcherWasm)
}

//go:build !wago_precompiled || !linux || !riscv64 || tinygo || wago_target_tinygo

package wago

func compileManagedVoidDispatcher(cfg *RuntimeConfig) (*Compiled, error) {
	return Compile(cfg, managedVoidDispatcherWasm)
}

package wago

func compileManagedVoidDispatcher(cfg *RuntimeConfig) (*Compiled, error) {
	return Compile(cfg, managedVoidDispatcherWasm)
}

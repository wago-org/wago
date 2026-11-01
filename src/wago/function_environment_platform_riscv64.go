//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

func compiledManagedFunctionReachability(c *Compiled) bool { return c.usesManagedFunctionStorage() }
func (in *Instance) managedFunctionEnsureStore(domain *gcStoreDomain) error {
	_, err := in.compactExternCollector(domain)
	return err
}
func managedFunctionGateOwner(state *instancePluginState) invocationID {
	state.activations.mu.Lock()
	defer state.activations.mu.Unlock()
	return state.railshotInvocation.gateOwner
}

func managedFunctionAfterLifetimeBoundary(*Instance)                               {}
func functionEnvironmentHostQuiescent(*managedFunctionGraph) bool                  { return true }
func (*managedFunctionCollector) publishHostEnvironment(*managedFunctionEnv) error { return nil }
func managedFunctionDrainDeadDependencies(*managedFunctionGraph)                   {}

func managedFunctionMemberActive(member *managedFunctionMember, id invocationID) bool {
	state := member.owner.pluginState.Load()
	if state == nil {
		return false
	}
	owner := managedFunctionGateOwner(state)
	return owner != 0 && (id == 0 || owner == id)
}

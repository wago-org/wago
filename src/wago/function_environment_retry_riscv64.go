//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

// RV64 retains its existing receipt and invocation-return scheduling contract.
type managedFunctionRetryState struct{}

func (*managedFunctionGraph) queueHostRetry(managedFunctionRetryWait) {}

func tryManagedFunctionInvocation(g *invocationGate) bool {
	return g.state.CompareAndSwap(0, invocationGateHeld)
}

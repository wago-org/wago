//go:build linux && !riscv64 && !tinygo && !wago_target_tinygo

package wago

// Host exception ownership uses its existing representation, and host import
// dependencies are frozen by the joint domain lease. Neither needs RV64 scratch
// or dependency receipt snapshots in the joint function-owner state.
type managedFunctionCollectorTarget struct{}
type managedFunctionMarkTarget struct{}

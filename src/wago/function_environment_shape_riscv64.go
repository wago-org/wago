//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

// Keep RV64's exception scratch and dependency proofs in their original slots.
// Embedded target state lets host builds omit these RV64-only fields without
// changing the RV64 ownership algorithms or their physical representation.
type managedFunctionCollectorTarget struct {
	exception *managedFunctionExceptionScratch
}

type managedFunctionMarkTarget struct {
	receipts []managedFunctionReceiptProof
}

//go:build linux && !tinygo

package riscv64

// Bound fallback control scratch independently from the shared scalar fast
// path. The root frame is additional; operand, signature, body and code budgets
// still apply. Control depth does not allocate guest runtime frames.
const maxControlNesting = 128

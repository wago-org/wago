// Package riscv64 implements the native RV64 railshot backend.
//
// The backend consumes validated WebAssembly and lowers it directly to RV64G.
// Comparisons produce integer predicates, branches compare registers without a
// synthetic flags register, addresses are built explicitly, and calls use
// fixed-size AUIPC+JALR relocation sites. The architectural encoder remains a
// real RISC-V encoder; the machine layer in this package owns WebAssembly and
// railshot-specific lowering policy.
//
// The supported production baseline includes scalar integer and floating-point
// operations, complete core and relaxed SIMD through RV64G SWAR, structured
// control flow, direct and indirect calls, synchronous host re-entry,
// explicit-bounds linear memory, bulk memory, tables, globals, references,
// traps, and the public wrapper ABI. RVV is reserved for an optional future
// optimization tier and is not required for WebAssembly SIMD semantics.
package riscv64

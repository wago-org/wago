//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

// Identity 3 is the framed, cooperatively polled RV64G wrapper ABI. It cannot
// load the experimental GuestContext ABI or AMD64/ARM64 executable artifacts.
const compiledArchitecture byte = 3

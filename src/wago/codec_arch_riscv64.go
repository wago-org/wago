//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

// Identity4 adds authenticated invocation views to the framed, cooperative
// RV64G ABI. Older RV64 images lack the required entry/selector protocol and
// cannot be combined with future public tails. Other target identities remain
// unchanged. No native pointer is persisted in an artifact.
const compiledArchitecture byte = 4

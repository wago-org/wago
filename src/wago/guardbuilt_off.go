//go:build !wago_guardpage || (!linux && !darwin) || (linux && !amd64 && !arm64 && !riscv64) || (darwin && !arm64)

package wago

// guardPageBuilt is false in default builds; signals-based bounds checks are
// unavailable (no guard-page runtime / signal handler is compiled in).
const guardPageBuilt = false

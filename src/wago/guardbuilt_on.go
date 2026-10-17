//go:build wago_guardpage && (((linux || darwin || windows) && (amd64 || arm64)) || (linux && riscv64 && !tinygo && !wago_target_tinygo))

package wago

// guardPageBuilt is true when compiled with -tags wago_guardpage, enabling
// signals-based (guard-page) bounds checks.
const guardPageBuilt = true

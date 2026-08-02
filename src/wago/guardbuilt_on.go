//go:build wago_guardpage && (linux || darwin || windows) && (amd64 || arm64)

package wago

// guardPageBuilt is true when compiled with -tags wago_guardpage, enabling
// signals-based (guard-page) bounds checks.
const guardPageBuilt = true

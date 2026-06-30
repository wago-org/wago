//go:build !wago_guardpage

package wago

// guardPageBuilt is false in default builds; signals-based bounds checks are
// unavailable (no guard-page runtime / signal handler is compiled in).
const guardPageBuilt = false

//go:build wago_guardpage

package wago

// guardPageBuilt is true when compiled with -tags wago_guardpage, enabling
// signals-based (guard-page) bounds checks.
const guardPageBuilt = true

//go:build !tinygo

package wago

// Standard Go grows its host stack, allowing nested component adapter chains.
// Keep native stacks and call buffers bounded even when the host stack can grow.
const maxHostReentryDepth = 32

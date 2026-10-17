//go:build !tinygo

package wago

// Every re-entry level owns another native stack, control frame, and call
// buffers. Four remains useful for ordinary component cycles.
const maxHostReentryDepth = 4

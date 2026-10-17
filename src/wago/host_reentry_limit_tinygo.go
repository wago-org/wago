//go:build tinygo

package wago

// TinyGo's smaller fixed task stack needs the original conservative limit.
const maxHostReentryDepth = 4

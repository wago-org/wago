//go:build amd64 && !tinygo

package amd64

// hostSupportsAVX512 reports whether both the processor and operating system
// support the AVX-512 state used by generated ZMM instructions.
func hostSupportsAVX512() bool

// hostPrefersFullWidthAVX512 distinguishes CPUs with native 512-bit execution
// from Zen 4, where ZMM operations are cracked into 256-bit halves.
func hostPrefersFullWidthAVX512() bool

package amd64

import "os"

// hostSupportsAVX512 reports whether both the processor and operating system
// support the AVX-512 state used by generated ZMM instructions.
func hostSupportsAVX512() bool

// hostPrefersFullWidthAVX512 distinguishes CPUs with native 512-bit execution
// from Zen 4, where ZMM operations are cracked into 256-bit halves. On the
// latter, simple one-for-one replacements lose to the denser AVX2 loop.
func hostPrefersFullWidthAVX512() bool

func canUseAVX512() bool {
	return os.Getenv("WAGO_DISABLE_AVX512") != "1" && hostSupportsAVX512()
}

func canUseZMM(subopcode uint32) bool {
	if !canUseAVX512() {
		return false
	}
	if os.Getenv("WAGO_FORCE_AVX512") == "1" || hostPrefersFullWidthAVX512() {
		return true
	}
	// On 256-bit execution units, use ZMM only when AVX-512 also collapses a
	// multi-instruction YMM sequence. These remain throughput wins on Zen 4.
	return subopcode == 82 || subopcode == 192 || subopcode == 213 ||
		subopcode >= 265 && subopcode <= 268
}

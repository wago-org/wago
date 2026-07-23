//go:build amd64

package amd64

import "os"

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

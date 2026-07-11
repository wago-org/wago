//go:build amd64 && !tinygo

package amd64

import "os"

func cpuid(eaxArg, ecxArg uint32) (eax, ebx, ecx, edx uint32)
func xgetbvLow() uint32

// hasAVX2 / hasAVX512 report the widest vector width this host can execute.
// Because wago is a JIT that runs its output on the machine that compiled it,
// a one-time CPUID at package load is an exact runtime check — the codegen can
// emit YMM/ZMM directly without a per-call feature dispatch. Both require the OS
// to have enabled the corresponding XSAVE state (XGETBV), not just CPU support.
// WAGO_AMD64_NO_WIDE_COPY=1 forces the 128-bit fallback for A/B and for pinning
// down a suspected wide-copy regression.
var (
	hasAVX2   bool
	hasAVX512 bool
)

func init() {
	if os.Getenv("WAGO_AMD64_NO_WIDE_COPY") == "1" {
		return
	}
	_, _, c1, _ := cpuid(1, 0)
	const osxsave, avx = 1 << 27, 1 << 28
	if c1&osxsave == 0 || c1&avx == 0 {
		return // no XGETBV, or CPU lacks AVX
	}
	xcr0 := xgetbvLow()
	if xcr0&0x6 != 0x6 {
		return // OS has not enabled XMM+YMM state
	}
	_, b7, _, _ := cpuid(7, 0)
	const avx2Bit, avx512F, avx512BW = 1 << 5, 1 << 16, 1 << 30
	hasAVX2 = b7&avx2Bit != 0
	// AVX-512 additionally needs opmask + ZMM_Hi256 + Hi16_ZMM state enabled, and
	// both F (dword/qword ops) and BW (byte/word ops) for a general vector memmove.
	if xcr0&0xE0 == 0xE0 {
		hasAVX512 = b7&avx512F != 0 && b7&avx512BW != 0
	}
}

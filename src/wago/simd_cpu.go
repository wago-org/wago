package wago

import (
	"sync"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

// simdHostFeaturesSupported checks the amd64 backend baseline for all modules,
// including scalar code, and the SIMD capability on other architectures. The
// amd64 backend emits VEX.128 instructions and uses SSSE3, SSE4.1, and SSE4.2
// operations, so AVX OS support plus SSSE3/SSE4.1/SSE4.2 are required.
// Linux exposes AVX in
// /proc/cpuinfo only when the kernel has enabled the XSAVE state needed to run
// AVX instructions. On arm64, Advanced SIMD/NEON is part of the baseline AArch64
// profile used by Go.
var simdHostFeaturesSupported = cachedSIMDHostFeatures

var (
	simdHostFeaturesOnce sync.Once
	simdHostFeaturesOK   bool
	bmi2HostFeaturesOnce sync.Once
	bmi2HostFeaturesOK   bool
)

func cachedSIMDHostFeatures() bool {
	simdHostFeaturesOnce.Do(func() { simdHostFeaturesOK = detectSIMDHostFeatures() })
	return simdHostFeaturesOK
}

func hostSupportsSIMD() bool { return simdHostFeaturesSupported() }

var bmi2HostFeaturesSupported = cachedBMI2HostFeatures

func cachedBMI2HostFeatures() bool {
	bmi2HostFeaturesOnce.Do(func() { bmi2HostFeaturesOK = architectureSupportsBMI2() })
	return bmi2HostFeaturesOK
}

func hostSupportsBMI2() bool { return bmi2HostFeaturesSupported() }

var bitCountHostFeaturesSupported = cachedBitCountHostFeatures

var (
	bitCountHostFeaturesOnce sync.Once
	bitCountHostFeaturesOK   uint8
)

func cachedBitCountHostFeatures() uint8 {
	bitCountHostFeaturesOnce.Do(func() { bitCountHostFeaturesOK = architectureAMD64BitCountFeatures() })
	return bitCountHostFeaturesOK
}

func amd64BitCountFeatures(ecx1, ebx7, extECX uint32) (features uint8) {
	if extECX&(uint32(1)<<5) != 0 {
		features |= shared.BitCountLZCNT
	}
	if ebx7&(uint32(1)<<3) != 0 {
		features |= shared.BitCountTZCNT
	}
	if ecx1&(uint32(1)<<23) != 0 {
		features |= shared.BitCountPOPCNT
	}
	return
}

func detectSIMDHostFeatures() bool { return architectureSupportsSIMD() }

func amd64SIMDFeaturesSupported(ecx, xcr0 uint32) bool {
	const (
		ssse3   = uint32(1) << 9
		sse41   = uint32(1) << 19
		sse42   = uint32(1) << 20
		osxsave = uint32(1) << 27
		avx     = uint32(1) << 28
	)
	required := ssse3 | sse41 | sse42 | osxsave | avx
	return ecx&required == required && xcr0&0x6 == 0x6
}

//go:noinline
func cpuFlagPresent(data []byte, flag string) bool {
	for i := 0; i < len(data); {
		if data[i] <= ' ' {
			i++
			continue
		}
		start := i
		for i < len(data) && data[i] > ' ' {
			i++
		}
		if i-start != len(flag) {
			continue
		}
		j := 0
		for j < len(flag) && data[start+j] == flag[j] {
			j++
		}
		if j == len(flag) {
			return true
		}
	}
	return false
}

// simdCPUFlagsSupported checks exact Linux cpuinfo tokens without allocation.
func simdCPUFlagsSupported(data []byte) bool {
	return cpuFlagPresent(data, "avx") && cpuFlagPresent(data, "ssse3") &&
		cpuFlagPresent(data, "sse4_1") && cpuFlagPresent(data, "sse4_2")
}

func bmi2CPUFlagsSupported(data []byte) bool {
	return cpuFlagPresent(data, "bmi2")
}

// Linux reports LZCNT as "abm" in /proc/cpuinfo.
func bitCountCPUFlags(data []byte) (features uint8) {
	if cpuFlagPresent(data, "abm") {
		features |= shared.BitCountLZCNT
	}
	if cpuFlagPresent(data, "bmi1") {
		features |= shared.BitCountTZCNT
	}
	if cpuFlagPresent(data, "popcnt") {
		features |= shared.BitCountPOPCNT
	}
	return
}

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

// simdHostFeaturesSupported retains the test seam for native admission. AMD64
// needs only its architectural SSE2 baseline and successful CPU detection;
// optional features select compiler optimizations. ARM64 guarantees NEON.
var simdHostFeaturesSupported = cachedSIMDHostFeatures

func cachedSIMDHostFeatures() bool { return detectSIMDHostFeatures() }

func hostSupportsSIMD() bool { return simdHostFeaturesSupported() }

var bmi2HostFeaturesSupported = cachedBMI2HostFeatures

func cachedBMI2HostFeatures() bool { return architectureSupportsBMI2() }

func hostSupportsBMI2() bool { return bmi2HostFeaturesSupported() }

var bitCountHostFeaturesSupported = cachedBitCountHostFeatures

func cachedBitCountHostFeatures() uint8 { return architectureAMD64BitCountFeatures() }

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

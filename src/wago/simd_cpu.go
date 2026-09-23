package wago

import (
	"fmt"
	"sync"
)

// simdHostFeaturesSupported reports whether generated SIMD code can execute on
// this host. On amd64, the railshot SIMD backend emits VEX.128 instructions and
// uses SSSE3, SSE4.1, and SSE4.2 operations (for example pshufb, pmulld,
// roundps/pd, and pcmpgtq), so AVX OS support plus SSSE3/SSE4.1/SSE4.2 are
// required. Linux exposes AVX in
// /proc/cpuinfo only when the kernel has enabled the XSAVE state needed to run
// AVX instructions. On arm64, Advanced SIMD/NEON is part of the baseline AArch64
// profile used by Go.
var simdHostFeaturesSupported = cachedSIMDHostFeatures

var (
	simdHostFeaturesOnce sync.Once
	simdHostFeaturesOK   bool
	bmi2HostFeaturesOnce sync.Once
	bmi2HostFeaturesOK   bool
	avxHostFeaturesOnce  sync.Once
	avx2HostFeaturesOK   bool
	avx512HostFeaturesOK bool
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

var (
	avx2HostFeaturesSupported   = cachedAVX2HostFeatures
	avx512HostFeaturesSupported = cachedAVX512HostFeatures
)

func detectAVXHostFeatures() {
	avx2HostFeaturesOK, avx512HostFeaturesOK = architectureSupportsAVX()
}

func cachedAVX2HostFeatures() bool {
	avxHostFeaturesOnce.Do(detectAVXHostFeatures)
	return avx2HostFeaturesOK
}

func cachedAVX512HostFeatures() bool {
	avxHostFeaturesOnce.Do(detectAVXHostFeatures)
	return avx512HostFeaturesOK
}

func checkCompiledAVXRequirements(avx2, avx512 bool) error {
	if avx2 && !avx2HostFeaturesSupported() {
		return fmt.Errorf("wago: compiled module requires AVX2 CPU features unavailable on this host")
	}
	if avx512 && !avx512HostFeaturesSupported() {
		return fmt.Errorf("wago: compiled module requires AVX-512 CPU features unavailable on this host")
	}
	return nil
}

func amd64AVX2FeaturesSupported(ecx, xcr0, ebx uint32) bool {
	const avxOS = uint32(1)<<27 | uint32(1)<<28
	return ecx&avxOS == avxOS && xcr0&0x6 == 0x6 && ebx&(uint32(1)<<5) != 0
}

func amd64AVX512FeaturesSupported(ecx, xcr0, ebx uint32) bool {
	const avxOS = uint32(1)<<27 | uint32(1)<<28
	return ecx&avxOS == avxOS && xcr0&0xe6 == 0xe6 && ebx&(uint32(1)<<16) != 0
}

func avxCPUFlagsSupported(data []byte) (avx2, avx512 bool) {
	var avx, avx512f bool
	for i := 0; i < len(data); {
		for i < len(data) && data[i] <= ' ' {
			i++
		}
		start := i
		for i < len(data) && data[i] > ' ' {
			i++
		}
		token := data[start:i]
		switch len(token) {
		case 3:
			avx = avx || token[0] == 'a' && token[1] == 'v' && token[2] == 'x'
		case 4:
			avx2 = avx2 || token[0] == 'a' && token[1] == 'v' && token[2] == 'x' && token[3] == '2'
		case 7:
			avx512f = avx512f || token[0] == 'a' && token[1] == 'v' && token[2] == 'x' && token[3] == '5' && token[4] == '1' && token[5] == '2' && token[6] == 'f'
		}
	}
	return avx && avx2, avx && avx512f
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

// simdCPUFlagsSupported recognizes the four exact whitespace-delimited Linux
// cpuinfo flags without converting the complete file to a string, lowercasing it,
// splitting every token, or building a hash map. It normally returns from the
// first processor's flags line and performs no allocation.
func simdCPUFlagsSupported(data []byte) bool {
	var avx, ssse3, sse41, sse42 bool
	for i := 0; i < len(data); {
		for i < len(data) && data[i] <= ' ' {
			i++
		}
		start := i
		for i < len(data) && data[i] > ' ' {
			i++
		}
		token := data[start:i]
		switch len(token) {
		case 3:
			avx = avx || token[0] == 'a' && token[1] == 'v' && token[2] == 'x'
		case 5:
			ssse3 = ssse3 || token[0] == 's' && token[1] == 's' && token[2] == 's' && token[3] == 'e' && token[4] == '3'
		case 6:
			sse41 = sse41 || token[0] == 's' && token[1] == 's' && token[2] == 'e' && token[3] == '4' && token[4] == '_' && token[5] == '1'
			sse42 = sse42 || token[0] == 's' && token[1] == 's' && token[2] == 'e' && token[3] == '4' && token[4] == '_' && token[5] == '2'
		}
		if avx && ssse3 && sse41 && sse42 {
			return true
		}
	}
	return false
}

func bmi2CPUFlagsSupported(data []byte) bool {
	for i := 0; i < len(data); {
		for i < len(data) && data[i] <= ' ' {
			i++
		}
		start := i
		for i < len(data) && data[i] > ' ' {
			i++
		}
		token := data[start:i]
		if len(token) == 4 && token[0] == 'b' && token[1] == 'm' && token[2] == 'i' && token[3] == '2' {
			return true
		}
	}
	return false
}

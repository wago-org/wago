package wago

import "sync"

// simdHostFeaturesSupported reports whether generated SIMD code can execute on
// this host. On amd64, the railshot SIMD backend emits VEX.128 instructions and
// uses SSSE3 and SSE4.1 operations (for example pshufb, pmulld, roundps/pd), so
// AVX OS support plus SSSE3/SSE4.1 are required. Linux exposes AVX in
// /proc/cpuinfo only when the kernel has enabled the XSAVE state needed to run
// AVX instructions. On arm64, Advanced SIMD/NEON is part of the baseline AArch64
// profile used by Go.
var simdHostFeaturesSupported = cachedSIMDHostFeatures

var (
	simdHostFeaturesOnce sync.Once
	simdHostFeaturesOK   bool
)

func cachedSIMDHostFeatures() bool {
	simdHostFeaturesOnce.Do(func() { simdHostFeaturesOK = detectSIMDHostFeatures() })
	return simdHostFeaturesOK
}

func hostSupportsSIMD() bool { return simdHostFeaturesSupported() }

func detectSIMDHostFeatures() bool { return architectureSupportsSIMD() }

// simdCPUFlagsSupported recognizes the three exact whitespace-delimited Linux
// cpuinfo flags without converting the complete file to a string, lowercasing it,
// splitting every token, or building a hash map. It normally returns from the
// first processor's flags line and performs no allocation.
func simdCPUFlagsSupported(data []byte) bool {
	var avx, ssse3, sse41 bool
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
		}
		if avx && ssse3 && sse41 {
			return true
		}
	}
	return false
}

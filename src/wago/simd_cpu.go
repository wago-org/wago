package wago

import (
	"os"
	"runtime"
	"strings"
	"sync"
)

// simdHostFeaturesSupported reports whether generated SIMD code can execute on
// this host. On amd64, the railshot SIMD backend emits VEX.128 instructions and
// uses SSSE3 and SSE4.1 operations (for example pshufb, pmulld, roundps/pd), so
// AVX OS support plus SSSE3/SSE4.1 are required. Linux exposes AVX in
// /proc/cpuinfo only when the kernel has enabled the XSAVE state needed to run
// AVX instructions. On arm64, Advanced SIMD/NEON is part of the baseline AArch64
// profile used by Go. On linux/riscv64, detection requires ratified RVV 1.0 on
// every online CPU plus process-level vector permission, although the backend
// continues to reject SIMD until its RVV lowering is complete.
var simdHostFeaturesSupported = cachedSIMDHostFeatures

var (
	simdHostFeaturesOnce sync.Once
	simdHostFeaturesOK   bool
)

func cachedSIMDHostFeatures() bool {
	simdHostFeaturesOnce.Do(func() { simdHostFeaturesOK = detectSIMDHostFeatures() })
	return simdHostFeaturesOK
}

func backendSupportsSIMD() bool {
	return runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64"
}

func hostSupportsSIMD() bool {
	return backendSupportsSIMD() && simdHostFeaturesSupported()
}

func detectSIMDHostFeatures() bool {
	switch runtime.GOARCH {
	case "arm64":
		return true
	case "riscv64":
		return detectRISCV64SIMDHostFeatures()
	case "amd64":
		data, err := os.ReadFile("/proc/cpuinfo")
		if err != nil {
			// Be conservative: without a reliable feature source, don't admit SIMD wasm.
			return false
		}
		flags := strings.Fields(strings.ToLower(string(data)))
		seen := map[string]bool{}
		for _, f := range flags {
			seen[f] = true
		}
		return seen["avx"] && seen["ssse3"] && seen["sse4_1"]
	default:
		return false
	}
}

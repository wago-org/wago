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
// profile used by Go. RISC-V uses the baseline RV64G SWAR backend, so WebAssembly
// SIMD admission does not depend on RVV; RVV detection remains available for a
// future optimized tier.
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
	return runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64" || runtime.GOARCH == "riscv64"
}

func hostSupportsSIMD() bool {
	return backendSupportsSIMD() && simdHostFeaturesSupported()
}

func detectSIMDHostFeatures() bool {
	switch runtime.GOARCH {
	case "arm64":
		return true
	case "riscv64":
		return true // baseline RV64G SWAR; detectRISCV64SIMDHostFeatures selects future RVV tiers
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

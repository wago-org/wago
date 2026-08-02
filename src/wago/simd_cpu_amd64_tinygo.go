//go:build amd64 && tinygo

package wago

import (
	"os"
	"strings"
)

// TinyGo cannot assemble the standard Go CPUID helper. Linux remains the only
// amd64 TinyGo runtime target; /proc/cpuinfo includes AVX only when the kernel
// enabled the required XSAVE state.
func architectureSupportsSIMD() bool {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return false
	}
	flags := strings.Fields(strings.ToLower(string(data)))
	seen := map[string]bool{}
	for _, flag := range flags {
		seen[flag] = true
	}
	return seen["avx"] && seen["ssse3"] && seen["sse4_1"]
}

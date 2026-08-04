//go:build amd64 && tinygo

package wago

import "os"

// TinyGo cannot assemble the standard Go CPUID helper. Linux remains the only
// amd64 TinyGo runtime target; /proc/cpuinfo includes AVX only when the kernel
// enabled the required XSAVE state.
func architectureSupportsSIMD() bool {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return false
	}
	return simdCPUFlagsSupported(data)
}

//go:build amd64 && !tinygo

package wago

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

func cpuid(eax, ecx uint32) (a, b, c, d uint32)
func xgetbv() (eax, edx uint32)

func architectureAMD64CPUFeatures() (shared.AMD64Features, bool) {
	maxID, _, _, _ := cpuid(0, 0)
	if maxID < 1 {
		return 0, false
	}
	_, _, ecx1, edx1 := cpuid(1, 0)
	if edx1&(1<<26) == 0 {
		return 0, false
	}
	var ebx7, extECX, xcr0 uint32
	if maxID >= 7 {
		_, ebx7, _, _ = cpuid(7, 0)
	}
	maxExt, _, _, _ := cpuid(0x80000000, 0)
	if maxExt >= 0x80000001 {
		_, _, extECX, _ = cpuid(0x80000001, 0)
	}
	if ecx1&(1<<26|1<<27) == 1<<26|1<<27 {
		xcr0, _ = xgetbv()
	}
	return amd64CPUIDFeatures(ecx1, ebx7, extECX, xcr0), true
}

func architectureSupportsSIMD() bool {
	f, ok := cachedAMD64CPUFeatures()
	return ok && f.Has(shared.AMD64ModernBaseline)
}
func architectureSupportsBMI2() bool {
	f, ok := cachedAMD64CPUFeatures()
	return ok && f.Has(shared.AMD64BMI2)
}
func architectureAMD64BitCountFeatures() uint8 {
	f, ok := cachedAMD64CPUFeatures()
	if !ok {
		return 0
	}
	return f.BitCountCapabilities()
}

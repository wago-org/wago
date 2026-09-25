//go:build amd64 && !tinygo

package wago

func cpuid(eax, ecx uint32) (a, b, c, d uint32)
func xgetbv() (eax, edx uint32)

func architectureSupportsSIMD() bool {
	maxID, _, _, _ := cpuid(0, 0)
	if maxID < 1 {
		return false
	}
	_, _, ecx, _ := cpuid(1, 0)
	const osxsave = uint32(1) << 27
	if ecx&osxsave == 0 {
		return false
	}
	xcr0, _ := xgetbv()
	return amd64SIMDFeaturesSupported(ecx, xcr0)
}

func architectureSupportsBMI2() bool {
	maxID, _, _, _ := cpuid(0, 0)
	if maxID < 7 {
		return false
	}
	_, ebx, _, _ := cpuid(7, 0)
	return ebx&(uint32(1)<<8) != 0
}

func architectureAMD64BitCountFeatures() uint8 {
	maxID, _, _, _ := cpuid(0, 0)
	if maxID < 1 {
		return 0
	}
	_, _, ecx1, _ := cpuid(1, 0)
	var ebx7 uint32
	if maxID >= 7 {
		_, ebx7, _, _ = cpuid(7, 0)
	}
	maxExtID, _, _, _ := cpuid(0x80000000, 0)
	var extECX uint32
	if maxExtID >= 0x80000001 {
		_, _, extECX, _ = cpuid(0x80000001, 0)
	}
	return amd64BitCountFeatures(ecx1, ebx7, extECX)
}

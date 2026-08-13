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

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
	const (
		ssse3   = uint32(1) << 9
		sse41   = uint32(1) << 19
		osxsave = uint32(1) << 27
		avx     = uint32(1) << 28
	)
	if ecx&(ssse3|sse41|osxsave|avx) != ssse3|sse41|osxsave|avx {
		return false
	}
	xcr0, _ := xgetbv()
	return xcr0&0x6 == 0x6 // OS preserves both XMM and YMM state.
}

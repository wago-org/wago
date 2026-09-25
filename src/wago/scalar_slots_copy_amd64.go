//go:build amd64 && !tinygo

package wago

//go:noescape
func copyNarrowScalarSlotsAVX2(dst, src *uint64, n uintptr)

var narrowScalarAVX2Supported = detectNarrowScalarAVX2()

func detectNarrowScalarAVX2() bool {
	maxID, _, _, _ := cpuid(0, 0)
	if maxID < 7 || !architectureSupportsSIMD() {
		return false
	}
	_, ebx, _, _ := cpuid(7, 0)
	return ebx&(uint32(1)<<5) != 0
}

func copyNarrowScalarSlots(dst, src []uint64, n int) {
	dst, src = dst[:n], src[:n]
	if n >= 16 && narrowScalarAVX2Supported {
		copyNarrowScalarSlotsAVX2(&dst[0], &src[0], uintptr(n))
		return
	}
	i := 0
	for ; i+4 <= n; i += 4 {
		_ = dst[i+3]
		_ = src[i+3]
		dst[i] = uint64(uint32(src[i]))
		dst[i+1] = uint64(uint32(src[i+1]))
		dst[i+2] = uint64(uint32(src[i+2]))
		dst[i+3] = uint64(uint32(src[i+3]))
	}
	for ; i < n; i++ {
		dst[i] = uint64(uint32(src[i]))
	}
}

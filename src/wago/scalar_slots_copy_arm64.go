//go:build arm64 && !tinygo

package wago

//go:noescape
func copyNarrowScalarSlotsSIMD(dst, src *uint64, n uintptr)

func copyNarrowScalarSlots(dst, src []uint64, n int) {
	dst, src = dst[:n], src[:n]
	if n < 4 {
		for i := range src {
			dst[i] = uint64(uint32(src[i]))
		}
		return
	}
	copyNarrowScalarSlotsSIMD(&dst[0], &src[0], uintptr(n))
}

//go:build tinygo

package wago

func copyNarrowScalarSlots(dst, src []uint64, n int) {
	dst, src = dst[:n], src[:n]
	for i := 0; i < n; i++ {
		dst[i] = uint64(uint32(src[i]))
	}
}

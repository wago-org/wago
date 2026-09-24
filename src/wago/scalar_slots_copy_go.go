//go:build !tinygo && !arm64 && !amd64

package wago

func copyNarrowScalarSlots(dst, src []uint64, n int) {
	dst, src = dst[:n], src[:n]
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

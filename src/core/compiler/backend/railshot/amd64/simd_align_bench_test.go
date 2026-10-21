//go:build linux && amd64

package amd64

import "testing"

func BenchmarkSIMDByteAlignLoop(b *testing.B) {
	body := []byte{2, 2, 0x7b, 1, 0x7f}
	body = append(body, v128ConstBytes(i8x16Bytes(0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15))...)
	body = append(body, 0x21, 0)
	body = append(body, v128ConstBytes(i8x16Bytes(16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31))...)
	body = append(body, 0x21, 1, 0x03, 0x40, 0x20, 0, 0x20, 1)
	body = append(body, simdOp(13)...)
	for i := byte(14); i < 30; i++ {
		body = append(body, i)
	}
	body = append(body, 0x21, 0, 0x20, 2, 0x41, 1, 0x6a, 0x22, 2, 0x41, 0x80, 0x20, 0x49, 0x0d, 0, 0x0b, 0x20, 0, 0x0b)
	benchmarkSIMDV128Body(b, body)
}

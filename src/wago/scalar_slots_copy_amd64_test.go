//go:build amd64 && !tinygo

package wago

import "testing"

func TestNarrowScalarSlotCopyWithoutAVX2(t *testing.T) {
	available := narrowScalarAVX2Supported
	narrowScalarAVX2Supported = false
	defer func() { narrowScalarAVX2Supported = available }()
	const canary = uint64(0xdeadbeefcafebabe)
	for n := 0; n <= 129; n++ {
		src, dst := make([]uint64, n+2), make([]uint64, n+2)
		dst[0], dst[n+1] = canary, canary
		for i := 0; i < n; i++ {
			src[i+1] = 0xffffffff00000000 | uint64(i+1)
		}
		copyNarrowScalarSlots(dst[1:n+1], src[1:n+1], n)
		if dst[0] != canary || dst[n+1] != canary {
			t.Fatalf("n=%d: fallback touched a canary", n)
		}
		for i := 0; i < n; i++ {
			if got, want := dst[i+1], uint64(i+1); got != want {
				t.Fatalf("n=%d slot=%d: got %x, want %x", n, i, got, want)
			}
		}
	}
}

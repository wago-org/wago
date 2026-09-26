package wago

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"testing"
)

func TestAMD64CapabilityDetection(t *testing.T) {
	const ecx = uint32(1<<9 | 1<<12 | 1<<19 | 1<<20 | 1<<23 | 1<<26 | 1<<27 | 1<<28)
	const ebx = uint32(1<<3 | 1<<5 | 1<<8 | 1<<16 | 1<<17 | 1<<30 | 1<<31)
	for _, tc := range []struct {
		name     string
		ecx, xcr uint32
		want     shared.AMD64Features
	}{
		{"all", ecx, 0xe6, shared.AMD64KnownFeatures},
		{"no-osxsave", ecx &^ (1 << 27), 0xe6, shared.AMD64KnownFeatures &^ (shared.AMD64AVX | shared.AMD64AVX2 | shared.AMD64AVX512 | shared.AMD64FMA)},
		{"no-xsave", ecx &^ (1 << 26), 0xe6, shared.AMD64KnownFeatures &^ (shared.AMD64AVX | shared.AMD64AVX2 | shared.AMD64AVX512 | shared.AMD64FMA)},
		{"no-ymm", ecx, 2, shared.AMD64KnownFeatures &^ (shared.AMD64AVX | shared.AMD64AVX2 | shared.AMD64AVX512 | shared.AMD64FMA)},
		{"no-zmm", ecx, 6, shared.AMD64KnownFeatures &^ shared.AMD64AVX512},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := amd64CPUIDFeatures(tc.ecx, ebx, 1<<5, tc.xcr); got != tc.want {
				t.Fatalf("got=%x want=%x", got, tc.want)
			}
		})
	}
	if got := amd64CPUIDFeatures(0, 0, 0, 0); got != 0 {
		t.Fatalf("SSE2 profile has optional bits %x", got)
	}
}

func TestAMD64LinuxCapabilityIntersection(t *testing.T) {
	for _, tc := range []struct {
		data string
		want shared.AMD64Features
		ok   bool
	}{
		{"flags : sse2\n", 0, true},
		{"flags : sse2 avx avx2 bmi2\nflags : sse2 bmi2\n", shared.AMD64BMI2, true},
		{"flags : sse2 avx2 fma\n", 0, true},
		{"flags : avx avx2\n", 0, false},
		{"", 0, false},
	} {
		got, ok := amd64LinuxCPUFeatures([]byte(tc.data))
		if ok != tc.ok || (ok && got != tc.want) {
			t.Fatalf("%q got=%x/%v want=%x/%v", tc.data, got, ok, tc.want, tc.ok)
		}
	}
	if n := testing.AllocsPerRun(100, func() { amd64LinuxCPUFeatures([]byte("flags : sse2 avx avx2\n")) }); n != 0 {
		t.Fatalf("flag parsing allocates: %g", n)
	}
}

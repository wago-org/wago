package wago

import (
	"strings"
	"testing"
)

func TestAMD64SIMDFeaturesSupported(t *testing.T) {
	const all = uint32(1)<<9 | uint32(1)<<19 | uint32(1)<<20 | uint32(1)<<27 | uint32(1)<<28
	for _, tc := range []struct {
		name string
		ecx  uint32
		xcr0 uint32
		want bool
	}{
		{name: "all", ecx: all, xcr0: 0x6, want: true},
		{name: "missing ssse3", ecx: all &^ (uint32(1) << 9), xcr0: 0x6},
		{name: "missing sse41", ecx: all &^ (uint32(1) << 19), xcr0: 0x6},
		{name: "missing sse42", ecx: all &^ (uint32(1) << 20), xcr0: 0x6},
		{name: "missing osxsave", ecx: all &^ (uint32(1) << 27), xcr0: 0x6},
		{name: "missing avx", ecx: all &^ (uint32(1) << 28), xcr0: 0x6},
		{name: "missing xmm state", ecx: all, xcr0: 0x4},
		{name: "missing ymm state", ecx: all, xcr0: 0x2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := amd64SIMDFeaturesSupported(tc.ecx, tc.xcr0); got != tc.want {
				t.Fatalf("amd64SIMDFeaturesSupported(%#x, %#x) = %v, want %v", tc.ecx, tc.xcr0, got, tc.want)
			}
		})
	}
}

func TestSIMDCPUFlagsSupported(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
		want bool
	}{
		{name: "all", data: "flags : fpu ssse3 sse4_1 sse4_2 avx avx2\n", want: true},
		{name: "split lines", data: "avx\nssse3\nsse4_1\nsse4_2", want: true},
		{name: "missing avx", data: "flags : ssse3 sse4_1 sse4_2", want: false},
		{name: "missing ssse3", data: "flags : avx sse4_1 sse4_2", want: false},
		{name: "missing sse41", data: "flags : avx ssse3 sse4_2", want: false},
		{name: "missing sse42", data: "flags : avx ssse3 sse4_1", want: false},
		{name: "substrings reject", data: "flags : avx2 xssse3 sse4_10 sse4_20", want: false},
		{name: "empty", data: "", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := simdCPUFlagsSupported([]byte(tc.data)); got != tc.want {
				t.Fatalf("simdCPUFlagsSupported(%q) = %v, want %v", tc.data, got, tc.want)
			}
		})
	}
}

func TestBMI2CPUFlagsSupported(t *testing.T) {
	for _, tc := range []struct {
		data string
		want bool
	}{
		{data: "flags : fpu bmi1 bmi2 avx2\n", want: true},
		{data: "bmi2", want: true},
		{data: "flags : bmi1 avx2", want: false},
		{data: "flags : xbmi2 bmi20", want: false},
	} {
		if got := bmi2CPUFlagsSupported([]byte(tc.data)); got != tc.want {
			t.Fatalf("bmi2CPUFlagsSupported(%q) = %v, want %v", tc.data, got, tc.want)
		}
	}
}

var simdCPUFlagsSink bool

func BenchmarkSIMDCPUFlags(b *testing.B) {
	data := []byte(strings.Repeat("processor : 0\nflags : fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush mmx fxsr sse sse2 ss ht syscall nx pdpe1gb rdtscp lm constant_tsc rep_good nopl xtopology cpuid tsc_known_freq pni pclmulqdq ssse3 fma cx16 pcid sse4_1 sse4_2 movbe popcnt aes xsave avx f16c rdrand\n", 16))
	b.Run("scanner", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			simdCPUFlagsSink = simdCPUFlagsSupported(data)
		}
	})
	b.Run("fields-map", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			flags := strings.Fields(strings.ToLower(string(data)))
			seen := map[string]bool{}
			for _, flag := range flags {
				seen[flag] = true
			}
			simdCPUFlagsSink = seen["avx"] && seen["ssse3"] && seen["sse4_1"] && seen["sse4_2"]
		}
	})
}

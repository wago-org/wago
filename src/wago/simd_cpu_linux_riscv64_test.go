//go:build linux && riscv64

package wago

import (
	"encoding/binary"
	"testing"
)

func TestRISCV64SIMDCapabilityPolicy(t *testing.T) {
	const vectorHWCAP = riscvHWCAPISAV
	const vectorProbe = riscvHWProbeIMAExtV
	tests := []struct {
		name       string
		probeKey   int64
		extensions uint64
		probeOK    bool
		hwcap      uint64
		hwcapOK    bool
		want       bool
	}{
		{"all-required", riscvHWProbeKeyIMAExt0, vectorProbe, true, vectorHWCAP, true, true},
		{"probe-syscall-failed", riscvHWProbeKeyIMAExt0, vectorProbe, false, vectorHWCAP, true, false},
		{"probe-key-unknown", -1, vectorProbe, true, vectorHWCAP, true, false},
		{"probe-key-wrong", 3, vectorProbe, true, vectorHWCAP, true, false},
		{"probe-v-absent", riscvHWProbeKeyIMAExt0, 0, true, vectorHWCAP, true, false},
		{"hwcap-unavailable", riscvHWProbeKeyIMAExt0, vectorProbe, true, vectorHWCAP, false, false},
		{"process-v-disabled", riscvHWProbeKeyIMAExt0, vectorProbe, true, 0, true, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := riscv64SIMDCapabilitiesOK(tc.probeKey, tc.extensions, tc.probeOK, tc.hwcap, tc.hwcapOK)
			if got != tc.want {
				t.Fatalf("supported = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseRISCV64HWCAP(t *testing.T) {
	appendEntry := func(dst []byte, tag, value uint64) []byte {
		var entry [16]byte
		binary.LittleEndian.PutUint64(entry[:8], tag)
		binary.LittleEndian.PutUint64(entry[8:], value)
		return append(dst, entry[:]...)
	}

	auxv := appendEntry(nil, 3, 0x1234)
	auxv = appendEntry(auxv, auxvATHWCAP, 0xabcdef)
	auxv = appendEntry(auxv, 0, 0)
	if got, ok := parseRISCV64HWCAP(auxv); !ok || got != 0xabcdef {
		t.Fatalf("HWCAP = %#x, %v", got, ok)
	}

	missing := appendEntry(nil, 3, 0x1234)
	missing = appendEntry(missing, 0, 0)
	if got, ok := parseRISCV64HWCAP(missing); ok || got != 0 {
		t.Fatalf("missing HWCAP = %#x, %v", got, ok)
	}
	if got, ok := parseRISCV64HWCAP([]byte{1, 2, 3}); ok || got != 0 {
		t.Fatalf("truncated HWCAP = %#x, %v", got, ok)
	}
}

func TestRISCV64SIMDDetectorMatchesKernelCapabilities(t *testing.T) {
	key, extensions, probeOK := probeRISCV64IMAExtensions()
	hwcap, hwcapOK := readRISCV64HWCAP()
	want := riscv64SIMDCapabilitiesOK(key, extensions, probeOK, hwcap, hwcapOK)
	if got := detectRISCV64SIMDHostFeatures(); got != want {
		t.Fatalf("detector = %v, want %v", got, want)
	}
	t.Logf("hwprobe_ok=%v key=%d extensions=%#x hwcap_ok=%v hwcap=%#x rvv=%v", probeOK, key, extensions, hwcapOK, hwcap, want)
}

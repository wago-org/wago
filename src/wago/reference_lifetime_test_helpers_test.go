//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import "testing"

func itoa32(v int32) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	n := int64(v)
	if neg {
		n = -n
	}
	var buf [12]byte
	i := len(buf)
	for n != 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func assertRetainedInstanceState(t *testing.T, name string, in *Instance, wantRefs int, wantPhysical bool) {
	t.Helper()
	state := in.referenceLifetime().snapshot()
	if state.ResourceRoots != wantRefs || state.PhysicalResources != wantPhysical {
		t.Fatalf("%s: roots=%d physical=%v, want roots=%d physical=%v", name, state.ResourceRoots, state.PhysicalResources, wantRefs, wantPhysical)
	}
}

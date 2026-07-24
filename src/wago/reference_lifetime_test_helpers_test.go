//go:build ((linux && (amd64 || arm64)) || (darwin && arm64)) && !tinygo

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
	in.lifeMu.Lock()
	refs, physical := in.resourceRefs, !in.resourcesClosed
	in.lifeMu.Unlock()
	if refs != int32(wantRefs) || physical != wantPhysical {
		t.Fatalf("%s: roots=%d physical=%v, want roots=%d physical=%v", name, refs, physical, wantRefs, wantPhysical)
	}
}

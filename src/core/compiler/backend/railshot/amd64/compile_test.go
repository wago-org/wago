//go:build linux && amd64

package amd64

import "testing"

func TestAsmCapForBodyClamps(t *testing.T) {
	for _, tc := range []struct {
		bodyLen int
		wantMin int
		wantMax int
	}{
		{0, 128, 128},
		{8, 128, 128},
		{64, 320, 320},
		{1 << 20, 64 << 10, 64 << 10},
	} {
		got := asmCapForBody(tc.bodyLen)
		if got < tc.wantMin || got > tc.wantMax {
			t.Fatalf("asmCapForBody(%d) = %d, want in [%d,%d]", tc.bodyLen, got, tc.wantMin, tc.wantMax)
		}
	}
}

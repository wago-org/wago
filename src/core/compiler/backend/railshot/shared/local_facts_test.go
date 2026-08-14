package shared

import "testing"

func TestUnreadLocalMask(t *testing.T) {
	for _, tc := range []struct {
		name    string
		nLocals int
		read    uint64
		want    uint64
	}{
		{name: "none", nLocals: 0},
		{name: "bounded", nLocals: 3, read: 0b101, want: 0b010},
		{name: "exact-limit", nLocals: 64, read: 1, want: ^uint64(1)},
		{name: "conservative-overflow", nLocals: 65, read: 1, want: ^uint64(1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := UnreadLocalMask(tc.nLocals, tc.read); got != tc.want {
				t.Fatalf("UnreadLocalMask(%d, %#x) = %#x, want %#x", tc.nLocals, tc.read, got, tc.want)
			}
		})
	}
}

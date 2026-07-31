package wago

import "testing"

func TestGCFrameLocalLivenessIsArchitectureIndependent(t *testing.T) {
	tests := []struct {
		name    string
		body    []byte
		indexes []uint32
		calls   bool
		want    []uint64
	}{
		{
			name:    "live after allocation",
			body:    []byte{0xfb, 0x01, 0x00, 0x1a, 0x20, 0x00, 0x1a, 0x0b},
			indexes: []uint32{0},
			want:    []uint64{1},
		},
		{
			name:    "dead before allocation",
			body:    []byte{0x20, 0x00, 0x1a, 0xfb, 0x01, 0x00, 0x1a, 0x0b},
			indexes: []uint32{0},
			want:    []uint64{0},
		},
		{
			name:    "native call",
			body:    []byte{0x10, 0x00, 0x20, 0x01, 0x1a, 0x0b},
			indexes: []uint32{0, 1},
			calls:   true,
			want:    []uint64{2},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := gcFrameLocalLiveness(tc.body, tc.indexes, tc.calls)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("mask count = %d, want %d (%v)", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("mask %d = %#x, want %#x", i, got[i], tc.want[i])
				}
			}
		})
	}
}

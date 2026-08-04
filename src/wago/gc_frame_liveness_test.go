package wago

import (
	"strings"
	"testing"
	"unsafe"
)

func gcFrameLivenessBenchmarkBody(n int) []byte {
	body := make([]byte, 0, n*3)
	for i := 0; i < n; i++ {
		body = append(body, 0x20, 0x00, 0x1a) // local.get 0; drop
		if i&15 == 0 {
			body = append(body, 0xfb, 0x01, 0x00, 0x1a) // struct.new_default 0; drop
		}
	}
	return append(body, 0x0b)
}

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
		{
			name: "if successors",
			body: []byte{
				0x20, 0x00, 0x04, 0x40, // local.get 0; if
				0xfb, 0x01, 0x00, 0x1a, // struct.new_default 0; drop
				0x05,                   // else
				0xfb, 0x01, 0x00, 0x1a, // struct.new_default 0; drop
				0x0b, 0x20, 0x01, 0x1a, 0x0b,
			},
			indexes: []uint32{0, 1},
			want:    []uint64{2, 2},
		},
		{
			name: "loop backedge",
			body: []byte{
				0x03, 0x40, 0x10, 0x00, // loop; call 0
				0x20, 0x00, 0x0d, 0x00, // local.get 0; br_if 0
				0x0b, 0x20, 0x01, 0x1a, 0x0b,
			},
			indexes: []uint32{0, 1},
			calls:   true,
			want:    []uint64{3},
		},
		{
			name: "br_table target arena",
			body: []byte{
				0x02, 0x40, 0x02, 0x40, 0x10, 0x00, // block; block; call 0
				0x20, 0x00, 0x0e, 0x01, 0x00, 0x01, // local.get 0; br_table 0 1
				0x0b, 0x0b, 0x20, 0x01, 0x1a, 0x0b,
			},
			indexes: []uint32{0, 1},
			calls:   true,
			want:    []uint64{3},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var callMasks []uint64
			got, err := gcFrameLocalLiveness(tc.body, tc.indexes, &callMasks)
			if err != nil {
				t.Fatal(err)
			}
			if tc.calls {
				got = callMasks
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

func TestGCFrameLocalLivenessRejectsUnrepresentableBrTable(t *testing.T) {
	body := []byte{0x0e, 0xff, 0xff, 0xff, 0xff, 0x0f, 0x0b}
	_, err := gcFrameLocalLiveness(body, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "br_table target count exceeds implementation limit") {
		t.Fatalf("error = %v, want target-count implementation-limit rejection", err)
	}
}

func TestGCFrameLocalLivenessAllocationBudget(t *testing.T) {
	if got := unsafe.Sizeof(gcLiveNode{}); got > 40 {
		t.Fatalf("gcLiveNode size = %d, want at most 40 bytes", got)
	}
	body := gcFrameLivenessBenchmarkBody(1024)
	allocs := testing.AllocsPerRun(5, func() {
		var callMasks []uint64
		if _, err := gcFrameLocalLiveness(body, []uint32{0}, &callMasks); err != nil {
			t.Fatal(err)
		}
	})
	if allocs > 100 {
		t.Fatalf("GC frame liveness allocations = %.0f, want at most 100", allocs)
	}
}

func BenchmarkGCFrameLocalLiveness(b *testing.B) {
	body := gcFrameLivenessBenchmarkBody(16 * 1024)
	b.ReportAllocs()
	b.SetBytes(int64(len(body)))
	for b.Loop() {
		var callMasks []uint64
		masks, err := gcFrameLocalLiveness(body, []uint32{0}, &callMasks)
		if err != nil {
			b.Fatal(err)
		}
		if len(masks) != 1024 || len(callMasks) != 0 {
			b.Fatalf("mask counts = %d allocations, %d calls; want 1024, 0", len(masks), len(callMasks))
		}
	}
}

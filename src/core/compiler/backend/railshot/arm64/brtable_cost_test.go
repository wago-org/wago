//go:build arm64

package arm64

import "testing"

func repeatedBrTableLabelsArm64(n int) []uint32 {
	return make([]uint32, n)
}

func TestBrTableUseJumpCompactionCostArm64(t *testing.T) {
	tests := []struct {
		name    string
		labels  []uint32
		def     uint32
		compact bool
		want    bool
	}{
		{"ordinary threshold", []uint32{0, 1, 2, 3, 4}, 5, false, true},
		{"compact unique five", []uint32{0, 1, 2, 3, 4}, 5, true, false},
		{"compact unique six", []uint32{0, 1, 2, 3, 4, 5}, 6, true, false},
		{"compact unique seven tie", []uint32{0, 1, 2, 3, 4, 5, 6}, 7, true, true},
		{"compact duplicate five pays", []uint32{0, 0, 1, 1, 2}, 3, true, true},
		{"compact one duplicate five insufficient", []uint32{0, 0, 1, 2, 3}, 4, true, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := brTableUseJump(tc.labels, tc.def, CodegenPolicy{CompactNative: tc.compact}); got != tc.want {
				t.Fatalf("brTableUseJump(%v, %d, compact=%v) = %v, want %v", tc.labels, tc.def, tc.compact, got, tc.want)
			}
		})
	}
}

func TestBrTableCompactPlanArm64(t *testing.T) {
	tests := []struct {
		name    string
		labels  []uint32
		compact bool
		want    bool
		unique  int
	}{
		{"compact duplicate", []uint32{0, 0, 0, 1, 1, 1, 2, 2, 2, 3, 3, 3}, true, true, 4},
		{"compact unique", []uint32{0, 1, 2, 3, 4, 5, 6, 7}, true, false, 0},
		{"ordinary duplicate", []uint32{0, 0, 0, 1, 1, 1, 2, 2, 2, 3, 3, 3}, false, false, 0},
		{"compact 4092 labels", repeatedBrTableLabelsArm64(4092), true, true, 1},
		{"compact 4093 labels", repeatedBrTableLabelsArm64(4093), true, false, 0},
		{"compact 4095 labels", repeatedBrTableLabelsArm64(4095), true, false, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := fn{policy: CodegenPolicy{CompactNative: tc.compact}, sc: &scratch{}, ctrl: make([]ctrlFrame, 9)}
			got, unique, _ := f.brTableCompactPlan(tc.labels, 8)
			if got != tc.want || unique != tc.unique {
				t.Fatalf("compact plan = %v, %d unique, want %v, %d", got, unique, tc.want, tc.unique)
			}
		})
	}
}

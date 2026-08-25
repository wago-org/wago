//go:build amd64

package amd64

import "testing"

func TestBrTableCompactPlanAMD64(t *testing.T) {
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

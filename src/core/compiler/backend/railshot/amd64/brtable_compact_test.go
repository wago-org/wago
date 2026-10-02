//go:build amd64

package amd64

import "testing"

func TestBrTableCompactPlanAMD64(t *testing.T) {
	policy := func(objective OptimizationObjective) CodegenPolicy {
		p := CodegenPolicy{}
		p.Objective = objective
		return p
	}
	tests := []struct {
		name      string
		labels    []uint32
		objective OptimizationObjective
		want      bool
		unique    int
	}{
		{"size duplicate", []uint32{0, 0, 0, 1, 1, 1, 2, 2, 2, 3, 3, 3}, OptimizeSize, true, 4},
		{"size unique", []uint32{0, 1, 2, 3, 4, 5, 6, 7}, OptimizeSize, false, 0},
		{"balanced duplicate", []uint32{0, 0, 0, 1, 1, 1, 2, 2, 2, 3, 3, 3}, OptimizeBalanced, false, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := fn{policy: policy(tc.objective), sc: &scratch{}, ctrl: make([]ctrlFrame, 9)}
			got, unique, _ := f.brTableCompactPlan(tc.labels, 8)
			if got != tc.want || unique != tc.unique {
				t.Fatalf("compact plan = %v, %d unique, want %v, %d", got, unique, tc.want, tc.unique)
			}
		})
	}
}

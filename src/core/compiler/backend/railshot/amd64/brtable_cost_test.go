//go:build amd64

package amd64

import "testing"

func TestBrTableUseJumpObjectiveCostAMD64(t *testing.T) {
	policy := func(objective OptimizationObjective) CodegenPolicy {
		p := CodegenPolicy{}
		p.Objective = objective
		return p
	}
	tests := []struct {
		name      string
		labels    []uint32
		def       uint32
		reg       Reg
		objective OptimizationObjective
		want      bool
	}{
		{"balanced threshold", []uint32{0, 1, 2, 3, 4}, 5, RCX, OptimizeBalanced, true},
		{"size unique five", []uint32{0, 1, 2, 3, 4}, 5, RCX, OptimizeSize, false},
		{"size unique six", []uint32{0, 1, 2, 3, 4, 5}, 6, RCX, OptimizeSize, true},
		{"size duplicate five", []uint32{0, 0, 1, 2, 3}, 4, RCX, OptimizeSize, true},
		{"size rax six ambiguous", []uint32{0, 1, 2, 3, 4, 5}, 6, RAX, OptimizeSize, false},
		{"size rax seven pays", []uint32{0, 1, 2, 3, 4, 5, 6}, 7, RAX, OptimizeSize, true},
		{"embedded follows size", []uint32{0, 1, 2, 3, 4}, 5, RCX, OptimizeEmbedded, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := brTableUseJump(tc.labels, tc.def, tc.reg, policy(tc.objective)); got != tc.want {
				t.Fatalf("brTableUseJump(%v, %d, %v, %v) = %v, want %v", tc.labels, tc.def, tc.reg, tc.objective, got, tc.want)
			}
		})
	}
}

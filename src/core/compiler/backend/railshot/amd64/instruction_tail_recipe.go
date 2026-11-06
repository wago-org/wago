//go:build amd64

package amd64

import railcore "github.com/wago-org/wago/src/core/compiler/backend/railshot"

const (
	maxTailInstructionRecipeNodes = 128
	maxTailInstructionRecipeWork  = 256
	maxTailInstructionRecipeDepth = 32
)

// tailInstructionRecipeEligible bounds newly native imported tail recipes. The
// ordinary AMD64 call path keeps its existing admission. This limits the tree
// emitted from a recipe DAG. Shared parents can otherwise multiply a small
// recipe into large code.
// Prepared nodes are topological; fixed arrays keep this check allocation-free.
func tailInstructionRecipeEligible(custom railcore.CustomInstruction) bool {
	if len(custom.Nodes) == 0 || len(custom.Nodes) > maxTailInstructionRecipeNodes || custom.Output < 0 || custom.Output >= len(custom.Nodes) {
		return false
	}
	var work [maxTailInstructionRecipeNodes]uint16
	var depth [maxTailInstructionRecipeNodes]uint8
	for id, node := range custom.Nodes {
		count, height := uint32(1), uint8(1)
		var children [3]int
		n := 0
		switch node.Op {
		case railcore.CustomInstructionInput, railcore.CustomInstructionConst:
		case railcore.CustomInstructionNot, railcore.CustomInstructionIsZero:
			children[0], n = node.A, 1
		case railcore.CustomInstructionSelect:
			children, n = [3]int{node.A, node.B, node.C}, 3
		default:
			if node.Op < railcore.CustomInstructionAdd || node.Op > railcore.CustomInstructionGeS {
				return false
			}
			children[0], children[1], n = node.A, node.B, 2
		}
		for _, child := range children[:n] {
			if child < 0 || child >= id {
				return false
			}
			count += uint32(work[child])
			if depth[child]+1 > height {
				height = depth[child] + 1
			}
		}
		if count > maxTailInstructionRecipeWork || height > maxTailInstructionRecipeDepth {
			return false
		}
		work[id], depth[id] = uint16(count), height
	}
	return true
}

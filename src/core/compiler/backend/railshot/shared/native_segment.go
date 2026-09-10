package shared

// NativeSegmentNode describes trusted compiler control flow, not guest-supplied
// metadata. Work must include the worst-case cost of all instructions/helpers in
// the node. Zero means unknown. A Boundary is a scheduler-safe park, trap, or
// top-level return; an ordinary callee return is NOT a boundary.
//
// This analysis is not wired to native entry/resume admission. In particular,
// Wasm body size is not a conservative helper or continuation work estimate.
type NativeSegmentNode struct {
	Work       uint32
	Successors []uint32
	Boundary   bool
}

const (
	maxNativeSegmentNodes = 4096
	maxNativeSegmentEdges = 8192
	maxNativeSegmentDepth = 128
)

// AnalyzeNativeSegment proves a bound only for an acyclic, fully known graph
// whose every path reaches an explicit safe boundary. Unknown work, missing
// edges, cycles, excessive analysis depth, and exhausted budgets return false.
// It bounds its own time and storage independently of the requested work limit.
func AnalyzeNativeSegment(nodes []NativeSegmentNode, entry, limit uint32) (uint32, bool) {
	if len(nodes) == 0 || len(nodes) > maxNativeSegmentNodes || uint64(entry) >= uint64(len(nodes)) || limit == 0 {
		return 0, false
	}
	edges := 0
	for _, node := range nodes {
		if len(node.Successors) > maxNativeSegmentEdges-edges {
			return 0, false
		}
		edges += len(node.Successors)
	}
	state := make([]uint8, len(nodes))
	work := make([]uint32, len(nodes))
	var visit func(uint32, int) (uint32, bool)
	visit = func(index uint32, depth int) (uint32, bool) {
		if uint64(index) >= uint64(len(nodes)) || depth > maxNativeSegmentDepth || state[index] == 1 {
			return 0, false
		}
		if state[index] == 2 {
			return work[index], true
		}
		node := nodes[index]
		if node.Work == 0 || node.Work > limit {
			return 0, false
		}
		if node.Boundary {
			if len(node.Successors) != 0 {
				return 0, false
			}
			state[index], work[index] = 2, node.Work
			return node.Work, true
		}
		if len(node.Successors) == 0 {
			return 0, false
		}
		state[index] = 1
		var rest uint32
		for _, next := range node.Successors {
			cost, ok := visit(next, depth+1)
			if !ok || cost > limit-node.Work {
				return 0, false
			}
			if cost > rest {
				rest = cost
			}
		}
		state[index], work[index] = 2, node.Work+rest
		return work[index], true
	}
	return visit(entry, 1)
}

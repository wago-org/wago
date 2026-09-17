package railmach

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
)

// ScheduleFreedomMetrics describes the dependency-graph opportunity available
// to a scheduler. ReadyWidthTotal/Steps is the mean source-stable ready width;
// CriticalPathCost uses the selected target latency costs.
type ScheduleFreedomMetrics struct {
	ReadySteps       uint32
	ReadyWidthTotal  uint64
	ReadyWidthMax    uint32
	CriticalPathCost uint64
}

// MeasureScheduleFreedom is deliberately outside the production scheduling
// path. Metrics collection is opt-in, so its temporary successor graph cannot
// affect ordinary compile latency or retained compiler capacity.
func MeasureScheduleFreedom(f *Func, selection *SelectionPlan, dag *DependencyDAG) (ScheduleFreedomMetrics, error) {
	if err := Verify(f); err != nil {
		return ScheduleFreedomMetrics{}, err
	}
	if selection == nil || len(selection.Selections) != len(f.Insts) || dag == nil || len(dag.Offsets) != len(f.Insts)+1 {
		return ScheduleFreedomMetrics{}, fmt.Errorf("railmach: schedule-freedom measurement requires selection and dependency DAG")
	}
	if err := VerifyDependencyDAG(f, dag); err != nil {
		return ScheduleFreedomMetrics{}, err
	}
	n := len(f.Insts)
	indegree := make([]uint32, n)
	successorCount := make([]uint32, n)
	critical := make([]uint64, n)
	blockOf := make([]railssa.BlockID, n)
	for blockID, block := range f.Blocks {
		for instruction := block.InstStart; instruction < block.InstStart+block.InstCount; instruction++ {
			blockOf[instruction] = railssa.BlockID(blockID)
		}
	}
	for instruction := range f.Insts {
		dependencies := dag.Dependencies[dag.Offsets[instruction]:dag.Offsets[instruction+1]]
		indegree[instruction] = uint32(len(dependencies))
		cost := uint64(max(selection.Selections[instruction].Cost.Latency, 1))
		var predecessor uint64
		for _, dependency := range dependencies {
			if int(dependency.Instruction) >= n {
				return ScheduleFreedomMetrics{}, fmt.Errorf("railmach: schedule-freedom dependency is out of range")
			}
			successorCount[dependency.Instruction]++
			predecessor = max(predecessor, critical[dependency.Instruction])
		}
		critical[instruction] = predecessor + cost
	}
	offsets := make([]uint32, n+1)
	for instruction, count := range successorCount {
		offsets[instruction+1] = offsets[instruction] + count
	}
	successors := make([]uint32, offsets[n])
	cursors := append([]uint32(nil), offsets[:n]...)
	for instruction := range f.Insts {
		for _, dependency := range dag.Dependencies[dag.Offsets[instruction]:dag.Offsets[instruction+1]] {
			at := cursors[dependency.Instruction]
			successors[at] = uint32(instruction)
			cursors[dependency.Instruction]++
		}
	}

	var result ScheduleFreedomMetrics
	for _, cost := range critical {
		result.CriticalPathCost = max(result.CriticalPathCost, cost)
	}
	ready := make([]uint32, 0, n)
	for blockID, block := range f.Blocks {
		ready = ready[:0]
		for instruction := block.InstStart; instruction < block.InstStart+block.InstCount; instruction++ {
			if indegree[instruction] == 0 {
				ready = appendMinHeap(ready, instruction)
			}
		}
		for len(ready) != 0 {
			result.ReadySteps++
			result.ReadyWidthTotal += uint64(len(ready))
			result.ReadyWidthMax = max(result.ReadyWidthMax, uint32(len(ready)))
			var instruction uint32
			ready, instruction = popMinHeap(ready)
			for _, successor := range successors[offsets[instruction]:offsets[instruction+1]] {
				indegree[successor]--
				if indegree[successor] == 0 && blockOf[successor] == railssa.BlockID(blockID) {
					ready = appendMinHeap(ready, successor)
				}
			}
		}
	}
	if result.ReadySteps != uint32(n) {
		return ScheduleFreedomMetrics{}, fmt.Errorf("railmach: schedule-freedom graph emitted %d of %d instructions", result.ReadySteps, n)
	}
	return result, nil
}

func appendMinHeap(heap []uint32, value uint32) []uint32 {
	heap = append(heap, value)
	for child := len(heap) - 1; child > 0; {
		parent := (child - 1) / 2
		if heap[parent] <= heap[child] {
			break
		}
		heap[parent], heap[child] = heap[child], heap[parent]
		child = parent
	}
	return heap
}

func popMinHeap(heap []uint32) ([]uint32, uint32) {
	result := heap[0]
	last := heap[len(heap)-1]
	heap = heap[:len(heap)-1]
	if len(heap) == 0 {
		return heap, result
	}
	heap[0] = last
	for parent := 0; ; {
		left := parent*2 + 1
		if left >= len(heap) {
			break
		}
		child := left
		if right := left + 1; right < len(heap) && heap[right] < heap[left] {
			child = right
		}
		if heap[parent] <= heap[child] {
			break
		}
		heap[parent], heap[child] = heap[child], heap[parent]
		parent = child
	}
	return heap, result
}

package shared

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
	"unsafe"
)

var scalarTestRegs = []uint8{1, 2, 3}

type scalarTestTarget struct{}

func (*scalarTestTarget) Registers() ([]uint8, uint64)                        { return scalarTestRegs, 0 }
func (*scalarTestTarget) ParameterRegister(int) (uint8, bool)                 { return 0, false }
func (*scalarTestTarget) LocalOffset(i int) int32                             { return int32(i * 8) }
func (*scalarTestTarget) SpillOffset(i int) int32                             { return int32(i * 8) }
func (*scalarTestTarget) Constant(uint8, int64, bool)                         {}
func (*scalarTestTarget) Move(uint8, uint8, bool)                             {}
func (*scalarTestTarget) NormalizeI32(uint8)                                  {}
func (*scalarTestTarget) Load(uint8, int32, bool)                             {}
func (*scalarTestTarget) Store(int32, uint8, bool)                            {}
func (*scalarTestTarget) Clobbers(IntOp) uint64                               { return 0 }
func (*scalarTestTarget) Operand(IntOp, bool, ScalarOperand) bool             { return true }
func (*scalarTestTarget) Binary(IntOp, bool, uint8, uint8, ScalarOperand)     {}
func (*scalarTestTarget) ScaledAdd(bool, uint8, uint8, uint8, uint8) bool     { return false }
func (*scalarTestTarget) BranchZero(uint8) int                                { return 0 }
func (*scalarTestTarget) BranchCompare(IntOp, bool, uint8, ScalarOperand) int { return 0 }
func (*scalarTestTarget) Jump() int                                           { return 0 }
func (*scalarTestTarget) Patch(int, int) error                                { return nil }
func (*scalarTestTarget) Position() int                                       { return 0 }
func (*scalarTestTarget) Return(uint8, bool, int)                             {}

func scalarNestedBlocks(depth int) []byte {
	var body []byte
	for i := 0; i < depth; i++ {
		body = append(body, 0x02, 0x7f)
	}
	body = append(body, 0x41, 0)
	for i := 0; i <= depth; i++ {
		body = append(body, 0x0b)
	}
	return body
}

func TestScalarControlColdAllocationBudget(t *testing.T) {
	body := scalarNestedBlocks(24)
	summary := AdmitScalar(body, &wasm.CompType{Results: []wasm.ValType{wasm.I32}}, nil)
	if !summary.Eligible {
		t.Fatal("deep fixture rejected")
	}
	target := &scalarTestTarget{}
	allocs := testing.AllocsPerRun(10, func() {
		var s ScalarState
		if _, err := s.CompileScalar(body, summary, nil, 0, target); err != nil {
			panic(err)
		}
	})
	// Node, operand and control backing are the only cold allocations needed
	// for a constant result with no locals. Control nesting must not regrow.
	if allocs > 3 {
		t.Fatalf("cold deep compilation allocates %g times, budget 3", allocs)
	}
}

func TestScalarControlDepthAdmissionAndReuse(t *testing.T) {
	ft := &wasm.CompType{Results: []wasm.ValType{wasm.I32}}
	for _, depth := range []int{0, 1, 24, 32, 33} {
		summary := AdmitScalar(scalarNestedBlocks(depth), ft, nil)
		if summary.Eligible != (depth <= 32) {
			t.Fatalf("depth=%d eligible=%v", depth, summary.Eligible)
		}
		if summary.Eligible && int(summary.MaxControlDepth) != depth {
			t.Fatalf("depth=%d recorded=%d", depth, summary.MaxControlDepth)
		}
	}
	var siblings []byte
	for i := 0; i < 32; i++ {
		siblings = append(siblings, 0x02, 0x40, 0x0b)
	}
	siblings = append(siblings, 0x41, 0, 0x0b)
	if summary := AdmitScalar(siblings, ft, nil); !summary.Eligible || summary.MaxControlDepth != 1 {
		t.Fatalf("siblings summary=%+v", summary)
	}
	var state ScalarState
	target := &scalarTestTarget{}
	allocations := 0
	oldCapacity := 0
	for depth := 1; depth <= 32; depth++ {
		body := scalarNestedBlocks(depth)
		summary := AdmitScalar(body, ft, nil)
		if _, err := state.CompileScalar(body, summary, nil, 0, target); err != nil {
			t.Fatal(err)
		}
		if cap(state.controls) < depth || cap(state.controls) > 32 {
			t.Fatalf("depth=%d capacity=%d", depth, cap(state.controls))
		}
		if cap(state.controls) != oldCapacity {
			allocations++
			oldCapacity = cap(state.controls)
		}
	}
	if allocations > 6 {
		t.Fatalf("increasing depths grew controls %d times, budget 6", allocations)
	}
	first := &state.controls[:1][0]
	for _, depth := range []int{0, 1, 24, 32} {
		body := scalarNestedBlocks(depth)
		summary := AdmitScalar(body, ft, nil)
		if _, err := state.CompileScalar(body, summary, nil, 0, target); err != nil {
			t.Fatal(err)
		}
		if first != &state.controls[:1][0] {
			t.Fatal("control storage not reused")
		}
	}
	memory, discarded := state.Memory(), state.Discarded
	state.FinishWorker()
	if state.Memory() != 0 || state.Discarded != discarded+memory || state.target != nil || state.regs != nil {
		t.Fatal("worker cleanup lost storage accounting")
	}
}

func TestScalarSummaryDepthFitsExistingLayout(t *testing.T) {
	type previousSummary struct {
		Eligible, HasIf bool
		MaxStack, Nodes int
	}
	if unsafe.Sizeof(ScalarSummary{}) != unsafe.Sizeof(previousSummary{}) {
		t.Fatalf("summary grew from %d to %d", unsafe.Sizeof(previousSummary{}), unsafe.Sizeof(ScalarSummary{}))
	}
}

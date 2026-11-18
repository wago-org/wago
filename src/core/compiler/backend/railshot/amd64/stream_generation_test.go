//go:build (linux || darwin || windows) && amd64 && !tinygo && !wago_profile

package amd64

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"reflect"
	"testing"
)

func streamGenerationTestEnable(t *testing.T) {
	t.Helper()
	streamResidencyEnable(t)
	a, b := streamingResidencyEnabled, streamingCopyProofEnabled
	streamingResidencyEnabled, streamingCopyProofEnabled = true, true
	t.Cleanup(func() { streamingResidencyEnabled, streamingCopyProofEnabled = a, b })
}
func TestStreamGenerationMoveAndSpill(t *testing.T) {
	streamGenerationTestEnable(t)
	for _, frame := range []bool{false, true} {
		t.Run(map[bool]string{false: "register-backing", true: "frame-backing"}[frame], func(t *testing.T) {
			s := streamResidencyUnitState()
			id := s.locals[0]
			cell := s.residency.homes[0]
			if frame {
				s.evacuateBacking(cell)
			}
			refs := s.residency.cells[cell].refs
			r := s.materialize(id, 0)
			if s.residency.cells[cell].refs != refs {
				t.Fatal("materializing dropped generation proof")
			}
			moved := s.materialize(id, uint64(1)<<r)
			if moved == r || s.owners[r] != 0 || s.owners[moved] != id || s.residency.cells[cell].refs != refs {
				t.Fatal("register move lost proof or ownership")
			}
			before, slots, spills := s.target.a.Len(), s.nextSlot, s.Spills
			s.spill(id)
			if s.target.a.Len() != before || s.nextSlot != slots || s.Spills != spills || s.node(id).kind != streamBacking || uint32(s.node(id).slot) != cell || s.residency.cells[cell].refs != refs || s.owners[moved] != 0 {
				t.Fatal("immutable proof spill must return to existing cell without store")
			}
		})
	}
}
func TestStreamGenerationCopiedAliasBlocksSink(t *testing.T) {
	streamGenerationTestEnable(t)
	s := streamResidencyUnitState()
	cell := s.residency.homes[0]
	old := s.locals[0]
	alias := s.borrowBacking(cell, 0)
	s.materialize(alias, 0)
	s.retain(old)
	rhs := s.add(streamNode{kind: shared.ScalarConstant, constant: 2, refs: 1})
	expr := s.add(streamNode{kind: streamDeferred, op: shared.IntAdd, left: old, right: rhs, refs: 1})
	if s.node(old).refs != 2 || s.residency.cells[cell].refs != 3 {
		t.Fatal("ordinary copied alias failed to retain a generation reference")
	}
	nodes := append([]streamNode{}, s.nodes...)
	cells := append([]streamBackingCell{}, s.residency.cells...)
	code := append([]byte{}, s.target.a.B...)
	if s.tryHomeSet(0, expr, old) {
		t.Fatal("ordinary copied alias must prevent destructive home sink")
	}
	if !reflect.DeepEqual(nodes, s.nodes) || !reflect.DeepEqual(cells, s.residency.cells) || !bytes.Equal(code, s.target.a.B) {
		t.Fatal("rejected sink mutated state")
	}
	s.release(alias)
	if s.residency.cells[cell].refs != 2 {
		t.Fatal("ordinary alias final release failed")
	}
}
func TestStreamGenerationDestructiveResultDropsProof(t *testing.T) {
	streamGenerationTestEnable(t)
	s := streamResidencyUnitState()
	cell := s.residency.homes[0]
	old := s.locals[0]
	s.locals[0] = 0
	s.materialize(old, 0)
	rhs := s.add(streamNode{kind: shared.ScalarConstant, constant: 3, refs: 1})
	expr := s.add(streamNode{kind: streamDeferred, op: shared.IntAdd, left: old, right: rhs, refs: 1})
	s.materialize(expr, 0)
	if s.residency.cells[cell].refs != 1 {
		t.Fatalf("destructive expression retained obsolete proof refs%d", s.residency.cells[cell].refs)
	}
	before := s.target.a.Len()
	s.spill(expr)
	if s.node(expr).kind != shared.ScalarFrame || s.target.a.Len() == before {
		t.Fatal("changed result incorrectly reused immutable old backing")
	}
	s.release(expr)
	s.releaseBacking(cell)
	s.residency.homes[0] = 0
}
func TestStreamGenerationFrameLastReference(t *testing.T) {
	streamGenerationTestEnable(t)
	s := streamResidencyUnitState()
	id := s.locals[0]
	cell := s.residency.homes[0]
	s.evacuateBacking(cell)
	slot := int(s.residency.cells[cell].slot)
	s.materialize(id, 0)
	s.releaseBacking(cell)
	s.residency.homes[0] = 0
	if s.residency.cells[cell].refs != 1 || len(s.freeSlots) != 0 {
		t.Fatal("ordinary proof must own final frame backing")
	}
	before := s.target.a.Len()
	s.spill(id)
	if s.target.a.Len() != before || len(s.freeSlots) != 0 {
		t.Fatal("return to backing freed or duplicated its slot")
	}
	s.release(id)
	s.locals[0] = 0
	if len(s.freeSlots) != 1 || s.freeSlots[0] != slot {
		t.Fatal("last retained generation did not free exactly once")
	}
	if got := s.slot(); got != slot {
		t.Fatal("freed generation slot not reused")
	}
	if next := s.slot(); next == slot {
		t.Fatal("duplicate generation free")
	}
}

func TestStreamGenerationDirtyMarker(t *testing.T) {
	streamGenerationTestEnable(t)
	s := streamResidencyUnitState()
	id := s.locals[0]
	cell := s.residency.homes[0]
	reg := s.materialize(id, 0)
	s.node(id).constant |= 1
	if s.registerBacking(id) != cell || streamNodeBacking(*s.node(id)) != cell {
		t.Fatal("dirty marker corrupted packed generation ID")
	}
	reg = s.materialize(id, 1<<reg)
	if s.registerBacking(id) != cell || s.node(id).constant&1 != 0 {
		t.Fatal("normalizing move dropped generation or retained dirty flag")
	}
	s.node(id).constant |= 1
	refs := s.residency.cells[cell].refs
	s.forgetRegisterBacking(reg)
	if s.node(id).constant != 1 || s.registerBacking(id) != 0 || s.residency.cells[cell].refs != refs-1 {
		t.Fatal("destructive invalidation must preserve only dirty marker and release once")
	}
	s.forgetRegisterBacking(reg)
	if s.residency.cells[cell].refs != refs-1 {
		t.Fatal("proof invalidation released twice")
	}
}
func TestStreamGenerationRuntimeSnapshots(t *testing.T) {
	streamGenerationTestEnable(t)
	t.Run("copied-old-aliases-and-dirty-i32", TestStreamResidencyRuntimeSnapshots)
	t.Run("ordinary-and-fixed-pressure", TestStreamResidencyPressure)
	t.Run("memory-pressure", TestStreamResidencyMemoryPressure)
	t.Run("restored-dead-path", TestStreamResidencyDeadPathRestore)
	t.Run("wide-cycle", TestStreamResidencyWideCycle)
	t.Run("stable-branch-snapshots", TestStreamResidencyStableBranchSnapshots)
}

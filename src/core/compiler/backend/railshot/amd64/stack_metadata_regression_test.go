//go:build amd64

package amd64

import (
	"math/rand"
	"testing"
)

func TestConstantFoldingPreservesLogicalRootAndPrefix(t *testing.T) {
	for _, tc := range []struct {
		name   string
		op     wOp
		input  machineType
		output machineType
		left   int64
		right  int64
		want   int64
	}{
		{"i32-wrap", opAdd, mtI32, mtI32, 0x7fffffff, 1, -0x80000000},
		{"i64-add", opAdd, mtI64, mtI64, 0x100000000, 3, 0x100000003},
		{"i64-compare", opEq, mtI64, mtI32, 0x100000000, 0x100000000, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := fn{s: newStack()}
			prefix := f.pushValue(storage{kind: stSlot, typ: mtI64})
			f.markGCReference(prefix)
			left := f.pushValue(storage{kind: stConst, typ: tc.input, cval: tc.left})
			f.pushValue(storage{kind: stConst, typ: tc.input, cval: tc.right})
			f.pushBinOp(tc.op, tc.input)
			if f.depth() != 2 || f.s.back() != left || left.prev != prefix || prefix.prev != f.s.head {
				t.Fatal("fold changed the logical prefix or failed to reuse its root")
			}
			if left.st.kind != stConst || left.st.typ != tc.output || left.st.cval != tc.want || !left.st.hasLogicalRoot() {
				t.Fatalf("folded storage = %+v", left.st)
			}
			if !prefix.st.hasGCRoot() || !prefix.st.hasLogicalRoot() || !f.s.hasGCRoots {
				t.Fatal("fold lost the prefix GC root")
			}
		})
	}
}

// Maintain a separate logical operand model while changing the physical
// deferred-expression list. In particular, deleting expression children must
// not reduce depth, and exposing a peeled child must restore its root status.
func TestIncrementalStackMetadataMatchesReconstruction(t *testing.T) {
	for seed := int64(0); seed < 32; seed++ {
		rng := rand.New(rand.NewSource(seed))
		f := fn{s: newStack()}
		var roots []*elem
		for step := 0; step < 512; step++ {
			op := rng.Intn(5)
			if len(roots) == 0 || op == 0 {
				st := storage{kind: stConst, typ: mtI64}
				st.setGCRoot(rng.Intn(4) == 0)
				roots = append(roots, f.s.pushValue(st))
			} else if op == 1 || op == 2 && len(roots) >= 2 {
				arity := 1
				if op == 2 {
					arity = 2
				}
				node := f.s.alloc()
				node.setElemKind(ekDeferred)
				node.setValueType(mtI64)
				node.arg0 = roots[len(roots)-arity]
				if arity == 2 {
					node.arg1 = roots[len(roots)-1]
				}
				f.s.pushDeferred(node)
				roots = append(roots[:len(roots)-arity], node)
			} else {
				top := roots[len(roots)-1]
				if op == 3 && top.isDeferred() && top.arg1 == nil {
					f.s.erase(top)
					f.s.exposeLogicalRoot(top.arg0)
					roots[len(roots)-1] = top.arg0
				} else {
					// Remove the whole top expression, including its non-root nodes.
					stop := baseOfValentBlock(top).prev
					for node := top; node != stop; {
						previous := node.prev
						f.s.erase(node)
						node = previous
					}
					roots = roots[:len(roots)-1]
				}
			}
			if f.depth() != len(roots) {
				t.Fatalf("seed %d step %d: depth=%d, reconstructed=%d", seed, step, f.depth(), len(roots))
			}
			for i, root := range roots {
				if !root.st.hasLogicalRoot() || f.logicalStackRootAtDepth(i+1) != root {
					t.Fatalf("seed %d step %d: stale logical root %d", seed, step, i)
				}
				if root.isValue() && root.st.hasGCRoot() && !f.s.hasGCRoots {
					t.Fatal("GC root omitted from conservative stack metadata")
				}
			}
		}
	}
}

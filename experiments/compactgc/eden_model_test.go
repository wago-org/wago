//go:build wago_gc_compact_eden && !wago_gc_compact_cohort && !wago_gc_compact_shadow && !wago_gc_compact_age2_metrics && !wago_gc_compact_age2_queue_reuse && !wago_gc_compact8

package compactgc

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"sort"
	"testing"
)

// IDs and edges live outside the collector. Only root references survive a
// collection; all other usable handles are rediscovered by traversing the heap.
// Unreachable old objects may conservatively remain, so the oracle constrains
// the reachable graph and lifetime allocations, not immediate garbage removal.
func TestEdenRandomizedGraphModel(t *testing.T) {
	for _, geometry := range [][2]uint32{{128, 64}, {512, 16}, {64, 128}} {
		for _, seed := range []int64{1, 0x5eed} {
			t.Run(fmt.Sprintf("eden%d-survivor%d-seed%d", geometry[0], geometry[1], seed), func(t *testing.T) {
				c := edenHeap(t, geometry[0], geometry[1], 16384)
				rng := rand.New(rand.NewSource(seed))
				type modelNode struct{ edges [2]uint32 }
				model := map[uint32]modelNode{}
				const rootCount = 8
				roots := make([]Ref, rootCount)
				rootIDs := make([]uint32, rootCount)
				rootPointers := make([]*Ref, rootCount+1)
				for i := range roots {
					rootPointers[i] = &roots[i]
				}
				rootPointers[rootCount] = &roots[0] // Deliberately duplicate a mutable root slot.
				var nextID uint32 = 1
				step := -1
				verify := func() map[uint32]Ref {
					t.Helper()
					type pending struct {
						id  uint32
						ref Ref
					}
					todo := make([]pending, 0)
					for i, id := range rootIDs {
						todo = append(todo, pending{id, roots[i]})
					}
					seen := map[uint32]Ref{}
					inverse := map[Ref]uint32{}
					for len(todo) > 0 {
						p := todo[len(todo)-1]
						todo = todo[:len(todo)-1]
						if p.id == 0 {
							if p.ref != 0 {
								t.Fatalf("step%d: null edge/root became%x", step, p.ref)
							}
							continue
						}
						want, ok := model[p.id]
						if !ok {
							t.Fatalf("step%d: unknown model ID%d", step, p.id)
						}
						if old, ok := seen[p.id]; ok {
							if old != p.ref {
								t.Fatalf("step%d: ID%d alias split%x/%x", step, p.id, old, p.ref)
							}
							continue
						}
						if old, ok := inverse[p.ref]; ok && old != p.id {
							t.Fatalf("step%d: IDs%d/%d alias%x", step, old, p.id, p.ref)
						}
						if !p.ref.IsObj() || !c.IsValid(p.ref) {
							t.Fatalf("step%d: ID%d invalid ref%x", step, p.id, p.ref)
						}
						typ, err := c.ObjectType(p.ref)
						if err != nil || typ != 0 {
							t.Fatalf("step%d: ID%d type%d err%v", step, p.id, typ, err)
						}
						payload, err := c.Payload(p.ref)
						if err != nil {
							t.Fatal(err)
						}
						if got := binary.LittleEndian.Uint32(payload[8:12]); got != p.id {
							t.Fatalf("step%d: immutable ID changed: got%d want%d", step, got, p.id)
						}
						seen[p.id] = p.ref
						inverse[p.ref] = p.id
						for i, id := range want.edges {
							r, err := c.RefField(p.ref, uint32(i))
							if err != nil {
								t.Fatal(err)
							}
							todo = append(todo, pending{id, r})
						}
					}
					allocations, live := c.AllocationSnapshot()
					if allocations != uint64(nextID-1) || live < uint32(len(seen)) {
						t.Fatalf("step%d: accounting allocations%d want%d live%d reachable%d", step, allocations, nextID-1, live, len(seen))
					}
					return seen
				}
				refs := verify()
				collect := func(full bool) {
					t.Helper()
					var err error
					if full {
						err = c.Full(rootPointers)
					} else {
						err = c.Minor(rootPointers)
					}
					if err == ErrOOM && !full {
						refs = verify() // Failed preflight must leave every reachable edge intact.
						err = c.Full(rootPointers)
					}
					if err != nil {
						t.Fatalf("step%d: collection full%v: %v", step, full, err)
					}
					refs = verify()
				}
				allocate := func() (uint32, Ref) {
					t.Helper()
					r, err := c.NewStruct(0)
					if err == ErrOOM {
						collect(false)
						r, err = c.NewStruct(0)
					}
					if err != nil {
						t.Fatalf("step%d: allocation: %v", step, err)
					}
					id := nextID
					nextID++
					var bytes [4]byte
					binary.LittleEndian.PutUint32(bytes[:], id)
					if err = c.WriteBytes(r, 8, bytes[:]); err != nil {
						t.Fatal(err)
					}
					model[id] = modelNode{}
					return id, r
				}
				ids := func() []uint32 {
					out := make([]uint32, 0, len(refs))
					for id := range refs {
						out = append(out, id)
					}
					sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
					return out
				}
				oldToYoung := 0
				setEdge := func(parent uint32, index int, child uint32, childRef Ref) {
					t.Helper()
					p := refs[parent]
					if !c.IsYoung(p) && c.IsYoung(childRef) {
						oldToYoung++
					}
					if err := c.SetRef(p, uint32(index), childRef); err != nil {
						t.Fatalf("step%d: set ID%d[%d]=%d: %v", step, parent, index, child, err)
					}
					n := model[parent]
					n.edges[index] = child
					model[parent] = n
				}
				// Force an old-to-Eden edge before randomized operations, rather than
				// relying on a seed merely happening to exercise the remembered barrier.
				rootIDs[0], roots[0] = allocate()
				refs = verify()
				collect(false)
				collect(false)
				id, r := allocate()
				setEdge(rootIDs[0], 0, id, r)
				refs = verify()
				for step = 0; step < 600; step++ {
					reachable := ids()
					switch rng.Intn(6) {
					case 0, 1:
						id, r := allocate()
						if len(reachable) > 0 && rng.Intn(2) == 0 {
							parent := reachable[rng.Intn(len(reachable))]
							setEdge(parent, rng.Intn(2), id, r)
						} else {
							i := rng.Intn(rootCount)
							rootIDs[i], roots[i] = id, r
						}
					case 2:
						if len(reachable) > 0 {
							parent := reachable[rng.Intn(len(reachable))]
							var child uint32
							if rng.Intn(3) != 0 {
								child = reachable[rng.Intn(len(reachable))]
							}
							setEdge(parent, rng.Intn(2), child, refs[child])
						}
					case 3:
						i := rng.Intn(rootCount)
						if len(reachable) > 0 && rng.Intn(3) == 0 {
							id := reachable[rng.Intn(len(reachable))]
							rootIDs[i], roots[i] = id, refs[id]
						} else {
							rootIDs[i], roots[i] = 0, 0
						}
					case 4:
						collect(false)
					case 5:
						collect(true)
					}
					refs = verify()
				}
				collect(false)
				collect(false)
				collect(true)
				if oldToYoung == 0 {
					t.Fatal("old-to-young barrier not exercised")
				}
			})
		}
	}
}

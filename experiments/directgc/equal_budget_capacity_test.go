package directgc

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"testing"
)

// Opt-in capacity qualification, not a benchmark. Identical rooted graphs are
// attempted for each split; explicit collections make capacity boundaries clear.
func TestEqualBudgetOldCapacity(t *testing.T) {
	if os.Getenv("GC_EQUAL_CAPACITY") != "1" {
		t.Skip("set GC_EQUAL_CAPACITY=1")
	}
	for _, split := range [][2]uint32{{2, 8}, {4, 7}, {6, 6}} {
		for _, boundary := range []uint32{6 << 20, 7 << 20, 8 << 20} {
			for _, delta := range []int32{-16, 0, 16} {
				target := uint32(int64(boundary) + int64(delta))
				c, err := NewWithTypes(split[0]<<20, split[1]<<20, []Descriptor{{Kind: KindArray, Align: 8, ElementSize: 4, ElementRef: true}, {Kind: KindArray, Align: 8, ElementSize: 1}})
				if err != nil {
					t.Fatal(err)
				}
				root, err := c.NewArray(0, 64)
				if err != nil {
					t.Fatal(err)
				}
				roots := []*Ref{&root}
				if err = c.Minor(roots); err != nil {
					t.Fatal(err)
				}
				live := uint32(272) // 16-byte header plus 64 reference slots.
				lengths := []uint32{}
				verify := func() {
					t.Helper()
					for i, n := range lengths {
						r, e := c.RefField(root, uint32(i))
						if e != nil {
							t.Fatal(e)
						}
						p, e := c.Payload(r)
						if e != nil || uint32(len(p)) != n {
							t.Fatalf("payload length %d want %d err %v", len(p), n, e)
						}
						for off := 0; off+4 <= len(p); off += 4 {
							if binary.LittleEndian.Uint32(p[off:]) != uint32(i+1)^uint32(off) {
								t.Fatalf("corrupt child %d offset %d", i, off)
							}
						}
					}
				}
				minor, full, oom := 1, 0, false
				attempted := live
				for live < target {
					stride := min(uint32(256<<10), target-live)
					// Target remainders here exceed a header and are 16-aligned.
					if stride <= 16 {
						t.Fatal("invalid graph remainder", stride)
					}
					child, e := c.NewArray(1, stride-16)
					if e != nil {
						t.Fatalf("unexpected nursery allocation failure: %v", e)
					}
					p, e := c.Payload(child)
					if e != nil {
						t.Fatal(e)
					}
					i := len(lengths)
					for off := 0; off+4 <= len(p); off += 4 {
						binary.LittleEndian.PutUint32(p[off:], uint32(i+1)^uint32(off))
					}
					if e = c.SetRef(root, uint32(i), child); e != nil {
						t.Fatal(e)
					}
					lengths = append(lengths, stride-16)
					attempted = live + stride
					beforeRoot := root
					beforeOld := c.oldBump
					e = c.Minor(roots)
					minor++
					if attempted > split[1]<<20 {
						if !errors.Is(e, ErrOOM) {
							t.Fatalf("expected capacity OOM at %d: %v", attempted, e)
						}
						oom = true
						if root != beforeRoot || c.oldBump != beforeOld {
							t.Fatal("failed Minor changed root/old cursor")
						}
						verify()
						e = c.Full(roots)
						full++
						if !errors.Is(e, ErrOOM) || root != beforeRoot || c.oldBump != beforeOld {
							t.Fatalf("failed Full was not atomic: %v", e)
						}
						verify()
						if e = c.SetRef(root, uint32(i), 0); e != nil {
							t.Fatal(e)
						}
						lengths = lengths[:i]
						if e = c.Full(roots); e != nil {
							t.Fatal("OOM recovery", e)
						}
						full++
						if root == beforeRoot {
							t.Fatal("recovery Full did not move root")
						}
						verify()
						break
					}
					if e != nil {
						t.Fatalf("unexpected collection failure below capacity: %v", e)
					}
					live = attempted
					verify()
				}
				if oom != (target > split[1]<<20) {
					t.Fatalf("capacity verdict mismatch target %d", target)
				}
				for j := 0; j < 2; j++ {
					before := root
					if e := c.Full(roots); e != nil {
						t.Fatal(e)
					}
					full++
					if root == before {
						t.Fatal("Full did not move graph")
					}
					verify()
				}
				if c.oldBump-c.oldStart[c.active] != live {
					t.Fatal("live accounting mismatch")
				}
				// After dropping the graph, reclaim it and prove allocation/collection retry.
				root = 0
				if e := c.Full(roots); e != nil {
					t.Fatal(e)
				}
				full++
				if c.oldBump != c.oldStart[c.active] {
					t.Fatal("dropped graph remained live")
				}
				root, err = c.NewArray(1, 16)
				if err != nil {
					t.Fatal(err)
				}
				if err = c.Minor(roots); err != nil {
					t.Fatal(err)
				}
				minor++
				receipt, _ := json.Marshal(map[string]any{"young_mib": split[0], "each_old_mib": split[1], "target_live_bytes": target, "last_attempted_live_bytes": attempted, "retained_after_recovery_bytes": live, "expected_capacity_oom": oom, "minor_calls": minor, "full_calls": full, "fixed_bytes": c.FixedBytes(), "checksum_verified": true, "recovery_verified": true})
				t.Log("EQUAL_CAPACITY_RESULT " + string(receipt))
			}
		}
	}
}

package gc

import "testing"

func TestDeadReservationHelpersAllocateAndPollIndividually(t *testing.T) {
	structDesc, err := NewStructDesc(0, []StorageKind{StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	arrayDesc, err := NewArrayDesc(1, StorageI32)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		reserve func(*Collector, uint32, func()) error
	}{
		{"struct", func(c *Collector, count uint32, poll func()) error {
			return c.ReserveDeadStructAllocationsWithPoll(0, count, EmptyRoots{}, poll)
		}},
		{"array", func(c *Collector, count uint32, poll func()) error {
			return c.ReserveDeadArrayAllocationsWithPoll(1, 4, count, EmptyRoots{}, poll)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, count := range []uint32{0, 1, 7} {
				c, err := NewCollector(Config{StressNurseryBytes: 64}, []TypeDesc{structDesc, arrayDesc})
				if err != nil {
					t.Fatal(err)
				}
				polls := uint32(0)
				err = tc.reserve(c, count, func() {
					if got := c.Stats().Allocations; got != uint64(polls) {
						t.Fatalf("count=%d poll=%d observed %d prior allocations", count, polls, got)
					}
					polls++
				})
				if err != nil {
					t.Fatalf("count=%d: %v", count, err)
				}
				if polls != count || c.Stats().Allocations != uint64(count) {
					t.Fatalf("count=%d polls=%d allocations=%d", count, polls, c.Stats().Allocations)
				}
				c.Close()
			}
		})
	}
}

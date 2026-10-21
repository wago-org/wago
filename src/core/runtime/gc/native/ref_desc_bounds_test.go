package gc

import "testing"

func TestRefDescRejectsInvalidObjectExtent(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cfg     Config
		space   spaceKind
		promote bool
	}{
		{"nursery", Config{NurseryBytes: 4096, SurvivorBytes: 1024}, spaceNursery, false},
		{"survivor", Config{NurseryBytes: 4096, SurvivorBytes: 1024}, spaceNursery, false},
		{"old", Config{NurseryBytes: 4096, SurvivorBytes: 1024}, spaceOld, true},
		{"large", Config{LargeObjectBytes: 16}, spaceLarge, false},
		{"tiny", Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, spaceTiny, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestCollector(t, tc.cfg)
			ref, err := c.NewStructDefault(0)
			if err != nil {
				t.Fatal(err)
			}
			if tc.promote {
				if err := c.ForcePromote(ref); err != nil {
					t.Fatal(err)
				}
			}
			if tc.name == "survivor" {
				oldOffset := c.handles[handleOf(ref)].off
				if err := c.CollectMinor(RefSliceRoots{ref}); err != nil {
					t.Fatal(err)
				}
				if c.survivorBump == 0 || c.handles[handleOf(ref)].off == oldOffset {
					t.Fatal("fixture must move into survivor space")
				}
			}
			h := handleOf(ref)
			saved := c.handles[h]
			defer func() { c.handles[h] = saved }()
			if saved.space != tc.space {
				t.Fatalf("fixture space=%d, want%d", saved.space, tc.space)
			}
			check := func(name string) {
				t.Helper()
				ok, err := c.RefTest(ref, RefTestTarget{Kind: RefTestAny})
				if ok || err == nil || err.Error() != "gc: invalid object ref" {
					t.Fatalf("%s space=%d offset=%d size=%d: %v,%v; want diagnostic invalid object ref", name, c.handles[h].space, c.handles[h].off, c.handles[h].size, ok, err)
				}
			}
			for space := 0; space <= 255; space++ {
				if space >= 1 && space <= 4 {
					continue
				}
				c.handles[h] = saved
				c.handles[h].space = spaceKind(space)
				check("invalid space")
			}
			for _, bad := range []struct {
				name      string
				off, size uint32
			}{
				{"empty", saved.off, 0}, {"short header", saved.off, HeaderSize - 1},
				{"oversize", saved.off, ^uint32(0)}, {"offset beyond space", ^uint32(0), saved.size},
				{"offset plus size overflow", ^uint32(0) - HeaderSize + 1, HeaderSize},
			} {
				c.handles[h] = saved
				c.handles[h].off, c.handles[h].size = bad.off, bad.size
				check(bad.name)
			}
			c.handles[h] = saved
			ok, err := c.RefTest(ref, RefTestTarget{Kind: RefTestAny})
			if err != nil || !ok {
				t.Fatalf("restored ref=%v,%v", ok, err)
			}
		})
	}
}

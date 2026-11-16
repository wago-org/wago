package gc

import "testing"

func TestCollectorProfileOwnsOnlyRequiredColorMetadata(t *testing.T) {
	desc, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range []Profile{ProfileThroughput, ProfileTiny} {
		c, err := NewCollector(Config{Profile: profile}, []TypeDesc{desc})
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 128; i++ {
			if _, err := c.NewStructDefault(0); err != nil {
				t.Fatal(err)
			}
		}
		if profile == ProfileTiny {
			if len(c.tinyGC.color) != len(c.handles) {
				t.Fatal("Tiny must retain one color per handle")
			}
		} else {
			if len(c.tinyGC.color) != 0 || cap(c.tinyGC.color) != 0 {
				t.Fatal("Throughput allocated unused Tiny colors")
			}
			if !c.PrepareNativeStructHandles() {
				t.Fatal("native reservation failed")
			}
			if len(c.tinyGC.color) != 0 || cap(c.tinyGC.color) != 0 {
				t.Fatal("native reservation allocated unused Tiny colors")
			}
		}
		if err := c.CollectFull(EmptyRoots{}); err != nil {
			t.Fatal(err)
		}
		if err := c.Verify(EmptyRoots{}); err != nil {
			t.Fatal(err)
		}
		c.Close()
	}
}

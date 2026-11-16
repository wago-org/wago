//go:build amd64 || arm64

package gc

import "testing"

func TestSparseMajorMarksNativeAndYoungTransitions(t *testing.T) {
	c := newMajorBlocksEmptyCollector(t, 8191, false)
	if !c.PrepareNativeStructHandles() {
		t.Fatal("native reservation failed")
	}
	roots := publishMajorBlocksNative(c, 8)
	if err := c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	if c.majorYoungDirty {
		t.Fatal("native ledger must be synchronized before resetting marks")
	}
	// Full collection leaves survivors marked. A minor collection must reset
	// those marks, then the next full collection must not resurrect a root
	// dropped between the two collections.
	if err := c.CollectMinor(roots[:1]); err != nil {
		t.Fatal(err)
	}
	if err := c.CollectFull(roots[:1]); err != nil {
		t.Fatal(err)
	}
	if got := c.Stats().LiveObjects; got != 1 {
		t.Fatalf("live objects = %d, want 1", got)
	}
	if err := c.CollectFull(EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
	if got := c.Stats().LiveObjects; got != 0 {
		t.Fatalf("stale full mark retained %d objects", got)
	}
}

func TestSparseMajorMarksResetOnlyOccupiedBlocks(t *testing.T) {
	c := newMajorBlocksEmptyCollector(t, 8191, false)
	r, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.CollectFull(RefSliceRoots{r}); err != nil {
		t.Fatal(err)
	}
	h := handleOf(r)
	if !c.mark[h] {
		t.Fatal("full collection did not mark live object")
	}
	c.clearMarks()
	if c.mark[h] || len(c.markStack) != 0 {
		t.Fatal("sparse reset retained traversal state")
	}
	if err := c.CollectFull(EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
	if got := c.Stats().LiveObjects; got != 0 {
		t.Fatalf("live objects = %d, want 0", got)
	}
}

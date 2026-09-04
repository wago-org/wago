//go:build wago_gcstats

package gc

import "testing"

func TestSparseCardTelemetryIsBoundedByDirtyCards(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := NewArrayDesc(1, StorageRefNull)
	if err != nil {
		t.Fatal(err)
	}
	telemetry := new(Telemetry)
	c, err := NewCollector(Config{NurseryBytes: 2 << 20, ThroughputHeapBytes: 8 << 20, Telemetry: telemetry}, []TypeDesc{leaf, refs})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	const length = uint32(256 << 10)
	array, err := c.NewArrayDefault(1, length)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ForcePromote(array); err != nil {
		t.Fatal(err)
	}
	first, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	last, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if !c.ResetTelemetry() {
		t.Fatal("telemetry reset failed")
	}
	for i := 0; i < 8; i++ {
		if err := c.ArraySet(array, 0, RefValue(first)); err != nil {
			t.Fatal(err)
		}
		if err := c.ArraySet(array, length-1, RefValue(last)); err != nil {
			t.Fatal(err)
		}
	}
	root := Root(array)
	if err := c.CollectMinor(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	snapshot, ok := c.TelemetrySnapshot()
	if !ok {
		t.Fatal("telemetry unavailable")
	}
	cards := snapshot.Minor.Cards
	if cards.DirtyObjectCards != 2 || cards.UsefulObjectCards != 2 {
		t.Fatalf("sparse card counts = %+v", cards)
	}
	if cards.ScannedSlots > 64 {
		t.Fatalf("sparse card scan visited %d slots, want at most 64", cards.ScannedSlots)
	}
	if cards.WholeObjectScans != 0 || cards.WholeObjectScansAvoided != 1 {
		t.Fatalf("sparse whole-object telemetry = %+v", cards)
	}
	if cards.DuplicateDirties < 14 {
		t.Fatalf("duplicate dirty count = %d, want at least 14", cards.DuplicateDirties)
	}
}

func TestDirtyRootTelemetryDoesNotScaleWithCleanSlots(t *testing.T) {
	telemetry := new(Telemetry)
	c := newTestCollector(t, Config{StressNurseryBytes: 1 << 20, Telemetry: telemetry})
	for i := 0; i < 4096; i++ {
		c.NewGlobalSlot(Null())
	}
	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if !c.ResetTelemetry() {
		t.Fatal("telemetry reset failed")
	}
	if err := c.SetGlobalSlot(2048, child); err != nil {
		t.Fatal(err)
	}
	if err := c.CollectMinor(nil); err != nil {
		t.Fatal(err)
	}
	snapshot, ok := c.TelemetrySnapshot()
	if !ok {
		t.Fatal("telemetry unavailable")
	}
	if got := snapshot.Minor.Cards.DirtyRootCards; got != 1 {
		t.Fatalf("dirty root cards=%d, want 1", got)
	}
	if got := snapshot.Minor.Cards.UsefulRootCards; got != 1 {
		t.Fatalf("useful root cards=%d, want 1", got)
	}
	if got := snapshot.Minor.Roots.Globals; got != 1 {
		t.Fatalf("enumerated global roots=%d, want 1", got)
	}
}

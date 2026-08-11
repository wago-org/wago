//go:build wago_gcstats

package gc

import "testing"

func TestTinyPartialScanTelemetry(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := NewArrayDesc(1, StorageRefNull)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{
		Profile:        ProfileTiny,
		TinyHeapBytes:  64 << 10,
		TinyBlockBytes: 16,
		Telemetry:      new(Telemetry),
	}, []TypeDesc{leaf, refs})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	array, err := c.NewArrayDefault(1, 1024)
	if err != nil {
		t.Fatal(err)
	}
	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	for i := uint32(0); i < 1024; i++ {
		if err := c.ArraySet(array, i, RefValue(child)); err != nil {
			t.Fatal(err)
		}
	}
	root := Root(array)
	if err := c.CollectFull(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	snapshot, ok := c.TelemetrySnapshot()
	if !ok {
		t.Fatal("telemetry unavailable")
	}
	trace := snapshot.Full.Trace
	if trace.ObjectsVisited != 2 || trace.ScanEntriesVisited != 1024 || trace.ReferenceSlotsVisited != 1024 || trace.PayloadBytesVisited != 4096 {
		t.Fatalf("partial trace totals = %+v", trace)
	}
	if trace.ObjectScansBegun != 2 || trace.ObjectScansResumed != 3 || trace.ObjectScansCompleted != 2 {
		t.Fatalf("partial lifecycle counters = %+v", trace)
	}
	if trace.MaxStepObjectRanges > uint64(tinyStepObjectRanges) || trace.MaxStepScanEntries != uint64(tinyStepScanEntries) || trace.MaxStepReferenceSlots != uint64(tinyStepRefSlots) || trace.MaxStepPayloadBytes != uint64(tinyStepPayloadBytes) {
		t.Fatalf("partial step maxima = %+v", trace)
	}
}

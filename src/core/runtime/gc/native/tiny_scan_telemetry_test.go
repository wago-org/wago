//go:build wago_gcstats && !wago_tiny_nonincremental

package gc

import "testing"

func TestTinyTelemetryCollectFullRestartsPartialEpoch(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := NewArrayDesc(1, StorageRefNull)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{Profile: ProfileTiny, TinyHeapBytes: 1 << 20, TinyBlockBytes: 16, Telemetry: new(Telemetry)}, []TypeDesc{leaf, refs})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	partial, err := c.NewArrayDefault(1, tinyStepScanEntries*2)
	if err != nil {
		t.Fatal(err)
	}
	keep, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	partialRoot := Root(partial)
	if err := c.Step(Slots{&partialRoot}); err != nil {
		t.Fatal(err)
	}
	if err := c.Step(Slots{&partialRoot}); err != nil {
		t.Fatal(err)
	}
	if c.tinyGC.scan.handle != handleOf(partial) {
		t.Fatal("test did not establish a partial scan")
	}
	keepRoot := Root(keep)
	if err := c.CollectFull(Slots{&keepRoot}); err != nil {
		t.Fatal(err)
	}
	if c.tinyGC.telemetryOwned || c.cfg.Telemetry.active.active {
		t.Fatal("synchronous restart left Tiny telemetry active")
	}
	if !c.validObjectRef(keep) || c.validObjectRef(partial) {
		t.Fatal("telemetry-enabled restart retained the wrong epoch population")
	}
	snapshot, ok := c.TelemetrySnapshot()
	if !ok {
		t.Fatal("telemetry snapshot unavailable")
	}
	if snapshot.Full.Cycles != 2 || snapshot.Full.FailedCycles != 1 {
		t.Fatalf("restart telemetry cycles=%d failed=%d, want 2/1", snapshot.Full.Cycles, snapshot.Full.FailedCycles)
	}
}

func TestObjectScanTelemetryPreservesLogicalPayloadBytes(t *testing.T) {
	mixed, err := NewStructDesc(0, []StorageKind{StorageI8, StorageRefNull})
	if err != nil {
		t.Fatal(err)
	}
	numeric, err := NewArrayDesc(1, StorageI8)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{
		Profile:        ProfileTiny,
		TinyHeapBytes:  4096,
		TinyBlockBytes: 16,
		Telemetry:      new(Telemetry),
	}, []TypeDesc{mixed, numeric})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	object, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	array, err := c.NewArrayDefault(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	objectRoot, arrayRoot := Root(object), Root(array)
	if err := c.CollectFull(Slots{&objectRoot, &arrayRoot}); err != nil {
		t.Fatal(err)
	}
	snapshot, ok := c.TelemetrySnapshot()
	if !ok {
		t.Fatal("telemetry unavailable")
	}
	// The mixed struct's padded layout and the one-byte numeric array each occupy
	// an 8-byte object payload. Pointer-free payloads retain the established
	// payload-visit accounting.
	if got := snapshot.Full.Trace.PayloadBytesVisited; got != 16 {
		t.Fatalf("payload bytes visited = %d, want 16", got)
	}
}

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

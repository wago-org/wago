//go:build wago_gcstats

package gc

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

type telemetryClassifiedDirectRoots struct {
	refs    []Root
	classes []RootClass
}

func (r *telemetryClassifiedDirectRoots) RangeRoots(fn func(RootSlot) bool) {
	for i := range r.refs {
		if !fn(&r.refs[i]) {
			return
		}
	}
}

func (r *telemetryClassifiedDirectRoots) RangeClassifiedRootRefs(sink ClassifiedRootRefSink) bool {
	for i := range r.refs {
		if !sink.VisitClassifiedRootRef(r.classes[i], Ref(r.refs[i])) {
			return false
		}
	}
	return true
}

func telemetryTestTypes(t *testing.T) []TypeDesc {
	t.Helper()
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := NewArrayDesc(1, StorageRefNull)
	if err != nil {
		t.Fatal(err)
	}
	return []TypeDesc{leaf, refs}
}

func TestThroughputCollectorTelemetry(t *testing.T) {
	if !TelemetryAvailable() {
		t.Fatal("wago_gcstats build reports collector telemetry unavailable")
	}
	c, err := NewCollector(Config{
		Telemetry:           new(Telemetry),
		NurseryBytes:        64 << 10,
		ThroughputHeapBytes: 1 << 20,
		ThroughputPageBytes: 64 << 10,
		VerifyAfterCollect:  true,
	}, telemetryTestTypes(t))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	parent, err := c.NewArrayDefault(1, 64)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ForcePromote(parent); err != nil {
		t.Fatal(err)
	}
	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ArraySet(parent, 0, RefValue(child)); err != nil {
		t.Fatal(err)
	}
	if err := c.ArraySet(parent, 0, RefValue(child)); err != nil {
		t.Fatal(err)
	}
	global := c.NewGlobalSlot(Null())
	public, err := c.NewCheckedClassifiedGlobalSlot(Null(), RootPublicToken)
	if err != nil {
		t.Fatal(err)
	}
	table := c.NewTableSlot(Null())
	if err := c.SetGlobalSlot(global, child); err != nil {
		t.Fatal(err)
	}
	if err := c.SetGlobalSlot(public, child); err != nil {
		t.Fatal(err)
	}
	if err := c.SetTableSlot(table, child); err != nil {
		t.Fatal(err)
	}
	parentRoot, childRoot := Root(parent), Root(child)
	roots := RootGroups{
		{Class: RootNativeFrame, Roots: Slots{&parentRoot}},
		{Class: RootForeignInstance, Roots: Slots{&childRoot}},
		{Class: RootSnapshotTemporary, Roots: Slots{&childRoot}},
	}
	if err := c.CollectMinor(roots); err != nil {
		t.Fatal(err)
	}

	snapshot, ok := c.TelemetrySnapshot()
	if !ok {
		t.Fatal("telemetry snapshot disabled")
	}
	if snapshot.SchemaVersion != TelemetrySchemaVersion || snapshot.Profile != ProfileThroughput {
		t.Fatalf("snapshot identity = schema %d profile %d", snapshot.SchemaVersion, snapshot.Profile)
	}
	if snapshot.Minor.Cycles != 1 || snapshot.Minor.FailedCycles != 0 || snapshot.Paths.MinorCollections != 1 {
		t.Fatalf("minor cycle counters = %+v paths=%+v", snapshot.Minor, snapshot.Paths)
	}
	if snapshot.Minor.Roots.NativeFrames != 1 || snapshot.Minor.Roots.PublicTokens != 1 || snapshot.Minor.Roots.Globals != 1 || snapshot.Minor.Roots.Tables != 1 || snapshot.Minor.Roots.ForeignInstances != 1 || snapshot.Minor.Roots.SnapshotTemporaries != 1 {
		t.Fatalf("root classes = %+v", snapshot.Minor.Roots)
	}
	if snapshot.Minor.Nursery.AllocatedObjects != 1 || snapshot.Minor.Nursery.SurvivedObjects != 1 || snapshot.Minor.Nursery.PromotedObjects != 0 || snapshot.Minor.Nursery.CopiedBytes == 0 {
		t.Fatalf("nursery counters = %+v", snapshot.Minor.Nursery)
	}
	if snapshot.Minor.Nursery.AgeObjects[1] != 1 || snapshot.Minor.Nursery.AgeBytes[1] == 0 || snapshot.Minor.Nursery.PointerFreeAgeObjects[1] != 1 {
		t.Fatalf("age histogram = objects %v bytes %v", snapshot.Minor.Nursery.AgeObjects, snapshot.Minor.Nursery.AgeBytes)
	}
	if snapshot.Minor.Cards.DirtyObjectCards != 1 || snapshot.Minor.Cards.DirtyRootCards != 3 || snapshot.Minor.Cards.UsefulObjectCards != 1 || snapshot.Minor.Cards.UsefulRootCards != 3 {
		t.Fatalf("card counters = %+v", snapshot.Minor.Cards)
	}
	if snapshot.Minor.Cards.DuplicateDirties == 0 || snapshot.Minor.Cards.WholeObjectScans != 0 || snapshot.Minor.Cards.WholeObjectScansAvoided != 1 || snapshot.Minor.Cards.ScannedSlots != 32 || snapshot.Minor.Cards.ClearedCards != 0 {
		t.Fatalf("card scan counters = %+v", snapshot.Minor.Cards)
	}
	if snapshot.Minor.Trace.ObjectsVisited != 2 || snapshot.Minor.Trace.ReferenceSlotsVisited != 32 {
		t.Fatalf("trace counters = %+v", snapshot.Minor.Trace)
	}
	if snapshot.Minor.Pause.Count != 1 || snapshot.Minor.Pause.MaxNS == 0 || snapshot.Minor.TotalNS == 0 {
		t.Fatalf("pause counters = %+v total=%d", snapshot.Minor.Pause, snapshot.Minor.TotalNS)
	}
	if snapshot.Minor.Phases.NativeFrameRootsNS == 0 || snapshot.Minor.Phases.PersistentRootsNS == 0 || snapshot.Minor.Phases.ReferenceScanningNS == 0 || snapshot.Minor.Phases.PromotionCopyNS == 0 {
		t.Fatalf("minor phases = %+v", snapshot.Minor.Phases)
	}
	if snapshot.Paths.GoAllocationPaths != 2 || snapshot.Paths.BackingGrowths == 0 {
		t.Fatalf("allocation paths = %+v", snapshot.Paths)
	}
	if snapshot.Heap.LiveObjects != 2 || snapshot.Heap.LiveBytes == 0 || snapshot.Heap.CommittedBytes == 0 || snapshot.Heap.ReservedBytes < snapshot.Heap.CommittedBytes || snapshot.Heap.MetadataBytes == 0 {
		t.Fatalf("managed heap = %+v", snapshot.Heap)
	}

	parentRoot, childRoot = Root(Null()), Root(Null())
	if err := c.SetGlobalSlot(global, Null()); err != nil {
		t.Fatal(err)
	}
	if err := c.SetGlobalSlot(public, Null()); err != nil {
		t.Fatal(err)
	}
	if err := c.SetTableSlot(table, Null()); err != nil {
		t.Fatal(err)
	}
	if err := c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	snapshot, _ = c.TelemetrySnapshot()
	if snapshot.Full.Cycles != 1 || snapshot.Full.Trace.ObjectsSwept != 2 || snapshot.Heap.LiveObjects != 0 {
		t.Fatalf("full collection telemetry = cycle %+v heap %+v", snapshot.Full, snapshot.Heap)
	}
	if snapshot.Full.Phases.SweepNS == 0 || snapshot.Full.Pause.Count != 1 {
		t.Fatalf("full phases = %+v pause=%+v", snapshot.Full.Phases, snapshot.Full.Pause)
	}
}

func TestDirectClassifiedRootTelemetry(t *testing.T) {
	c, err := NewCollector(Config{Telemetry: new(Telemetry)}, telemetryTestTypes(t))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	object, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	classes := []RootClass{RootNativeFrame, RootGlobal, RootTable, RootPublicToken, RootForeignInstance, RootSnapshotTemporary}
	roots := &telemetryClassifiedDirectRoots{refs: make([]Root, len(classes)), classes: classes}
	for i := range roots.refs {
		roots.refs[i] = Root(object)
	}
	if err := c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := c.TelemetrySnapshot()
	got := snapshot.Full.Roots
	if got.NativeFrames != 1 || got.Globals != 1 || got.Tables != 1 || got.PublicTokens != 1 || got.ForeignInstances != 1 || got.SnapshotTemporaries != 1 {
		t.Fatalf("direct classified roots = %+v", got)
	}
}

func TestClassifiedPersistentRootSurvivesTelemetryReset(t *testing.T) {
	c, err := NewCollector(Config{Telemetry: new(Telemetry)}, telemetryTestTypes(t))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.NewCheckedClassifiedGlobalSlot(Null(), RootPublicToken); err != nil {
		t.Fatal(err)
	}
	if _, err := c.NewCheckedClassifiedGlobalSlot(Null(), rootClassCount); err == nil {
		t.Fatal("invalid root telemetry class accepted")
	}
	if !c.ResetTelemetry() {
		t.Fatal("telemetry reset failed")
	}
	if err := c.CollectFull(nil); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := c.TelemetrySnapshot()
	if snapshot.Full.Roots.PublicTokens != 1 || snapshot.Full.Roots.Globals != 0 {
		t.Fatalf("persistent root classes after reset = %+v", snapshot.Full.Roots)
	}
}

func TestTinyCollectorTelemetryAndIncrementalCycle(t *testing.T) {
	requireTinyIncrementalBuild(t)
	c, err := NewCollector(Config{
		Profile:        ProfileTiny,
		TinyHeapBytes:  64 << 10,
		TinyBlockBytes: 16,
		Telemetry:      new(Telemetry),
	}, telemetryTestTypes(t))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	parent, err := c.NewArrayDefault(1, 4)
	if err != nil {
		t.Fatal(err)
	}
	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ArraySet(parent, 0, RefValue(child)); err != nil {
		t.Fatal(err)
	}
	root := Root(parent)
	roots := ClassifiedRoots{Class: RootSnapshotTemporary, Roots: Slots{&root}}
	if err := c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	snapshot, ok := c.TelemetrySnapshot()
	// Tiny enumerates roots once for initial mark and once for remark.
	if !ok || snapshot.Full.Cycles != 1 || snapshot.Full.Roots.SnapshotTemporaries != 2 || snapshot.Full.Trace.ObjectsVisited != 2 || snapshot.Full.Trace.ReferenceSlotsVisited != 4 {
		t.Fatalf("tiny full telemetry = %+v, enabled=%v", snapshot.Full, ok)
	}
	if snapshot.Profile != ProfileTiny || snapshot.Heap.LiveObjects != 2 || snapshot.Heap.MetadataBytes == 0 {
		t.Fatalf("tiny heap telemetry = %+v", snapshot.Heap)
	}
	if !snapshot.Tiny.IncrementalBuild || snapshot.Tiny.PacingStepLimit != 1 || snapshot.Tiny.TransientRootLimit != tinyTransientRootLimit || snapshot.Tiny.PersistentRootsPerStep != tinyStepPersistentRoots || snapshot.Tiny.SweepHandlesPerStep != tinyStepSweepHandles || snapshot.Tiny.SweepBlocksPerStep != tinyStepSweepBlocks || snapshot.Tiny.SweepPoisonBytesPerStep != tinyStepSweepBytes {
		t.Fatalf("tiny policy telemetry = %+v", snapshot.Tiny)
	}

	if !c.ResetTelemetry() {
		t.Fatal("reset telemetry failed")
	}
	sawSuspendedMutatorTime := false
	for steps := 0; ; steps++ {
		if steps > len(c.handles)+8 {
			t.Fatal("incremental telemetry cycle did not finish")
		}
		if err := c.Step(roots); err != nil {
			t.Fatal(err)
		}
		if c.tinyGC.state == tinyIdle {
			break
		}
		if c.cfg.Telemetry.active.suspendStart.IsZero() {
			t.Fatal("incremental telemetry did not suspend between steps")
		}
		if steps == 0 {
			time.Sleep(time.Millisecond)
		}
		if c.cfg.Telemetry.active.suspendedNS > 0 {
			sawSuspendedMutatorTime = true
		}
	}
	if !sawSuspendedMutatorTime {
		t.Fatal("incremental telemetry did not exclude mutator time")
	}
	snapshot, _ = c.TelemetrySnapshot()
	if snapshot.Full.Cycles != 1 || snapshot.Full.Trace.ObjectsVisited != 2 || snapshot.Full.Phases.MarkingNS == 0 || snapshot.Full.Phases.SweepNS == 0 {
		t.Fatalf("tiny incremental telemetry = %+v", snapshot.Full)
	}
}

func TestCollectorTelemetryVerificationDoesNotInflateWork(t *testing.T) {
	run := func(verify bool) TelemetrySnapshot {
		t.Helper()
		c, err := NewCollector(Config{Telemetry: new(Telemetry), VerifyAfterCollect: verify}, telemetryTestTypes(t))
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		object, err := c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
		root := Root(object)
		if err := c.CollectFull(Slots{&root}); err != nil {
			t.Fatal(err)
		}
		snapshot, _ := c.TelemetrySnapshot()
		return snapshot
	}
	withoutVerify, withVerify := run(false), run(true)
	if withoutVerify.Full.Trace != withVerify.Full.Trace || withoutVerify.Full.Roots.NativeFrames != withVerify.Full.Roots.NativeFrames {
		t.Fatalf("verification changed deterministic work: without=%+v with=%+v", withoutVerify.Full, withVerify.Full)
	}
}

func TestCollectorTelemetryJSONAndMemoryDomains(t *testing.T) {
	c, err := NewCollector(Config{Telemetry: new(Telemetry)}, telemetryTestTypes(t))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.CollectMinor(nil); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := c.TelemetrySnapshot()
	report := NewBenchmarkTelemetryReport("smoke")
	report.Configuration = BenchmarkConfiguration{Profile: "throughput", Collection: "minor", SurvivalPercent: 10, Objects: 100}
	report.Collector = snapshot
	report.Memory = CaptureMemoryDomains(123, 456, snapshot.Heap)
	report.NativeCode = NativeCodeTelemetry{TotalBytes: 789, AllocationBytes: 12}
	report.Operations = 1
	report.SemanticChecksum = 42
	var out bytes.Buffer
	if err := report.WriteJSON(&out); err != nil {
		t.Fatal(err)
	}
	if out.Len() == 0 || out.Bytes()[out.Len()-1] != '\n' {
		t.Fatalf("JSON report is not newline terminated: %q", out.Bytes())
	}
	var decoded BenchmarkTelemetryReport
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.SchemaVersion != TelemetrySchemaVersion || decoded.Name != "smoke" || decoded.Configuration.SurvivalPercent != 10 || decoded.Collector.Minor.Cycles != 1 || decoded.SemanticChecksum != 42 {
		t.Fatalf("decoded report = %+v", decoded)
	}
	if decoded.Memory.GoCompilerHeapBytes != 123 || decoded.Memory.GoRuntimeHeapBytes == 0 || decoded.Memory.WasmManagedBytes != snapshot.Heap.CommittedBytes || decoded.Memory.ExecutableJITBytes != 456 {
		t.Fatalf("memory domains = %+v", decoded.Memory)
	}
}

func TestBarrierTelemetryCountsRuntimeStates(t *testing.T) {
	telemetry := new(Telemetry)
	c := newTestCollectorWithTypes(t, Config{Telemetry: telemetry, StressNurseryBytes: 1 << 20}, bulkTestTypes(t))
	parent, err := c.NewArrayDefault(3, 128)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ForcePromote(parent); err != nil {
		t.Fatal(err)
	}
	oldChild, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ForcePromote(oldChild); err != nil {
		t.Fatal(err)
	}
	youngChild, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	youngParent, err := c.NewArrayDefault(3, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !c.ResetTelemetry() {
		t.Fatal("telemetry reset failed")
	}
	if err := c.ArraySet(parent, 2, RefValue(oldChild)); err != nil {
		t.Fatal(err)
	}
	if err := c.ArrayFillNoBarrier(parent, 3, Value{Kind: StorageRefNull}, 1); err != nil {
		t.Fatal(err)
	}
	c.writeBarrierObjectRange(youngParent, youngChild, 0, 3)
	if err := c.ArraySet(parent, 0, RefValue(youngChild)); err != nil {
		t.Fatal(err)
	}
	if err := c.ArraySet(parent, 1, RefValue(youngChild)); err != nil {
		t.Fatal(err)
	}
	if err := c.ArraySet(parent, 32, RefValue(youngChild)); err != nil {
		t.Fatal(err)
	}
	snapshot, ok := c.TelemetrySnapshot()
	if !ok {
		t.Fatal("telemetry unavailable")
	}
	want := BarrierTelemetry{NoBarrier: 1, YoungParent: 1, KnownOldChild: 1, ExistingCard: 1, CardMark: 1, SlowBarrier: 1}
	if snapshot.Barriers != want {
		t.Fatalf("barrier telemetry = %+v, want %+v", snapshot.Barriers, want)
	}
}

func TestCollectorTelemetryDisabledAndEnabledRemainAllocationFree(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[enabled], func(t *testing.T) {
			cfg := Config{}
			if enabled {
				cfg.Telemetry = new(Telemetry)
			}
			c, err := NewCollector(cfg, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if allocs := testing.AllocsPerRun(1000, func() {
				if err := c.CollectMinor(nil); err != nil {
					t.Fatal(err)
				}
			}); allocs != 0 {
				t.Fatalf("minor collection allocations = %v, want 0", allocs)
			}
			_, ok := c.TelemetrySnapshot()
			if ok != enabled {
				t.Fatalf("snapshot enabled = %v, want %v", ok, enabled)
			}
		})
	}
}

func BenchmarkCollectorTelemetryOverhead(b *testing.B) {
	for _, enabled := range []bool{false, true} {
		b.Run(map[bool]string{false: "disabled", true: "enabled"}[enabled], func(b *testing.B) {
			cfg := Config{}
			if enabled {
				cfg.Telemetry = new(Telemetry)
			}
			c, err := NewCollector(cfg, nil)
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(c.Close)
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if err := c.CollectMinor(nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

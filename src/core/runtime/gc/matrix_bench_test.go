package gc

import (
	"fmt"
	"math/bits"
	"testing"
	"time"
)

const pauseSubBuckets = 16

// pauseHistogram is a bounded, allocation-free nanosecond histogram with 16
// linear sub-buckets per power-of-two interval. Its relative bucket width is at
// most 6.25%, while memory remains fixed across arbitrarily long benchmark runs.
// The reported percentile is the upper bound of its bucket.
type pauseHistogram struct {
	buckets [1 + 63*pauseSubBuckets]uint64
	count   uint64
	maxNS   uint64
}

func (h *pauseHistogram) record(d time.Duration) {
	ns := uint64(d)
	h.buckets[pauseBucket(ns)]++
	h.count++
	if ns > h.maxNS {
		h.maxNS = ns
	}
}

func pauseBucket(ns uint64) int {
	if ns == 0 {
		return 0
	}
	exponent := bits.Len64(ns) - 1
	base := uint64(1) << exponent
	width := base / pauseSubBuckets
	if width == 0 {
		width = 1
	}
	sub := int((ns - base) / width)
	if sub >= pauseSubBuckets {
		sub = pauseSubBuckets - 1
	}
	return 1 + exponent*pauseSubBuckets + sub
}

func pauseBucketUpper(index int) uint64 {
	if index == 0 {
		return 0
	}
	index--
	exponent, sub := index/pauseSubBuckets, index%pauseSubBuckets
	base := uint64(1) << exponent
	width := base / pauseSubBuckets
	if width == 0 {
		width = 1
	}
	return base + uint64(sub+1)*width - 1
}

func (h *pauseHistogram) percentile(numerator, denominator uint64) uint64 {
	if h.count == 0 {
		return 0
	}
	target := (h.count*numerator + denominator - 1) / denominator
	var seen uint64
	for i, n := range h.buckets {
		seen += n
		if seen >= target {
			upper := pauseBucketUpper(i)
			if upper > h.maxNS {
				return h.maxNS
			}
			return upper
		}
	}
	return h.maxNS
}

func (h *pauseHistogram) report(b *testing.B, prefix string) {
	b.ReportMetric(float64(h.percentile(50, 100)), prefix+"-p50-ns")
	b.ReportMetric(float64(h.percentile(90, 100)), prefix+"-p90-ns")
	b.ReportMetric(float64(h.percentile(95, 100)), prefix+"-p95-ns")
	b.ReportMetric(float64(h.percentile(99, 100)), prefix+"-p99-ns")
	b.ReportMetric(float64(h.maxNS), prefix+"-max-ns")
}

type gcMatrixTelemetryTotals struct {
	cycles  uint64
	totalNS uint64
	phases  PhaseTelemetry
	roots   RootTelemetry
	trace   TraceTelemetry
	nursery NurseryTelemetry
	cards   CardTelemetry
}

func (t *gcMatrixTelemetryTotals) add(c CollectionTelemetry) {
	t.cycles += c.Cycles
	t.totalNS += c.TotalNS
	addPhaseTelemetry(&t.phases, c.Phases)
	addRootTelemetry(&t.roots, c.Roots)
	addTraceTelemetry(&t.trace, c.Trace)
	addNurseryTelemetry(&t.nursery, c.Nursery)
	addCardTelemetry(&t.cards, c.Cards)
}

func rootClassTelemetry(roots RootTelemetry, class RootClass) (count, ns uint64) {
	switch class {
	case RootGlobal:
		return roots.Globals, roots.GlobalNS
	case RootTable:
		return roots.Tables, roots.TableNS
	case RootPublicToken:
		return roots.PublicTokens, roots.PublicTokenNS
	case RootForeignInstance:
		return roots.ForeignInstances, roots.ForeignInstanceNS
	case RootSnapshotTemporary:
		return roots.SnapshotTemporaries, roots.SnapshotTemporaryNS
	default:
		return roots.NativeFrames, roots.NativeFrameNS
	}
}

func (t *gcMatrixTelemetryTotals) report(b *testing.B) {
	if t.cycles == 0 {
		return
	}
	perOp := 1 / float64(b.N)
	b.ReportMetric(float64(t.totalNS)*perOp, "collector-total-ns/op")
	b.ReportMetric(float64(t.phases.RootEnumerationNS+t.phases.PersistentRootsNS+t.phases.NativeFrameRootsNS)*perOp, "root-ns/op")
	b.ReportMetric(float64(t.phases.ReferenceScanningNS)*perOp, "reference-scan-ns/op")
	b.ReportMetric(float64(t.phases.PromotionCopyNS)*perOp, "promotion-ns/op")
	b.ReportMetric(float64(t.phases.SweepNS)*perOp, "sweep-ns/op")
	b.ReportMetric(float64(t.trace.ObjectsVisited)*perOp, "objects-visited/op")
	b.ReportMetric(float64(t.trace.PayloadBytesVisited)*perOp, "payload-bytes-visited/op")
	b.ReportMetric(float64(t.trace.ReferenceSlotsVisited)*perOp, "reference-slots-visited/op")
	b.ReportMetric(float64(t.nursery.PromotedBytes)*perOp, "promoted-bytes/op")
	b.ReportMetric(float64(t.cards.ScannedSlots)*perOp, "card-slots-scanned/op")
	b.ReportMetric(float64(t.cards.WholeObjectScans)*perOp, "whole-object-scans/op")
}

func TestGCPauseHistogram(t *testing.T) {
	var h pauseHistogram
	for _, ns := range []time.Duration{0, 1, 2, 3, 4, 8, 16, 32, 64, 128} {
		h.record(ns * time.Nanosecond)
	}
	if got := h.percentile(50, 100); got != 4 {
		t.Fatalf("p50 = %d ns, want bucket upper bound 4", got)
	}
	if got := h.percentile(90, 100); got != 67 {
		t.Fatalf("p90 = %d ns, want bucket upper bound 67", got)
	}
	if got := h.percentile(99, 100); got != 128 {
		t.Fatalf("p99 = %d ns, want bucket upper bound 128", got)
	}
	if h.maxNS != 128 || h.count != 10 {
		t.Fatalf("histogram max/count = %d/%d, want 128/10", h.maxNS, h.count)
	}
}

func populateGCMatrixObject(c *Collector, layout gcMatrixLayout, r Ref) error {
	d := c.types[layout.typeID]
	if !d.HasRefs {
		return nil
	}
	if d.Kind == KindStruct {
		for i, field := range d.Fields {
			if isCollectorRefKind(field.Kind) {
				if err := c.StructSet(r, uint32(i), RefValue(r)); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for i := uint32(0); i < c.header(r).Aux; i++ {
		if err := c.ArraySet(r, i, RefValue(r)); err != nil {
			return err
		}
	}
	return nil
}

func validateGCMatrixObject(c *Collector, layout gcMatrixLayout, r Ref) (uint64, error) {
	d := c.types[layout.typeID]
	if !d.HasRefs {
		return uint64(layout.typeID) + 1, nil
	}
	var checksum uint64
	if d.Kind == KindStruct {
		for i, field := range d.Fields {
			if !isCollectorRefKind(field.Kind) {
				continue
			}
			v, err := c.StructGet(r, uint32(i))
			if err != nil || v.Ref != r {
				return 0, fmt.Errorf("self reference field %d = %v, %v; want %v", i, v.Ref, err, r)
			}
			checksum += uint64(v.Ref)
		}
		return checksum, nil
	}
	for i := uint32(0); i < c.header(r).Aux; i++ {
		v, err := c.ArrayGet(r, i)
		if err != nil || v.Ref != r {
			return 0, fmt.Errorf("self reference element %d = %v, %v; want %v", i, v.Ref, err, r)
		}
		checksum += uint64(v.Ref)
	}
	return checksum, nil
}

type gcMatrixLayout struct {
	name   string
	typeID TypeID
	alloc  func(*Collector) (Ref, error)
}

func gcMatrixTypes(tb testing.TB) ([]TypeDesc, []gcMatrixLayout) {
	tb.Helper()
	pointerFree, err := NewStructDesc(0, []StorageKind{
		StorageI64, StorageI64, StorageI64, StorageI64,
		StorageI64, StorageI64, StorageI64, StorageI64,
	})
	if err != nil {
		tb.Fatal(err)
	}
	sparseFields := make([]StorageKind, 16)
	for i := range sparseFields {
		sparseFields[i] = StorageI64
	}
	sparseFields[8] = StorageRefNull
	sparse, err := NewStructDesc(1, sparseFields)
	if err != nil {
		tb.Fatal(err)
	}
	denseFields := make([]StorageKind, 16)
	for i := range denseFields {
		denseFields[i] = StorageRefNull
	}
	dense, err := NewStructDesc(2, denseFields)
	if err != nil {
		tb.Fatal(err)
	}
	denseArray, err := NewArrayDesc(3, StorageRefNull)
	if err != nil {
		tb.Fatal(err)
	}
	types := []TypeDesc{pointerFree, sparse, dense, denseArray}
	return types, []gcMatrixLayout{
		{name: "pointer-free", typeID: 0, alloc: func(c *Collector) (Ref, error) { return c.NewStructDefault(0) }},
		{name: "sparse-struct-refs", typeID: 1, alloc: func(c *Collector) (Ref, error) { return c.NewStructDefault(1) }},
		{name: "dense-struct-refs", typeID: 2, alloc: func(c *Collector) (Ref, error) { return c.NewStructDefault(2) }},
		{name: "dense-array-refs", typeID: 3, alloc: func(c *Collector) (Ref, error) { return c.NewArrayDefault(3, 16) }},
	}
}

func gcMatrixConfig(profile Profile) Config {
	if profile == ProfileTiny {
		return Config{Profile: ProfileTiny, TinyHeapBytes: 16 << 20, TinyBlockBytes: 16}
	}
	return Config{NurseryBytes: 1 << 20, ThroughputHeapBytes: 64 << 20, ThroughputPageBytes: 64 << 10}
}

// BenchmarkGCCollectionMatrix isolates collector pause work across the object
// layouts and survival ratios called out by issue #300. Allocation and cleanup
// are outside the benchmark timer. Every operation validates the surviving
// object count and type, so a fast but semantically wrong collector cannot win.
func BenchmarkGCCollectionMatrix(b *testing.B) {
	types, layouts := gcMatrixTypes(b)
	profiles := []struct {
		name       string
		profile    Profile
		collection string
	}{
		{name: "throughput-minor", profile: ProfileThroughput, collection: "minor"},
		{name: "throughput-full", profile: ProfileThroughput, collection: "full"},
		{name: "tiny-full", profile: ProfileTiny, collection: "full"},
	}
	for _, profile := range profiles {
		for _, layout := range layouts {
			for _, survival := range []int{0, 1, 10, 50, 90} {
				name := fmt.Sprintf("%s/%s/survival=%d", profile.name, layout.name, survival)
				b.Run(name, func(b *testing.B) {
					cfg := gcMatrixConfig(profile.profile)
					if collectorTelemetryEnabled {
						cfg.Telemetry = new(Telemetry)
					}
					c, err := NewCollector(cfg, types)
					if err != nil {
						b.Fatal(err)
					}
					b.Cleanup(c.Close)
					rootValues := make([]Root, survival)
					roots := make(Slots, survival)
					for i := range rootValues {
						roots[i] = &rootValues[i]
					}
					var pauses pauseHistogram
					var telemetryTotals gcMatrixTelemetryTotals
					var checksum uint64
					b.ReportAllocs()
					b.ReportMetric(100, "objects/op")
					b.ReportMetric(float64(survival), "survival-percent")
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						b.StopTimer()
						if collectorTelemetryEnabled {
							c.ResetTelemetry()
						}
						for j := 0; j < 100; j++ {
							r, err := layout.alloc(c)
							if err != nil {
								b.Fatal(err)
							}
							if err := populateGCMatrixObject(c, layout, r); err != nil {
								b.Fatal(err)
							}
							if j < survival {
								rootValues[j] = Root(r)
							}
						}
						b.StartTimer()
						start := time.Now()
						if profile.collection == "minor" {
							err = c.CollectMinor(roots)
						} else {
							err = c.CollectFull(roots)
						}
						elapsed := time.Since(start)
						b.StopTimer()
						pauses.record(elapsed)
						if err != nil {
							b.Fatal(err)
						}
						if collectorTelemetryEnabled {
							telemetrySnapshot, ok := c.TelemetrySnapshot()
							if !ok {
								b.Fatal("collector telemetry disabled")
							}
							if profile.collection == "minor" {
								telemetryTotals.add(telemetrySnapshot.Minor)
							} else {
								telemetryTotals.add(telemetrySnapshot.Full)
							}
						}
						if got := c.Stats().LiveObjects; got != uint32(survival) {
							b.Fatalf("live objects = %d, want %d", got, survival)
						}
						for j := range rootValues {
							typeID, err := c.ObjectType(Ref(rootValues[j]))
							if err != nil || typeID != layout.typeID {
								b.Fatalf("survivor type = %d, %v; want %d", typeID, err, layout.typeID)
							}
							objectChecksum, err := validateGCMatrixObject(c, layout, Ref(rootValues[j]))
							if err != nil {
								b.Fatal(err)
							}
							checksum += objectChecksum + uint64(typeID) + uint64(j) + 1
							rootValues[j] = Root(Null())
						}
						if err := c.CollectFull(nil); err != nil {
							b.Fatal(err)
						}
						if got := c.Stats().LiveObjects; got != 0 {
							b.Fatalf("cleanup live objects = %d, want 0", got)
						}
						b.StartTimer()
					}
					b.StopTimer()
					if survival != 0 && checksum == 0 {
						b.Fatal("semantic checksum is zero")
					}
					pauses.report(b, "pause")
					telemetryTotals.report(b)
				})
			}
		}
	}
}

// BenchmarkGCSparseRememberedArray compares two distant young writes with a
// fully dirty old reference array at each candidate fixed-card size. Sparse
// collection work must remain proportional to the two dirty cards.
func BenchmarkGCSparseRememberedArray(b *testing.B) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		b.Fatal(err)
	}
	refs, err := NewArrayDesc(1, StorageRefNull)
	if err != nil {
		b.Fatal(err)
	}
	for _, length := range []uint32{4 << 10, 256 << 10} {
		for _, cardBytes := range []uint32{128, 256, 512} {
			for _, density := range []string{"sparse", "dense"} {
				b.Run(fmt.Sprintf("elements=%d/card-bytes=%d/%s", length, cardBytes, density), func(b *testing.B) {
					cfg := Config{NurseryBytes: 1 << 20, ThroughputHeapBytes: 64 << 20}
					if collectorTelemetryEnabled {
						cfg.Telemetry = new(Telemetry)
					}
					c, err := NewCollector(cfg, []TypeDesc{leaf, refs})
					if err != nil {
						b.Fatal(err)
					}
					b.Cleanup(c.Close)
					c.cardBytes = cardBytes
					array, err := c.NewArrayDefault(1, length)
					if err != nil {
						b.Fatal(err)
					}
					if err := c.ForcePromote(array); err != nil {
						b.Fatal(err)
					}
					arrayRoot := Root(array)
					roots := Slots{&arrayRoot}
					var children [2]Ref
					var pauses pauseHistogram
					var checksum uint64
					b.ReportAllocs()
					b.ReportMetric(float64(length), "array-elements")
					b.ReportMetric(float64(cardBytes), "card-bytes")
					b.ReportMetric(2, "young-allocations/op")
					if density == "sparse" {
						b.ReportMetric(2, "dirty-writes/op")
					} else {
						b.ReportMetric(float64(length), "dirty-writes/op")
					}
					if collectorTelemetryEnabled {
						c.ResetTelemetry()
					}
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						b.StopTimer()
						for j := range children {
							children[j], err = c.NewStructDefault(0)
							if err != nil {
								b.Fatal(err)
							}
						}
						if density == "sparse" {
							if err := c.ArraySet(array, 0, RefValue(children[0])); err != nil {
								b.Fatal(err)
							}
							if err := c.ArraySet(array, length-1, RefValue(children[1])); err != nil {
								b.Fatal(err)
							}
						} else {
							for j := uint32(0); j < length; j++ {
								if err := c.ArraySet(array, j, RefValue(children[j&1])); err != nil {
									b.Fatal(err)
								}
							}
						}
						before := c.Stats()
						b.StartTimer()
						start := time.Now()
						err = c.CollectMinor(roots)
						elapsed := time.Since(start)
						b.StopTimer()
						pauses.record(elapsed)
						if err != nil {
							b.Fatal(err)
						}
						after := c.Stats()
						checksum += after.MinorRememberedScanned - before.MinorRememberedScanned
						for j, index := range []uint32{0, length - 1} {
							v, err := c.ArrayGet(array, index)
							if err != nil || v.Ref != children[j] {
								b.Fatalf("array[%d] = %v, %v; want %v", index, v.Ref, err, children[j])
							}
						}
						if density == "sparse" {
							if err := c.ArraySet(array, 0, RefValue(Null())); err != nil {
								b.Fatal(err)
							}
							if err := c.ArraySet(array, length-1, RefValue(Null())); err != nil {
								b.Fatal(err)
							}
						} else {
							for j := uint32(0); j < length; j++ {
								if err := c.ArraySet(array, j, RefValue(Null())); err != nil {
									b.Fatal(err)
								}
							}
						}
						if err := c.CollectFull(roots); err != nil {
							b.Fatal(err)
						}
						b.StartTimer()
					}
					b.StopTimer()
					if checksum == 0 {
						b.Fatal("remembered-set semantic checksum is zero")
					}
					if collectorTelemetryEnabled {
						snapshot, ok := c.TelemetrySnapshot()
						if !ok {
							b.Fatal("collector telemetry disabled")
						}
						b.ReportMetric(float64(snapshot.Minor.Cards.ScannedSlots)/float64(b.N), "card-slots-scanned/op")
						b.ReportMetric(float64(snapshot.Minor.Cards.WholeObjectScans)/float64(b.N), "whole-object-scans/op")
						b.ReportMetric(float64(snapshot.Minor.Cards.DuplicateDirties)/float64(b.N), "duplicate-dirties/op")
					}
					pauses.report(b, "minor-pause")
				})
			}
		}
	}
}

// BenchmarkGCDirtyPersistentRoots proves Throughput minor root work depends on
// dirty slots rather than the complete collector-owned global directory.
func BenchmarkGCDirtyPersistentRoots(b *testing.B) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		b.Fatal(err)
	}
	for _, slots := range []int{1, 64, 4096} {
		b.Run(fmt.Sprintf("global-slots=%d", slots), func(b *testing.B) {
			// This benchmark isolates dirty persistent-root scanning. Keep the
			// survivor policy out of the fixture so one minor deterministically
			// promotes the retained child before the cleanup full collection.
			cfg := Config{NurseryBytes: 1 << 20, DisableMovingNursery: true}
			if collectorTelemetryEnabled {
				cfg.Telemetry = new(Telemetry)
			}
			c, err := NewCollector(cfg, []TypeDesc{leaf})
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(c.Close)
			for i := 0; i < slots; i++ {
				c.NewGlobalSlot(Null())
			}
			dirty := uint32(slots - 1)
			var pauses pauseHistogram
			if collectorTelemetryEnabled {
				c.ResetTelemetry()
			}
			b.ReportAllocs()
			b.ReportMetric(1, "dirty-root-slots/op")
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				child, err := c.NewStructDefault(0)
				if err != nil {
					b.Fatal(err)
				}
				if err := c.SetGlobalSlot(dirty, child); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				start := time.Now()
				err = c.CollectMinor(nil)
				elapsed := time.Since(start)
				b.StopTimer()
				pauses.record(elapsed)
				if err != nil {
					b.Fatal(err)
				}
				if c.entry(child).space != spaceOld {
					b.Fatal("dirty global did not preserve nursery child")
				}
				if err := c.SetGlobalSlot(dirty, Null()); err != nil {
					b.Fatal(err)
				}
				if err := c.CollectFull(nil); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
			}
			b.StopTimer()
			if collectorTelemetryEnabled {
				snapshot, ok := c.TelemetrySnapshot()
				if !ok {
					b.Fatal("collector telemetry disabled")
				}
				b.ReportMetric(float64(snapshot.Minor.Cards.DirtyRootCards)/float64(b.N), "dirty-root-cards/op")
				b.ReportMetric(float64(snapshot.Minor.Roots.Globals)/float64(b.N), "global-root-visits/op")
			}
			pauses.report(b, "minor-pause")
		})
	}
}

// BenchmarkGCRootClassMatrix scales every required ownership class while
// holding the live object graph constant. Product fixtures separately verify
// native/runtime adapters preserve these classifications.
func BenchmarkGCRootClassMatrix(b *testing.B) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		b.Fatal(err)
	}
	profiles := []struct {
		name    string
		profile Profile
	}{
		{name: "throughput", profile: ProfileThroughput},
		{name: "tiny", profile: ProfileTiny},
	}
	rootClasses := []struct {
		name  string
		class RootClass
		owned bool
	}{
		{name: "native-frame", class: RootNativeFrame},
		{name: "global", class: RootGlobal, owned: true},
		{name: "table", class: RootTable, owned: true},
		{name: "public-token", class: RootPublicToken},
		{name: "foreign-instance", class: RootForeignInstance},
		{name: "snapshot-temporary", class: RootSnapshotTemporary},
	}
	for _, profile := range profiles {
		for _, rootClass := range rootClasses {
			for _, rootCount := range []int{1, 64, 4096} {
				b.Run(fmt.Sprintf("%s/%s/count=%d", profile.name, rootClass.name, rootCount), func(b *testing.B) {
					cfg := gcMatrixConfig(profile.profile)
					if collectorTelemetryEnabled {
						cfg.Telemetry = new(Telemetry)
					}
					c, err := NewCollector(cfg, []TypeDesc{leaf})
					if err != nil {
						b.Fatal(err)
					}
					b.Cleanup(c.Close)
					object, err := c.NewStructDefault(0)
					if err != nil {
						b.Fatal(err)
					}
					var roots RootSet
					if rootClass.owned {
						for i := 0; i < rootCount; i++ {
							if rootClass.class == RootGlobal {
								c.NewGlobalSlot(object)
							} else {
								c.NewTableSlot(object)
							}
						}
					} else {
						values := make([]Root, rootCount)
						slots := make(Slots, rootCount)
						for i := range values {
							values[i] = Root(object)
							slots[i] = &values[i]
						}
						roots = ClassifiedRoots{Class: rootClass.class, Roots: slots}
					}
					var pauses pauseHistogram
					var checksum uint64
					b.ReportAllocs()
					b.ReportMetric(float64(rootCount), "roots/op")
					if collectorTelemetryEnabled {
						c.ResetTelemetry()
					}
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						start := time.Now()
						if err := c.CollectFull(roots); err != nil {
							b.Fatal(err)
						}
						elapsed := time.Since(start)
						b.StopTimer()
						pauses.record(elapsed)
						checksum += uint64(c.Stats().LiveObjects)
						b.StartTimer()
					}
					b.StopTimer()
					if checksum != uint64(b.N) {
						b.Fatalf("live checksum = %d, want %d", checksum, b.N)
					}
					if collectorTelemetryEnabled {
						snapshot, ok := c.TelemetrySnapshot()
						if !ok {
							b.Fatal("collector telemetry disabled")
						}
						count, ns := rootClassTelemetry(snapshot.Full.Roots, rootClass.class)
						b.ReportMetric(float64(count)/float64(b.N), "classified-roots/op")
						b.ReportMetric(float64(ns)/float64(b.N), "classified-root-ns/op")
					}
					pauses.report(b, "full-pause")
				})
			}
		}
	}
}

// BenchmarkGCTinyStepMatrix qualifies #319's bounded object scanner. Steps grow
// with array length while each mark Step remains capped by the internal
// entry/reference-slot/payload-byte work vector.
func BenchmarkGCTinyStepMatrix(b *testing.B) {
	refs, err := NewArrayDesc(0, StorageRefNull)
	if err != nil {
		b.Fatal(err)
	}
	for _, length := range []uint32{16, 4 << 10, 64 << 10} {
		b.Run(fmt.Sprintf("reference-elements=%d", length), func(b *testing.B) {
			c, err := NewCollector(Config{Profile: ProfileTiny, TinyHeapBytes: 8 << 20, TinyBlockBytes: 16}, []TypeDesc{refs})
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(c.Close)
			array, err := c.NewArrayDefault(0, length)
			if err != nil {
				b.Fatal(err)
			}
			root := Root(array)
			roots := Slots{&root}
			var steps uint64
			var stepPauses pauseHistogram
			b.ReportAllocs()
			b.ReportMetric(float64(length), "reference-slots")
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for {
					start := time.Now()
					if err := c.Step(roots); err != nil {
						b.Fatal(err)
					}
					elapsed := time.Since(start)
					b.StopTimer()
					stepPauses.record(elapsed)
					steps++
					idle := c.tinyGC.state == tinyIdle
					b.StartTimer()
					if idle {
						break
					}
				}
			}
			b.StopTimer()
			if c.Stats().LiveObjects != 1 {
				b.Fatalf("live objects = %d, want 1", c.Stats().LiveObjects)
			}
			b.ReportMetric(float64(steps)/float64(b.N), "steps/op")
			stepPauses.report(b, "step")
		})
	}
}

func TestGCMatrixWorkloadSmoke(t *testing.T) {
	types, layouts := gcMatrixTypes(t)
	collections := []struct {
		name       string
		profile    Profile
		collection string
	}{
		{name: "throughput-minor", profile: ProfileThroughput, collection: "minor"},
		{name: "throughput-full", profile: ProfileThroughput, collection: "full"},
		{name: "tiny-full", profile: ProfileTiny, collection: "full"},
	}
	for _, collection := range collections {
		for _, layout := range layouts {
			t.Run(collection.name+"/"+layout.name, func(t *testing.T) {
				c, err := NewCollector(gcMatrixConfig(collection.profile), types)
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				var root Root
				for i := 0; i < 10; i++ {
					r, err := layout.alloc(c)
					if err != nil {
						t.Fatal(err)
					}
					if err := populateGCMatrixObject(c, layout, r); err != nil {
						t.Fatal(err)
					}
					if i == 0 {
						root = Root(r)
					}
				}
				roots := Slots{&root}
				if collection.collection == "minor" {
					err = c.CollectMinor(roots)
				} else {
					err = c.CollectFull(roots)
				}
				if err != nil {
					t.Fatal(err)
				}
				if got := c.Stats().LiveObjects; got != 1 {
					t.Fatalf("live objects = %d, want 1", got)
				}
				if _, err := validateGCMatrixObject(c, layout, Ref(root)); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestGCSparseRememberedArrayWorkloadSmoke(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := NewArrayDesc(1, StorageRefNull)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		length uint32
		writes int
	}{{length: 256 << 10, writes: 2}, {length: 4 << 10, writes: 1024}} {
		t.Run(fmt.Sprintf("elements=%d/writes=%d", tc.length, tc.writes), func(t *testing.T) {
			c, err := NewCollector(Config{NurseryBytes: 1 << 20, ThroughputHeapBytes: 64 << 20}, []TypeDesc{leaf, refs})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			array, err := c.NewArrayDefault(1, tc.length)
			if err != nil {
				t.Fatal(err)
			}
			if err := c.ForcePromote(array); err != nil {
				t.Fatal(err)
			}
			root := Root(array)
			for i := 0; i < tc.writes; i++ {
				child, err := c.NewStructDefault(0)
				if err != nil {
					t.Fatal(err)
				}
				index := uint32(uint64(i) * uint64(tc.length-1) / uint64(max(1, tc.writes-1)))
				if err := c.ArraySet(array, index, RefValue(child)); err != nil {
					t.Fatal(err)
				}
			}
			if err := c.CollectMinor(Slots{&root}); err != nil {
				t.Fatal(err)
			}
			if got := c.Stats().LiveObjects; got != uint32(tc.writes+1) {
				t.Fatalf("live objects = %d, want %d", got, tc.writes+1)
			}
		})
	}
}

func TestGCRootClassWorkloadSmoke(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range []Profile{ProfileThroughput, ProfileTiny} {
		for _, rootClass := range []string{"direct", "global", "table"} {
			t.Run(fmt.Sprintf("profile=%d/%s", profile, rootClass), func(t *testing.T) {
				c, err := NewCollector(gcMatrixConfig(profile), []TypeDesc{leaf})
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				object, err := c.NewStructDefault(0)
				if err != nil {
					t.Fatal(err)
				}
				var roots RootSet
				switch rootClass {
				case "direct":
					values := make([]Root, 64)
					slots := make(Slots, len(values))
					for i := range values {
						values[i], slots[i] = Root(object), &values[i]
					}
					roots = slots
				case "global":
					for i := 0; i < 64; i++ {
						c.NewGlobalSlot(object)
					}
				case "table":
					for i := 0; i < 64; i++ {
						c.NewTableSlot(object)
					}
				}
				if err := c.CollectFull(roots); err != nil {
					t.Fatal(err)
				}
				if got := c.Stats().LiveObjects; got != 1 {
					t.Fatalf("live objects = %d, want 1", got)
				}
			})
		}
	}
}

func TestGCTinyStepWorkloadSmoke(t *testing.T) {
	refs, err := NewArrayDesc(0, StorageRefNull)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{Profile: ProfileTiny, TinyHeapBytes: 8 << 20, TinyBlockBytes: 16}, []TypeDesc{refs})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	array, err := c.NewArrayDefault(0, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	root := Root(array)
	maxSteps := int((uint32(64<<10)+tinyStepScanEntries-1)/tinyStepScanEntries) + len(c.handles) + 8
	for steps := 0; ; steps++ {
		if steps > maxSteps {
			t.Fatal("Tiny cycle did not finish within bounded scan/state/handle steps")
		}
		if err := c.Step(Slots{&root}); err != nil {
			t.Fatal(err)
		}
		if c.tinyGC.state == tinyIdle {
			break
		}
	}
	if got := c.Stats().LiveObjects; got != 1 {
		t.Fatalf("live objects = %d, want 1", got)
	}
}

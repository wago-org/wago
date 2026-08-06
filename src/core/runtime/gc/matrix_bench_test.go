package gc

import (
	"fmt"
	"math/bits"
	"testing"
	"time"
)

// pauseHistogram is a bounded, allocation-free log2 nanosecond histogram. It
// deliberately trades sub-bucket precision for stable memory use across long
// benchmark runs. The reported percentile is the upper bound of its bucket.
type pauseHistogram struct {
	buckets [64]uint64
	count   uint64
	maxNS   uint64
}

func (h *pauseHistogram) record(d time.Duration) {
	ns := uint64(d)
	h.buckets[bits.Len64(ns)]++
	h.count++
	if ns > h.maxNS {
		h.maxNS = ns
	}
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
			if i == 0 {
				return 0
			}
			upper := (uint64(1) << i) - 1
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

func TestGCPauseHistogram(t *testing.T) {
	var h pauseHistogram
	for _, ns := range []time.Duration{0, 1, 2, 3, 4, 8, 16, 32, 64, 128} {
		h.record(ns * time.Nanosecond)
	}
	if got := h.percentile(50, 100); got != 7 {
		t.Fatalf("p50 = %d ns, want bucket upper bound 7", got)
	}
	if got := h.percentile(90, 100); got != 127 {
		t.Fatalf("p90 = %d ns, want bucket upper bound 127", got)
	}
	if got := h.percentile(99, 100); got != 128 {
		t.Fatalf("p99 = %d ns, want bucket upper bound 128", got)
	}
	if h.maxNS != 128 || h.count != 10 {
		t.Fatalf("histogram max/count = %d/%d, want 128/10", h.maxNS, h.count)
	}
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
					c, err := NewCollector(gcMatrixConfig(profile.profile), types)
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
					var checksum uint64
					b.ReportAllocs()
					b.ReportMetric(100, "objects/op")
					b.ReportMetric(float64(survival), "survival-percent")
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						b.StopTimer()
						for j := 0; j < 100; j++ {
							r, err := layout.alloc(c)
							if err != nil {
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
						pauses.record(time.Since(start))
						b.StopTimer()
						if err != nil {
							b.Fatal(err)
						}
						if got := c.Stats().LiveObjects; got != uint32(survival) {
							b.Fatalf("live objects = %d, want %d", got, survival)
						}
						for j := range rootValues {
							typeID, err := c.ObjectType(Ref(rootValues[j]))
							if err != nil || typeID != layout.typeID {
								b.Fatalf("survivor type = %d, %v; want %d", typeID, err, layout.typeID)
							}
							checksum += uint64(typeID) + uint64(j) + 1
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
				})
			}
		}
	}
}

// BenchmarkGCSparseRememberedArray is the future card-table discriminator: an
// old reference array receives either two distant young writes or dense writes.
// Today's collector scans the whole remembered array; card-driven collection
// should make the sparse case proportional to dirty regions instead.
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
		for _, writes := range []int{2, 1024} {
			b.Run(fmt.Sprintf("elements=%d/writes=%d", length, writes), func(b *testing.B) {
				c, err := NewCollector(Config{NurseryBytes: 1 << 20, ThroughputHeapBytes: 64 << 20}, []TypeDesc{leaf, refs})
				if err != nil {
					b.Fatal(err)
				}
				b.Cleanup(c.Close)
				array, err := c.NewArrayDefault(1, length)
				if err != nil {
					b.Fatal(err)
				}
				if err := c.ForcePromote(array); err != nil {
					b.Fatal(err)
				}
				arrayRoot := Root(array)
				roots := Slots{&arrayRoot}
				children := make([]Ref, writes)
				var pauses pauseHistogram
				var checksum uint64
				b.ReportAllocs()
				b.ReportMetric(float64(length), "array-elements")
				b.ReportMetric(float64(writes), "dirty-writes/op")
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					b.StopTimer()
					for j := range children {
						children[j], err = c.NewStructDefault(0)
						if err != nil {
							b.Fatal(err)
						}
						index := uint32(uint64(j) * uint64(length-1) / uint64(max(1, writes-1)))
						if err := c.ArraySet(array, index, RefValue(children[j])); err != nil {
							b.Fatal(err)
						}
					}
					before := c.Stats()
					b.StartTimer()
					start := time.Now()
					err = c.CollectMinor(roots)
					pauses.record(time.Since(start))
					b.StopTimer()
					if err != nil {
						b.Fatal(err)
					}
					after := c.Stats()
					checksum += after.MinorRememberedScanned - before.MinorRememberedScanned
					for j := range children {
						index := uint32(uint64(j) * uint64(length-1) / uint64(max(1, writes-1)))
						v, err := c.ArrayGet(array, index)
						if err != nil || v.Ref != children[j] {
							b.Fatalf("array[%d] = %v, %v; want %v", index, v.Ref, err, children[j])
						}
						if err := c.ArraySet(array, index, RefValue(Null())); err != nil {
							b.Fatal(err)
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
				pauses.report(b, "minor-pause")
			})
		}
	}
}

// BenchmarkGCRootClassMatrix isolates root enumeration owned directly by the
// collector. Native frames, public tokens, foreign instances, and snapshot
// temporaries are integration-layer roots and belong in product fixtures.
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
	for _, profile := range profiles {
		for _, rootClass := range []string{"direct", "global", "table"} {
			for _, rootCount := range []int{1, 64, 4096} {
				b.Run(fmt.Sprintf("%s/%s/count=%d", profile.name, rootClass, rootCount), func(b *testing.B) {
					c, err := NewCollector(gcMatrixConfig(profile.profile), []TypeDesc{leaf})
					if err != nil {
						b.Fatal(err)
					}
					b.Cleanup(c.Close)
					object, err := c.NewStructDefault(0)
					if err != nil {
						b.Fatal(err)
					}
					var roots RootSet
					switch rootClass {
					case "direct":
						values := make([]Root, rootCount)
						slots := make(Slots, rootCount)
						for i := range values {
							values[i] = Root(object)
							slots[i] = &values[i]
						}
						roots = slots
					case "global":
						for i := 0; i < rootCount; i++ {
							c.NewGlobalSlot(object)
						}
					case "table":
						for i := 0; i < rootCount; i++ {
							c.NewTableSlot(object)
						}
					}
					var pauses pauseHistogram
					var checksum uint64
					b.ReportAllocs()
					b.ReportMetric(float64(rootCount), "roots/op")
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						start := time.Now()
						if err := c.CollectFull(roots); err != nil {
							b.Fatal(err)
						}
						pauses.record(time.Since(start))
						checksum += uint64(c.Stats().LiveObjects)
					}
					b.StopTimer()
					if checksum != uint64(b.N) {
						b.Fatalf("live checksum = %d, want %d", checksum, b.N)
					}
					pauses.report(b, "full-pause")
				})
			}
		}
	}
}

// BenchmarkGCTinyStepMatrix exposes the unbounded-object problem tracked by
// #319. A future slot-budgeted scanner should increase steps with array length
// while keeping p99/max step latency bounded.
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
					stepPauses.record(time.Since(start))
					steps++
					if c.tinyGC.state == tinyIdle {
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

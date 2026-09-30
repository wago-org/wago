//go:build !wago_tiny_nonincremental

package gc

import (
	"fmt"
	"testing"
	"unsafe"
)

func BenchmarkTinyStepReferenceArray(b *testing.B) {
	refs, err := NewArrayDesc(0, StorageRefNull)
	if err != nil {
		b.Fatal(err)
	}
	for _, length := range []uint32{16, 256, 4096, 64 << 10} {
		b.Run(fmt.Sprintf("elements=%d", length), func(b *testing.B) {
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
			roots := &tinyDirectRoot{root: &root}
			var maxWork objectScanWork
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := c.Step(roots); err != nil {
					b.Fatal(err)
				}
				maxObjectScanWork(&maxWork, c.tinyGC.lastStepWork.objectScanWork())
			}
			b.StopTimer()
			reportTinyScanWork(b, maxWork, c)
		})
	}
}

func BenchmarkTinyStepSparseReferenceStruct(b *testing.B) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		b.Fatal(err)
	}
	for _, fieldCount := range []int{64, 1024, 4096} {
		b.Run(fmt.Sprintf("fields=%d", fieldCount), func(b *testing.B) {
			fields := make([]StorageKind, fieldCount)
			for i := range fields {
				fields[i] = StorageI32
			}
			fields[0], fields[len(fields)-1] = StorageRefNull, StorageRefNull
			sparse, err := NewStructDesc(1, fields)
			if err != nil {
				b.Fatal(err)
			}
			c, err := NewCollector(Config{Profile: ProfileTiny, TinyHeapBytes: 8 << 20, TinyBlockBytes: 16}, []TypeDesc{leaf, sparse})
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(c.Close)
			parent, err := c.NewStructDefault(1)
			if err != nil {
				b.Fatal(err)
			}
			child, err := c.NewStructDefault(0)
			if err != nil {
				b.Fatal(err)
			}
			if err := c.StructSet(parent, 0, RefValue(child)); err != nil {
				b.Fatal(err)
			}
			if err := c.StructSet(parent, uint32(fieldCount-1), RefValue(child)); err != nil {
				b.Fatal(err)
			}
			root := Root(parent)
			roots := &tinyDirectRoot{root: &root}
			var maxWork objectScanWork
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := c.Step(roots); err != nil {
					b.Fatal(err)
				}
				maxObjectScanWork(&maxWork, c.tinyGC.lastStepWork.objectScanWork())
			}
			b.StopTimer()
			reportTinyScanWork(b, maxWork, c)
		})
	}
}

func BenchmarkTinyCompleteCycleReferenceArray(b *testing.B) {
	refs, err := NewArrayDesc(0, StorageRefNull)
	if err != nil {
		b.Fatal(err)
	}
	for _, length := range []uint32{16, 256, 4096, 64 << 10} {
		b.Run(fmt.Sprintf("elements=%d", length), func(b *testing.B) {
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
			roots := &tinyDirectRoot{root: &root}
			var totalSteps uint64
			var maxWork objectScanWork
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for {
					if err := c.Step(roots); err != nil {
						b.Fatal(err)
					}
					totalSteps++
					maxObjectScanWork(&maxWork, c.tinyGC.lastStepWork.objectScanWork())
					if c.tinyGC.state == tinyIdle {
						break
					}
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(totalSteps)/float64(b.N), "steps/cycle")
			reportTinyScanWork(b, maxWork, c)
		})
	}
}

func maxObjectScanWork(dst *objectScanWork, work objectScanWork) {
	if work.ObjectRanges > dst.ObjectRanges {
		dst.ObjectRanges = work.ObjectRanges
	}
	if work.ScanEntries > dst.ScanEntries {
		dst.ScanEntries = work.ScanEntries
	}
	if work.RefSlots > dst.RefSlots {
		dst.RefSlots = work.RefSlots
	}
	if work.PayloadBytes > dst.PayloadBytes {
		dst.PayloadBytes = work.PayloadBytes
	}
}

func reportTinyScanWork(b *testing.B, maxWork objectScanWork, c *Collector) {
	b.ReportMetric(float64(maxWork.ObjectRanges), "max-object-ranges/Step")
	b.ReportMetric(float64(maxWork.ScanEntries), "max-scan-entries/Step")
	b.ReportMetric(float64(maxWork.RefSlots), "max-ref-slots/Step")
	b.ReportMetric(float64(maxWork.PayloadBytes), "max-payload-bytes/Step")
	b.ReportMetric(float64(c.tiny.metadataBytes()), "allocator-metadata-bytes")
	b.ReportMetric(float64(unsafe.Sizeof(c.tinyGC.scan)+unsafe.Sizeof(c.tinyGC.lastStepWork)), "scan-state-bytes")
}

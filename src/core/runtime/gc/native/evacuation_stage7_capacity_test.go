//go:build !tinygo && !wago_target_tinygo

package gc

import (
	"runtime"
	"testing"
)

func TestEvacuationStage7SingleProcessor(t *testing.T) {
	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)
	c, roots := stage7Heap(t, 8, 128, 6, true)
	c.throughput.mature.pressure = true
	if err := c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	d := c.CollectionDiagnostics()
	if d.Evacuation != "moved" || d.CopyWorkers != 1 || d.ReservationWorkers != 8 || d.Need != 4096+8*32768 {
		t.Fatal(d)
	}
	if c.parallel != nil {
		t.Fatal("single processor started pool")
	}
}

// Source-sized live old capacity, using literal 4096-byte nodes and 65536
// stable identities. This is a correctness test, with no benchmark or timing.
func TestEvacuationStage7Capacity256MiB(t *testing.T) {
	parallelTestProcessors(t)
	fields := []StorageKind{StorageRefNull}
	for range 509 {
		fields = append(fields, StorageI64)
	}
	d, err := NewStructDesc(0, fields)
	if err != nil || d.Size+PayloadOffset != 4096 {
		t.Fatal("capacity layout", d.Size, err)
	}
	c := newTestCollectorWithTypes(t, Config{Workers: 8, NurseryBytes: 8 << 20, NurseryRegionShift: 12, MatureEvacuation: true, ThroughputHeapBytes: 272 << 20, ThroughputPageBytes: 4096}, []TypeDesc{d})
	head := Null()
	roots := Slots{(*Root)(&head)}
	for i := 0; i < 65536; i++ {
		r, err := c.NewStructDefaultWithRoots(0, roots)
		if err != nil {
			t.Fatal(i, err)
		}
		if err = c.StructSet(r, 0, RefValue(head)); err != nil {
			t.Fatal(err)
		}
		if err = c.StructSet(r, 509, I64Value(int64(i))); err != nil {
			t.Fatal(err)
		}
		head = r
		if (i+1)%1024 == 0 {
			if err = c.CollectMinor(roots); err != nil {
				t.Fatal(i, err)
			}
		}
	}
	if c.throughput.bump != 256<<20 {
		t.Fatal("physical capacity not reached", c.throughput.bump)
	}
	c.cfg.VerifyAfterCollect = true
	c.throughput.mature.pressure = true
	if err = c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	if c.Stats().LiveObjects != 65536 || c.CollectionDiagnostics().Evacuation != "no-candidates" || c.CollectionDiagnostics().Pressure {
		t.Fatal("dense capacity pressure", c.CollectionDiagnostics())
	}
	if a := c.ParallelActivity(); a.Trace[7] == 0 || a.Sweep[7] == 0 {
		t.Fatal("capacity did not exercise all participants", a)
	}
	r := head
	for i := 65535; i >= 0; i-- {
		v, err := c.StructGet(r, 509)
		if err != nil || v.Bits != uint64(i) {
			t.Fatal("capacity payload", i, v, err)
		}
		v, err = c.StructGet(r, 0)
		if err != nil {
			t.Fatal(err)
		}
		r = v.Ref
	}
	if !r.IsNull() {
		t.Fatal("chain tail")
	}
	head = Null()
	if err = c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	if c.Stats().LiveObjects != 0 || c.throughput.bump != 0 || c.throughput.pendingBytes != 0 {
		t.Fatal("capacity not reclaimed")
	}
}

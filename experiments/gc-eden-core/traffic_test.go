//go:build wago_gc_compact_eden && wago_gc_compact_age2

package edencore_test

import (
	"encoding/binary"
	"errors"
	"fmt"
	gc "github.com/wago-org/wago/experiments/compactgc"
	"os"
	"testing"
)

// Core-only allocation-driven traffic. No native compiler, adapter, or V8 path.
func BenchmarkEdenTraffic(b *testing.B) {
	policies := []string{"promote", "age2", "eden", "eden-same-fresh"}
	if p := os.Getenv("GC_CORE_POLICY"); p != "" {
		policies = []string{p}
	}
	for _, policy := range policies {
		for _, shape := range []string{"dead-heavy", "persistent344KiB", "array-bulk", "rolling84x4096", "dense-persistent", "old-young-mutation"} {
			b.Run(policy+"/"+shape, func(b *testing.B) {
				ds := []gc.Descriptor{{Kind: gc.KindStruct, Align: 8, PayloadBytes: 16}, {Kind: gc.KindArray, Align: 8, ElementSize: 1}, {Kind: gc.KindArray, Align: 8, ElementSize: 4, ElementRef: true}}
				var c *gc.Collector
				var err error
				switch policy {
				case "promote":
					c, err = gc.NewWithTypes(2<<20, 8<<20, ds)
				case "age2":
					c, err = gc.NewAge2WithTypes(2<<20, 8<<20, 512<<10, ds)
				case "eden":
					c, err = gc.NewEdenWithTypes(1536<<10, 256<<10, 8<<20, ds)
				case "eden-same-fresh":
					c, err = gc.NewEdenWithTypes(2<<20, 256<<10, 8<<20, ds)
				default:
					b.Fatal("unknown policy", policy)
				}
				if err != nil {
					b.Fatal(err)
				}
				var root gc.Ref
				var expected [84]uint64
				var nextID uint64
				roots := []*gc.Ref{&root}
				var allocations, minor, full, minorFailed uint64
				alloc := func(id gc.TypeID, n uint32) gc.Ref {
					makeOne := func() (gc.Ref, error) {
						if id == 0 {
							return c.NewStruct(0)
						}
						return c.NewArray(id, n)
					}
					r, e := makeOne()
					if errors.Is(e, gc.ErrOOM) {
						if e = c.Minor(roots); e != nil {
							minorFailed++
							if !errors.Is(e, gc.ErrOOM) {
								b.Fatal(e)
							}
							if e = c.Full(roots); e != nil {
								b.Fatal(e)
							}
							full++
						} else {
							minor++
						}
						r, e = makeOne()
						if errors.Is(e, gc.ErrOOM) {
							if e = c.Full(roots); e != nil {
								b.Fatal(e)
							}
							full++
							r, e = makeOne()
						}
					}
					if e != nil {
						b.Fatal(e)
					}
					allocations++
					return r
				}
				if shape == "persistent344KiB" {
					// Layout16 header plus scalar payload is exactly344KiB live, over256KiB survivor.
					root = alloc(1, (344<<10)-16)
					p, e := c.Payload(root)
					if e != nil {
						b.Fatal(e)
					}
					binary.LittleEndian.PutUint64(p, 0x12345678)
				}
				if shape == "dense-persistent" || shape == "old-young-mutation" {
					root = alloc(2, 256)
					for i := uint32(0); i < 256; i++ {
						child := alloc(2, 32)
						if e := c.SetRef(root, i, child); e != nil {
							b.Fatal(e)
						}
						if shape == "dense-persistent" {
							for j := uint32(0); j < 32; j++ {
								if e := c.SetRef(child, j, root); e != nil {
									b.Fatal(e)
								}
							}
						}
					}
					if e := c.Full(roots); e != nil {
						b.Fatal(e)
					} // Equal old-root starting state.
				}
				if shape == "rolling84x4096" {
					root = alloc(2, 84)
					if e := c.Full(roots); e != nil {
						b.Fatal(e)
					}
				}
				allocations = 0
				minor = 0
				full = 0
				minorFailed = 0
				before, _ := c.AllocationSnapshot()
				b.ReportAllocs()
				b.ResetTimer()
				for epoch := 0; epoch < b.N; epoch++ {
					count := uint32(65537)
					if shape == "array-bulk" || shape == "rolling84x4096" {
						count = 2048
					}
					for i := uint32(0); i < count; i++ {
						var r gc.Ref
						if shape == "array-bulk" {
							r = alloc(1, 4096)
						} else if shape == "rolling84x4096" {
							r = alloc(1, 4080)
							nextID++
							slot := (nextID - 1) % 84
							p, e := c.Payload(r)
							if e != nil {
								b.Fatal(e)
							}
							binary.LittleEndian.PutUint64(p, nextID)
							if e := c.SetRef(root, uint32(slot), r); e != nil {
								b.Fatal(e)
							}
							expected[slot] = nextID
						} else {
							r = alloc(0, 0)
						}
						if shape == "old-young-mutation" {
							if e := c.SetRef(root, i%256, r); e != nil {
								b.Fatal(e)
							}
						}
					}
					if shape == "dense-persistent" {
						if e := c.Full(roots); e != nil {
							b.Fatal(e)
						}
						full++
					}
				}
				b.StopTimer()
				after, _ := c.AllocationSnapshot()
				if after-before != allocations {
					b.Fatal("allocation accounting", after-before, allocations)
				}
				if root != 0 {
					if _, e := c.Header(root); e != nil {
						b.Fatal(e)
					}
				}
				if shape == "persistent344KiB" {
					p, e := c.Payload(root)
					if e != nil || binary.LittleEndian.Uint64(p) != 0x12345678 {
						b.Fatal("retained payload", e)
					}
				}
				if shape == "dense-persistent" {
					for i := uint32(0); i < 256; i++ {
						child, e := c.RefField(root, i)
						if e != nil {
							b.Fatal(e)
						}
						for j := uint32(0); j < 32; j++ {
							back, e := c.RefField(child, j)
							if e != nil || back != root {
								b.Fatal("lost dense edge", e)
							}
						}
					}
				}
				if shape == "old-young-mutation" {
					for i := uint32(0); i < 256; i++ {
						child, e := c.RefField(root, i)
						if e != nil {
							b.Fatal(e)
						}
						if _, e := c.Header(child); e != nil {
							b.Fatal(e)
						}
					}
				}
				if shape == "rolling84x4096" {
					for i, id := range expected {
						child, e := c.RefField(root, uint32(i))
						if e != nil {
							b.Fatal(e)
						}
						p, e := c.Payload(child)
						if e != nil || binary.LittleEndian.Uint64(p) != id {
							b.Fatal("rolling payload mismatch", i, id, e)
						}
					}
				}
				total, fresh, cap := c.YoungBudget()
				n := float64(b.N)
				b.ReportMetric(float64(allocations)/n, "objects/op")
				b.ReportMetric(float64(minor)/n, "minor/op")
				b.ReportMetric(float64(full)/n, "full/op")
				b.ReportMetric(float64(minorFailed)/n, "minor-failed/op")
				b.ReportMetric(float64(c.FixedBytes()), "fixed-B")
				memory := c.MemoryUsage()
				b.ReportMetric(float64(memory.Collector), "collector-B")
				b.ReportMetric(float64(memory.Total()), "accounted-B")
				b.ReportMetric(float64(total), "young-B")
				b.ReportMetric(float64(fresh), "fresh-max-B")
				b.ReportMetric(float64(cap), "survivor-cap-B")
				b.Log(fmt.Sprintf("policy=%s shape=%s allocations=%d minor=%d full=%d failedMinor=%d", policy, shape, allocations, minor, full, minorFailed))
			})
		}
	}
}

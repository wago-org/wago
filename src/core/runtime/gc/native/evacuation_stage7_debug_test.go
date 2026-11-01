//go:build wagodebug && !tinygo && !wago_target_tinygo

package gc

import (
	"bytes"
	"reflect"
	"testing"
)

func TestEvacuationStage7AdmissionFaults(t *testing.T) {
	for _, point := range []failurePoint{failMatureMetadata, failBackingGrowth, failEvacuationCopy, failEvacuationCommit} {
		t.Run(string(rune('a'+point)), func(t *testing.T) {
			c, roots := stage7Heap(t, 1, 129, 6, true)
			h := &c.throughput
			before := append([]handleEntry(nil), c.handles...)
			payload := bytes.Clone(c.bytes(roots[0]))
			free := h.freeSpans()
			bump := h.bump
			h.mature.pressure = true
			target := any(c)
			if point == failMatureMetadata || point == failBackingGrowth {
				target = h
			}
			disarm := armFailure(target, point, 0)
			err := c.CollectFull(roots)
			disarm()
			if point == failEvacuationCopy || point == failEvacuationCommit {
				if err == nil {
					t.Fatal("fault not reached")
				}
			} else {
				if err != nil || c.CollectionDiagnostics().Evacuation != "metadata" {
					t.Fatal("optional failure did not fall back", err, c.CollectionDiagnostics())
				}
			}
			if !reflect.DeepEqual(before, c.handles) || !bytes.Equal(payload, c.bytes(roots[0])) || bump != h.bump || !reflect.DeepEqual(free, h.freeSpans()) {
				t.Fatal("rollback source/ownership")
			}
			if err := c.Verify(roots); err != nil {
				t.Fatal(err)
			}
			h.mature.pressure = true
			if err := c.CollectFull(roots); err != nil {
				t.Fatal(err)
			}
			if c.CollectionDiagnostics().Evacuation != "moved" {
				t.Fatal("retry")
			}
		})
	}
}
func TestEvacuationStage7CommittedMaintenance(t *testing.T) {
	c, roots := stage7Heap(t, 1, 129, 6, true)
	before := *c.entry(roots[0])
	c.throughput.mature.pressure = true
	// No pending debt exists until committed source retirement.
	disarm := armFailure(&c.throughput, failThroughputReconciliation, 0)
	err := c.CollectFull(roots)
	disarm()
	if err == nil || *c.entry(roots[0]) == before || c.CollectionDiagnostics().Result != "committed-maintenance" || len(c.throughput.pendingFree) == 0 {
		t.Fatal("committed failure was rolled back", err, c.CollectionDiagnostics())
	}
	if err := c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	if err := c.Verify(roots); err != nil {
		t.Fatal(err)
	}
}

func TestEvacuationStage7PartialAdmissionRollback(t *testing.T) {
	for _, point := range []failurePoint{failMatureMetadata, failBackingGrowth} {
		c, roots := stage7Heap(t, 8, 1, 6, true)
		h := &c.throughput
		before := *c.entry(roots[0])
		bump := h.bump
		backing := len(h.mem)
		h.mature.pressure = true
		disarm := armFailure(h, point, 1)
		err := c.CollectFull(roots)
		disarm()
		if err != nil || c.CollectionDiagnostics().Evacuation != "metadata" {
			t.Fatal("second run fault not reached", point, err, c.CollectionDiagnostics())
		}
		if *c.entry(roots[0]) != before || h.bump != bump || len(h.mem) != backing {
			t.Fatal("partial run rollback")
		}
		if err := c.Verify(roots); err != nil {
			t.Fatal(err)
		}
	}
}
func TestEvacuationStage7SerialPartialCopyRollback(t *testing.T) {
	for _, after := range []int{1, 129} {
		c, roots := stage7Heap(t, 1, 129, 6, true)
		before := append([]handleEntry(nil), c.handles...)
		payloads := make([][]byte, len(roots))
		for i, r := range roots {
			payloads[i] = bytes.Clone(c.bytes(r))
		}
		c.throughput.mature.pressure = true
		disarm := armFailure(c, failEvacuationCopy, after)
		err := c.CollectFull(roots)
		disarm()
		if err == nil {
			t.Fatal("partial serial copy failure not reached")
		}
		if !reflect.DeepEqual(before, c.handles) {
			t.Fatal("partial publication")
		}
		for i, r := range roots {
			if !bytes.Equal(payloads[i], c.bytes(r)) {
				t.Fatal("partial copy source damaged")
			}
		}
		if err = c.Verify(roots); err != nil {
			t.Fatal(err)
		}
	}
}

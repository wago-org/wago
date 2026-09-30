package gc

import (
	"errors"
	"fmt"
	"math/rand"
	"slices"
	"testing"
	"unsafe"
)

type throughputIntervalModel struct {
	limit        uint32
	classLimit   uint32
	bump         uint32
	free         []throughputFreeSpan
	pending      []throughputFreeSpan
	pendingBytes uint64
}

func (m *throughputIntervalModel) alloc(size uint32, sp spaceKind) (handleEntry, error) {
	if size > ^uint32(0)-15 {
		return handleEntry{}, ErrAllocationTooLarge
	}
	allocSize := Align16(size)
	class := uint16(len(throughputClassSizes))
	if sp != spaceLarge && allocSize <= m.classLimit {
		cls := linearThroughputClassFor(allocSize, m.classLimit)
		if cls < 0 {
			return handleEntry{}, fmt.Errorf("model: no class for %d", allocSize)
		}
		allocSize = throughputClassSizes[cls]
		class = uint16(cls)
	}
	for {
		for i, span := range m.free {
			if span.size < allocSize {
				continue
			}
			off := span.off
			if span.size-allocSize >= 32 {
				m.free[i].off += allocSize
				m.free[i].size -= allocSize
			} else {
				allocSize = span.size
				class = uint16(len(throughputClassSizes))
				m.free = append(m.free[:i], m.free[i+1:]...)
			}
			return handleEntry{off: off, size: size, allocSize: allocSize, class: class, space: sp}, nil
		}
		if len(m.pending) == 0 {
			break
		}
		if err := m.reconcileOne(); err != nil {
			return handleEntry{}, err
		}
	}
	off := Align16(m.bump)
	end := uint64(off) + uint64(allocSize)
	if end > uint64(m.limit) {
		return handleEntry{}, errThroughputHeapExhausted
	}
	m.bump = uint32(end)
	return handleEntry{off: off, size: size, allocSize: allocSize, class: class, space: sp}, nil
}

func (m *throughputIntervalModel) deferRelease(e handleEntry) error {
	if e.allocSize == 0 || e.off%16 != 0 || e.off+e.allocSize < e.off || e.off+e.allocSize > m.bump {
		return errors.New("model: invalid deferred free")
	}
	m.pending = append(m.pending, throughputFreeSpan{off: e.off, size: e.allocSize})
	m.pendingBytes += uint64(e.allocSize)
	return nil
}

func (m *throughputIntervalModel) reconcileOne() error {
	last := len(m.pending) - 1
	span := m.pending[last]
	if err := m.release(handleEntry{off: span.off, allocSize: span.size}); err != nil {
		return err
	}
	m.pending[last] = throughputFreeSpan{}
	m.pending = m.pending[:last]
	m.pendingBytes -= uint64(span.size)
	return nil
}

func (m *throughputIntervalModel) reconcileAll() error {
	if len(m.pending) > 1 {
		ascending, descending := true, true
		for i := 1; i < len(m.pending); i++ {
			previous, current := m.pending[i-1], m.pending[i]
			if current.off < previous.off+previous.size {
				ascending = false
			}
			if current.off+current.size > previous.off {
				descending = false
			}
			if !ascending && !descending {
				break
			}
		}
		if ascending || descending {
			spans := m.pending
			out := spans[:1]
			for _, span := range spans[1:] {
				last := &out[len(out)-1]
				if ascending && last.off+last.size == span.off {
					last.size += span.size
				} else if descending && span.off+span.size == last.off {
					last.off = span.off
					last.size += span.size
				} else {
					out = append(out, span)
				}
			}
			clear(spans[len(out):])
			m.pending = out
		}
	}
	for len(m.pending) != 0 {
		if err := m.reconcileOne(); err != nil {
			return err
		}
	}
	return nil
}

func (m *throughputIntervalModel) release(e handleEntry) error {
	if e.allocSize == 0 || e.off%16 != 0 || e.off+e.allocSize < e.off || e.off+e.allocSize > m.bump {
		return errors.New("model: invalid free")
	}
	span := throughputFreeSpan{off: e.off, size: e.allocSize}
	pos := 0
	for pos < len(m.free) && m.free[pos].off < span.off {
		pos++
	}
	if pos > 0 {
		prev := m.free[pos-1]
		if prev.off+prev.size > span.off {
			return errors.New("model: overlapping free")
		}
		if prev.off+prev.size == span.off {
			span.off = prev.off
			span.size += prev.size
			m.free = append(m.free[:pos-1], m.free[pos:]...)
			pos--
		}
	}
	if pos < len(m.free) {
		next := m.free[pos]
		if span.off+span.size > next.off {
			return errors.New("model: overlapping free")
		}
		if span.off+span.size == next.off {
			span.size += next.size
			m.free = append(m.free[:pos], m.free[pos+1:]...)
		}
	}
	if span.off+span.size == m.bump {
		m.bump = span.off
		return nil
	}
	m.free = append(m.free, throughputFreeSpan{})
	copy(m.free[pos+1:], m.free[pos:])
	m.free[pos] = span
	return nil
}

func assertThroughputMatchesModel(t testing.TB, h *throughputHeap, m *throughputIntervalModel, live []handleEntry) {
	t.Helper()
	if h.bump != m.bump {
		t.Fatalf("bump = %d, want %d", h.bump, m.bump)
	}
	got := h.freeSpans()
	if !slices.Equal(got, m.free) {
		t.Fatalf("free spans = %v, want %v", got, m.free)
	}
	if !slices.Equal(h.pendingFree, m.pending) || h.pendingBytes != m.pendingBytes {
		t.Fatalf("pending spans/bytes = %v/%d, want %v/%d", h.pendingFree, h.pendingBytes, m.pending, m.pendingBytes)
	}
	var freeBytes uint64
	var largest uint32
	for _, span := range m.free {
		freeBytes += uint64(span.size)
		largest = max(largest, span.size)
	}
	if h.freeBytes != freeBytes || h.spanCount != uint32(len(m.free)) || h.largestFree() != largest {
		t.Fatalf("summary free=%d/%d spans=%d/%d largest=%d/%d", h.freeBytes, freeBytes, h.spanCount, len(m.free), h.largestFree(), largest)
	}
	handles := make([]handleEntry, 1, len(live)+1)
	handles = append(handles, live...)
	if err := h.verify(handles); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func runThroughputModelOperations(t testing.TB, operations []byte) {
	t.Helper()
	const limit = 64 << 10
	var h throughputHeap
	if err := h.Init(Config{ThroughputHeapBytes: limit, ThroughputPageBytes: 4096, ThroughputClassLimit: 4096}); err != nil {
		t.Fatal(err)
	}
	model := throughputIntervalModel{limit: limit, classLimit: 4096}
	live := make([]handleEntry, 0, 256)
	for at := 0; at < len(operations); {
		op := operations[at]
		at++
		switch op & 7 {
		case 0:
			if len(live) == 0 {
				break
			}
			index := int(op>>3) % len(live)
			e := live[index]
			if err := h.free(e); err != nil {
				t.Fatalf("free %d: %v", index, err)
			}
			if err := model.release(e); err != nil {
				t.Fatalf("model free %d: %v", index, err)
			}
			live[index] = live[len(live)-1]
			live = live[:len(live)-1]
		case 4:
			if len(live) == 0 {
				break
			}
			index := int(op>>3) % len(live)
			e := live[index]
			if err := h.deferFree(e); err != nil {
				t.Fatalf("defer free %d: %v", index, err)
			}
			if err := model.deferRelease(e); err != nil {
				t.Fatalf("model defer free %d: %v", index, err)
			}
			live[index] = live[len(live)-1]
			live = live[:len(live)-1]
		case 6:
			if err := h.sweepAllPending(); err != nil {
				t.Fatalf("reconcile all: %v", err)
			}
			if err := model.reconcileAll(); err != nil {
				t.Fatalf("model reconcile all: %v", err)
			}
		default:
			if at+1 >= len(operations) {
				break
			}
			size := uint32(operations[at]) | uint32(operations[at+1])<<8
			at += 2
			size = 1 + size%(8<<10)
			space := spaceOld
			if op&0x80 != 0 {
				space = spaceLarge
			}
			got, gotErr := h.alloc(size, space)
			want, wantErr := model.alloc(size, space)
			if !errors.Is(gotErr, wantErr) && (gotErr == nil || wantErr == nil || gotErr.Error() != wantErr.Error()) {
				t.Fatalf("alloc size=%d space=%d errors = %v, %v", size, space, gotErr, wantErr)
			}
			if gotErr == nil {
				if got != want {
					t.Fatalf("alloc size=%d space=%d = %+v, want %+v", size, space, got, want)
				}
				live = append(live, got)
			}
		}
		assertThroughputMatchesModel(t, &h, &model, live)
	}
}

func TestThroughputFindSpanCountedMatchesFirstFit(t *testing.T) {
	var h throughputHeap
	if err := h.Init(Config{ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096, ThroughputClassLimit: 128}); err != nil {
		t.Fatal(err)
	}
	if idx, steps := h.findSpanCounted(16); idx != throughputNoSlot || steps != 1 {
		t.Fatalf("empty counted search = %d/%d", idx, steps)
	}
	first, err := h.alloc(32, spaceOld)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.alloc(32, spaceOld); err != nil {
		t.Fatal(err)
	}
	second, err := h.alloc(64, spaceOld)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.alloc(32, spaceOld); err != nil {
		t.Fatal(err)
	}
	if err := h.free(first); err != nil {
		t.Fatal(err)
	}
	if err := h.free(second); err != nil {
		t.Fatal(err)
	}

	idx, steps := h.findSpanCounted(16)
	if idx == throughputNoSlot {
		t.Fatal("small counted search missed")
	}
	if h.spanNodes[idx].off != first.off || steps == 0 || steps > h.spanCount {
		t.Fatalf("small counted search = idx %d off %d steps %d spans %d", idx, h.spanNodes[idx].off, steps, h.spanCount)
	}
	idx, steps = h.findSpanCounted(48)
	if idx == throughputNoSlot {
		t.Fatal("large counted search missed")
	}
	if h.spanNodes[idx].off != second.off || steps == 0 || steps > h.spanCount {
		t.Fatalf("large counted search = idx %d off %d steps %d spans %d", idx, h.spanNodes[idx].off, steps, h.spanCount)
	}
	if idx, steps = h.findSpanCounted(128); idx != throughputNoSlot || steps != 1 {
		t.Fatalf("impossible counted search = %d/%d", idx, steps)
	}
}

func TestThroughputAllocatorRandomizedIntervalModel(t *testing.T) {
	for seed := int64(1); seed <= 32; seed++ {
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			operations := make([]byte, 20_000)
			if _, err := rng.Read(operations); err != nil {
				t.Fatal(err)
			}
			runThroughputModelOperations(t, operations)
		})
	}
}

func FuzzThroughputAllocatorAgainstIntervalModel(f *testing.F) {
	f.Add([]byte{1, 32, 0, 2, 96, 0, 0, 3, 0x00, 0x10, 0})
	f.Add([]byte{0x81, 0xff, 0x1f, 1, 1, 0, 0, 0})
	f.Fuzz(func(t *testing.T, operations []byte) {
		// The interval oracle verifies the complete heap after every operation and
		// is intentionally superlinear on adversarial fragmentation. Favor fuzzing
		// breadth here; the deterministic randomized test above retains 20,000-step
		// depth coverage.
		if len(operations) > 256 {
			operations = operations[:256]
		}
		runThroughputModelOperations(t, operations)
	})
}

func BenchmarkThroughputRandomFragmentation(b *testing.B) {
	for _, slots := range []int{64, 1024, 16384} {
		b.Run(fmt.Sprintf("live-slots=%d", slots), func(b *testing.B) {
			limit := uint32(slots*4096 + 4096)
			var h throughputHeap
			if err := h.Init(Config{ThroughputHeapBytes: limit, ThroughputPageBytes: 4096, ThroughputClassLimit: 4096}); err != nil {
				b.Fatal(err)
			}
			live := make([]handleEntry, slots)
			for i := range live {
				size := uint32(16 + (i*80)%4096)
				e, err := h.alloc(size, spaceOld)
				if err != nil {
					b.Fatal(err)
				}
				live[i] = e
			}
			rng := uint32(1)
			var checksum uint64
			churn := func() {
				rng = rng*1664525 + 1013904223
				index := int(rng % uint32(len(live)))
				old := live[index]
				if err := h.free(old); err != nil {
					b.Fatal(err)
				}
				rng = rng*1664525 + 1013904223
				size := uint32(1 + rng%4096)
				e, err := h.alloc(size, spaceOld)
				if err != nil {
					// Keep the workload bounded if a larger random replacement
					// temporarily cannot fit the fragmented heap.
					e, err = h.alloc(old.size, old.space)
					if err != nil {
						b.Fatal(err)
					}
				}
				live[index] = e
				checksum += uint64(e.off) + uint64(e.allocSize)
			}
			for i := 0; i < slots*4; i++ {
				churn()
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				churn()
			}
			b.StopTimer()
			if checksum == 0 {
				b.Fatal("zero checksum")
			}
			if err := h.verify(live); err != nil {
				b.Fatal(err)
			}
			b.ReportMetric(float64(h.spanCount), "free-spans")
			b.ReportMetric(float64(h.freeBytes), "free-bytes")
			b.ReportMetric(float64(len(h.spanNodes))*float64(unsafe.Sizeof(throughputSpanNode{})), "metadata-bytes")
			b.ReportMetric(float64(h.largestFree()), "largest-free-bytes")
			if h.freeBytes != 0 {
				b.ReportMetric(100*(1-float64(h.largestFree())/float64(h.freeBytes)), "fragmentation-percent")
			}
		})
	}
}

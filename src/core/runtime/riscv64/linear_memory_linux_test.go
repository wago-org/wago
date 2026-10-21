//go:build linux && !tinygo

package riscv64

import (
	"encoding/binary"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestLinearMemoryBoundsGrowthAndLimits(t *testing.T) {
	m, err := NewLinearMemory(1, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err = m.Write(65535, []byte{17}); err != nil {
		t.Fatal(err)
	}
	for _, off := range []uint32{65536, 0xffffffff} {
		if err = m.Write(off, []byte{99}); err == nil {
			t.Fatal("OOB store", off)
		}
	}
	if old, ok := m.Grow(0); !ok || old != 1 {
		t.Fatal(old, ok)
	}
	if old, ok := m.Grow(2); !ok || old != 1 {
		t.Fatal(old, ok)
	}
	if old, ok := m.Grow(1); ok || old != 3 {
		t.Fatal(old, ok)
	}
	if old, ok := m.Grow(^uint32(0)); ok || old != 3 {
		t.Fatal(old, ok)
	}
	b := make([]byte, 2*MemoryPageBytes)
	if err = m.Read(65536, b); err != nil {
		t.Fatal(err)
	}
	for _, v := range b {
		if v != 0 {
			t.Fatal("new pages not zero")
		}
	}
	if err = m.Read(3*MemoryPageBytes, nil); err != nil {
		t.Fatal("zero-length end", err)
	}
	if err = m.Read(3*MemoryPageBytes+1, nil); err == nil {
		t.Fatal("zero-length beyond end")
	}
	for _, hasMax := range []bool{false, true} {
		x, err := NewLinearMemoryWithLimits(1, 65536, hasMax)
		if err != nil {
			t.Fatal(err)
		}
		if l, err := x.LeaseFor(1, 256, true); l != nil || err == nil {
			t.Fatal("quota confused with declared max", l, err)
		}
		l, err := x.LeaseFor(1, 65536, false)
		if err != nil {
			t.Fatal(err)
		}
		l.Close()
		x.Close()
	}
	z, err := NewLinearMemory(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	if err = z.Write(0, nil); err != nil {
		t.Fatal(err)
	}
	if old, ok := z.Grow(0); !ok || old != 0 {
		t.Fatal(old, ok)
	}
	if _, ok := z.Grow(1); ok {
		t.Fatal("zero maximum grew")
	}
}
func TestLinearMemoryImportedInitializationOrder(t *testing.T) {
	m, err := NewLinearMemory(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	l, err := m.Lease()
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	err = l.Initialize([]MemoryInitializer{{Offset: 3, Bytes: []byte{1, 2, 3}}, {Offset: 65535, Bytes: []byte{9, 9}}})
	if err == nil {
		t.Fatal("expected second segment failure")
	}
	var got [3]byte
	if err = m.Read(3, got[:]); err != nil || got != [3]byte{1, 2, 3} {
		t.Fatal(got, err)
	}
	var last [1]byte
	if err = m.Read(65535, last[:]); err != nil || last[0] != 0 {
		t.Fatal("failed segment partially wrote", last, err)
	}
}
func TestLinearMemoryLeaseCloseAndCleanupRetry(t *testing.T) {
	for _, failDescriptor := range []bool{false, true} {
		ops := testMemory()
		m, err := newLinearMemory(1, 2, ops)
		if err != nil {
			t.Fatal(err)
		}
		l, err := m.Lease()
		if err != nil {
			t.Fatal(err)
		}
		base, desc := pointer(m.mem), pointer(m.descriptor)
		lengths := [2]int{len(m.mem), len(m.descriptor)}
		failed := 0
		if failDescriptor {
			failed = 1
		}
		failure := errors.New("injected retirement failure")
		injectFailure := false
		var calls, unmapped [2]int
		unmap := ops.unmap
		ops.unmap = func(b []byte) error {
			i := 0
			if pointer(b) == desc {
				i = 1
			} else if pointer(b) != base {
				t.Fatalf("unmap unexpected address %#x", pointer(b))
			}
			if len(b) != lengths[i] {
				t.Fatalf("unmap length %d, want %d", len(b), lengths[i])
			}
			calls[i]++
			if injectFailure && i == failed {
				return failure
			}
			if err := unmap(b); err != nil {
				return err
			}
			unmapped[i]++
			return nil
		}
		if err = m.Close(); err != nil {
			t.Fatal(err)
		}
		if m.refs != 1 || calls != [2]int{} {
			t.Fatal("creator close retired live lease", m.refs, calls)
		}
		if _, err = m.Lease(); !errors.Is(err, ErrClosed) {
			t.Fatal("new lease after creator close", err)
		}
		if _, err = l.Descriptor(); err != nil {
			t.Fatal("live lease rejected", err)
		}
		if permissions(t, base) == "" || permissions(t, desc) == "" {
			t.Fatal("retired live lease")
		}
		injectFailure = true
		wantCalls, wantUnmapped := [2]int{1, 1}, [2]int{1, 1}
		wantUnmapped[failed] = 0
		retained := [2]uintptr{}
		retained[failed] = [2]uintptr{base, desc}[failed]
		for attempt := 1; attempt <= 2; attempt++ {
			if err = l.Close(); !errors.Is(err, failure) || m.refs != 0 || !l.token.closed {
				t.Fatal("failed cleanup", err, m.refs, l.token.closed)
			}
			wantCalls[failed] = attempt
			if calls != wantCalls || unmapped != wantUnmapped || [2]uintptr{pointer(m.mem), pointer(m.descriptor)} != retained {
				t.Fatal("retry ownership", calls, unmapped, pointer(m.mem), pointer(m.descriptor))
			}
		}
		injectFailure = false
		// A successfully unmapped address can be reused before /proc/self/maps
		// is read. Check exact unmap operations and released ownership instead.
		if err = l.Close(); err != nil || m.mem != nil || m.descriptor != nil || m.refs != 0 {
			t.Fatal("retry", err)
		}
		wantCalls[failed] = 3
		if calls != wantCalls || unmapped != [2]int{1, 1} {
			t.Fatal("cleanup operations", calls, unmapped)
		}
		if err = l.Close(); err != nil {
			t.Fatal("closed lease", err)
		}
		if err = m.Close(); err != nil || m.refs != 0 || calls != wantCalls {
			t.Fatal("repeated cleanup", err, m.refs, calls)
		}
	}
}
func TestLinearMemoryConstructionFailureRetainsOwner(t *testing.T) {
	ops := testMemory()
	mapRW := ops.mapRW
	unmap := ops.unmap
	var base uintptr
	ops.mapRW = func(n int) ([]byte, error) {
		if n == 24 {
			return nil, errors.New("descriptor map failure")
		}
		b, err := mapRW(n)
		base = pointer(b)
		return b, err
	}
	ops.unmap = func([]byte) error { return errors.New("memory unmap failure") }
	m, err := newLinearMemory(1, 1, ops)
	if m == nil || err == nil || permissions(t, base) == "" {
		t.Fatal(m, err)
	}
	ops.unmap = unmap
	if err = m.Close(); err != nil || permissions(t, base) != "" {
		t.Fatal(err)
	}
}
func TestGuestContextSerializesMemoryCallAndClose(t *testing.T) {
	ops := testMemory()
	m, err := newLinearMemory(1, 2, ops)
	if err != nil {
		t.Fatal(err)
	}
	l, err := m.Lease()
	if err != nil {
		t.Fatal(err)
	}
	desc, err := l.Descriptor()
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := NewGuestContext(desc, []uint64{11, 22})
	if err != nil {
		t.Fatal(err)
	}
	frame, err := newCallFrame(256, ops)
	if err != nil {
		t.Fatal(err)
	}
	defer frame.Close()
	entered, release := make(chan struct{}), make(chan struct{})
	e, err := newEngine(MinNativeStackBytes, func(code, a0, a1, low, top, n, trap uintptr) (uint64, uint64) {
		if binary.LittleEndian.Uint64(frame.mem[257*8:]) != uint64(pointer(ctx.mem)) {
			panic("hidden context cell")
		}
		if binary.LittleEndian.Uint64(ctx.mem) != desc {
			panic("memory descriptor")
		}
		close(entered)
		<-release
		// A simulated guest updates the sole authoritative length descriptor.
		binary.LittleEndian.PutUint64(m.descriptor[8:], 2*MemoryPageBytes)
		return 0, 0
	}, ops)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	b, err := newCodeBuffer(4, 4, ops)
	if err != nil {
		t.Fatal(err)
	}
	b.Append([]byte{0x13, 0, 0, 0})
	p, err := publishImage(b, 4, ops)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := ctx.Call(l, frame, e, p, 0, nil, 16); err != nil {
			t.Error(err)
		}
	}()
	<-entered
	grown := make(chan struct{})
	go func() { m.Grow(0); close(grown) }()
	select {
	case <-grown:
		t.Fatal("host growth overlapped guest")
	case <-time.After(10 * time.Millisecond):
	}
	closed := make(chan error, 1)
	go func() { closed <- ctx.Close() }()
	select {
	case err := <-closed:
		t.Fatal("context retired active call", err)
	case <-time.After(10 * time.Millisecond):
	}
	close(release)
	wg.Wait()
	<-grown
	if pages, err := m.Pages(); err != nil || pages != 2 {
		t.Fatal("stale host length", pages, err)
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	if err = l.Close(); err != nil {
		t.Fatal(err)
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestMemoryZeroOwnersAndContextPairing(t *testing.T) {
	var zero LinearMemory
	if _, err := zero.Pages(); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if err := zero.Read(0, nil); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if err := zero.Write(0, nil); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if _, ok := zero.Grow(0); ok {
		t.Fatal("zero owner grew")
	}
	if _, err := zero.Lease(); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if err := zero.Close(); err != nil || zero.refs != 0 {
		t.Fatal(err, zero.refs)
	}
	m, err := NewLinearMemory(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	other, err := NewLinearMemory(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	l, err := m.Lease()
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	wrong, err := other.Lease()
	if err != nil {
		t.Fatal(err)
	}
	defer wrong.Close()
	d, err := l.Descriptor()
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := NewGuestContext(d, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ctx.Close()
	// These must fail before any use of the deliberately nil frame/engine.
	for _, lease := range []*MemoryLease{nil, wrong} {
		if _, err := ctx.Call(lease, nil, nil, nil, 0, nil, 16); err == nil {
			t.Fatal("context/lease mismatch accepted")
		}
	}
	copyLease := *l
	if err = l.Close(); err != nil {
		t.Fatal(err)
	}
	if err = copyLease.Close(); err != nil || m.refs != 1 {
		t.Fatal("copied lease double release", err, m.refs)
	}
}

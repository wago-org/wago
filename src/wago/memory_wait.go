package wago

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

const (
	memoryWaitNotified uint32 = iota
	memoryWaitNotEqual
	memoryWaitTimedOut
)

var errMemoryWaitClosed = errors.New("wago: shared memory closed while waiting")

type memoryWaiterState struct {
	byOffset map[uint64]*memoryWaitQueue
	active   uint32
}

// memoryWaiters contains entries only for memories with active parked calls.
// The exact memoryState pointer is the identity; its own mutex serializes queue
// operations, while this mutex protects the short-lived registry map itself.
var memoryWaiters struct {
	sync.Mutex
	byMemory map[*memoryState]*memoryWaiterState
}

type memoryWaitQueue struct {
	head *memoryWaiter
	tail *memoryWaiter
}

type memoryWaiter struct {
	result chan error
	queue  *memoryWaitQueue
	prev   *memoryWaiter
	next   *memoryWaiter
	offset uint64
}

func (m *Memory) wait32(ctx context.Context, offset uint64, expected uint32, timeout int64) (uint32, error) {
	return m.wait(ctx, offset, uint64(expected), timeout, 4)
}

func (m *Memory) wait64(ctx context.Context, offset uint64, expected uint64, timeout int64) (uint32, error) {
	return m.wait(ctx, offset, expected, timeout, 8)
}

func (m *Memory) wait(ctx context.Context, offset, expected uint64, timeout int64, size uint64) (uint32, error) {
	if m == nil {
		return 0, errMemoryWaitClosed
	}
	if offset&(size-1) != 0 {
		return 0, &TrapError{Code: TrapAtomicUnaligned}
	}
	s := m.state.Load()
	if s == nil {
		return 0, &TrapError{Code: TrapExpectedSharedMemory}
	}
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	if !s.has(memoryStateWasmShared) {
		s.mu.Unlock()
		return 0, &TrapError{Code: TrapExpectedSharedMemory}
	}
	if s.has(memoryStateClosed) || m.jm == nil {
		s.mu.Unlock()
		return 0, errMemoryWaitClosed
	}
	b := m.jm.HostBytes()
	if offset > uint64(len(b)) || size > uint64(len(b))-offset {
		s.mu.Unlock()
		return 0, &TrapError{Code: TrapLinMemOutOfBounds}
	}
	p := unsafe.Pointer(&b[offset])
	var actual uint64
	if size == 4 {
		actual = uint64(atomic.LoadUint32((*uint32)(p)))
	} else {
		actual = atomic.LoadUint64((*uint64)(p))
	}
	if actual != expected {
		s.mu.Unlock()
		return memoryWaitNotEqual, nil
	}
	if timeout == 0 {
		s.mu.Unlock()
		return memoryWaitTimedOut, nil
	}
	w := s.addWaiterLocked(offset)
	s.mu.Unlock()

	if timeout < 0 {
		select {
		case err := <-w.result:
			return memoryWaitNotified, err
		case <-ctx.Done():
			return m.cancelWait(s, w, context.Cause(ctx))
		}
	}
	timer := time.NewTimer(time.Duration(timeout))
	defer timer.Stop()
	select {
	case err := <-w.result:
		return memoryWaitNotified, err
	case <-timer.C:
		return m.cancelWait(s, w, nil)
	case <-ctx.Done():
		return m.cancelWait(s, w, context.Cause(ctx))
	}
}

func (s *memoryState) addWaiterLocked(offset uint64) *memoryWaiter {
	ws := s.waiterStateLocked(true)
	q := ws.byOffset[offset]
	if q == nil {
		q = &memoryWaitQueue{}
		ws.byOffset[offset] = q
	}
	w := &memoryWaiter{result: make(chan error, 1), queue: q, prev: q.tail, offset: offset}
	if q.tail == nil {
		q.head = w
	} else {
		q.tail.next = w
	}
	q.tail = w
	ws.active++
	return w
}

func (m *Memory) cancelWait(s *memoryState, w *memoryWaiter, cancelErr error) (uint32, error) {
	s.mu.Lock()
	removed := s.removeWaiterLocked(w)
	s.mu.Unlock()
	if removed {
		if cancelErr != nil {
			return 0, cancelErr
		}
		return memoryWaitTimedOut, nil
	}
	return memoryWaitNotified, <-w.result
}

func (s *memoryState) removeWaiterLocked(w *memoryWaiter) bool {
	ws := s.waiterStateLocked(false)
	if w.queue == nil || ws == nil {
		return false
	}
	q := w.queue
	if w.prev == nil {
		q.head = w.next
	} else {
		w.prev.next = w.next
	}
	if w.next == nil {
		q.tail = w.prev
	} else {
		w.next.prev = w.prev
	}
	w.queue, w.prev, w.next = nil, nil, nil
	ws.active--
	if q.head == nil {
		delete(ws.byOffset, w.offset)
	}
	if ws.active == 0 {
		memoryWaiters.Lock()
		delete(memoryWaiters.byMemory, s)
		if len(memoryWaiters.byMemory) == 0 {
			memoryWaiters.byMemory = nil
		}
		memoryWaiters.Unlock()
	}
	return true
}

func (s *memoryState) waiterStateLocked(create bool) *memoryWaiterState {
	memoryWaiters.Lock()
	defer memoryWaiters.Unlock()
	ws := memoryWaiters.byMemory[s]
	if ws == nil && create {
		if memoryWaiters.byMemory == nil {
			memoryWaiters.byMemory = make(map[*memoryState]*memoryWaiterState)
		}
		ws = &memoryWaiterState{byOffset: make(map[uint64]*memoryWaitQueue)}
		memoryWaiters.byMemory[s] = ws
	}
	return ws
}

func (m *Memory) notify(offset uint64, count uint32) (uint32, error) {
	if m == nil {
		return 0, errMemoryWaitClosed
	}
	if offset&3 != 0 {
		return 0, &TrapError{Code: TrapAtomicUnaligned}
	}
	s := m.state.Load()
	if s == nil {
		jm := m.jm
		if jm == nil {
			return 0, errMemoryWaitClosed
		}
		b := jm.HostBytes()
		if offset > uint64(len(b)) || 4 > uint64(len(b))-offset {
			return 0, &TrapError{Code: TrapLinMemOutOfBounds}
		}
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.has(memoryStateClosed) || m.jm == nil {
		return 0, errMemoryWaitClosed
	}
	b := m.jm.HostBytes()
	if offset > uint64(len(b)) || 4 > uint64(len(b))-offset {
		return 0, &TrapError{Code: TrapLinMemOutOfBounds}
	}
	if !s.has(memoryStateWasmShared) {
		return 0, nil
	}
	ws := s.waiterStateLocked(false)
	if count == 0 || ws == nil {
		return 0, nil
	}
	var notified uint32
	for notified < count {
		ws = s.waiterStateLocked(false)
		if ws == nil {
			break
		}
		q := ws.byOffset[offset]
		if q == nil || q.head == nil {
			break
		}
		w := q.head
		s.removeWaiterLocked(w)
		w.result <- nil
		notified++
	}
	return notified, nil
}

func (s *memoryState) closeWaitersLocked() {
	for {
		ws := s.waiterStateLocked(false)
		if ws == nil {
			return
		}
		var w *memoryWaiter
		for _, q := range ws.byOffset {
			w = q.head
			break
		}
		if w == nil {
			memoryWaiters.Lock()
			delete(memoryWaiters.byMemory, s)
			memoryWaiters.Unlock()
			return
		}
		s.removeWaiterLocked(w)
		w.result <- errMemoryWaitClosed
	}
}

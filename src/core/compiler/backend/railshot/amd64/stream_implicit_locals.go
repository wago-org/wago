//go:build amd64 && !tinygo && !wago_profile

package amd64

import (
	"math/bits"
	"os"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

var streamingImplicitLocalsEnabled = os.Getenv("WAGO_EXPERIMENTAL_STREAM_IMPLICIT_LOCALS") == "1"

type streamLocalMask [4]uint64

func (m *streamLocalMask) put(i int)     { m[i/64] |= uint64(1) << uint(i%64) }
func (m *streamLocalMask) drop(i int)    { m[i/64] &^= uint64(1) << uint(i%64) }
func (m streamLocalMask) has(i int) bool { return m[i/64]&(uint64(1)<<uint(i%64)) != 0 }
func (m *streamLocalMask) take() int {
	for w, v := range m {
		if v != 0 {
			b := bits.TrailingZeros64(v)
			m[w] &^= uint64(1) << uint(b)
			return w*64 + b
		}
	}
	return -1
}
func (m streamLocalMask) count() int {
	n := 0
	for _, v := range m {
		n += bits.OnesCount64(v)
	}
	return n
}
func (m streamLocalMask) rank(i int) int {
	n := 0
	for w := 0; w < i/64; w++ {
		n += bits.OnesCount64(m[w])
	}
	return n + bits.OnesCount64(m[i/64]&(^uint64(0)>>uint(63-i%64)))
}

// Implicit means the current value exists in its canonical LocalOffset, with
// no node or reference. Zero without an implicit bit means retired, not zero.
// ageMask is immutable between restores, even when gets clear implicit bits.
type streamImplicitLocals struct {
	active, implicit, ageMask streamLocalMask
	ageBase                   uint16
}

func (s *streamState) implicitLocalBytes() uint64 {
	if s.implicitLocals == nil {
		return 0
	}
	return uint64(unsafe.Sizeof(streamImplicitLocals{}))
}
func (s *streamState) initializeImplicitLocals() {
	if !streamingImplicitLocalsEnabled {
		s.Discarded += s.implicitLocalBytes()
		s.implicitLocals = nil
		return
	}
	if s.implicitLocals == nil {
		s.implicitLocals = &streamImplicitLocals{}
	}
	*s.implicitLocals = streamImplicitLocals{}
	for i, id := range s.locals {
		if id != 0 {
			s.implicitLocals.active.put(i)
		}
	}
}
func (s *streamState) setLocalBinding(i int, id streamID) {
	s.locals[i] = id
	if p := s.implicitLocals; p != nil {
		p.implicit.drop(i)
		if id == 0 {
			p.active.drop(i)
		} else {
			p.active.put(i)
		}
	}
}
func (s *streamState) localValue(i int) streamID {
	if p := s.implicitLocals; p != nil && p.implicit.has(i) {
		if streamValueChecks && s.locals[i] != 0 {
			panic("stream implicit: active implicit local")
		}
		age := p.ageBase + uint16(p.ageMask.rank(i))
		id := s.addOrdered(streamNode{kind: streamBorrow, slot: int32(i), home: s.localHomeTag(i), wide: s.widths[i], refs: 1}, age)
		s.setLocalBinding(i, id)
		s.target.stats.peep("stream-implicit-local-get")
	}
	return s.locals[i]
}
func (s *streamState) releaseActiveLocals() {
	p := s.implicitLocals
	active := p.active
	for i := active.take(); i >= 0; i = active.take() {
		id := s.locals[i]
		s.locals[i] = 0
		s.release(id)
	}
	p.active = streamLocalMask{}
}
func (s *streamState) restoreImplicitBindings(live streamLocalMask) {
	p := s.implicitLocals
	count := live.count()
	if int(s.nodes[0].order)+count > streamMaxNodes {
		panic("stream implicit: admitted creation bound exceeded")
	}
	p.ageBase = s.nodes[0].order
	p.ageMask = live
	s.nodes[0].order += uint16(count)
	p.implicit = live
	// Only selected homes need concrete nodes immediately. They retain their old
	// ascending-local allocator ages, irrespective of home score ordering.
	var selected streamLocalMask
	for _, x := range s.homeLocals {
		if x != 0 && live.has(int(x)-1) {
			selected.put(int(x) - 1)
		}
	}
	for i := selected.take(); i >= 0; i = selected.take() {
		n := streamNode{kind: streamBorrow, slot: int32(i), home: s.localHomeTag(i), wide: s.widths[i], refs: 1}
		if s.residency != nil {
			for k, x := range s.homeLocals {
				if x != 0 && int(x)-1 == i {
					cell := s.residency.homes[k]
					s.retainBacking(cell)
					n.kind = streamBacking
					n.slot = int32(cell)
					break
				}
			}
		}
		id := s.addOrdered(n, p.ageBase+uint16(live.rank(i)))
		s.setLocalBinding(i, id)
	}
	s.target.stats.peep("stream-implicit-restore")
}
func (s *streamState) restoreImplicit(depth int) {
	s.clearBoundsFacts()
	p := s.implicitLocals
	live := p.active
	for i := range live {
		live[i] |= p.implicit[i]
	}
	s.releaseActiveLocals()
	for i, id := range s.stack {
		wide := s.node(id).wide
		s.release(id)
		if i < depth {
			s.stack[i] = s.add(streamNode{kind: shared.ScalarFrame, slot: int32(i + 1), wide: wide, refs: 1})
		}
	}
	s.stack = s.stack[:depth]
	s.restoreImplicitBindings(live)
}
func (s *streamState) streamRestoreImplicit(fr streamFlow, result bool) {
	s.clearBoundsFacts()
	s.releaseActiveLocals()
	for _, id := range s.stack {
		s.release(id)
	}
	s.stack = s.stack[:0]
	if s.residency != nil {
		s.restoreBackingMaps()
	}
	for i, w := range s.flowTypes[fr.typeStart : fr.typeStart+fr.base] {
		s.stack = append(s.stack, s.add(streamNode{kind: shared.ScalarFrame, slot: int32(i + 1), wide: w, refs: 1}))
	}
	if result && fr.result != 0 {
		s.stack = append(s.stack, s.add(streamNode{kind: shared.ScalarFrame, slot: int32(fr.base + 1), wide: fr.wide, refs: 1}))
	}
	var live streamLocalMask
	for i := range s.widths {
		live.put(i)
	}
	s.restoreImplicitBindings(live)
}
func (s *streamState) canonicalizeImplicit(registerResult bool) {
	s.clearBoundsFacts()
	capture := func(id streamID) {
		if id == 0 {
			return
		}
		n := s.node(id)
		if n.kind == streamBorrow && s.locals[n.slot] != id || n.kind == shared.ScalarFrame && int(n.slot) < s.tempBase {
			s.materialize(id, 0)
		}
	}
	for _, id := range s.stack {
		capture(id)
	}
	active := s.implicitLocals.active
	for i := active.take(); i >= 0; i = active.take() {
		capture(s.locals[i])
	}
	// All old canonical aliases, including deferred descendants, must become
	// independent BEFORE the first mutable local/stack agreement slot is written.
	for _, id := range s.stack {
		if s.node(id).kind == streamDeferred {
			s.materialize(id, 0)
		}
	}
	active = s.implicitLocals.active
	for i := active.take(); i >= 0; i = active.take() {
		id := s.locals[i]
		if s.node(id).kind == streamDeferred {
			s.materialize(id, 0)
		}
	}
	active = s.implicitLocals.active
	for i := active.take(); i >= 0; i = active.take() {
		id := s.locals[i]
		if s.node(id).home == s.localHomeTag(i) {
			continue
		}
		if _, selected := s.localHomeReg(i); selected {
			continue
		}
		r := s.materialize(id, 0)
		s.target.Store(s.target.LocalOffset(i), r, s.node(id).wide)
	}
	s.canonicalizeStack(registerResult)
}

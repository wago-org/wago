//go:build amd64 && !tinygo && !wago_profile

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"os"
	"time"
)

var streamingMinimumBoundsEnabled = os.Getenv("WAGO_EXPERIMENTAL_STREAM_MIN_BOUNDS") == "1"

// Only the existing immutable declaration minimum is used. This query runs
// before materialization erases deferred arithmetic; it never changes nodes.
func (s *streamState) streamMinimumMemoryAccess(id streamID, offset uint64, size int) bool {
	f := s.target
	if !streamingMinimumBoundsEnabled || f.threadedMemory0 || f.guardMode || !f.opt(optMemoryMinimumBounds) {
		return false
	}
	mt, ok := f.memoryType(0)
	if !ok || mt.Shared || mt.Limits.Addr64 || !shared.ProvenMinimumAccess(mt, 0, offset, size) {
		return false
	}
	var begin time.Time
	if diagnosticsEnabled && f.stats != nil {
		begin = time.Now()
	}
	budget := 32
	maximum := s.streamUnsignedAddressMaximum(id, &budget)
	proven := shared.ProvenMinimumAccess(mt, maximum, offset, size)
	if diagnosticsEnabled && f.stats != nil {
		f.stats.peep("stream-min-bounds-query")
		f.stats.Peephole["stream-min-bounds-node-visits"] += 32 - budget
		f.stats.Peephole["stream-min-bounds-query-ns"] += int(time.Since(begin).Nanoseconds())
	}
	return proven
}

// Budget counts occurrences, not depth, so shared expression DAGs cannot cause
// unbounded query work. Materialized values and mutable bindings are unknown.
func (s *streamState) streamUnsignedAddressMaximum(id streamID, budget *int) uint64 {
	const unknown = uint64(^uint32(0))
	if *budget <= 0 || id == 0 || uint64(id) >= uint64(len(s.nodes)) {
		return unknown
	}
	*budget--
	n := s.node(id)
	if n.refs == 0 || n.wide {
		return unknown
	}
	if n.kind == shared.ScalarConstant {
		return uint64(uint32(n.constant))
	}
	if n.kind != streamDeferred || n.operandWide {
		return unknown
	}
	switch n.op {
	case shared.IntAdd, shared.IntAnd, shared.IntOr, shared.IntXor, shared.IntMul, shared.IntShl, shared.IntShrU:
	default:
		return unknown
	}
	a := s.streamUnsignedAddressMaximum(n.left, budget)
	b := s.streamUnsignedAddressMaximum(n.right, budget)
	var constant uint64
	known := false
	if n.right != 0 && uint64(n.right) < uint64(len(s.nodes)) {
		r := s.node(n.right)
		known = r.refs != 0 && r.kind == shared.ScalarConstant && !r.wide
		if known {
			constant = uint64(uint32(r.constant))
		}
	}
	return shared.UnsignedOperationMaximum(n.op, a, b, constant, known, false)
}

//go:build arm64

package arm64

import (
	"unsafe"
)

func (s *stack) nodeMemory() (used, reserved uint64) {
	for i := range s.chunks {
		reserved += uint64(cap(s.chunks[i]))
		if i <= int(s.cur) {
			used += uint64(len(s.chunks[i]))
		}
	}
	size := uint64(unsafe.Sizeof(elem{}))
	coldSize := uint64(unsafe.Sizeof(elemCold{}))
	return used*size + uint64(len(s.cold))*coldSize,
		reserved*size + uint64(cap(s.cold))*coldSize
}

func (t machineType) is64() bool { return t == mtI64 || t == mtF64 }

func (t machineType) isFloat() bool { return t == mtF32 || t == mtF64 }

func (t machineType) isV128() bool { return t == mtV128 }

// isXMM reports whether the value lives in the SIMD/FP register file (V0–V31 on
// arm64 — the analog of x86's XMM registers). Name kept per the port contract's
// type/method-name-parity rule so the sibling emit files read like the originals.
func (t machineType) isXMM() bool { return t.isFloat() || t.isV128() || t == mtCustom }

func (t machineType) stackSlots() int {
	if t == mtV128 {
		return 2
	}
	return 1
}

// memRefStorage builds the storage for a deferred integer load: the bounds check
// has already run and `ea` holds the effective address, but the load (LDR) is
// deferred to the consuming op. On arm64 there are no memory operands to fold the
// load into, but deferring still lets the consumer pick the destination register
// and elide dead loads (WARP liftToRegInPlace). borrow >= 0 marks ea as local
// `borrow`'s pinned register read in place: consumers must not write or release
// it, and a local.set of that local materializes the load first (realizeLocalRefs).
func memRefStorage(ea Reg, disp int32, size int, signed, wide bool, borrow, aliasLocal int) storage {
	typ := mtI32
	if wide {
		typ = mtI64
	}
	sidx := size
	if signed {
		sidx |= 0x100
	}
	if aliasLocal >= 0 {
		sidx |= (aliasLocal + 1) << 10
	}
	return storage{kind: stMemRef, typ: typ, reg: ea, slot: uint32(disp), idx: uint32(sidx), cval: int64(borrow + 1)}
}

func fmemRefStorage(ea Reg, disp int32, f64 bool, borrow, aliasLocal int) storage {
	typ := mtF32
	size := 4
	if f64 {
		typ = mtF64
		size = 8
	}
	if aliasLocal >= 0 {
		size |= (aliasLocal + 1) << 10
	}
	return storage{kind: stMemRef, typ: typ, reg: ea, slot: uint32(disp), idx: uint32(size), cval: int64(borrow + 1)}
}

func (st storage) memDisp() int32 { return int32(st.slot) }

func (st storage) slotIndex() int { return int(st.slot) }

func (st storage) index() int { return int(st.idx) }

func (st storage) memSize() int { return int(st.idx & 0xff) }

func (st storage) memSigned() bool { return st.idx&0x100 != 0 }

func (st storage) memAliasLocal() int { return int(st.idx>>10) - 1 }

// memBorrow returns the local whose pinned register serves as this deferred
// load's address, or -1 when the address register is owned.
func (st storage) memBorrow() int { return int(st.cval) - 1 }

func (e *elem) elemKind() elemKind { return elemKind(e.st.meta >> elemKindShift) }

func (e *elem) setElemKind(kind elemKind) {
	e.st.meta = e.st.meta&^elemKindMask | uint8(kind)<<elemKindShift
}

func (e *elem) deferredOp() wOp { return wOp(e.st.slot) }

func (e *elem) setDeferredOp(op wOp) {
	e.st.slot = e.st.slot&^0xff | uint32(op)
}

func (e *elem) deferredDepth() int16 { return int16(e.st.slot >> 8) }

func (e *elem) setDeferredDepth(depth int16) {
	e.st.slot = uint32(e.deferredOp()) | uint32(uint16(depth))<<8
}

func (e *elem) child0ID() nodeID { return nodeID(uint32(uint64(e.st.cval))) }

func (e *elem) child1ID() nodeID { return nodeID(uint32(uint64(e.st.cval) >> 32)) }

func (e *elem) setChildren(arg0, arg1 nodeID) {
	e.st.cval = int64(uint64(arg0) | uint64(arg1)<<32)
}

// deferDepthOf is the subtree height contributed by an operand: its deferDepth
// when deferred, else 0 (a concrete value is a leaf).
func deferDepthOf(e *elem) int16 {
	if e != nil && e.elemKind() == ekDeferred {
		return e.deferredDepth()
	}
	return 0
}

// isDeferred reports whether e is an un-emitted operation.
func (e *elem) isDeferred() bool { return e.elemKind() == ekDeferred }

// initWithCap initializes an empty stack without replacing its address. The
// function compiler may already hold an alias when fallback first needs it.
func (s *stack) initWithCap(capHint int) {
	if capHint < minStackArenaCap {
		capHint = minStackArenaCap
	}
	next, geometric := stackArenaGrowthCaps(capHint)
	*s = stack{
		chunks:           [][]elem{make([]elem, 0, capHint)},
		nextChunkCap:     uint16(next),
		nextGeometricCap: uint16(geometric),
	}
	s.initSentinel()
}

func stackArenaGrowthCaps(firstCap int) (next, geometric int) {
	if firstCap < defaultStackArenaCap {
		// Fill to the next power-of-two total, then double the total.
		// A one-node underestimate must not triple retained small arenas.
		total := minStackArenaCap
		for total <= firstCap {
			total *= 2
		}
		return total - firstCap, total
	}
	total, geometric := defaultStackArenaCap, defaultStackArenaCap*2
	for total < firstCap {
		total += geometric
		if geometric < maxStackChunkCap {
			geometric *= 2
			if geometric > maxStackChunkCap {
				geometric = maxStackChunkCap
			}
		}
	}
	if remainder := total - firstCap; remainder > 0 {
		return remainder, geometric
	}
	next = geometric
	if geometric < maxStackChunkCap {
		geometric *= 2
		if geometric > maxStackChunkCap {
			geometric = maxStackChunkCap
		}
	}
	return next, geometric
}

func (s *stack) initSentinel() {
	s.detached = s.detached[:0]
	s.cur = 0
	chunk := &s.chunks[0]
	*chunk = append((*chunk)[:0], elem{})
	s.head = &(*chunk)[0]
	s.head.prev, s.head.next = sentinelNodeID, sentinelNodeID
	s.freeHead = nilNodeID
	s.pendingDeferred = 0
	s.maxPending = 0
}

// reset rewinds the stack to empty for reuse by the next function in a module
// compile, preserving the arena's backing capacity so the common case allocates
// nothing per function. The prior function's nodes are dead by the time this is
// called (its code is already emitted). alloc rezeroes every reused chunk slot,
// so no stale fields survive.
func (s *stack) reset() {
	clear(s.cold[:cap(s.cold)])
	s.cold = s.cold[:0]
	s.initSentinel()
}

func (s *stack) elemCold(e *elem) *elemCold {
	if e.st.cold == 0 {
		return nil
	}
	return &s.cold[e.st.cold-1]
}

func (s *stack) clearElemCold(e *elem) {
	if cold := s.elemCold(e); cold != nil {
		*cold = elemCold{}
	}
}

func stackArenaCapForBody(bodyLen, nLocals int) int {
	// Most node-producing opcodes are one byte, while locals, constants, calls,
	// memory operations, and prefixed instructions also carry immediates. One
	// node per two body bytes is a cheap corpus-backed estimate that avoids the
	// former per-opcode predictor. Stable chunk growth preserves correctness when
	// an unusually dense function exceeds it.
	nodes := bodyLen / 2
	return nodes + nLocals/4 + 1
}

func (s *stack) node(id nodeID) *elem {
	if id == nilNodeID {
		return nil
	}
	chunkIndex := int(uint32(id) >> 16)
	slot := int(uint16(id)) - 1
	return &s.chunks[chunkIndex][slot]
}

func (s *stack) prev(e *elem) *elem { return s.node(e.prev) }

func (s *stack) next(e *elem) *elem { return s.node(e.next) }

func (s *stack) arg0(e *elem) *elem { return s.node(e.child0ID()) }

func (s *stack) arg1(e *elem) *elem { return s.node(e.child1ID()) }

// alloc returns a stable ID and fresh zeroed node from the arena.
func (s *stack) alloc() (nodeID, *elem) {
	if s.freeHead != nilNodeID {
		id := s.freeHead
		e := s.node(id)
		s.freeHead = e.prev
		*e = elem{}
		return id, e
	}
	chunk := &s.chunks[s.cur]
	if len(*chunk) == cap(*chunk) {
		if s.cur == ^uint16(0) {
			panic("arm64: operand arena exceeds compact chunk domain")
		}
		s.cur++
		if int(s.cur) == len(s.chunks) {
			s.chunks = append(s.chunks, make([]elem, 0, int(s.nextChunkCap)))
			s.nextChunkCap = s.nextGeometricCap
			if s.nextGeometricCap < maxStackChunkCap {
				s.nextGeometricCap *= 2
				if s.nextGeometricCap > maxStackChunkCap {
					s.nextGeometricCap = maxStackChunkCap
				}
			}
		}
		chunk = &s.chunks[s.cur]
		*chunk = (*chunk)[:0]
	}
	*chunk = append(*chunk, elem{})
	slot := len(*chunk) - 1
	id := nodeID(uint32(s.cur)<<16 | uint32(slot+1))
	return id, &(*chunk)[slot]
}

// recycle makes an already-unlinked node available to the next allocation.
// IDs, unlike pointers, remain stable when an arena slot is reused. Callers
// must not retain a reference to e after this point.
func (s *stack) recycle(id nodeID, e *elem) {
	e.prev, e.next = s.freeHead, nilNodeID
	s.freeHead = id
}

// Popped values remain addressable while their consuming instruction lowers.
// Nested inlining retires only its own suffix, preserving the caller's values.
func (s *stack) recycleDetached(start int) {
	for _, id := range s.detached[start:] {
		s.recycle(id, s.node(id))
	}
	s.detached = s.detached[:start]
}

// retireLinked abandons the old physical model before a control-flow rebuild.
// Readers of the old roots may still be live in this lowering instruction, so
// defer reuse just as for individually popped roots. Unlinked caller operands
// are outside this walk and retain their enclosing instruction's lifetime.
func (s *stack) retireLinked() {
	for id := s.head.next; id != sentinelNodeID; {
		e := s.node(id)
		s.detached = append(s.detached, id)
		s.clearElemCold(e)
		id = e.next
	}
	s.pendingDeferred = 0
}

// push appends e as the new top of the stack and returns it.
func (s *stack) push(id nodeID, e *elem) *elem {
	lastID := s.head.prev
	last := s.node(lastID)
	e.prev, e.next = lastID, sentinelNodeID
	last.next, s.head.prev = id, id
	if e.isDeferred() {
		if s.pendingDeferred == maxPendingNodes {
			panic("arm64: pending operand packet exceeds hard node limit")
		}
		s.pendingDeferred++
		if s.pendingDeferred > s.maxPending {
			s.maxPending = s.pendingDeferred
		}
	}
	return e
}

// pushValue pushes a concrete value with the given storage.
func (s *stack) pushValue(st storage) *elem {
	// Constants materialize through W-register writes. Record the proof only
	// for concrete values, not deferred nodes that share this storage payload.
	if st.kind == stConst && st.typ == mtI32 {
		st.setValueFacts(st.valueFacts() | factUpper32Zero)
	}
	id, e := s.alloc()
	e.st = st
	e.setElemKind(ekValue)
	return s.push(id, e)
}

// back returns the top element, or nil when empty.
func (s *stack) back() *elem {
	if s.head.prev == sentinelNodeID {
		return nil
	}
	return s.node(s.head.prev)
}

// erase unlinks e from the physical list (used when a node is condensed away or
// consumed). It does not touch parent/sibling links.
func (s *stack) erase(e *elem) {
	if e.isDeferred() {
		s.removePendingDeferred()
	}
	s.prev(e).next, s.next(e).prev = e.next, e.prev
	e.prev, e.next = nilNodeID, nilNodeID
}

func (s *stack) removePendingDeferred() {
	if s.pendingDeferred == 0 {
		panic("arm64: pending operand packet accounting underflow")
	}
	s.pendingDeferred--
}

func (f *fn) boundPendingPacket() {
	for f.s.pendingDeferred >= defaultPendingNodes {
		root := f.s.oldestDeferredRoot()
		if root == nil {
			panic("arm64: pending operand packet has no deferred root")
		}
		before := f.s.pendingDeferred
		f.materialize(root)
		if f.s.pendingDeferred >= before {
			panic("arm64: pending operand packet did not make progress")
		}
		f.stats.peep("pending-packet-cap")
	}
}

// baseOfValentBlock walks the left spine of the valent block rooted at `root`
// down to its deepest leaf — the physical bottom of the block. Mirrors WARP's
// findBaseOfValentBlock.
func (s *stack) baseOfValentBlock(root *elem) *elem {
	top := root
	for top.isDeferred() {
		top = s.arg0(top)
	}
	return top
}

// pushBinOp pushes a deferred binary operation over the top two valent blocks:
// the right operand is the current top block, the left is the block below it.
// Guard-mode div/rem resolves pending traps now; other ops wait for a sink.
func (f *fn) pushBinOp(op wOp, typ machineType) {
	rightID := f.s.head.prev
	right := f.s.node(rightID)
	leftID := f.s.baseOfValentBlock(right).prev
	left := f.s.node(leftID)
	// Constant-fold when both operands are constants (WARP tryConstantPropagation).
	if right.elemKind() == ekValue && right.st.kind == stConst &&
		left.elemKind() == ekValue && left.st.kind == stConst {
		if foldable(op) {
			f.stats.peep("const-fold")
			v := foldBin(op, left.st.cval, right.st.cval, typ.is64())
			f.erase(right)
			f.erase(left)
			f.pushValue(storage{kind: stConst, typ: typ, cval: v})
			return
		}
		if isCompare(op) {
			// typ carries the operand width; a compare's result is always i32.
			f.stats.peep("const-fold")
			v := foldCompare(op, left.st.cval, right.st.cval, typ.is64())
			f.erase(right)
			f.erase(left)
			f.pushValue(storage{kind: stConst, typ: mtI32, cval: v})
			return
		}
	}
	// One-constant algebraic simplification + strength reduction (P4): identities
	// collapse without emitting a node; expensive ops rewrite to cheaper ones.
	if right.elemKind() == ekValue && right.st.kind == stConst {
		if op2, done := f.simplifyConstRHS(op, typ, left, right); done {
			f.stats.peep("alu-identity")
			return
		} else if op2 != op {
			f.stats.peep("strength-reduce")
			op = op2 // strength-reduced (mul 2ⁿ → shl, div_u 2ⁿ → shr_u, rem_u 2ⁿ → and)
		}
	}
	if f.simplifySameOperand(op, typ, left, right) {
		f.stats.peep("same-operand")
		return
	}
	// Cap deferred-tree height: condense the deeper operand now if deferring this
	// op would push the subtree past maxDeferDepth, so the tree condense() later
	// walks never pins more registers than the file holds. Rare on real code
	// (shallow trees), essential for pathological chains.
	if deferDepthOf(left) >= maxDeferDepth {
		f.materialize(left)
	}
	if deferDepthOf(right) >= maxDeferDepth {
		f.materialize(right)
	}
	f.boundPendingPacket()
	id, node := f.s.alloc()
	node.st.typ = typ
	node.setElemKind(ekDeferred)
	if profileEnabled && f.stats != nil && f.stats.RecordSources {
		f.rememberProfileNode(node)
	}
	node.setDeferredOp(op)
	if f.opt(optValueFacts) {
		node.st.setValueFacts(deferredResultFacts(op, typ))
	}
	node.setChildren(leftID, rightID)
	node.setDeferredDepth(1 + max16(deferDepthOf(left), deferDepthOf(right)))
	f.s.push(id, node)
	if f.guardMode && isDivRem(op) {
		// A later guard-backed load can execute when its address is evicted.
		// Emit this mandatory trap before such a load becomes a spill candidate.
		f.materializePendingTraps()
	}
}

func max16(a, b int16) int16 {
	if a > b {
		return a
	}
	return b
}

// simplifyConstRHS applies algebraic identities and strength reduction for a
// constant right operand. Returns (newOp, true) when fully handled (identity
// collapsed or constant result pushed), or (possibly rewritten op, false) when a
// deferred node should still be created. The right elem's cval may be rewritten
// (shift count / mask) alongside an op rewrite.
func (f *fn) simplifyConstRHS(op wOp, typ machineType, left, right *elem) (wOp, bool) {
	w := typ.is64()
	c := right.st.cval
	cu := uint64(c)
	ones := ^uint64(0)
	shiftMask := int64(63)
	if !w {
		cu = uint64(uint32(c))
		ones = uint64(^uint32(0))
		shiftMask = 31
	}
	dropRight := func() (wOp, bool) { f.erase(right); return op, true }
	switch op {
	case opAdd, opSub, opOr, opXor:
		if cu == 0 {
			return dropRight() // x±0, x|0, x^0 → x
		}
	case opShl, opShrU, opShrS, opRotl, opRotr:
		if c&shiftMask == 0 {
			return dropRight() // shift/rotate by 0 (mod width) → x
		}
	case opAnd:
		if cu == ones {
			return dropRight() // x & ~0 → x
		}
		if cu == 0 && f.discardSimple(left) {
			f.erase(right)
			f.pushValue(storage{kind: stConst, typ: typ})
			return op, true // x & 0 → 0
		}
	case opMul:
		switch {
		case cu == 1:
			return dropRight() // x*1 → x
		case cu == 0 && f.discardSimple(left):
			f.erase(right)
			f.pushValue(storage{kind: stConst, typ: typ})
			return op, true // x*0 → 0
		case cu != 0 && cu&(cu-1) == 0: // x * 2ⁿ → x << n
			right.st.cval = int64(log2u(cu))
			return opShl, false
		}
	case opDivU:
		switch {
		case cu == 1:
			return dropRight() // x/1 → x
		case cu != 0 && cu&(cu-1) == 0: // x /ᵤ 2ⁿ → x >>ᵤ n
			right.st.cval = int64(log2u(cu))
			return opShrU, false
		}
	case opRemU:
		switch {
		case cu == 1 && f.discardSimple(left): // x %ᵤ 1 → 0
			f.erase(right)
			f.pushValue(storage{kind: stConst, typ: typ})
			return op, true
		case cu != 0 && cu&(cu-1) == 0: // x %ᵤ 2ⁿ → x & (2ⁿ-1)
			right.st.cval = int64(cu - 1)
			return opAnd, false
		}
	}
	return op, false
}

// simplifySameOperand handles `local.get x; local.get x; <op>` — both operands
// reading the same local (borrowed or lazy): sub/xor → 0, and/or → x.
func (f *fn) simplifySameOperand(op wOp, typ machineType, left, right *elem) bool {
	if left.elemKind() != ekValue || right.elemKind() != ekValue {
		return false
	}
	sameLocal := (left.st.kind == stLocalRef || left.st.kind == stLocalReg) &&
		left.st.kind == right.st.kind && left.st.idx == right.st.idx
	if !sameLocal {
		return false
	}
	switch op {
	case opSub, opXor:
		f.erase(right)
		f.erase(left)
		f.pushValue(storage{kind: stConst, typ: typ})
		return true
	case opAnd, opOr:
		f.erase(right) // x stays
		return true
	case opEq, opLeS, opLeU, opGeS, opGeU:
		// x==x, x<=x, x>=x → 1 (integer compares only; floats go through fp.go).
		f.erase(right)
		f.erase(left)
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: 1})
		return true
	case opNe, opLtS, opLtU, opGtS, opGtU:
		// x!=x, x<x, x>x → 0.
		f.erase(right)
		f.erase(left)
		f.pushValue(storage{kind: stConst, typ: mtI32})
		return true
	}
	return false
}

func log2u(v uint64) int {
	n := 0
	for v > 1 {
		v >>= 1
		n++
	}
	return n
}

// pushUnOp pushes a deferred unary operation over the top valent block (clz/ctz/
// popcnt/eqz). typ carries the operand width; compare-style results become i32
// when condensed.
func (f *fn) pushUnOp(op wOp, typ machineType) {
	operandID := f.s.head.prev
	operand := f.s.node(operandID)
	// Constant-fold clz/ctz/popcnt/eqz and the width conversions over a constant.
	if operand.elemKind() == ekValue && operand.st.kind == stConst {
		if v, rtyp, ok := foldUnaryConst(op, operand.st.cval, typ); ok {
			f.stats.peep("const-fold")
			f.erase(operand)
			f.pushValue(storage{kind: stConst, typ: rtyp, cval: v})
			return
		}
	}
	if deferDepthOf(operand) >= maxDeferDepth {
		f.materialize(operand) // cap deferred-tree height (see pushBinOp)
	}
	f.boundPendingPacket()
	id, node := f.s.alloc()
	node.st.typ = typ
	node.setElemKind(ekDeferred)
	if profileEnabled && f.stats != nil && f.stats.RecordSources {
		f.rememberProfileNode(node)
	}
	node.setDeferredOp(op)
	if f.opt(optValueFacts) {
		node.st.setValueFacts(deferredResultFacts(op, typ))
	}
	node.setChildren(operandID, nilNodeID)
	node.setDeferredDepth(1 + deferDepthOf(operand))
	f.s.push(id, node)
}

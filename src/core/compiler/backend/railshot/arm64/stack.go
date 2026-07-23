//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

// The operand stack and its element model — ported from WARP's Stack /
// StackElement / StackType / VariableStorage (warp/src/core/compiler/common/).
//
// The stack is the compiler's working state: a list of not-yet-emitted operands
// and deferred operations ("valent blocks"). It is a doubly-linked list of *elem
// (Go's pointer-stable equivalent of WARP's intrusive list + iterators), so the
// parent/sibling links that overlay a deferred-action tree onto the physical
// stack stay valid across pushes and pops.
//
// A binary op like `i32.add` pushes a deferred-action node ON TOP of its two
// operand sub-trees; the operands stay on the stack and become the node's
// children (wired via parent/sibling). So a whole expression sits on the stack in
// postfix order with its root (the action node) on top, emitting no machine code
// until a "sink" forces it to condense.
//
// This file is architecture-neutral: it holds the operand-stack data model and
// the peephole/const-fold/strength-reduce simplifications, and emits NO machine
// code (no a64 encoder calls). The arm64 twin therefore differs from the amd64
// original in only one place — memRefFoldable (see §4a of the port contract) —
// plus comment wording (RSP→SP, "fold" → "LDR").

// machineType is the lowered value type (WARP's MachineType).
type machineType uint8

const (
	mtNone machineType = iota
	mtI32
	mtI64
	mtF32
	mtF64
	mtV128
)

func (t machineType) is64() bool    { return t == mtI64 || t == mtF64 }
func (t machineType) isFloat() bool { return t == mtF32 || t == mtF64 }
func (t machineType) isV128() bool  { return t == mtV128 }

// isXMM reports whether the value lives in the SIMD/FP register file (V0–V31 on
// arm64 — the analog of x86's XMM registers). Name kept per the port contract's
// type/method-name-parity rule so the sibling emit files read like the originals.
func (t machineType) isXMM() bool { return t.isFloat() || t.isV128() }
func (t machineType) stackSlots() int {
	if t == mtV128 {
		return 2
	}
	return 1
}

// storageKind is where a variable's value currently lives (WARP's VariableStorage
// location discriminant).
type storageKind uint8

const (
	stInvalid   storageKind = iota
	stConst                 // an immediate; cval holds the value/bits
	stReg                   // a physical register the value OWNS; reg holds it
	stSlot                  // a frame stack slot; slot holds the SP-relative slot index
	stLocalRef              // a frame-resident local read (lazy); idx = local index
	stLocalReg              // a register-pinned local read (borrowed); reg = pinned reg, idx = local
	stGlobalRef             // a reference to a wasm global; idx = global index
	stGlobReg               // a value-pinned global read (borrowed); reg = pinned reg, idx = global
	stMemRef                // a bounds-checked but not-yet-loaded memory value (deferred load):
	//	reg = effective-address register, slot = static disp, idx = size|(signed<<8)
)

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
	return storage{kind: stMemRef, typ: typ, reg: ea, slot: int(disp), idx: sidx, cval: int64(borrow + 1)}
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
	return storage{kind: stMemRef, typ: typ, reg: ea, slot: int(disp), idx: size, cval: int64(borrow + 1)}
}

func (st storage) memDisp() int32     { return int32(st.slot) }
func (st storage) memSize() int       { return st.idx & 0xff }
func (st storage) memSigned() bool    { return st.idx&0x100 != 0 }
func (st storage) memAliasLocal() int { return (st.idx >> 10) - 1 }

// memBorrow returns the local whose pinned register serves as this deferred
// load's address, or -1 when the address register is owned.
func (st storage) memBorrow() int { return int(st.cval) - 1 }

// storage records where a value lives and its machine type.
type storage struct {
	kind storageKind
	typ  machineType
	reg  Reg
	slot int
	idx  int   // local/global index for stLocalRef/stGlobalRef
	cval int64 // constant value/bits for stConst
}

// elemKind tags a stack node.
type elemKind uint8

const (
	ekValue    elemKind = iota // a concrete value (const / reg / slot / local-or-global ref) — storage is live
	ekDeferred                 // an un-emitted operation with operand children
	ekBlock                    // structural control frame (block/loop/if)
	ekSkip                     // tombstone: condensed-away node, skipped in traversal
)

// elem is one node on the operand stack: a value, a deferred operation, or a
// control-frame marker. Deferred nodes carry their opcode and operand links.
type elem struct {
	kind elemKind
	st   storage // valid when kind == ekValue

	// Intrusive doubly-linked list (physical stack order).
	prev, next *elem

	// Deferred-action tree (valid when kind == ekDeferred): the two operand
	// sub-tree roots. arg0 is the left/first operand (deeper on the stack), arg1
	// the right/second. This is the explicit-child form of WARP's implicit
	// sibling-over-the-physical-stack layout — architecturally equivalent (still a
	// deferred tree condensed by the same allocator), simpler for nesting.
	arg0, arg1 *elem

	// Deferred operation payload.
	op  wOp
	typ machineType // result type of a deferred op

	// deferDepth is the height of this deferred subtree (1 + max child height;
	// leaves/values are 0). Used to cap how deep a tree condense() may recurse so a
	// pathological left-spine cannot pin one register per level and exhaust the
	// file — see maxDeferDepth in pushBinOp. Valid only when kind == ekDeferred.
	deferDepth int16
}

// deferDepthOf is the subtree height contributed by an operand: its deferDepth
// when deferred, else 0 (a concrete value is a leaf).
func deferDepthOf(e *elem) int16 {
	if e != nil && e.kind == ekDeferred {
		return e.deferDepth
	}
	return 0
}

// maxDeferDepth caps deferred-tree height. condense() pins up to one register per
// level, so an unbounded left-spine (e.g. a long chain of variable shifts or
// adds) exhausts the register file. When a new node would exceed this, the deeper
// operand is condensed now, breaking the chain into register-sized segments. Set
// well under the neutral-register count so the segment always fits (even on the
// pinning-off recompile).
const maxDeferDepth = 6

// isDeferred reports whether e is an un-emitted operation.
func (e *elem) isDeferred() bool { return e.kind == ekDeferred }

// stack is the operand stack: a sentinel-terminated doubly-linked list with a
// bump arena of elems (never freed mid-function; that matches single-pass usage
// and keeps *elem pointers stable).
type stack struct {
	chunks [][]elem
	cur    int
	head   *elem
}

const (
	defaultStackArenaCap = 256
	minStackArenaCap     = 16
	maxStackChunkCap     = 8192
)

func newStack() *stack { return newStackWithCap(defaultStackArenaCap) }

func newStackWithCap(capHint int) *stack {
	if capHint < minStackArenaCap {
		capHint = minStackArenaCap
	}
	s := &stack{chunks: [][]elem{make([]elem, 0, capHint)}}
	s.initSentinel()
	return s
}

func (s *stack) initSentinel() {
	s.cur = 0
	chunk := &s.chunks[0]
	*chunk = append((*chunk)[:0], elem{})
	s.head = &(*chunk)[0]
	s.head.prev, s.head.next = s.head, s.head
}

// reset rewinds the stack to empty for reuse by the next function in a module
// compile, preserving the arena's backing capacity so the common case allocates
// nothing per function. The prior function's nodes are dead by the time this is
// called (its code is already emitted). alloc rezeroes every reused chunk slot,
// so no stale fields survive.
func (s *stack) reset() {
	s.initSentinel()
}

func stackArenaCapForBody(bodyLen, nLocals int) int {
	return stackArenaCapForHints(bodyLen, nLocals, 0)
}

func stackArenaCapForHints(bodyLen, nLocals, nodeHint int) int {
	// The arena is a per-function bump allocation for all stack nodes created while
	// walking the bytecode. Historically the hint was one node per body byte; keep
	// that as a ceiling, but let the pre-scan's opcode-based estimate avoid
	// reserving nodes for long immediates (notably 16-byte SIMD constants). The
	// chunked arena grows past an underestimate without moving prior nodes, so
	// pointer stability is preserved.
	return shared.StackArenaCapacity(bodyLen, nLocals, nodeHint)
}

// alloc returns a fresh zeroed node from the arena.
func (s *stack) alloc() *elem {
	chunk := &s.chunks[s.cur]
	if len(*chunk) == cap(*chunk) {
		s.cur++
		if s.cur == len(s.chunks) {
			nextCap := cap(*chunk) * 2
			if nextCap > maxStackChunkCap {
				nextCap = maxStackChunkCap
			}
			s.chunks = append(s.chunks, make([]elem, 0, nextCap))
		}
		chunk = &s.chunks[s.cur]
		*chunk = (*chunk)[:0]
	}
	*chunk = append(*chunk, elem{})
	return &(*chunk)[len(*chunk)-1]
}

// push appends e as the new top of the stack and returns it.
func (s *stack) push(e *elem) *elem {
	last := s.head.prev
	e.prev, e.next = last, s.head
	last.next, s.head.prev = e, e
	return e
}

// pushValue pushes a concrete value with the given storage.
func (s *stack) pushValue(st storage) *elem {
	e := s.alloc()
	e.kind, e.st = ekValue, st
	return s.push(e)
}

// back returns the top element, or nil when empty.
func (s *stack) back() *elem {
	if s.head.prev == s.head {
		return nil
	}
	return s.head.prev
}

// erase unlinks e from the physical list (used when a node is condensed away or
// consumed). It does not touch parent/sibling links.
func (s *stack) erase(e *elem) {
	e.prev.next, e.next.prev = e.next, e.prev
	e.prev, e.next = nil, nil
}

// --- deferred-tree navigation (WARP: getFirstOperand / findBaseOfValentBlock) ---

// baseOfValentBlock walks the left spine of the valent block rooted at `root`
// down to its deepest leaf — the physical bottom of the block. Mirrors WARP's
// findBaseOfValentBlock.
func baseOfValentBlock(root *elem) *elem {
	top := root
	for top.isDeferred() {
		top = top.arg0
	}
	return top
}

// pushBinOp pushes a deferred binary operation over the top two valent blocks:
// the right operand is the current top block, the left is the block below it. No
// machine code is emitted; the op condenses later when a sink forces it.
func (f *fn) pushBinOp(op wOp, typ machineType) {
	right := f.s.back()
	left := baseOfValentBlock(right).prev
	// Constant-fold when both operands are constants (WARP tryConstantPropagation).
	if right.kind == ekValue && right.st.kind == stConst &&
		left.kind == ekValue && left.st.kind == stConst {
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
	if right.kind == ekValue && right.st.kind == stConst {
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
	node := f.s.alloc()
	node.kind, node.op, node.typ = ekDeferred, op, typ
	node.arg0, node.arg1 = left, right
	node.deferDepth = 1 + max16(deferDepthOf(left), deferDepthOf(right))
	f.s.push(node)
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
	if left.kind != ekValue || right.kind != ekValue {
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

// discardSimple erases a left operand whose value is no longer needed (x*0,
// x&0) — only for simple, resource-light elems: a deferred tree or a pending
// memRef keeps its node (the simplification is skipped) rather than growing a
// full recursive release path for a rare case.
func (f *fn) discardSimple(left *elem) bool {
	if left.kind != ekValue {
		return false
	}
	switch left.st.kind {
	case stConst, stLocalRef, stLocalReg, stGlobReg, stSlot:
		f.erase(left)
		return true
	case stReg:
		f.release(left.st.reg)
		f.erase(left)
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
	operand := f.s.back()
	// Constant-fold clz/ctz/popcnt/eqz and the width conversions over a constant.
	if operand.kind == ekValue && operand.st.kind == stConst {
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
	node := f.s.alloc()
	node.kind, node.op, node.typ = ekDeferred, op, typ
	node.arg0 = operand
	node.deferDepth = 1 + deferDepthOf(operand)
	f.s.push(node)
}

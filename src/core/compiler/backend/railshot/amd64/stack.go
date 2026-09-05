//go:build amd64

package amd64

import (
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	coreplugins "github.com/wago-org/wago/src/core/plugins"
)

func (s *stack) nodeMemory() (used, reserved uint64) {
	for i := range s.chunks {
		reserved += uint64(cap(s.chunks[i]))
		if i <= s.cur {
			used += uint64(len(s.chunks[i]))
		}
	}
	size := uint64(unsafe.Sizeof(elem{}))
	coldSize := uint64(unsafe.Sizeof(elemCold{}))
	return used*size + uint64(len(s.cold))*coldSize,
		reserved*size + uint64(cap(s.cold))*coldSize
}

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

// machineType is the lowered value type (WARP's MachineType).
type machineType uint8

const (
	mtNone machineType = iota
	mtI32
	mtI64
	mtF32
	mtF64
	mtV128
	mtCustom
)

func (t machineType) is64() bool    { return t == mtI64 || t == mtF64 }
func (t machineType) isFloat() bool { return t == mtF32 || t == mtF64 }
func (t machineType) isV128() bool  { return t == mtV128 }
func (t machineType) isXMM() bool   { return t.isFloat() || t.isV128() || t == mtCustom }
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
	stFuncRef               // exact staged local ref.func provenance; idx = function index
	stReg                   // a physical register the value OWNS; reg holds it
	stSlot                  // a frame stack slot; slot holds the RSP-relative slot index
	stLocalRef              // a frame-resident local read (lazy); idx = local index
	stLocalReg              // a register-pinned local read (borrowed); reg = pinned reg, idx = local
	stGlobalRef             // a reference to a wasm global; idx = global index
	stGlobReg               // a value-pinned global read (borrowed); reg = pinned reg, idx = global
	stMemRef                // a bounds-checked but not-yet-loaded memory value (deferred load):
	//	reg = effective-address register, slot = static disp, idx = size|(signed<<8)
)

// memRefStorage builds the storage for a deferred integer load: the bounds check
// has already run and `ea` holds the effective address, but the mov is deferred
// so it can be folded as an r/m operand into a consuming op. borrow >= 0 marks
// ea as local `borrow`'s pinned register read in place (WARP liftToRegInPlace):
// consumers must not write or release it, and a local.set of that local
// materializes the load first (realizeLocalRefs).
func memRefStorage(ea Reg, disp int32, size int, signed, wide bool, borrow int) storage {
	typ := mtI32
	if wide {
		typ = mtI64
	}
	sidx := size
	if signed {
		sidx |= 0x100
	}
	return storage{kind: stMemRef, typ: typ, reg: ea, slot: uint32(disp), idx: uint32(sidx), cval: int64(borrow + 1)}
}

func fmemRefStorage(ea Reg, disp int32, f64 bool, borrow int) storage {
	typ := mtF32
	size := 4
	if f64 {
		typ = mtF64
		size = 8
	}
	return storage{kind: stMemRef, typ: typ, reg: ea, slot: uint32(disp), idx: uint32(size), cval: int64(borrow + 1)}
}

func (st storage) memDisp() int32  { return int32(st.slot) }
func (st storage) slotIndex() int  { return int(st.slot) }
func (st storage) index() int      { return int(st.idx) }
func (st storage) memSize() int    { return int(st.idx & 0xff) }
func (st storage) memSigned() bool { return st.idx&0x100 != 0 }

// memBorrow returns the local whose pinned register serves as this deferred
// load's address, or -1 when the address register is owned.
func (st storage) memBorrow() int { return int(st.cval) - 1 }

// memRefFoldable reports whether a deferred load can be folded directly as an
// ALU/CMP r/m operand of the given width — only full-width loads (no sub-width
// sign/zero extension) matching the op width.
func memRefFoldable(st storage, w bool) bool {
	return (w && st.memSize() == 8) || (!w && st.memSize() == 4)
}

// storage records where a value lives and its machine type.
type storage struct {
	cval int64  // constant value/bits for stConst
	slot uint32 // spill-slot index, bounded GC array length for local refs, or int32 displacement bits for stMemRef
	cold uint32 // index+1 into stack.cold for custom plugin values
	idx  uint32 // local/global/function index, bounded GC array length, or packed stMemRef metadata
	kind storageKind
	typ  machineType
	reg  Reg
	meta uint8 // semantic value facts and root-state bits
}

// elemCold contains state used only by custom plugin values. Keeping it out of
// storage removes a pointer and slice header from every ordinary operand node.
type elemCold struct {
	custom *coreplugins.CustomType
	vregs  []Reg
}

// elemKind tags a stack node.
type elemKind uint8

const (
	ekValue    elemKind = iota // a concrete value (const / reg / slot / local-or-global ref) — storage is live
	ekDeferred                 // an un-emitted operation with operand children
)

// elem is one node on the operand stack: a value, a deferred operation, or a
// control-frame marker. Deferred nodes carry their opcode and operand links.
type elem struct {
	// st is the variant payload. Concrete values use it as storage. Deferred
	// nodes reuse slot for their opcode, depth, and register-need labels; those
	// bits are otherwise dead for the variant. typ and metadata retain their
	// ordinary value meaning in both variants.
	st storage

	// Intrusive doubly-linked list (physical stack order).
	prev, next *elem

	// Deferred-action tree (valid when kind == ekDeferred): the two operand
	// sub-tree roots. arg0 is the left/first operand (deeper on the stack), arg1
	// the right/second. This is the explicit-child form of WARP's implicit
	// sibling-over-the-physical-stack layout — architecturally equivalent (still a
	// deferred tree condensed by the same allocator), simpler for nesting.
	arg0, arg1 *elem
}

const deferredStorageKind storageKind = 1 << 6

func (e *elem) elemKind() elemKind {
	if e.st.kind == deferredStorageKind {
		return ekDeferred
	}
	return ekValue
}
func (e *elem) setElemKind(kind elemKind) {
	if kind == ekDeferred {
		e.st.kind = deferredStorageKind
		return
	}
	e.st.kind = stInvalid
}

func (e *elem) isValue() bool { return e.st.kind != deferredStorageKind }

func (e *elem) deferredOp() wOp { return wOp(e.st.slot) }
func (e *elem) setDeferredOp(op wOp) {
	e.st.slot = e.st.slot&^0xff | uint32(op)
}

// deferredDepth is the deferred-subtree height. registerNeed is its bounded
// Sethi-Ullman label. Both fit in a byte: depth is capped at maxDeferDepth and
// register demand cannot exceed that height plus one.
func (e *elem) deferredDepth() int16 { return int16(uint8(e.st.slot >> 8)) }
func (e *elem) setDeferredDepth(depth int16) {
	e.st.slot = e.st.slot&^0xff00 | uint32(uint8(depth))<<8
}
func (e *elem) registerNeed() int16 { return int16(uint8(e.st.slot >> 16)) }
func (e *elem) setRegisterNeed(need int16) {
	e.st.slot = e.st.slot&^0xff0000 | uint32(uint8(need))<<16
}

func (e *elem) valueType() machineType { return e.st.typ }
func (e *elem) setValueType(typ machineType) {
	e.st.typ = typ
}

// deferDepthOf is the subtree height contributed by an operand: its deferDepth
// when deferred, else 0 (a concrete value is a leaf).
func deferDepthOf(e *elem) int16 {
	if e != nil && e.isDeferred() {
		return e.deferredDepth()
	}
	return 0
}

// maxDeferDepth caps deferred-tree height. condense() pins up to one register per
// level, so an unbounded left-spine (e.g. a long chain of variable shifts or
// adds) exhausts the register file. When a new node would exceed this, the deeper
// operand is condensed now, breaking the chain into register-sized segments. Set
// well under the neutral-register count so the segment always fits in one pass.
const maxDeferDepth = 6

// isDeferred reports whether e is an un-emitted operation.
func (e *elem) isDeferred() bool { return e.st.kind == deferredStorageKind }

// stack is the operand stack: a sentinel-terminated doubly-linked list backed by
// a chunked bump arena of elems. Each chunk is a fixed-capacity []elem that is
// never reallocated once created, so every *elem handed out stays valid for the
// life of the function even as the arena grows without bound. Sub-default hints
// preserve direct doubling. At or above 256, growth fills to the next legacy
// 256/512/... cumulative boundary and then resumes the capped geometric sequence,
// so an underestimate cannot regress legacy retention. reset() reuses every chunk
// across the module compile up to a fixed byte ceiling, so ordinary recurring
// demand allocates once while giant-function overflow remains ephemeral. Nodes
// are never freed mid-function — that matches single-pass usage.
type stack struct {
	chunks           [][]elem
	cold             []elemCold
	cur              int
	head             *elem
	nextChunkCap     uint16
	nextGeometricCap uint16
}

const (
	defaultStackArenaCap = 256  // first chunk capacity
	minStackArenaCap     = 16   // floor for an explicitly-sized first chunk
	maxStackChunkCap     = 8192 // ceiling on geometric chunk growth
)

func newStack() *stack { return newStackWithCap(defaultStackArenaCap) }

func newStackWithCap(capHint int) *stack {
	if capHint < minStackArenaCap {
		capHint = minStackArenaCap
	}
	next, geometric := stackArenaGrowthCaps(capHint)
	s := &stack{
		chunks:           [][]elem{make([]elem, 0, capHint)},
		nextChunkCap:     uint16(next),
		nextGeometricCap: uint16(geometric),
	}
	s.initSentinel()
	return s
}

func stackArenaGrowthCaps(firstCap int) (next, geometric int) {
	if firstCap < defaultStackArenaCap {
		next = firstCap * 2
		if next > maxStackChunkCap {
			next = maxStackChunkCap
		}
		geometric = next * 2
		if geometric > maxStackChunkCap {
			geometric = maxStackChunkCap
		}
		return next, geometric
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

// initSentinel rewinds to the first chunk and installs the sentinel node.
func (s *stack) initSentinel() {
	s.cur = 0
	chunk := &s.chunks[0]
	*chunk = append((*chunk)[:0], elem{})
	s.head = &(*chunk)[0]
	s.head.prev, s.head.next = s.head, s.head
}

// reset rewinds the stack to empty for reuse by the next function in a module
// compile, retaining every chunk's backing array so the common case allocates
// nothing per function. The prior function's nodes are dead by the time this is
// called (its code is already emitted), so dropping them is safe; alloc rezeroes
// every reused slot, so no stale fields survive.
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

func (s *stack) setElemCold(e *elem, custom *coreplugins.CustomType, vregs []Reg) {
	if e.st.cold == 0 {
		s.cold = append(s.cold, elemCold{})
		e.st.cold = uint32(len(s.cold))
	}
	*s.elemCold(e) = elemCold{custom: custom, vregs: vregs}
}

func (s *stack) clearElemCold(e *elem) {
	if cold := s.elemCold(e); cold != nil {
		*cold = elemCold{}
	}
}

// finishFunction releases the suffix above the fixed worker-retention budget.
// The caller invokes it only after the function is complete and has severed
// scratch-owned node references. Keeping every chunk within the byte budget
// makes reuse independent of function ordering; giant overflow is ephemeral.
func (s *stack) finishFunction() (discarded uint64) {
	elemBytes := uint64(unsafe.Sizeof(elem{}))
	retained, keep := uint64(0), 0
	for i := range s.chunks {
		chunkBytes := uint64(cap(s.chunks[i])) * elemBytes
		if i != 0 && (retained >= shared.MaxRetainedStackArenaBytes || chunkBytes > shared.MaxRetainedStackArenaBytes-retained) {
			break
		}
		retained += chunkBytes
		keep = i + 1
	}
	if keep == len(s.chunks) {
		return 0
	}
	for i := 0; i < keep; i++ {
		clear(s.chunks[i][:cap(s.chunks[i])])
	}
	for i := keep; i < len(s.chunks); i++ {
		discarded += uint64(cap(s.chunks[i])) * elemBytes
		s.chunks[i] = nil
	}
	s.chunks = s.chunks[:keep]
	s.resetGrowthCaps()
	s.initSentinel()
	return discarded
}

func (s *stack) resetGrowthCaps() {
	next, geometric := stackArenaGrowthCaps(cap(s.chunks[0]))
	s.nextChunkCap, s.nextGeometricCap = uint16(next), uint16(geometric)
	for range s.chunks[1:] {
		s.nextChunkCap = s.nextGeometricCap
		if s.nextGeometricCap < maxStackChunkCap {
			s.nextGeometricCap *= 2
			if s.nextGeometricCap > maxStackChunkCap {
				s.nextGeometricCap = maxStackChunkCap
			}
		}
	}
}

func stackArenaCapForBody(bodyLen, nLocals int) int {
	// Most node-producing opcodes are one byte, while locals, constants, calls,
	// memory operations, and prefixed instructions also carry immediates. Three
	// nodes per four body bytes is a cheap corpus-backed estimate that avoids the
	// former per-opcode predictor. Stable chunk growth preserves correctness when
	// an unusually dense function exceeds it.
	nodes := bodyLen - bodyLen/4
	if bodyLen&3 != 0 {
		nodes--
	}
	return nodes + nLocals/4 + 1
}

// alloc returns a fresh zeroed node from the arena. The returned pointer is
// stable for the life of the function: chunks are never reallocated, and when the
// current chunk is full alloc advances to (or grows) the next one rather than
// growing the current backing array in place.
func (s *stack) alloc() *elem {
	chunk := &s.chunks[s.cur]
	if len(*chunk) == cap(*chunk) {
		s.cur++
		if s.cur == len(s.chunks) {
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
	e.setElemKind(ekValue)
	e.st = st
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
	if right.isValue() && right.st.kind == stConst &&
		left.isValue() && left.st.kind == stConst {
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
	if right.isValue() && right.st.kind == stConst {
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
	node.setElemKind(ekDeferred)
	node.setDeferredOp(op)
	node.setValueType(typ)
	if f.opt(optValueFacts) {
		node.st.setValueFacts(deferredResultFacts(op, typ))
	}
	node.arg0, node.arg1 = left, right
	labelDeferredNode(node)
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
	if !left.isValue() || !right.isValue() {
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
	if !left.isValue() {
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
	if operand.isValue() && operand.st.kind == stConst {
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
	node.setElemKind(ekDeferred)
	node.setDeferredOp(op)
	node.setValueType(typ)
	if f.opt(optValueFacts) {
		node.st.setValueFacts(deferredResultFacts(op, typ))
	}
	node.arg0 = operand
	labelDeferredNode(node)
	f.s.push(node)
}

package x64

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
)

func (t machineType) is64() bool    { return t == mtI64 || t == mtF64 }
func (t machineType) isFloat() bool { return t == mtF32 || t == mtF64 }
func (t machineType) size() int {
	switch t {
	case mtI32, mtF32:
		return 4
	case mtI64, mtF64:
		return 8
	}
	return 0
}

// storageKind is where a variable's value currently lives (WARP's VariableStorage
// location discriminant).
type storageKind uint8

const (
	stInvalid   storageKind = iota
	stConst                 // an immediate; cval holds the value/bits
	stReg                   // a physical register; reg holds it
	stSlot                  // a frame stack slot; slot holds the RBP-relative index
	stLocalRef              // a reference to a wasm local (not yet materialized); idx = local index
	stGlobalRef             // a reference to a wasm global; idx = global index
)

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
	op         wOp
	sideEffect bool // must be emitted in program order (loads, traps)
	dataOffset uint32
	typ        machineType // result type of a deferred op

	ctrl *ctrlFrame // valid when kind == ekBlock
}

// isDeferred reports whether e is an un-emitted operation.
func (e *elem) isDeferred() bool { return e.kind == ekDeferred }

// stack is the operand stack: a sentinel-terminated doubly-linked list with a
// bump arena of elems (never freed mid-function; that matches single-pass usage
// and keeps *elem pointers stable).
type stack struct {
	arena []elem
	head  *elem // sentinel (arena[0]); head.next is the bottom, back() is the top
}

func newStack() *stack {
	s := &stack{arena: make([]elem, 0, 64)}
	s.arena = append(s.arena, elem{}) // sentinel
	s.head = &s.arena[0]
	s.head.prev, s.head.next = s.head, s.head
	return s
}

// alloc returns a fresh zeroed node from the arena.
func (s *stack) alloc() *elem {
	// Growing the arena may move earlier elems, invalidating pointers — so we
	// never let it reallocate: reserve generously and, if exceeded, spill to
	// individually-heap-allocated nodes (still pointer-stable).
	if len(s.arena) < cap(s.arena) {
		s.arena = append(s.arena, elem{})
		return &s.arena[len(s.arena)-1]
	}
	return &elem{}
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

// empty reports whether the stack holds no elements.
func (s *stack) empty() bool { return s.head.prev == s.head }

// erase unlinks e from the physical list (used when a node is condensed away or
// consumed). It does not touch parent/sibling links.
func (s *stack) erase(e *elem) {
	e.prev.next, e.next.prev = e.next, e.prev
	e.prev, e.next = nil, nil
}

// insertAfter links n immediately after e in the physical list.
func (s *stack) insertAfter(e, n *elem) {
	n.prev, n.next = e, e.next
	e.next.prev, e.next = n, n
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
	node := f.s.alloc()
	node.kind, node.op, node.typ = ekDeferred, op, typ
	node.arg0, node.arg1 = left, right
	f.s.push(node)
}

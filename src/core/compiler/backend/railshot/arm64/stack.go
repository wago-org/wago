//go:build arm64

package arm64

import (
	"os"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	coreplugins "github.com/wago-org/wago/src/core/plugins"
)

var operandNodeReuseEnabled = os.Getenv("WAGO_ARM64_NO_OPERAND_NODE_REUSE") != "1"

// The operand stack and its element model — ported from WARP's Stack /
// StackElement / StackType / VariableStorage (warp/src/core/compiler/common/).
//
// The stack is the compiler's working state: a list of not-yet-emitted operands
// and deferred operations ("valent blocks"). It is a doubly-linked list using
// stable arena IDs, while deferred tree children remain pointers during the
// staged compact-node migration. Both forms stay valid across pushes and pops.
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
	mtCustom
)

// storageKind is where a variable's value currently lives (WARP's VariableStorage
// location discriminant).
type storageKind uint8

const (
	stInvalid   storageKind = iota
	stConst                 // an immediate; cval holds the value/bits
	stFuncRef               // exact staged local ref.func provenance; idx = function index
	stReg                   // a physical register the value OWNS; reg holds it
	stSlot                  // a frame stack slot; slot holds the SP-relative slot index
	stLocalRef              // a frame-resident local read (lazy); idx = local index
	stLocalReg              // a register-pinned local read (borrowed); reg = pinned reg, idx = local
	stGlobalRef             // a reference to a wasm global; idx = global index
	stGlobReg               // a value-pinned global read (borrowed); reg = pinned reg, idx = global
	stMemRef                // a bounds-checked but not-yet-loaded memory value (deferred load):
	//	reg = effective-address register, slot = static disp, idx = size|(signed<<8)
)

// storage records where a value lives and its machine type.
type storage struct {
	cval int64  // constant value/bits for stConst
	slot uint32 // spill-slot index, or int32 displacement bits for stMemRef
	cold uint32 // index+1 into stack.cold for custom plugin values
	idx  uint32 // local/global/function index, or packed stMemRef metadata
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
	ekBlock                    // structural control frame (block/loop/if)
	ekSkip                     // tombstone: condensed-away node, skipped in traversal
)

// elem is one node on the operand stack: a value, a deferred operation, or a
// control-frame marker. Deferred nodes carry their opcode and operand links.
type elem struct {
	// st is the variant payload. Concrete values use it as storage. Deferred
	// nodes reuse cval for two child IDs and slot for opcode plus tree depth;
	// those fields are otherwise dead for the variant. typ and the low metadata
	// bits retain their ordinary value meaning in both variants.
	st storage

	// Intrusive doubly-linked list (physical stack order).
	prev, next nodeID
}

const (
	elemKindShift = 6
	elemKindMask  = uint8(0x3 << elemKindShift)
)

// nodeID is a stable arena coordinate. The high 16 bits select a chunk and the
// low 16 bits hold the slot plus one, leaving zero as nil. Stack chunks are
// capped far below 2^16 elements; exhausting the chunk field would require more
// compiler memory than a process can practically address and is rejected at the
// allocation boundary.
type nodeID uint32

const (
	nilNodeID      nodeID = 0
	sentinelNodeID nodeID = 1 // chunk 0, slot 0
)

// maxDeferDepth caps deferred-tree height. condense() pins up to one register per
// level, so an unbounded left-spine (e.g. a long chain of variable shifts or
// adds) exhausts the register file. When a new node would exceed this, the deeper
// operand is condensed now, breaking the chain into register-sized segments. Set
// well under the neutral-register count so the segment always fits in one pass.
const maxDeferDepth = 6

const (
	defaultPendingNodes = 32
	maxPendingNodes     = 64
)

// stack is the operand stack: a sentinel-terminated doubly-linked list with a
// stable arena of elems. Consumed subtrees are reusable immediately; detached
// operands become reusable only after their consuming instruction finishes.
// Temporary *elem views remain stable throughout that instruction. Sub-default hints fill to a
// power-of-two total before doubling.
// At or above 256, growth fills to the next legacy cumulative boundary before
// resuming geometric chunks, so an underestimate cannot regress legacy retention.
type stack struct {
	detached         []nodeID // popped nodes awaiting the end of their lowering instruction
	chunks           [][]elem
	cold             []elemCold
	head             *elem
	freeHead         nodeID
	cur              uint16
	nextChunkCap     uint16
	nextGeometricCap uint16
	pendingDeferred  uint8
	maxPending       uint8
}

const (
	defaultStackArenaCap = 256
	minStackArenaCap     = 16
	maxStackChunkCap     = 8192
)

func newStack() *stack { return newStackWithCap(defaultStackArenaCap) }

func newStackWithCap(capHint int) *stack {
	s := &stack{}
	s.initWithCap(capHint)
	return s
}

func (s *stack) setElemCold(e *elem, custom *coreplugins.CustomType, vregs []Reg) {
	if e.st.cold == 0 {
		s.cold = append(s.cold, elemCold{})
		e.st.cold = uint32(len(s.cold))
	}
	*s.elemCold(e) = elemCold{custom: custom, vregs: vregs}
}

// finishFunction releases the suffix above the fixed worker-retention budget.
// The caller invokes it only after the function is complete and has severed
// scratch-owned node references. Keeping every chunk within the byte budget
// makes reuse independent of function ordering; giant overflow is ephemeral.
func (s *stack) finishFunction() (discarded uint64) {
	// Unusually wide calls may temporarily collect many detached operands.
	// Do not retain their bookkeeping high-water for every later function.
	if cap(s.detached) > 256 {
		s.detached = nil
	}
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

func (s *stack) oldestDeferredRoot() *elem {
	var oldest *elem
	for root := s.back(); root != nil; {
		if root.isDeferred() {
			oldest = root
		}
		root = s.prev(s.baseOfValentBlock(root))
		if root == s.head {
			break
		}
	}
	return oldest
}

// --- deferred-tree navigation (WARP: getFirstOperand / findBaseOfValentBlock) ---

// discardSimple erases a left operand whose value is no longer needed (x*0,
// x&0) — only for simple, resource-light elems: a deferred tree or a pending
// memRef keeps its node (the simplification is skipped) rather than growing a
// full recursive release path for a rare case.
func (f *fn) discardSimple(left *elem) bool {
	if left.elemKind() != ekValue {
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

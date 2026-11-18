//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
)

// Independently isolate producer savings from the packed-consumer experiment.
var packedProducerEnabled = os.Getenv("WAGO_EXPERIMENTAL_PACKED_PRODUCER") == "1"

// A bounded parser result, not a materializer view or persistent graph node.
type packedProducerLeaf struct {
	index uint32
	kind  uint8
	reg   Reg
	typ   machineType
	value int64
}

func (f *fn) packedProducerLocal(x uint32) (packedProducerLeaf, bool) {
	if int(x) >= len(f.localType) || f.gcFrameLocal(int(x)) {
		return packedProducerLeaf{}, false
	}
	typ := f.localType[x]
	if typ != mtI32 && typ != mtI64 {
		return packedProducerLeaf{}, false
	}
	if f.localConstZero(int(x)) {
		return packedProducerLeaf{}, false
	}
	result := packedProducerLeaf{index: x, kind: uint8(terminalLocalRef), typ: typ}
	if reg, fp, pinned := f.pinReg(int(x)); pinned {
		state := f.locals[x].state
		if fp || reg >= 16 || (state != lsReg && state != lsStackReg) {
			return packedProducerLeaf{}, false
		}
		result.kind, result.reg = uint8(terminalLocalReg), reg
	}
	return result, true
}

// r is after the first decoded leaf, which has not yet been published. All
// rejection paths leave the real Reader and compiler state untouched.
func (f *fn) tryPackedProducer(r *wasm.Reader, first packedProducerLeaf) bool {
	if !packedProducerEnabled || !packedTerminalEnabled || profileEnabled || regallocCheckEnabled ||
		f.unreachable || f.inlineDepth != 0 || f.localBase != 0 || f.inlineForwardN != 0 || f.inlineTailConstants != nil ||
		len(f.intervalReg) != 0 || f.pinRelinquished || len(f.customInstructions) != 0 ||
		f.vectorRegion.enabled || f.storeFwd.valid || f.gcResolved.valid || f.gcSubtypeProofN&gcFreshStructFactMode != 0 ||
		f.s.logicalDepth > ^uint32(0)-2 {
		return false
	}
	if first.typ != mtI32 && first.typ != mtI64 {
		return false
	}
	if first.kind > uint8(terminalLocalReg) {
		return false
	}
	if first.kind != uint8(terminalConstant) {
		current, ok := f.packedProducerLocal(first.index)
		if !ok || current != first {
			return false
		}
	}
	q := *r
	leafOp, err := q.Byte()
	if err != nil {
		return false
	}
	var second packedProducerLeaf
	switch leafOp {
	case 0x20:
		x, e := q.U32()
		if e != nil {
			return false
		}
		var ok bool
		second, ok = f.packedProducerLocal(x)
		if !ok {
			return false
		}
	case 0x41:
		value, e := q.I32()
		if e != nil {
			return false
		}
		second = packedProducerLeaf{kind: uint8(terminalConstant), typ: mtI32, value: int64(value)}
	case 0x42:
		value, e := q.I64()
		if e != nil {
			return false
		}
		second = packedProducerLeaf{kind: uint8(terminalConstant), typ: mtI64, value: value}
	default:
		return false
	}
	if first.typ != second.typ || first.kind == uint8(terminalConstant) && second.kind == uint8(terminalConstant) {
		return false
	}
	binaryPC := q.Offset()
	binaryOp, err := q.Byte()
	if err != nil {
		return false
	}
	var op wOp
	switch binaryOp {
	case 0x6a, 0x6b, 0x71, 0x72, 0x73:
		if first.typ != mtI32 {
			return false
		}
		op = integerBinaryOps[binaryOp-0x6a]
	case 0x7c, 0x7d, 0x83, 0x84, 0x85:
		if first.typ != mtI64 {
			return false
		}
		op = integerBinaryOps[binaryOp-0x7c]
	default:
		return false
	}
	next, ok := q.Peek()
	if !ok || !packedSuccessorMayConsumeDirectly(next) {
		return false
	}
	if first.typ == mtI64 && (first.kind == uint8(terminalConstant) && !fitsImm32(first.value) || second.kind == uint8(terminalConstant) && !fitsImm32(second.value)) {
		return false
	}
	// Preserve the original identities and same-local simplifications rather
	// than reproducing their state changes in a second implementation.
	if second.kind == uint8(terminalConstant) {
		c := uint64(second.value)
		ones := ^uint64(0)
		if first.typ == mtI32 {
			c = uint64(uint32(second.value))
			ones = uint64(^uint32(0))
		}
		if c == 0 || op == opAnd && c == ones {
			return false
		}
	}
	if first.kind != uint8(terminalConstant) && second.kind != uint8(terminalConstant) && first.index == second.index && op != opAdd {
		return false
	}
	payload, idx := int64(second.index), first.index
	if first.kind == uint8(terminalConstant) {
		payload, idx = first.value, second.index
	} else if second.kind == uint8(terminalConstant) {
		payload = second.value
	}
	regs := Reg(uint8(first.reg)&15 | (uint8(second.reg)&15)<<4)
	// Original leaf pushes reach depth+2 before the binary reduces to depth+1.
	// No high-water observer exists in these uninstrumented leaf paths; overflow
	// is rejected above, and validation-derived maximum depth remains unchanged.
	n := f.s.tryAlloc()
	if n == nil {
		n = f.s.allocNextChunk()
	}
	f.initPackedTerminal(n, op, first.typ, payload, idx, regs, uint32(first.kind), uint32(second.kind))
	f.s.canonicalSlots = false
	f.s.pendingEffects |= pendingLocalRef
	f.s.logicalDepth++
	f.s.push(n)
	*r = q
	f.wasmPC = f.tracePCBase + uint32(binaryPC)
	f.stats.peep("packed-producer")
	return true
}

//go:build linux && !tinygo && wago_regalloccheck

package riscv64

import (
	"encoding/binary"

	"github.com/wago-org/wago/internal/regalloccheck"
)

// This standalone policy composes owned byte/event receipts with independently
// decoded frame accesses and the serialized return recipe. It installs no
// observer/adapter and produces no Graph Verified verdict. Arithmetic and
// branch operand semantics before receipt capture remain the existing semantic
// definition contract; graph path/Use checks remain necessary.
//
// Layout is the finalized compiler layout, not a requested access coordinate.
// G3 must bind Bytes to the measured wrapper frame and recheck the retained body
// at its measured prefix offset after framing AND module relocation. This
// policy alone does not audit the wrapper, its entry poll, or a module image.
type scalarBodyFrame struct {
	Bytes, Locals, Spills int
}

type scalarBodyPolicyFacts struct {
	FrameLoads, FrameStores int
	ReturnSpan              int
	// Derived from the actual final SD source. This effect is provisional until
	// the complete-body policy and the final framing/relocation checks succeed.
	ReturnEffect regalloccheck.Effect
}

// decodeScalarReturnSpan admits only the current leaf Return operation:
// optional ADDI move or exact SLLI/SRLI32 normalization into x30, then the
// consecutive pointer load, full-cell store and non-clobbering x10 mirror.
// The ABI assumes SP+8 initially holds a valid output pointer whose eight-byte
// cell is disjoint from this callee's frame. Body policy proves preservation of
// that assumption, not validity of arbitrary incoming pointer bits.
func decodeScalarReturnSpan(code []byte, start, end int) (regalloccheck.Effect, regalloccheck.Result, bool) {
	span, status := scalarSpanBytes(code, start, end, 5)
	if span == nil {
		return regalloccheck.Effect{}, status, false
	}
	bad := func() (regalloccheck.Effect, regalloccheck.Result, bool) {
		return regalloccheck.Effect{}, scalarSpanMismatch("scalar serialized return recipe changed"), false
	}
	if len(span) != 12 && len(span) != 16 && len(span) != 20 {
		return bad()
	}
	prefix := len(span) - 12
	if prefix == 4 {
		w := binary.LittleEndian.Uint32(span)
		// ADDI x30,rs,0. The real source is decoded, never the requested result reg.
		if w & ^uint32(31<<15) != 0x13|30<<7 {
			return bad()
		}
	} else if prefix == 8 {
		a, b := binary.LittleEndian.Uint32(span), binary.LittleEndian.Uint32(span[4:])
		if a & ^uint32(31<<15) != 0x13|30<<7|1<<12|32<<20 || b != 0x13|30<<7|5<<12|30<<15|32<<20 {
			return bad()
		}
	}
	load := binary.LittleEndian.Uint32(span[prefix:])
	store := binary.LittleEndian.Uint32(span[prefix+4:])
	mirror := binary.LittleEndian.Uint32(span[prefix+8:])
	// LD x31,8(sp); SD x30,0(x31); ADDI x10,x30,0. No interior
	// entry or intervening pointer/source clobber is permitted by this recipe.
	if load != 0x03|31<<7|3<<12|2<<15|8<<20 ||
		store != 0x23|3<<12|31<<15|30<<20 ||
		mirror != 0x13|10<<7|30<<15 {
		return bad()
	}
	return regalloccheck.Effect{Kind: regalloccheck.Copy,
		Dst: regalloccheck.Register(regalloccheck.ReturnArea, 0),
		Src: scalarWordGP(uint8(store >> 20 & 31)), Size: 8}, regalloccheck.Result{}, true
}

// CheckBodyPolicy reauthenticates the exact current code against its receipt,
// then visits every owned word (including unreachable operations). returnSpan
// names the registered outer Return operation, not requested operand metadata.
// The narrow shared leaf profile has one final Return; trailing empty scopes
// are harmless. Every other memory operation must be one complete independently
// decoded frame recipe. No access may straddle a physical eight-byte slot or
// refer to padding, the saved header, or storage beyond the finalized frame.
//
// All work is charged to the receipt's lifetime budget; there is no retained
// allocation or new per-call budget. Failure invalidates the receipt and yields
// zero facts so callers cannot accidentally use partial transport evidence.
func (r *scalarBodyReceipt) CheckBodyPolicy(code []byte, frame scalarBodyFrame, returnSpan int) (scalarBodyPolicyFacts, bool) {
	fail := func(status regalloccheck.Result) (scalarBodyPolicyFacts, bool) {
		r.fail(status)
		return scalarBodyPolicyFacts{}, false
	}
	if !r.Seal(code) {
		return scalarBodyPolicyFacts{}, false
	}
	// Frame arithmetic is independently bounded before any addition. The first
	// profile uses exactly the actual shared leaf header, [SP, SP+16).
	if frame.Locals < 0 || frame.Spills < 0 || frame.Locals > (MaxFrameBytes-16)/8 || frame.Spills > (MaxFrameBytes-16)/8-frame.Locals {
		return fail(scalarSpanFailure(regalloccheck.InvalidGraph, "invalid scalar body frame counts"))
	}
	slotEnd := 16 + 8*(frame.Locals+frame.Spills)
	if frame.Bytes != (slotEnd+15)&^15 || frame.Bytes > MaxFrameBytes {
		return fail(scalarSpanMismatch("scalar body frame does not match finalized slots"))
	}
	if returnSpan < 0 || returnSpan >= len(r.spans) {
		return fail(scalarSpanFailure(regalloccheck.InvalidGraph, "missing scalar Return operation"))
	}
	// Two scans of all words plus bounded per-span frame/return recipe decoding.
	// G1 frame recipes use at most ten words, Return at most five. Charge before
	// scanning, including unsuccessful attempts and repeated finalized checks.
	if !r.charge(2*(len(r.data)/4)+12*len(r.spans), 0) {
		return scalarBodyPolicyFacts{}, false
	}
	// Run the invariant scan first: a recognized SP/direct-header write cannot hide
	// behind an earlier unsupported span. Receipt decoding already authenticated
	// every word; a failure here is an internal unsupported-model boundary.
	for at := 0; at < len(r.data); at += 4 {
		d, ok := decodeScalarMachineWord(binary.LittleEndian.Uint32(r.data[at:]))
		if !ok {
			return fail(scalarSpanFailure(regalloccheck.UnsupportedOperation, "unknown body word"))
		}
		if d.Writes&(1<<2) != 0 {
			return fail(scalarSpanMismatch("scalar body writes SP"))
		}
		if d.Memory == scalarWordStore && d.Base == 2 && int64(d.Offset) < 16 && int64(d.Offset)+int64(d.Size) > 0 {
			return fail(scalarSpanMismatch("scalar body overwrites saved header"))
		}
	}
	var facts scalarBodyPolicyFacts
	facts.ReturnSpan = returnSpan
	for id, span := range r.spans {
		if id > returnSpan && span.Start != span.End {
			return fail(scalarSpanMismatch("scalar code follows terminal Return"))
		}
		if id == returnSpan {
			if span.HasEnvelope {
				return fail(scalarSpanMismatch("Return contains a branch envelope"))
			}
			effect, status, ok := decodeScalarReturnSpan(r.data, span.Start, span.End)
			if !ok {
				return fail(status)
			}
			facts.ReturnEffect = effect
			continue
		}
		memory := 0
		for at := span.Start; at < span.End; at += 4 {
			d, _ := decodeScalarMachineWord(binary.LittleEndian.Uint32(r.data[at:]))
			if d.Memory != scalarWordNoMemory {
				memory++
			}
		}
		if memory == 0 {
			continue
		}
		if memory != 1 || span.HasEnvelope {
			return fail(scalarSpanFailure(regalloccheck.UnsupportedOperation, "memory outside a complete scalar frame operation"))
		}
		transfer, status, ok := decodeScalarFrameSpan(r.data, span.Start, span.End)
		if !ok {
			return fail(status)
		}
		offset, end := int64(transfer.Offset), int64(transfer.Offset)+int64(transfer.Size)
		if !transfer.Load && offset < 16 && end > 0 {
			return fail(scalarSpanMismatch("scalar dynamic frame store overwrites saved header"))
		}
		if offset < 16 || end > int64(slotEnd) || end > int64(frame.Bytes) || (offset-16)%8+int64(transfer.Size) > 8 {
			return fail(scalarSpanMismatch("scalar frame access outside owned slot extent"))
		}
		if transfer.Load {
			facts.FrameLoads++
		} else {
			facts.FrameStores++
		}
	}
	return facts, true
}

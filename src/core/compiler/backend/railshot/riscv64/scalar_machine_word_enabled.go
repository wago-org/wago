//go:build linux && !tinygo && wago_regalloccheck

package riscv64

import (
	"encoding/binary"

	"github.com/wago-org/wago/internal/regalloccheck"
)

// Independent decoding prerequisite only. No target observer is installed, no
// byte receipt is retained, and no graph/ABI preservation verdict is produced.
// The trace models current encoder observations, not arithmetic semantics.
type scalarWordEvent struct {
	Write  bool
	Mask   uint32
	Effect regalloccheck.Effect
}

type scalarWordObservation struct {
	Position int // actual instruction offset; RV callbacks precede word emission
	Event    scalarWordEvent
}

type scalarWordMemory uint8

const (
	scalarWordNoMemory scalarWordMemory = iota
	scalarWordLoad
	scalarWordStore
)

type scalarWordControl uint8

const (
	scalarWordNoControl scalarWordControl = iota
	scalarWordBranch
	scalarWordJump
	scalarWordIndirect
)

type scalarWordInfo struct {
	// Logical shift32 has four byte copies, one kill and one physical write.
	Events  [6]scalarWordEvent
	Count   int
	Writes  uint32 // actual encoded GP destination, not a call's conservative mask
	Memory  scalarWordMemory
	Control scalarWordControl
	// Memory fields report actual accesses even when no event is emitted:
	// loads into x0 still access memory; stores through non-SP bases still write.
	Base, Register uint8
	Size           int
	Offset         int32
	// Control link zero means no call. Offset is a PC-relative displacement for
	// Branch/Jump and the signed base displacement for Indirect, not a target.
	Link uint8
}

func (d *scalarWordInfo) effect(e regalloccheck.Effect) {
	d.Events[d.Count] = scalarWordEvent{Effect: e}
	d.Count++
}
func (d *scalarWordInfo) write(reg uint8, call bool) {
	mask := uint32(1) << reg
	if call {
		mask = ^uint32(1)
	} else if reg == 0 {
		return
	}
	d.Events[d.Count] = scalarWordEvent{Write: true, Mask: mask}
	d.Count++
	if reg != 0 {
		d.Writes |= uint32(1) << reg
	}
}
func scalarWordGP(reg uint8) regalloccheck.Location {
	return regalloccheck.Register(regalloccheck.GP, reg)
}
func (d *scalarWordInfo) definition(reg uint8, call bool) {
	d.write(reg, call)
	if call {
		d.effect(regalloccheck.Effect{Kind: regalloccheck.Call})
	} else if reg != 0 {
		d.effect(regalloccheck.Effect{Kind: regalloccheck.Kill, Dst: scalarWordGP(reg), Size: 8})
	}
}

// decodeScalarMachineWord recognizes the scalar RV64I/M word subset independently
// of encoder helpers. Unknown/reserved encodings are never empty-effect success.
// A decoded call, indirect access or SP write is still forbidden/unsupported by
// the future leaf-body policy; recognition is not permission to execute it.
func decodeScalarMachineWord(word uint32) (scalarWordInfo, bool) {
	var d scalarWordInfo
	op, funct3, funct7 := word&127, word>>12&7, word>>25
	rd, rs1, rs2 := uint8(word>>7&31), uint8(word>>15&31), uint8(word>>20&31)
	imm := int32(word) >> 20
	switch op {
	case 0x33, 0x3b:
		valid := false
		if op == 0x33 {
			valid = funct7 == 0 || funct7 == 1 || funct7 == 0x20 && (funct3 == 0 || funct3 == 5)
		} else {
			valid = funct7 == 0 && (funct3 == 0 || funct3 == 1 || funct3 == 5) ||
				funct7 == 0x20 && (funct3 == 0 || funct3 == 5) ||
				funct7 == 1 && (funct3 == 0 || funct3 >= 4)
		}
		if !valid {
			return scalarWordInfo{}, false
		}
		d.definition(rd, false)
		return d, true
	case 0x37, 0x17:
		d.definition(rd, false)
		return d, true
	case 0x63:
		if funct3 != 0 && funct3 != 1 && (funct3 < 4 || funct3 > 7) {
			return scalarWordInfo{}, false
		}
		u := word>>31<<12 | (word>>7&1)<<11 | (word>>25&63)<<5 | (word>>8&15)<<1
		d.Control, d.Offset = scalarWordBranch, int32(u<<19)>>19
		if d.Offset&3 != 0 {
			return scalarWordInfo{}, false
		}
		return d, true
	case 0x6f:
		u := word>>31<<20 | (word>>12&255)<<12 | (word>>20&1)<<11 | (word>>21&1023)<<1
		d.Control, d.Link, d.Offset = scalarWordJump, rd, int32(u<<11)>>11
		if d.Offset&3 != 0 {
			return scalarWordInfo{}, false
		}
		d.definition(rd, rd != 0)
		return d, true
	case 0x23:
		switch funct3 {
		case 0:
			d.Size = 1
		case 2:
			d.Size = 4
		case 3:
			d.Size = 8
		default:
			// SH has no typed encoder contract in this scalar subset yet.
			return scalarWordInfo{}, false
		}
		u := word>>25<<5 | word>>7&31
		d.Memory, d.Base, d.Register, d.Offset = scalarWordStore, rs1, rs2, int32(u<<20)>>20
		if rs1 == 2 {
			e := regalloccheck.Effect{Kind: regalloccheck.Kill, Dst: regalloccheck.Slot(d.Offset), Size: d.Size}
			if rs2 != 0 {
				e.Kind, e.Src = regalloccheck.Copy, scalarWordGP(rs2)
			}
			d.effect(e)
		}
		return d, true
	case 0x03:
		switch funct3 {
		case 2, 6:
			d.Size = 4
		case 3:
			d.Size = 8
		case 4:
			d.Size = 1
		default:
			return scalarWordInfo{}, false
		}
		d.Memory, d.Base, d.Register, d.Offset = scalarWordLoad, rs1, rd, imm
	case 0x67:
		if funct3 != 0 {
			return scalarWordInfo{}, false
		}
		d.Control, d.Link, d.Base, d.Offset = scalarWordIndirect, rd, rs1, imm
		if rd != 0 {
			d.definition(rd, true)
			return d, true
		}
	case 0x13, 0x1b:
		valid := false
		if op == 0x13 {
			switch funct3 {
			case 0, 2, 3, 4, 6, 7:
				valid = true
			case 1:
				valid = word>>26 == 0
			case 5:
				valid = word>>26 == 0 || word>>26 == 0x10
			}
		} else {
			valid = funct3 == 0 || funct3 == 1 && funct7 == 0 || funct3 == 5 && (funct7 == 0 || funct7 == 0x20)
		}
		if !valid {
			return scalarWordInfo{}, false
		}
	default:
		return scalarWordInfo{}, false
	}
	if rd == 0 {
		return d, true
	}
	if op == 0x13 && (funct3 == 1 || funct3 == 5) && imm == 32 && rs1 != 0 {
		from, to, killed := uint8(0), uint8(4), uint8(0)
		if funct3 == 5 {
			from, to, killed = 4, 0, 4
		}
		for part := uint8(0); part < 4; part++ {
			dst, src := scalarWordGP(rd), scalarWordGP(rs1)
			dst.Byte, src.Byte = to+part, from+part
			d.effect(regalloccheck.Effect{Kind: regalloccheck.Copy, Dst: dst, Src: src, Size: 1})
		}
		dst := scalarWordGP(rd)
		dst.Byte = killed
		d.effect(regalloccheck.Effect{Kind: regalloccheck.Kill, Dst: dst, Size: 4})
		d.write(rd, false)
		return d, true
	}
	e := regalloccheck.Effect{Kind: regalloccheck.Kill, Dst: scalarWordGP(rd), Size: 8}
	if op == 0x13 && funct3 == 0 && imm == 0 && rs1 != 0 {
		e.Kind, e.Src = regalloccheck.Copy, scalarWordGP(rs1)
	} else if op == 0x03 {
		e.Size = d.Size
		if d.Size < 8 {
			e.ClearTo = 8
		}
		if rs1 == 2 {
			e.Kind, e.Src = regalloccheck.Copy, regalloccheck.Slot(imm)
		}
	}
	if e.Kind == regalloccheck.Copy {
		d.effect(e)
		d.write(rd, false)
	} else {
		d.write(rd, false)
		d.effect(e)
	}
	return d, true
}

type scalarWordAuditLimits struct{ Words, Events, Work int }

type scalarWordAuditSummary struct {
	Words, Events, Loads, Stores, Controls, SPWrites int
}

// auditScalarWordTrace covers every final word in the requested span and binds
// it to the exact ordered raw encoder trace. It neither owns immutable receipts
// nor authenticates surrounding body coverage, branch destinations, pointer
// lineage, semantic source definitions or frame preservation. In particular a
// decoded dynamic store/call/SP write can match its trace and remains a hazard
// for the separate body policy. No Verified verdict is returned here.
func auditScalarWordTrace(code []byte, start, end int, events []scalarWordObservation, limits scalarWordAuditLimits) (scalarWordAuditSummary, regalloccheck.Result, bool) {
	var out scalarWordAuditSummary
	defaults := scalarWordAuditLimits{Words: 1 << 18, Events: 1 << 18, Work: 8 << 20}
	for _, pair := range [][2]*int{{&limits.Words, &defaults.Words}, {&limits.Events, &defaults.Events}, {&limits.Work, &defaults.Work}} {
		if *pair[0] < 0 {
			return out, scalarSpanFailure(regalloccheck.InvalidGraph, "invalid scalar word audit budget"), false
		}
		if *pair[0] == 0 || *pair[0] > *pair[1] {
			*pair[0] = *pair[1]
		}
	}
	if len(code) > MaxCodeBytes || len(events) > limits.Events {
		return out, scalarSpanFailure(regalloccheck.ResourceLimit, "scalar word audit input budget"), false
	}
	if start < 0 || end < start || end > len(code) || start&3 != 0 || end&3 != 0 {
		return out, scalarSpanFailure(regalloccheck.UnsupportedOperation, "invalid scalar word audit span"), false
	}
	if (end-start)/4 > limits.Words {
		return out, scalarSpanFailure(regalloccheck.ResourceLimit, "scalar word audit byte budget"), false
	}
	work := 0
	for pos := start; pos < end; pos += 4 {
		if work == limits.Work {
			return out, scalarSpanFailure(regalloccheck.ResourceLimit, "scalar word audit work budget"), false
		}
		work++
		d, ok := decodeScalarMachineWord(binary.LittleEndian.Uint32(code[pos:]))
		if !ok {
			return out, scalarSpanFailure(regalloccheck.UnsupportedOperation, "unknown scalar machine word"), false
		}
		if d.Count > limits.Work-work {
			return out, scalarSpanFailure(regalloccheck.ResourceLimit, "scalar word audit event work budget"), false
		}
		work += d.Count
		if d.Count > len(events)-out.Events {
			return out, scalarSpanMismatch("missing scalar word observation"), false
		}
		for i := 0; i < d.Count; i++ {
			actual := events[out.Events+i]
			if actual.Position != pos || actual.Event != d.Events[i] {
				return out, scalarSpanMismatch("scalar word observation differs from actual instruction"), false
			}
		}
		out.Words++
		out.Events += d.Count
		if d.Memory == scalarWordLoad {
			out.Loads++
		} else if d.Memory == scalarWordStore {
			out.Stores++
		}
		if d.Control != scalarWordNoControl {
			out.Controls++
		}
		if d.Writes&(1<<2) != 0 {
			out.SPWrites++
		}
	}
	if out.Events != len(events) {
		return out, scalarSpanMismatch("extra scalar word observations"), false
	}
	return out, regalloccheck.Result{}, true
}

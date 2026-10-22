//go:build linux && !tinygo && wago_regalloccheck

package riscv64

import (
	"encoding/binary"

	"github.com/wago-org/wago/internal/regalloccheck"
)

// Decoder-only prerequisites. No target observer/interface is activated here.
// Constant64 emits at most eight words; address ADD and memory access add two.
const scalarFrameSpanWords = 10

func scalarSpanFailure(reason regalloccheck.FailureReason, message string) regalloccheck.Result {
	return regalloccheck.Result{Verdict: regalloccheck.Inconclusive, Reason: reason, Block: -1, Operation: -1, Message: message}
}
func scalarSpanMismatch(message string) regalloccheck.Result {
	return regalloccheck.Result{Verdict: regalloccheck.Rejected, Reason: regalloccheck.ProvenanceMismatch, Block: -1, Operation: -1, Message: message}
}
func scalarSpanBytes(code []byte, start, end, maxWords int) ([]byte, regalloccheck.Result) {
	if len(code) > MaxCodeBytes || end >= start && start >= 0 && end-start > maxWords*4 {
		return nil, scalarSpanFailure(regalloccheck.ResourceLimit, "scalar machine span bound")
	}
	if start < 0 || end < start || end > len(code) || start&3 != 0 || end&3 != 0 || end == start {
		return nil, scalarSpanFailure(regalloccheck.UnsupportedOperation, "invalid scalar machine span")
	}
	return code[start:end], regalloccheck.Result{}
}

// scalarFrameTransfer describes only the actual memory instruction and its
// independently decoded SP-relative address. No requested slot/register enters
// this decoder. It is not a proof that frame headers or SP survived the body.
type scalarFrameTransfer struct {
	Offset   int32
	Register uint8
	Size     int
	Load     bool
}

func (f scalarFrameTransfer) effect() (regalloccheck.Effect, bool) {
	frame, register := regalloccheck.Slot(f.Offset), regalloccheck.Register(regalloccheck.GP, f.Register)
	if f.Load {
		if f.Register == 0 {
			return regalloccheck.Effect{}, false
		}
		e := regalloccheck.Effect{Kind: regalloccheck.Copy, Dst: register, Src: frame, Size: f.Size}
		if f.Size < 8 {
			e.ClearTo = 8
		}
		return e, true
	}
	e := regalloccheck.Effect{Kind: regalloccheck.Copy, Dst: frame, Src: register, Size: f.Size}
	if f.Register == 0 {
		e.Kind, e.Src = regalloccheck.Kill, regalloccheck.Location{}
	}
	return e, true
}

func decodeScalarFrameSpan(code []byte, start, end int) (scalarFrameTransfer, regalloccheck.Result, bool) {
	fail := func(message string) (scalarFrameTransfer, regalloccheck.Result, bool) {
		return scalarFrameTransfer{}, scalarSpanFailure(regalloccheck.UnsupportedOperation, message), false
	}
	span, status := scalarSpanBytes(code, start, end, scalarFrameSpanWords)
	if span == nil {
		return scalarFrameTransfer{}, status, false
	}
	last := binary.LittleEndian.Uint32(span[len(span)-4:])
	var out scalarFrameTransfer
	base := (last >> 15) & 31
	offset := int64(int32(last) >> 20)
	switch last & 127 {
	case 0x03:
		out.Load, out.Register = true, uint8(last>>7&31)
		switch last >> 12 & 7 {
		case 3:
			out.Size = 8
		case 2, 6:
			out.Size = 4
		case 4:
			out.Size = 1
		default:
			return fail("unmodeled scalar frame load")
		}
	case 0x23:
		out.Register = uint8(last >> 20 & 31)
		u := last>>25<<5 | last>>7&31
		offset = int64(int32(u<<20) >> 20)
		switch last >> 12 & 7 {
		case 3:
			out.Size = 8
		case 2:
			out.Size = 4
		case 0:
			out.Size = 1
		default:
			return fail("unmodeled scalar frame store")
		}
	default:
		return fail("scalar span does not end in a frame access")
	}
	if len(span) == 4 {
		if base != 2 {
			return fail("untracked scalar memory base")
		}
	} else {
		if len(span) < 12 || base != 31 {
			return fail("unmodeled scalar frame address recipe")
		}
		add := binary.LittleEndian.Uint32(span[len(span)-8:])
		// The bytes themselves must form ADD x31,sp,x31. No nominal backend
		// offset can authenticate a different base, scratch or ALU operation.
		if add != 0x33|31<<7|2<<15|31<<20 {
			return fail("unmodeled scalar SP address formation")
		}
		constant, ok := decodeScalarAddressConstant(span[:len(span)-8])
		if !ok {
			return fail("unmodeled scalar address constant")
		}
		value := int64(constant)
		// Bound before addition so even arbitrary full-width materializations
		// cannot overflow the signed offset calculation.
		if value < -MaxFrameBytes-2048 || value > MaxFrameBytes+2048 {
			return fail("scalar address outside bounded frame coordinates")
		}
		offset += value
	}
	if offset < -MaxFrameBytes || offset > MaxFrameBytes-int64(out.Size) {
		return fail("scalar frame access outside coordinate bound")
	}
	out.Offset = int32(offset)
	return out, regalloccheck.Result{}, true
}

// Interpret only the emitted constant recipe, independently of Constant64's
// construction logic. Arithmetic uses actual RV64 wrap/sign-extension rules.
func decodeScalarAddressConstant(code []byte) (uint64, bool) {
	if len(code) == 0 || len(code)&3 != 0 || len(code) > 8*4 {
		return 0, false
	}
	first := binary.LittleEndian.Uint32(code)
	if first>>7&31 != 31 {
		return 0, false
	}
	var value uint64
	switch {
	case first&127 == 0x37:
		value = uint64(int64(int32(first & 0xfffff000)))
	case first&0xfffff == 0x13|31<<7:
		value = uint64(int64(int32(first) >> 20))
	default:
		return 0, false
	}
	for at := 4; at < len(code); at += 4 {
		w := binary.LittleEndian.Uint32(code[at:])
		if w>>7&31 != 31 || w>>15&31 != 31 {
			return 0, false
		}
		imm := int64(int32(w) >> 20)
		switch {
		case w&127 == 0x13 && w>>12&7 == 0:
			value += uint64(imm)
		case w&127 == 0x1b && w>>12&7 == 0:
			value = uint64(int64(int32(value + uint64(imm))))
		case w&127 == 0x13 && w>>12&7 == 1 && w>>20 == 12:
			value <<= 12
		default:
			return 0, false
		}
	}
	return value, true
}

// decodeScalarLongEnvelope uses the measured envelope boundaries, never the
// pending semantic branch kind or an unrelated preceding word. The returned
// target is the actual signed AUIPC/JALR target, including JALR bit-zero clear.
func decodeScalarLongEnvelope(code []byte, start, site, end int) (int64, bool, regalloccheck.Result, bool) {
	span, status := scalarSpanBytes(code, start, end, 3)
	if span == nil {
		return 0, false, status, false
	}
	unknown := func(message string) (int64, bool, regalloccheck.Result, bool) {
		return 0, false, scalarSpanFailure(regalloccheck.UnsupportedOperation, message), false
	}
	if len(span) != 8 && len(span) != 12 || site != end-8 {
		return unknown("unrecognized scalar long envelope extent")
	}
	hi, lo := binary.LittleEndian.Uint32(code[site:]), binary.LittleEndian.Uint32(code[site+4:])
	if hi&127 != 0x17 || lo&0x707f != 0x67 {
		return unknown("unrecognized scalar long envelope opcode")
	}
	if hi>>7&31 != 31 || lo>>15&31 != 31 || lo>>7&31 != 0 {
		return 0, false, scalarSpanMismatch("scalar long edge changes base or saves a link"), false
	}
	conditional := false
	if len(span) == 12 {
		branch := binary.LittleEndian.Uint32(span)
		if branch == 0x00000013 {
			// A mutated conditional prefix that is now an actual NOP must not
			// inherit its former branch kind from the semantic pending edge.
		} else {
			cond := branch >> 12 & 7
			if branch&127 != 0x63 || cond != 0 && cond != 1 && (cond < 4 || cond > 7) {
				return unknown("unrecognized scalar conditional prefix")
			}
			u := branch>>31<<12 | (branch>>7&1)<<11 | (branch>>25&63)<<5 | (branch>>8&15)<<1
			if int32(u<<19)>>19 != 12 {
				return 0, false, scalarSpanMismatch("scalar conditional does not skip its complete long envelope"), false
			}
			conditional = true
		}
	}
	target := (int64(site) + int64(int32(hi&0xfffff000)) + int64(int32(lo)>>20)) &^ 1
	if target&3 != 0 {
		return 0, false, scalarSpanMismatch("scalar long edge is not fixed-width aligned"), false
	}
	return target, conditional, regalloccheck.Result{}, true
}

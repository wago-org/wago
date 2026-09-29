package profile

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/wago-org/wago/internal/jitprofile"
)

// EncodeAMD64Unwind converts sparse, code-region-relative stack recovery rules
// into offline DWARF32 tables for perf's injected ELF layout. Only RSP-based CFAs
// and eight-byte-aligned saved return PCs are supported. Unknown gaps get no FDE;
// no rule is extended to the end of a symbol or inferred from a neighboring row.
// Other general-purpose and vector registers are explicitly unrecoverable.
// This encodes rules, not evidence that the producer's rules are correct.
func EncodeAMD64Unwind(codeSize uint64, ranges []jitprofile.UnwindRange) (UnwindInfo, error) {
	if codeSize == 0 || codeSize > math.MaxInt32-256 || len(ranges) == 0 {
		return UnwindInfo{}, fmt.Errorf("invalid AMD64 unwind code size or empty directory")
	}
	if err := jitprofile.ValidateUnwind(ranges, codeSize); err != nil {
		return UnwindInfo{}, err
	}
	for _, r := range ranges {
		if r.CFARegister != 7 || r.ReturnOffset%8 != 0 {
			return UnwindInfo{}, fmt.Errorf("unsupported AMD64 unwind recovery rule")
		}
	}
	// Bound all signed PC-relative displacements and the eventual jitdump size
	// before allocating. A row costs at most 64 bytes including its own FDE and
	// search-table entry; the CIE and terminator fit in the remaining 256 bytes.
	if uint64(len(ranges)) > (math.MaxInt32-256-codeSize)/64 {
		return UnwindInfo{}, fmt.Errorf("AMD64 unwind directory exceeds DWARF32 range")
	}
	alignedCode := (codeSize + 7) &^ uint64(7)
	// Version 1, zR augmentation, code alignment 1, data alignment -8,
	// return-address column RIP (16), FDE pointers pcrel|sdata4 (0x1b).
	cie := []byte{0, 0, 0, 0, 1, 'z', 'R', 0, 1, 0x78, 16, 1, 0x1b,
		0x0c, 7, 8, // DW_CFA_def_cfa RSP, 8
		0x90, 1, // DW_CFA_offset RIP, -8
		0x14, 7, 0, // DW_CFA_val_offset RSP, 0: caller SP is CFA
	}
	for reg := byte(0); reg <= 32; reg++ {
		if reg != 7 && reg != 16 {
			cie = append(cie, 0x07, reg) // DW_CFA_undefined
		}
	}
	frame := appendFrameRecord(nil, cie)
	type entry struct{ pc, fde uint64 }
	var entries []entry
	for first := 0; first < len(ranges); {
		last := first + 1
		for last < len(ranges) && ranges[last-1].Offset+ranges[last-1].Size == ranges[last].Offset {
			last++
		}
		fdeAt := uint64(len(frame))
		start := ranges[first].Offset
		end := ranges[last-1].Offset + ranges[last-1].Size
		body := make([]byte, 13)
		put32(body, 0, uint32(fdeAt+4)) // CIE pointer points back to frame offset 0.
		put32(body, 4, uint32(int64(start)-int64(alignedCode+fdeAt+8)))
		put32(body, 8, uint32(end-start))
		// body[12] is the zero-length FDE augmentation.
		pc := start
		for _, r := range ranges[first:last] {
			body = appendCFIAdvance(body, r.Offset-pc)
			body = append(body, 0x0e) // DW_CFA_def_cfa_offset
			body = appendULEB(body, uint64(r.CFAOffset))
			body = append(body, 0x90) // DW_CFA_offset RIP
			body = appendULEB(body, uint64(-r.ReturnOffset/8))
			pc = r.Offset
		}
		frame = appendFrameRecord(frame, body)
		entries = append(entries, entry{start, fdeAt})
		first = last
	}
	frame = append(frame, make([]byte, 8)...)
	// Header: version, pcrel|sdata4 .eh_frame pointer, udata4 FDE count,
	// datarel|sdata4 binary-search entries. Data-relative means header-relative.
	header := make([]byte, 12+8*len(entries))
	copy(header, []byte{1, 0x1b, 3, 0x3b})
	put32(header, 4, uint32(-int64(len(frame))-4))
	put32(header, 8, uint32(len(entries)))
	for i, e := range entries {
		put32(header, 12+i*8, uint32(int64(e.pc)-int64(alignedCode)-int64(len(frame))))
		put32(header, 16+i*8, uint32(int64(e.fde)-int64(len(frame))))
	}
	return UnwindInfo{EHFrame: frame, EHFrameHeader: header}, nil
}

func appendFrameRecord(dst, body []byte) []byte {
	start := len(dst)
	dst = append(dst, 0, 0, 0, 0)
	dst = append(dst, body...)
	for len(dst)%8 != 0 {
		dst = append(dst, 0) // DW_CFA_nop padding, included in record length.
	}
	binary.LittleEndian.PutUint32(dst[start:], uint32(len(dst)-start-4))
	return dst
}

func appendCFIAdvance(b []byte, delta uint64) []byte {
	switch {
	case delta == 0:
		return b
	case delta <= 63:
		return append(b, byte(0x40|delta))
	case delta <= math.MaxUint8:
		return append(b, 2, byte(delta))
	case delta <= math.MaxUint16:
		return append(b, 3, byte(delta), byte(delta>>8))
	default:
		return append(b, 4, byte(delta), byte(delta>>8), byte(delta>>16), byte(delta>>24))
	}
}

func appendULEB(b []byte, n uint64) []byte {
	for n >= 128 {
		b = append(b, byte(n)|0x80)
		n >>= 7
	}
	return append(b, byte(n))
}

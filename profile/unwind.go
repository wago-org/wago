package profile

import (
	"encoding/binary"
	"fmt"
	"math"
)

// UnwindInfo contains already-finalized, little-endian DWARF32 frame data for
// one native region. Addresses must describe the ELF layout produced by perf
// inject: aligned native code, then EHFrame, then EHFrameHeader. This encoder
// checks framing, not the correctness of asynchronous register recovery rules.
//
// MappedSize is zero for offline-only tables. A nonzero value asserts that the
// entire payload is actually mapped immediately after the aligned code region;
// the caller owns that mapping's lifetime. Never infer this from code symbols.
type UnwindInfo struct {
	EHFrame       []byte
	EHFrameHeader []byte
	MappedSize    uint64
}

func (u UnwindInfo) validate() error {
	size := uint64(len(u.EHFrame)) + uint64(len(u.EHFrameHeader))
	if size > math.MaxUint32-40 || (u.MappedSize != 0 && u.MappedSize != size) {
		return fmt.Errorf("invalid jitdump unwind size")
	}
	if len(u.EHFrameHeader) < 12 || u.EHFrameHeader[0] != 1 {
		return fmt.Errorf("invalid EH frame header")
	}
	cies := make(map[int]bool)
	fdes := 0
	for off := 0; off < len(u.EHFrame); {
		if len(u.EHFrame)-off < 4 {
			return fmt.Errorf("truncated EH frame length")
		}
		length := binary.LittleEndian.Uint32(u.EHFrame[off:])
		if length == 0 {
			for _, b := range u.EHFrame[off:] {
				if b != 0 {
					return fmt.Errorf("data after EH frame terminator")
				}
			}
			break
		}
		if length == math.MaxUint32 || length < 4 || uint64(length) > uint64(len(u.EHFrame)-off-4) {
			return fmt.Errorf("invalid or unsupported EH frame record length")
		}
		link := binary.LittleEndian.Uint32(u.EHFrame[off+4:])
		if link == 0 {
			cies[off] = true
		} else {
			if uint64(link) > uint64(off+4) || !cies[off+4-int(link)] {
				return fmt.Errorf("EH frame FDE has no preceding CIE")
			}
			fdes++
		}
		off += 4 + int(length)
	}
	if fdes == 0 {
		return fmt.Errorf("EH frame has no FDE")
	}
	return nil
}

// WriteUnwind emits metadata for the next executable region written by Write.
// imageID, codeAddress and codeSize bind it to that exact mapping generation
// and region; a mismatch fails the
// exporter. Metadata must precede its code-load record. Caller-provided slices
// are consumed synchronously and are not retained.
//
// Perf's genelf.c consumes EHFrame followed by EHFrameHeader, despite the inverse
// order described in the jitdump specification. This layout is qualified with
// perf 7.0.14/libdw using a frameless recursive native fixture. It does not imply
// that the supplied rules or Wago's guest stacks are qualified.
func (j *JITDump) WriteUnwind(imageID, codeAddress, codeSize uint64, info UnwindInfo, timestamp uint64) error {
	if j.err != nil {
		return j.err
	}
	if j.closed {
		return fmt.Errorf("jitdump is closed")
	}
	if j.unwindPending || imageID == 0 || codeSize == 0 || codeAddress > math.MaxUint64-codeSize {
		j.err = fmt.Errorf("invalid or pending jitdump unwind target")
		return j.err
	}
	if err := info.validate(); err != nil {
		j.err = err
		return err
	}
	size := uint64(len(info.EHFrame)) + uint64(len(info.EHFrameHeader))
	if info.MappedSize != 0 {
		if codeSize > math.MaxUint64-7 {
			j.err = fmt.Errorf("jitdump mapped unwind address overflow")
			return j.err
		}
		aligned := (codeSize + 7) &^ uint64(7)
		if codeAddress > math.MaxUint64-aligned || info.MappedSize > math.MaxUint64-codeAddress-aligned {
			j.err = fmt.Errorf("jitdump mapped unwind address overflow")
			return j.err
		}
	}
	b := make([]byte, 40)
	put32(b, 0, 4)
	put32(b, 4, uint32(40+size))
	put64(b, 8, timestamp)
	put64(b, 16, size)
	put64(b, 24, uint64(len(info.EHFrameHeader)))
	put64(b, 32, info.MappedSize)
	for _, part := range [][]byte{b, info.EHFrame, info.EHFrameHeader} {
		if err := writeAll(j.w, part); err != nil {
			j.err = err
			return err
		}
	}
	j.unwindPending = true
	j.unwindImage = imageID
	j.unwindAddress = codeAddress
	j.unwindSize = codeSize
	j.unwindTime = timestamp
	return nil
}

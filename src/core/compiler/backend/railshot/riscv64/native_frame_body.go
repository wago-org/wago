//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

// NativeFrameBodyEnd returns the first cold guard destination after the main
// framed function path. Helper stubs emitted later in a module are not its body.
func NativeFrameBodyEnd(code []byte, entry int) (int, error) {
	if _, err := NativeFrameBytes(code, entry); err != nil {
		return 0, err
	}
	b := code[entry:]
	hi := int64(int32(binary.LittleEndian.Uint32(b[8:]) & 0xfffff000))
	lo := int64(int32(binary.LittleEndian.Uint32(b[12:])) >> 20)
	return entry + int(8+hi+lo), nil
}

// NativeNonCollectingGCCall accepts only an image-local owned synchronous GC
// thunk with a known non-allocating selector and no safepoint identity. Exact
// thunk comparison also checks its frame guards and transport bounds.
func NativeNonCollectingGCCall(code []byte, returnOffset uint32, slots uint16) bool {
	if returnOffset < 8 || returnOffset&3 != 0 || uint64(returnOffset) > uint64(len(code)) {
		return false
	}
	site := int(returnOffset) - 8
	hi, lo := binary.LittleEndian.Uint32(code[site:]), binary.LittleEndian.Uint32(code[site+4:])
	if hi&0xfff != 31<<7|0x17 || lo&0xfffff != 31<<15|1<<7|0x67 {
		return false
	}
	target := int64(site) + int64(int32(hi&0xfffff000)) + int64(int32(lo)>>20)
	if target < 0 || target&15 != 0 || target >= int64(len(code)) {
		return false
	}
	b := code[int(target):]
	var dispatch, counts uint32
	readConstant := func(at int) (uint32, bool) {
		if at < 4 {
			return 0, false
		}
		w := binary.LittleEndian.Uint32(b[at-4:])
		rd, rs := w>>7&31, w>>15&31
		if rd != 5 || w>>12&7 != 0 || w&127 != 0x13 && w&127 != 0x1b {
			return 0, false
		}
		value := int32(w) >> 20
		if rs == 0 {
			return uint32(value), true
		}
		if rs != 5 || at < 8 {
			return 0, false
		}
		u := binary.LittleEndian.Uint32(b[at-8:])
		if u&0xfff != 5<<7|0x37 {
			return 0, false
		}
		return uint32(int32(u&0xfffff000) + value), true
	}
	foundDispatch, foundCounts := false, false
	for at := 0; at+4 <= min(len(b), 2560); at += 4 {
		w := binary.LittleEndian.Uint32(b[at:])
		if w&0x01fff07f != 5<<20|28<<15|2<<12|0x23 {
			continue
		}
		offset := int32((w>>25)<<5|w>>7&31) << 20 >> 20
		if offset == HostImportIndexOffset {
			dispatch, foundDispatch = readConstant(at)
		}
		if offset == HostSlotCountsOffset {
			counts, foundCounts = readConstant(at)
			break
		}
	}
	if !foundDispatch || !foundCounts || dispatch&^uint32((1<<30)-1) != gcStructDispatchBit {
		return false
	}
	helper, safepoint := shared.DecodeGCDispatch(dispatch &^ gcStructDispatchBit)
	if safepoint != 0 {
		return false
	}
	switch helper {
	case gcFunctionSubtypeTest, gcStructGet, gcStructSet, gcStructGetS, gcStructGetU, gcStructRefTest, gcStructTableSet, gcStructRefCast,
		gcArrayGet, gcArrayGetS, gcArrayGetU, gcArraySet, gcArrayLen, gcArrayDropElem, gcArrayFill, gcArrayCopy, gcArrayInitData, gcArrayInitElem,
		gcTableFill, gcTableCopy, gcTableInit, gcTableElemDrop, gcExceptionPayload:
	default:
		return false
	}
	params, results := int(counts&65535), int(counts>>16)
	if params > 64 || results > 2 || params > int(slots) || results > int(slots) {
		return false
	}
	expected := HostIndirectOwnedSyncThunk(dispatch, params, results)
	return len(expected) != 0 && len(expected) <= len(b) && bytes.Equal(expected, b[:len(expected)])
}

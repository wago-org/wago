//go:build amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const (
	tailEdgeZero32 uint8 = 1 + iota
	tailEdgeNonzero32
	tailEdgeZero64
	tailEdgeNonzero64
)

const tailReuseArgumentFlags uint8 = 1 << 7

// Thread only an exact frameless, single-integer-parameter entry. Matching at
// the known internal-entry boundary avoids recognizing instruction-like bytes
// in immediates. All skipped instructions are an empty frame adjustment, the
// RAX->R9 parameter home, and the same zero test emitted at the transfer site.
// Any other layout retains the ordinary internal entry for both edges.
func localTailEntryTarget(code []byte, start, end int, edge uint8) int {
	fallback := start
	if edge < tailEdgeZero32 || edge > tailEdgeNonzero64 || start < 0 || end > len(code) || end-start < 19 {
		return fallback
	}
	header := code[start : start+19]
	if !bytes.Equal(header[:7], []byte{0x48, 0x81, 0xec, 0, 0, 0, 0}) {
		return fallback
	}
	moveRex, testRex := byte(0x41), byte(0x45)
	if edge >= tailEdgeZero64 {
		moveRex, testRex = 0x49, 0x4d
	}
	if header[7] != moveRex || header[8] != 0x89 || header[9] != 0xc1 || header[10] != testRex || header[11] != 0x85 || header[12] != 0xc9 || header[13] != 0x0f || header[14] != 0x84 && header[14] != 0x85 {
		return fallback
	}
	fallthroughPC := start + 19
	taken := fallthroughPC + int(int32(binary.LittleEndian.Uint32(header[15:19])))
	if taken <= fallthroughPC || taken >= end {
		return fallback
	}
	nonzero := edge == tailEdgeNonzero32 || edge == tailEdgeNonzero64
	if (header[14] == 0x85) == nonzero {
		return taken
	}
	return fallthroughPC
}

func (f *fn) tryThreadLocalIntegerTail(ft *wasm.CompType, target int) bool {
	if !f.localTailLeaf || f.compactNative() || !f.opt(optBranchFold) || f.nParams != 1 || len(ft.Params) != 1 || !wasm.EqualValType(f.ft.Params[0], ft.Params[0]) {
		return false
	}
	home, floating, pinned := f.pinReg(0)
	if !pinned || floating || home != R9 {
		return false
	}
	assignment := f.a.Len()
	f.setLocal(nil, 0, false)
	wide := f.localType[0] == mtI64
	reuse := selfTailAssignmentSetsZero(f.a.B[assignment:], R9, wide)
	// Keep the ordinary ABI argument available when the callee layout declines
	// threading. A discarded prefix may still own RAX in the compiler's state.
	f.spillIfUsed(RAX)
	f.moveInt(RAX, R9, f.localType[0])
	f.emitTailFrameRelease()
	f.a.TestSelf(R9, wide)
	nonzero, zero := tailEdgeNonzero32, tailEdgeZero32
	if wide {
		nonzero, zero = tailEdgeNonzero64, tailEdgeZero64
	}
	if reuse {
		nonzero |= tailReuseArgumentFlags
	}
	emit := func(site int, edge uint8) {
		reloc := f.newCallReloc(site, target, true)
		reloc.tailEdge = edge
		f.relocs = append(f.relocs, reloc)
	}
	emit(f.a.JccPlaceholder(condNE), nonzero)
	emit(f.a.JmpPlaceholder(), zero)
	f.unreachable = true
	f.stats.peep("tail-local-entry-thread")
	return true
}

// Frame size is known only after emission. If this frame is empty, the last
// argument ADD/SUB flags survive the ABI MOV and replace the repeated TEST.
// Pack both terminal branches into the vacated adjustment/test range and keep
// unreachable padding after the unconditional branch. Other labels and loop
// alignment retain their offsets; only these two relocations move.
func rewriteZeroTailFlagPacket(code []byte, nonzero, zero *callReloc) (int, bool) {
	wide := nonzero.tailEdge == tailEdgeNonzero64
	if nonzero.tailEdge != tailEdgeNonzero32 && !wide || !nonzero.internal || !zero.internal || nonzero.target != zero.target || nonzero.gcStub != gcSharedStubNone || zero.gcStub != gcSharedStubNone {
		return 0, false
	}
	wantZero := tailEdgeZero32
	if wide {
		wantZero = tailEdgeZero64
	}
	start, end := int(nonzero.at)-12, int(zero.at)+4
	if zero.tailEdge != wantZero || int(zero.at) != int(nonzero.at)+5 || start < 0 || end > len(code) || end-start != 21 {
		return 0, false
	}
	packet := code[start:end]
	rex := byte(0x45)
	if wide {
		rex = 0x4d
	}
	if !bytes.Equal(packet[:7], []byte{0x48, 0x81, 0xc4, 0, 0, 0, 0}) || packet[7] != rex || packet[8] != 0x85 || packet[9] != 0xc9 || packet[10] != 0x0f || packet[11] != 0x85 || packet[16] != 0xe9 {
		return 0, false
	}
	copy(packet, packet[10:])
	for i := 11; i < len(packet); i++ {
		packet[i] = 0x90
	}
	nonzero.at -= 10
	zero.at -= 10
	return start + 3, true
}

func (f *fn) finishLocalTailArgumentFlags(frameBytes uint32) {
	for i := range f.relocs {
		if f.relocs[i].tailEdge&tailReuseArgumentFlags == 0 {
			continue
		}
		f.relocs[i].tailEdge &^= tailReuseArgumentFlags
		if frameBytes != 0 || !f.localTailLeaf || f.compactNative() || i+1 >= len(f.relocs) {
			continue
		}
		frameSite := int(f.relocs[i].at) - 9
		for j, site := range f.sc.tailFrameSites {
			if site != frameSite {
				continue
			}
			if _, ok := rewriteZeroTailFlagPacket(f.a.B, &f.relocs[i], &f.relocs[i+1]); ok {
				copy(f.sc.tailFrameSites[j:], f.sc.tailFrameSites[j+1:])
				f.sc.tailFrameSites = f.sc.tailFrameSites[:len(f.sc.tailFrameSites)-1]
				if f.stats != nil {
					f.stats.NativeSize.FrameAdjustmentBytes -= 7
					f.stats.NativeSize.DeadFrameReservationBytes -= 7
					f.stats.NativeSize.BranchFoldHoleBytes += 10
				}
				f.stats.peep("tail-local-zero-frame-flags")
			}
			break
		}
	}
}

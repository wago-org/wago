//go:build arm64

package arm64

import (
	"fmt"
	"os"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

// WAGO_FINALIZE=0 is the rollout oracle for the symbolic finalization seam. The
// default identity path retains explicit sites and remaps offsets without
// shrinking; later relaxation changes must remain byte-for-byte comparable
// through this switch until their corpus and metadata gates pass.
var nativeFinalizerEnabled = os.Getenv("WAGO_FINALIZE") != "0"
var nativeFinalizerValidate = os.Getenv("WAGO_FINALIZE_VALIDATE") == "1"

// Compaction remains opt-in while its bounded remapping cost is brought under
// the Balanced compile-time gate. Loop-bearing functions also retain the old
// size-stable path until their alignment fragments become relaxable.
var nativeCompactionEnabled = os.Getenv("WAGO_COMPACT") == "1"

const maxFinalizerDeletions = shared.MaxOffsetMapDeletions

type finalizerMarker uint8

const (
	markerDeadHole finalizerMarker = iota
	markerJumpDataStart
	markerJumpDataEnd
	markerPluginStart
	markerPluginEnd
	markerPCRelative
)

func finalizerMarkerKey(off int, marker finalizerMarker) int {
	return -((off << 3) | int(marker)) - 1
}

func decodeFinalizerMarker(key int) (off int, marker finalizerMarker, ok bool) {
	if key >= 0 {
		return 0, 0, false
	}
	value := -key - 1
	return value >> 3, finalizerMarker(value & 7), true
}

func (f *fn) recordFinalizerMarker(off int, marker finalizerMarker) {
	if !nativeFinalizerEnabled {
		return
	}
	sc := f.scratchState()
	if sc.branchTargets == nil {
		sc.branchTargets = make(map[int]bool, 16)
	}
	sc.branchTargets[finalizerMarkerKey(off, marker)] = true
}

func (f *fn) recordJumpTableData(start, end int) {
	f.opaqueFragments = true
	f.recordFinalizerMarker(start, markerJumpDataStart)
	f.recordFinalizerMarker(end, markerJumpDataEnd)
}

func (f *fn) recordOpaquePlugin(start, end int) {
	if end > start {
		f.opaqueFragments = true
		f.recordFinalizerMarker(start, markerPluginStart)
		f.recordFinalizerMarker(end, markerPluginEnd)
	}
}

func (f *fn) recordPCRelative(off int) {
	f.recordFinalizerMarker(off, markerPCRelative)
}

func (f *fn) finalizeNativeCode(internalOff int) (int, error) {
	if !nativeFinalizerEnabled {
		return internalOff, nil
	}
	if nativeFinalizerValidate {
		if err := f.validateFinalizerInventory(internalOff); err != nil {
			return 0, err
		}
	}
	oldLen := len(f.a.B)
	var result shared.FinalizeResult
	frameDeleted := 0
	if nativeCompactionEnabled {
		var storage [maxFinalizerDeletions]shared.DeletedRange
		deletions, deletedFrames, ok := f.buildCompactionPlan(storage[:0])
		if ok {
			offsets, err := shared.NewOffsetMap(oldLen, deletions)
			if err != nil {
				return 0, fmt.Errorf("arm64 finalizer: %w", err)
			}
			code, err := f.compactNativeCode(&offsets, deletions)
			if err != nil {
				return 0, err
			}
			result.Code = code
			result.Offsets = offsets
			frameDeleted = deletedFrames
		} else {
			offsets, err := shared.NewOffsetMap(oldLen, nil)
			if err != nil {
				return 0, fmt.Errorf("arm64 finalizer: %w", err)
			}
			result = shared.FinalizeResult{Code: f.a.B, Offsets: offsets}
		}
	} else {
		var err error
		result, err = shared.FinalizeIdentity(f.a.B, nil, nil, nil)
		if err != nil {
			return 0, fmt.Errorf("arm64 finalizer: %w", err)
		}
	}
	f.a.B = result.Code

	mappedInternal, err := mapFinalOffset(&result.Offsets, internalOff, len(result.Code), "internal entry")
	if err != nil {
		return 0, err
	}
	internalOff = mappedInternal
	for i := range f.relocs {
		mapped, err := mapFinalOffset(&result.Offsets, f.relocs[i].at, len(result.Code), "call relocation")
		if err != nil {
			return 0, err
		}
		f.relocs[i].at = mapped
	}
	if f.adapterReturnOff != 0 {
		mapped, err := mapFinalOffset(&result.Offsets, f.adapterReturnOff, len(result.Code), "adapter return")
		if err != nil {
			return 0, err
		}
		f.adapterReturnOff = mapped
	}
	if plan := f.gcFrameRoots; plan != nil {
		if plan.AdapterReturnOffset != 0 {
			mapped, err := mapFinalOffset(&result.Offsets, int(plan.AdapterReturnOffset), len(result.Code), "GC adapter return")
			if err != nil {
				return 0, err
			}
			plan.AdapterReturnOffset = uint32(mapped)
		}
		for i := range plan.Callsites {
			mapped, err := mapFinalOffset(&result.Offsets, int(plan.Callsites[i].ReturnOffset), len(result.Code), "GC call return")
			if err != nil {
				return 0, err
			}
			plan.Callsites[i].ReturnOffset = uint32(mapped)
		}
	}
	if len(result.Code) != oldLen {
		f.remapNativeSizeStats(&result.Offsets, internalOff, frameDeleted)
	}
	return internalOff, nil
}

func (f *fn) buildCompactionPlan(deletions []shared.DeletedRange) ([]shared.DeletedRange, int, bool) {
	// Any deletion before a loop would move its body away from alignment chosen
	// against maximal offsets. Preserve the size-stable path until loop padding
	// itself is an explicit relaxable fragment.
	if f.hasLoop {
		return nil, 0, false
	}
	add := func(off, length int) bool {
		if len(deletions) == cap(deletions) {
			return false
		}
		deletions = append(deletions, shared.DeletedRange{Off: uint32(off), Len: uint32(length)})
		return true
	}
	for key := range f.scratchState().branchTargets {
		off, marker, ok := decodeFinalizerMarker(key)
		if !ok {
			continue
		}
		if marker == markerPluginStart || marker == markerPluginEnd {
			return nil, 0, false
		}
		if marker == markerDeadHole && !add(off, 4) {
			return nil, 0, false
		}
	}

	frameSize := f.frameSize()
	frameDeleteLen := 0
	frameDeleteDelta := 0
	if f.opt(optSmallFrame) && frameSize <= 4095 {
		frameDeleteLen = 8
		frameDeleteDelta = 4
		if frameSize == 0 {
			frameDeleteLen = 12
			frameDeleteDelta = 0
		}
	}
	frameDeleted := 0
	if frameDeleteLen != 0 {
		if !add(f.subRspAt+frameDeleteDelta, frameDeleteLen) {
			return nil, 0, false
		}
		frameDeleted += frameDeleteLen
		for _, off := range f.tailFrameSites {
			if !add(off+frameDeleteDelta, frameDeleteLen) {
				return nil, 0, false
			}
			frameDeleted += frameDeleteLen
		}
		if !add(f.addRspAt+frameDeleteDelta, frameDeleteLen) {
			return nil, 0, false
		}
		frameDeleted += frameDeleteLen
	}

	for i := 1; i < len(deletions); i++ {
		value := deletions[i]
		j := i
		for j > 0 && deletions[j-1].Off > value.Off {
			deletions[j] = deletions[j-1]
			j--
		}
		deletions[j] = value
	}
	return deletions, frameDeleted, true
}

func (f *fn) compactNativeCode(offsets *shared.OffsetMap, deletions []shared.DeletedRange) ([]byte, error) {
	code := f.a.B
	if !f.compactionNeedsReencode() {
		src, dst := 0, 0
		for _, deletion := range deletions {
			off := int(deletion.Off)
			copy(code[dst:], code[src:off])
			dst += off - src
			src = off + int(deletion.Len)
		}
		copy(code[dst:], code[src:])
		return code[:offsets.FinalLen()], nil
	}
	markers := f.scratchState().branchTargets
	deletionIndex := 0
	dst := 0
	jumpData := false
	jumpBase := 0
	for src := 0; src < len(code); src += 4 {
		if markers[finalizerMarkerKey(src, markerJumpDataEnd)] {
			jumpData = false
		}
		if markers[finalizerMarkerKey(src, markerJumpDataStart)] {
			jumpData = true
			jumpBase = src
		}
		if deletionIndex < len(deletions) && src == int(deletions[deletionIndex].Off) {
			src += int(deletions[deletionIndex].Len) - 4
			deletionIndex++
			continue
		}
		word := rdWord(code, src)
		var err error
		if jumpData {
			word, err = remapJumpTableWord(word, jumpBase, offsets)
		} else if isPCRelativeWord(word) {
			word, err = remapPCRelativeWord(word, src, dst, offsets)
		}
		if err != nil {
			return nil, err
		}
		wrWord(code, dst, word)
		dst += 4
	}
	if dst != offsets.FinalLen() {
		return nil, fmt.Errorf("arm64 finalizer: compacted length %d, want %d", dst, offsets.FinalLen())
	}
	return code[:offsets.FinalLen()], nil
}

func (f *fn) compactionNeedsReencode() bool {
	if len(f.relocs) != 0 {
		return true
	}
	for key := range f.scratchState().branchTargets {
		if key >= 0 {
			return true
		}
		_, marker, ok := decodeFinalizerMarker(key)
		if ok && (marker == markerJumpDataStart || marker == markerJumpDataEnd || marker == markerPCRelative) {
			return true
		}
	}
	return false
}

func isPCRelativeWord(word uint32) bool {
	return word&0xFC000000 == 0x14000000 || word&0xFC000000 == 0x94000000 ||
		word&0xFF000010 == 0x54000000 || word&0x7E000000 == 0x34000000 ||
		word&0x7E000000 == 0x36000000 || word&0x9F000000 == 0x10000000
}

func remapJumpTableWord(word uint32, oldBase int, offsets *shared.OffsetMap) (uint32, error) {
	oldTarget := oldBase + int(int32(word))
	newBase, baseOK := offsets.Map(oldBase)
	newTarget, targetOK := offsets.Map(oldTarget)
	if !baseOK || !targetOK {
		return 0, fmt.Errorf("arm64 finalizer: jump-table target %d from base %d intersects deleted code", oldTarget, oldBase)
	}
	delta := int64(newTarget - newBase)
	if delta < -(1<<31) || delta >= 1<<31 {
		return 0, fmt.Errorf("arm64 finalizer: jump-table delta %d exceeds i32", delta)
	}
	return uint32(int32(delta)), nil
}

func remapPCRelativeWord(word uint32, oldPC, newPC int, offsets *shared.OffsetMap) (uint32, error) {
	oldTarget, branch := branchTarget(oldPC, word)
	if branch {
		newTarget, ok := offsets.Map(oldTarget)
		if !ok {
			return 0, fmt.Errorf("arm64 finalizer: branch at %d targets deleted offset %d", oldPC, oldTarget)
		}
		delta := newTarget - newPC
		if delta&3 != 0 {
			return 0, fmt.Errorf("arm64 finalizer: unaligned branch delta %d at %d", delta, oldPC)
		}
		d := delta / 4
		switch {
		case word&0xFC000000 == 0x14000000, word&0xFC000000 == 0x94000000:
			if d < -(1<<25) || d >= 1<<25 {
				return 0, fmt.Errorf("arm64 finalizer: branch26 at %d exceeds range", oldPC)
			}
			return word&^0x03FFFFFF | uint32(d)&0x03FFFFFF, nil
		case word&0xFF000010 == 0x54000000, word&0x7E000000 == 0x34000000:
			if d < -(1<<18) || d >= 1<<18 {
				return 0, fmt.Errorf("arm64 finalizer: branch19 at %d exceeds range", oldPC)
			}
			return word&^(0x7FFFF<<5) | (uint32(d)&0x7FFFF)<<5, nil
		case word&0x7E000000 == 0x36000000:
			if d < -(1<<13) || d >= 1<<13 {
				return 0, fmt.Errorf("arm64 finalizer: branch14 at %d exceeds range", oldPC)
			}
			return word&^(0x3FFF<<5) | (uint32(d)&0x3FFF)<<5, nil
		}
	}
	if oldTarget, adr := adrTarget(oldPC, word); adr {
		newTarget, ok := offsets.Map(oldTarget)
		if !ok {
			return 0, fmt.Errorf("arm64 finalizer: ADR at %d targets deleted offset %d", oldPC, oldTarget)
		}
		delta := newTarget - newPC
		if delta < -(1<<20) || delta >= 1<<20 {
			return 0, fmt.Errorf("arm64 finalizer: ADR at %d exceeds range", oldPC)
		}
		imm := uint32(delta) & 0x1FFFFF
		word &^= 3<<29 | 0x7FFFF<<5
		return word | (imm&3)<<29 | ((imm>>2)&0x7FFFF)<<5, nil
	}
	return word, nil
}

func (f *fn) remapNativeSizeStats(offsets *shared.OffsetMap, newInternalOff, frameDeleted int) {
	if f.stats == nil {
		return
	}
	s := &f.stats.NativeSize
	oldAdapterEnd := s.HostAdapterBytes
	if mapped, ok := offsets.Map(oldAdapterEnd); ok {
		s.HostAdapterBytes = mapped
		s.AdapterToInternalPaddingBytes = newInternalOff - mapped
	}
	s.FrameAdjustmentBytes -= frameDeleted
	s.DeadFrameReservationBytes = 0
	s.BranchFoldHoleBytes = 0
}

func (f *fn) validateFinalizerInventory(internalOff int) error {
	sc := f.scratchState()
	var labelStorage [3]shared.CodeLabel
	labels := labelStorage[:1]
	if internalOff != 0 {
		labels = append(labels, shared.CodeLabel{Off: uint32(internalOff)})
	}
	labels = append(labels, shared.CodeLabel{Off: uint32(len(f.a.B))})

	shortFrameLen := uint8(12)
	if f.opt(optSmallFrame) && f.frameSize() <= 4095 {
		shortFrameLen = 4
		if f.frameSize() == 0 {
			shortFrameLen = 0
		}
	}
	validateSite := func(off int, kind shared.RelaxKind, shortLen uint8) error {
		return shared.ValidateRelaxSite(len(f.a.B), shared.RelaxSite{Off: uint32(off), Kind: kind, LongLen: 12, ShortLen: shortLen})
	}
	if err := validateSite(f.subRspAt, shared.RelaxFrameSub, shortFrameLen); err != nil {
		return fmt.Errorf("arm64 identity finalizer: %w", err)
	}
	for _, off := range f.tailFrameSites {
		if err := validateSite(off, shared.RelaxFrameAdd, shortFrameLen); err != nil {
			return fmt.Errorf("arm64 identity finalizer: %w", err)
		}
	}
	if err := validateSite(f.addRspAt, shared.RelaxFrameAdd, shortFrameLen); err != nil {
		return fmt.Errorf("arm64 identity finalizer: %w", err)
	}
	var jumpStarts, jumpEnds, pluginStarts, pluginEnds int
	for encoded := range sc.branchTargets {
		off, marker, ok := decodeFinalizerMarker(encoded)
		if !ok {
			continue
		}
		if marker == markerDeadHole {
			if err := shared.ValidateRelaxSite(len(f.a.B), shared.RelaxSite{Off: uint32(off), Kind: shared.RelaxDeadHole, LongLen: 4}); err != nil {
				return fmt.Errorf("arm64 identity finalizer: %w", err)
			}
		} else if off < 0 || off > len(f.a.B) {
			return fmt.Errorf("arm64 identity finalizer: fragment marker %d at %d exceeds %d-byte function", marker, off, len(f.a.B))
		} else {
			switch marker {
			case markerJumpDataStart:
				jumpStarts++
			case markerJumpDataEnd:
				jumpEnds++
			case markerPluginStart:
				pluginStarts++
			case markerPluginEnd:
				pluginEnds++
			}
		}
	}
	if jumpStarts != jumpEnds || pluginStarts != pluginEnds {
		return fmt.Errorf("arm64 identity finalizer: unbalanced fragments: jump data %d/%d, plugin %d/%d", jumpStarts, jumpEnds, pluginStarts, pluginEnds)
	}
	if err := f.validatePCRelativeInventory(); err != nil {
		return err
	}

	_, err := shared.FinalizeIdentity(f.a.B, nil, labels, nil)
	if err != nil {
		return fmt.Errorf("arm64 identity finalizer: %w", err)
	}
	return nil
}

func (f *fn) validatePCRelativeInventory() error {
	markers := f.scratchState().branchTargets
	opaque := false
	for pc := 0; pc+4 <= len(f.a.B); pc += 4 {
		if finalizerOpaqueAt(markers, pc, &opaque) {
			continue
		}
		word := rdWord(f.a.B, pc)
		target, ok := branchTarget(pc, word)
		if !ok {
			target, ok = adrTarget(pc, word)
		}
		if ok && (target < 0 || target > len(f.a.B) || target&3 != 0) {
			return fmt.Errorf("arm64 identity finalizer: PC-relative reference at %d targets %d outside %d-byte function", pc, target, len(f.a.B))
		}
	}
	if opaque {
		return fmt.Errorf("arm64 identity finalizer: unterminated opaque fragment")
	}
	return nil
}

func adrTarget(pc int, word uint32) (int, bool) {
	if word&0x9F000000 != 0x10000000 {
		return 0, false
	}
	imm := int((word>>29)&3) | int((word>>5)&0x7FFFF)<<2
	if imm&(1<<20) != 0 {
		imm -= 1 << 21
	}
	return pc + imm, true
}

func mapFinalOffset(offsets *shared.OffsetMap, off, codeLen int, what string) (int, error) {
	mapped, ok := offsets.Map(off)
	if !ok {
		return 0, fmt.Errorf("arm64 identity finalizer: %s offset %d is outside %d-byte function", what, off, codeLen)
	}
	return mapped, nil
}

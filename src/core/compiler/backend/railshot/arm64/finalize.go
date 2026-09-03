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

// WAGO_COMPACT=1 forces bounded shrinking for measurement and rollout checks.
// CompileOptions.CompactNative selects the same path for an individual
// compilation; WAGO_COMPACT=0 disables it globally as a rollback oracle.
var nativeCompactionEnabled = os.Getenv("WAGO_COMPACT") == "1"
var nativeCompactionDisabled = os.Getenv("WAGO_COMPACT") == "0"
var loopCompactionEnabled = os.Getenv("WAGO_ARM64_NO_LOOP_COMPACTION") != "1"

// WAGO_ARM64_LOOP_COMPACTION_LIMIT selects the measured rollback/experiment
// bounds around the 32 KiB default. The immutable per-compilation policy remains
// the upper bound.
var arm64LoopCompactionLimit = func() int {
	switch os.Getenv("WAGO_ARM64_LOOP_COMPACTION_LIMIT") {
	case "16K":
		return 16 << 10
	case "24K":
		return 24 << 10
	case "64K":
		return 64 << 10
	default:
		return 32 << 10
	}
}()

// WAGO_FINALIZER_DELETIONS selects an older bounded compaction policy for
// exact rollout comparisons. It can only lower the immutable policy limit.
var finalizerDeletionLimitOverride = func() int {
	switch os.Getenv("WAGO_FINALIZER_DELETIONS") {
	case "8":
		return 8
	case "16":
		return 16
	case "32":
		return 32
	case "48":
		return 48
	case "64":
		return 64
	default:
		return 0
	}
}()

const maxFinalizerDeletions = shared.MaxOffsetMapDeletions

type finalizerMarker uint8

const (
	markerDeadHole finalizerMarker = iota
	markerJumpDataStart
	markerJumpDataEnd
	markerPluginStart
	markerPluginEnd
	markerPCRelative
	markerBranchNext
	markerOpaqueDataStart
	markerOpaqueDataEnd
)

type finalizerFragmentKind uint8

const (
	fragmentJumpData finalizerFragmentKind = iota + 1
	fragmentOpaqueData
	fragmentPlugin
)

type finalizerFragment struct {
	start int
	end   int
	kind  finalizerFragmentKind
}

type finalizerFragmentCursor struct {
	fragments []finalizerFragment
	index     int
}

func (c *finalizerFragmentCursor) at(pc int) (finalizerFragment, bool) {
	for c.index < len(c.fragments) && pc >= c.fragments[c.index].end {
		c.index++
	}
	if c.index == len(c.fragments) || pc < c.fragments[c.index].start {
		return finalizerFragment{}, false
	}
	return c.fragments[c.index], true
}

func finalizerMarkerKey(off int, marker finalizerMarker) int {
	return -((off << 4) | int(marker)) - 1
}

func decodeFinalizerMarker(key int) (off int, marker finalizerMarker, ok bool) {
	if key >= 0 {
		return 0, 0, false
	}
	value := -key - 1
	return value >> 4, finalizerMarker(value & 15), true
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
	f.recordFinalizerFragment(start, end, fragmentJumpData)
	f.recordFinalizerMarker(start, markerJumpDataStart)
	f.recordFinalizerMarker(end, markerJumpDataEnd)
}

func (f *fn) recordOpaqueData(start, end int) {
	if end > start {
		f.opaqueFragments = true
		f.recordFinalizerFragment(start, end, fragmentOpaqueData)
		f.recordFinalizerMarker(start, markerOpaqueDataStart)
		f.recordFinalizerMarker(end, markerOpaqueDataEnd)
	}
}

func (f *fn) recordOpaquePlugin(start, end int) {
	if end > start {
		f.opaqueFragments = true
		f.recordFinalizerFragment(start, end, fragmentPlugin)
		f.recordFinalizerMarker(start, markerPluginStart)
		f.recordFinalizerMarker(end, markerPluginEnd)
	}
}

func (f *fn) recordFinalizerFragment(start, end int, kind finalizerFragmentKind) {
	if !nativeFinalizerEnabled || !f.compactNative() || end <= start {
		return
	}
	sc := f.scratchState()
	sc.finalFragments = append(sc.finalFragments, finalizerFragment{start: start, end: end, kind: kind})
}

func (f *fn) recordPCRelative(off int) {
	f.recordFinalizerMarker(off, markerPCRelative)
}

func (f *fn) recordBranchNext(off int) {
	if !nativeFinalizerEnabled || !f.compactNative() {
		return
	}
	sc := f.scratchState()
	for i := range int(sc.branchNextN) {
		if sc.branchNextSites[i] == off {
			return
		}
	}
	if int(sc.branchNextN) < len(sc.branchNextSites) {
		sc.branchNextSites[sc.branchNextN] = off
		sc.branchNextN++
	} else {
		largest := 0
		for i := 1; i < len(sc.branchNextSites); i++ {
			if sc.branchNextSites[i] > sc.branchNextSites[largest] {
				largest = i
			}
		}
		if off < sc.branchNextSites[largest] {
			sc.branchNextSites[largest] = off
		}
	}
	if nativeFinalizerValidate {
		f.recordFinalizerMarker(off, markerBranchNext)
	}
}

func (f *fn) compactNative() bool {
	return !nativeCompactionDisabled && (nativeCompactionEnabled || f.policy.CompactNative)
}

func (f *fn) finalizerDeletionLimit() int {
	limit := int(f.policy.MaxFinalizerDeletions)
	if limit == 0 {
		limit = 8
	}
	if finalizerDeletionLimitOverride != 0 && limit > finalizerDeletionLimitOverride {
		limit = finalizerDeletionLimitOverride
	}
	return min(limit, maxFinalizerDeletions)
}

func loopCompactionLimitArm64(policy CodegenPolicy) int {
	limit := int(policy.MaxLoopCompactionBytes)
	if limit == 0 {
		limit = 16 << 10
	}
	return min(arm64LoopCompactionLimit, limit)
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
	code := f.a.B
	offsets := &f.scratchState().offsetMap
	frameDeleted := 0
	if f.compactNative() {
		var storage [maxFinalizerDeletions]shared.DeletedRange
		deletions, deletedFrames, ok := f.buildCompactionPlan(storage[:0:f.finalizerDeletionLimit()])
		if ok {
			if err := offsets.Reset(oldLen, deletions); err != nil {
				return 0, fmt.Errorf("arm64 finalizer: %w", err)
			}
			var err error
			code, err = f.compactNativeCode(offsets, deletions)
			if err != nil {
				return 0, err
			}
			frameDeleted = deletedFrames
		} else {
			if err := offsets.Reset(oldLen, nil); err != nil {
				return 0, fmt.Errorf("arm64 finalizer: %w", err)
			}
		}
	} else {
		if err := offsets.Reset(oldLen, nil); err != nil {
			return 0, fmt.Errorf("arm64 finalizer: %w", err)
		}
	}
	f.a.B = code

	mappedInternal, err := mapFinalOffset(offsets, internalOff, len(code), "internal entry")
	if err != nil {
		return 0, err
	}
	internalOff = mappedInternal
	for i := range f.relocs {
		mapped, err := mapFinalOffset(offsets, int(f.relocs[i].at), len(code), "call relocation")
		if err != nil {
			return 0, err
		}
		f.relocs[i].at = compactCallRelocField(mapped)
	}
	if f.adapterReturnOff != 0 {
		mapped, err := mapFinalOffset(offsets, f.adapterReturnOff, len(code), "adapter return")
		if err != nil {
			return 0, err
		}
		f.adapterReturnOff = mapped
	}
	if f.trapBodyEnd > f.trapBodyOff {
		mappedOff, err := mapFinalOffset(offsets, f.trapBodyOff, len(code), "trap body start")
		if err != nil {
			return 0, err
		}
		mappedEnd, err := mapFinalOffset(offsets, f.trapBodyEnd, len(code), "trap body end")
		if err != nil {
			return 0, err
		}
		f.trapBodyOff, f.trapBodyEnd = mappedOff, mappedEnd
	}
	if plan := f.gcFrameRoots; plan != nil {
		if plan.AdapterReturnOffset != 0 {
			mapped, err := mapFinalOffset(offsets, int(plan.AdapterReturnOffset), len(code), "GC adapter return")
			if err != nil {
				return 0, err
			}
			plan.AdapterReturnOffset = uint32(mapped)
		}
		for i := range plan.Callsites {
			mapped, err := mapFinalOffset(offsets, int(plan.Callsites[i].ReturnOffset), len(code), "GC call return")
			if err != nil {
				return 0, err
			}
			plan.Callsites[i].ReturnOffset = uint32(mapped)
		}
	}
	if len(code) != oldLen {
		f.remapNativeSizeStats(offsets, internalOff, frameDeleted)
	}
	return internalOff, nil
}

func (f *fn) buildCompactionPlan(deletions []shared.DeletedRange) ([]shared.DeletedRange, int, bool) {
	// Any deletion before an optionally aligned loop would move its body away
	// from the emission-time alignment. Native compaction clamps loop alignment
	// to ARM64's mandatory four-byte instruction alignment, which every deletion
	// preserves, so their loop-bearing functions are safe to compact.
	if f.hasLoop && !loopCompactionEnabled {
		return f.rejectCompaction("loop-disabled")
	}
	if f.hasLoop && f.policy.LoopAlignLog2 > 2 {
		return f.rejectCompaction("loop-alignment")
	}
	loopLimit := loopCompactionLimitArm64(f.policy)
	if f.hasLoop && len(f.a.B) > loopLimit {
		return f.rejectCompaction("loop-function-size")
	}
	add := func(off, length int) bool {
		if len(deletions) == cap(deletions) {
			return false
		}
		deletions = append(deletions, shared.DeletedRange{Off: uint32(off), Len: uint32(length)})
		return true
	}
	sc := f.scratchState()
	for key := range sc.branchTargets {
		_, marker, ok := decodeFinalizerMarker(key)
		if !ok {
			continue
		}
		if marker == markerPluginStart || marker == markerPluginEnd {
			return f.rejectCompaction("plugin-fragment")
		}
	}
	if sc.deadHoleOverflow {
		return f.rejectCompaction("dead-hole-overflow")
	}
	for _, off := range sc.deadHoleSites[:sc.deadHoleN] {
		if !add(off, 4) {
			return f.rejectCompaction("dead-hole-budget")
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
			return f.rejectCompaction("frame-site-budget")
		}
		frameDeleted += frameDeleteLen
		for _, off := range f.tailFrameSites {
			if !add(off+frameDeleteDelta, frameDeleteLen) {
				return f.rejectCompaction("frame-site-budget")
			}
			frameDeleted += frameDeleteLen
		}
		if !add(f.addRspAt+frameDeleteDelta, frameDeleteLen) {
			return f.rejectCompaction("frame-site-budget")
		}
		frameDeleted += frameDeleteLen
	}

	// Branches whose target was already the following instruction were recorded
	// during the existing peephole target scan. Spend only budget left after the
	// larger frame/hole wins. If the bound fills, deterministically retain the
	// earliest sites instead of depending on map iteration order.
	branchStart := len(deletions)
	for _, off := range sc.branchNextSites[:sc.branchNextN] {
		candidate := shared.DeletedRange{Off: uint32(off), Len: 4}
		duplicate := false
		for _, deletion := range deletions[:branchStart] {
			if deletion == candidate {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		if len(deletions) < cap(deletions) {
			deletions = append(deletions, candidate)
			continue
		}
		largest := branchStart
		for i := branchStart + 1; i < len(deletions); i++ {
			if deletions[i].Off > deletions[largest].Off {
				largest = i
			}
		}
		if largest < len(deletions) && candidate.Off < deletions[largest].Off {
			deletions[largest] = candidate
		}
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
	// A size-preserving peephole can turn a branch-to-next into the same NOP
	// already recorded as a dead hole. Keep one copy of an identical deletion;
	// reject any other overlap rather than guessing which fragment owns it.
	unique := deletions[:0]
	for _, deletion := range deletions {
		if len(unique) != 0 {
			previous := unique[len(unique)-1]
			if deletion.Off == previous.Off && deletion.Len == previous.Len {
				continue
			}
			if deletion.Off < previous.Off+previous.Len {
				return f.rejectCompaction("deletion-overlap")
			}
		}
		unique = append(unique, deletion)
	}
	deletions = unique
	return deletions, frameDeleted, true
}

func (f *fn) rejectCompaction(reason string) ([]shared.DeletedRange, int, bool) {
	f.stats.setFinalizerFallback(reason)
	return nil, 0, false
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
	fragments := finalizerFragmentCursor{fragments: f.scratchState().finalFragments}
	deletionIndex := 0
	dst := 0
	for src := 0; src < len(code); src += 4 {
		if deletionIndex < len(deletions) && src == int(deletions[deletionIndex].Off) {
			src += int(deletions[deletionIndex].Len) - 4
			deletionIndex++
			continue
		}
		word := rdWord(code, src)
		var err error
		fragment, inFragment := fragments.at(src)
		if inFragment && fragment.kind == fragmentOpaqueData {
			// Compact target-ID bytes are data, not instructions or relocations.
		} else if inFragment && fragment.kind == fragmentJumpData {
			word, err = remapJumpTableWord(word, fragment.start, offsets)
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
		if ok && (marker == markerJumpDataStart || marker == markerJumpDataEnd || marker == markerOpaqueDataStart || marker == markerOpaqueDataEnd || marker == markerPCRelative) {
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
	s.StoreLoadNopBytes = 0
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
	var jumpStarts, jumpEnds, pluginStarts, pluginEnds, dataStarts, dataEnds int
	for encoded := range sc.branchTargets {
		off, marker, ok := decodeFinalizerMarker(encoded)
		if !ok {
			continue
		}
		if marker == markerDeadHole || marker == markerBranchNext {
			kind := shared.RelaxDeadHole
			if marker == markerBranchNext {
				kind = shared.RelaxBranch
			}
			if err := shared.ValidateRelaxSite(len(f.a.B), shared.RelaxSite{Off: uint32(off), Kind: kind, LongLen: 4}); err != nil {
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
			case markerOpaqueDataStart:
				dataStarts++
			case markerOpaqueDataEnd:
				dataEnds++
			}
		}
	}
	if jumpStarts != jumpEnds || pluginStarts != pluginEnds || dataStarts != dataEnds {
		return fmt.Errorf("arm64 identity finalizer: unbalanced fragments: jump data %d/%d, opaque data %d/%d, plugin %d/%d", jumpStarts, jumpEnds, dataStarts, dataEnds, pluginStarts, pluginEnds)
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

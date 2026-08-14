//go:build amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	encoderamd64 "github.com/wago-org/wago/src/core/encoder/amd64"
)

// WAGO_FINALIZE=0 retains the pre-finalizer path as a rollout oracle. The
// identity finalizer changes no bytes; it establishes one owner for every
// function-relative metadata offset before AMD64 relaxation can shrink code.
var nativeFinalizerEnabled = os.Getenv("WAGO_FINALIZE") != "0"

// WAGO_COMPACT=1 forces bounded shrinking for every objective. Size and
// Embedded enable it through their immutable per-compilation policy;
// WAGO_COMPACT=0 is the rollout oracle that disables it for every objective.
var nativeCompactionEnabled = os.Getenv("WAGO_COMPACT") == "1"
var nativeCompactionDisabled = os.Getenv("WAGO_COMPACT") == "0"
var loopCompactionEnabled = os.Getenv("WAGO_AMD64_NO_LOOP_COMPACTION") != "1"
var jumpTableCompactionEnabled = os.Getenv("WAGO_AMD64_NO_JUMP_TABLE_COMPACTION") != "1"
var loopCompactionByteLimitOverride = func() int {
	if os.Getenv("WAGO_AMD64_LOOP_COMPACTION_LIMIT") == "16K" {
		return 16 << 10
	}
	return 0
}()
var finalizerRel32SiteLimitOverride = func() int {
	switch os.Getenv("WAGO_AMD64_FINALIZER_REL32_SITES") {
	case "256":
		return 256
	case "1024":
		return 1024
	case "1280":
		return 1280
	case "1536":
		return 1536
	case "2048":
		return 2048
	default:
		return 0
	}
}()
var partialHoleCompactionEnabled = os.Getenv("WAGO_AMD64_NO_PARTIAL_HOLE_COMPACTION") != "1"

// WAGO_FINALIZER_DELETIONS selects an older bounded Size/Embedded policy for
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
	case "128":
		return 128
	default:
		return 0
	}
}()

func compactNativePolicy(policy CodegenPolicy) bool {
	return !nativeCompactionDisabled && (nativeCompactionEnabled || policy.CompactNative)
}

func (f *fn) compactNative() bool { return compactNativePolicy(f.policy) }

func (f *fn) finalizerDeletionLimit() int {
	limit := int(f.policy.MaxFinalizerDeletions)
	if limit == 0 {
		limit = 8
	}
	if finalizerDeletionLimitOverride != 0 && limit > finalizerDeletionLimitOverride {
		limit = finalizerDeletionLimitOverride
	}
	return min(limit, shared.MaxWideOffsetMapDeletions)
}

func (f *fn) finalizerRelaxIterationLimit() int {
	limit := int(f.policy.MaxRelaxIterations)
	if limit == 0 {
		limit = 8
	}
	return min(limit, shared.MaxWideOffsetMapDeletions)
}

const maxAMD64FinalizerRel32Sites = 2048
const maxAMD64LoopCompactionBytes = 64 << 10
const maxAMD64LocalRefSites = shared.MaxWideOffsetMapDeletions

func finalizerRel32Limit(policy CodegenPolicy) int {
	limit := int(policy.MaxRel32Sites)
	if limit == 0 {
		limit = 256
	}
	if finalizerRel32SiteLimitOverride != 0 {
		limit = finalizerRel32SiteLimitOverride
	}
	return min(limit, maxAMD64FinalizerRel32Sites)
}

func loopCompactionLimit(policy CodegenPolicy) int {
	limit := int(policy.MaxLoopCompactionBytes)
	if limit == 0 {
		limit = 16 << 10
	}
	if loopCompactionByteLimitOverride != 0 && limit > loopCompactionByteLimitOverride {
		limit = loopCompactionByteLimitOverride
	}
	return min(limit, maxAMD64LoopCompactionBytes)
}

func (f *fn) loopCompactionAdmitted() bool {
	return !f.hasLoop || loopCompactionEnabled && len(f.a.B) <= loopCompactionLimit(f.policy)
}

func (f *fn) finalizeNativeCode(internalOff int) (int, error) {
	if !nativeFinalizerEnabled {
		return internalOff, nil
	}
	oldLen := len(f.a.B)
	result, frameDeleted, holeDeleted, err := f.finalizeFrameAdjustments()
	if err != nil {
		return 0, err
	}
	f.a.B = result.Code
	if len(result.Code) == oldLen && internalOff == 0 && len(f.relocs) == 0 && f.adapterReturnOff == 0 && f.gcFrameRoots == nil {
		// The common tiny internal leaf has no function-relative metadata to
		// remap. FinalizeIdentity has still validated the emitted image; avoid a
		// redundant map call for every such function in many-function modules.
		return 0, nil
	}

	internalOff, err = mapAMD64FinalOffset(&result.Offsets, internalOff, len(result.Code), "internal entry")
	if err != nil {
		return 0, err
	}
	for i := range f.relocs {
		mapped, err := mapAMD64FinalOffset(&result.Offsets, f.relocs[i].at, len(result.Code), "call relocation")
		if err != nil {
			return 0, err
		}
		f.relocs[i].at = mapped
	}
	if len(f.literalWords) != 0 {
		keyCount := int(f.literalWords[0])
		for i := 1 + 3*keyCount; i < len(f.literalWords); i++ {
			encoded := f.literalWords[i]
			mapped, err := mapAMD64FinalOffset(&result.Offsets, int(uint32(encoded>>32)), len(result.Code), "literal relocation")
			if err != nil {
				return 0, err
			}
			f.literalWords[i] = uint64(uint32(mapped))<<32 | uint64(uint32(encoded))
		}
	}
	if f.adapterReturnOff != 0 {
		mapped, err := mapAMD64FinalOffset(&result.Offsets, f.adapterReturnOff, len(result.Code), "adapter return")
		if err != nil {
			return 0, err
		}
		f.adapterReturnOff = mapped
	}
	if plan := f.gcFrameRoots; plan != nil {
		if plan.AdapterReturnOffset != 0 {
			mapped, err := mapAMD64FinalOffset(&result.Offsets, int(plan.AdapterReturnOffset), len(result.Code), "GC adapter return")
			if err != nil {
				return 0, err
			}
			plan.AdapterReturnOffset = uint32(mapped)
		}
		for i := range plan.Callsites {
			mapped, err := mapAMD64FinalOffset(&result.Offsets, int(plan.Callsites[i].ReturnOffset), len(result.Code), "GC call return")
			if err != nil {
				return 0, err
			}
			plan.Callsites[i].ReturnOffset = uint32(mapped)
		}
	}
	if frameDeleted != 0 && f.stats != nil {
		f.stats.NativeSize.FrameAdjustmentBytes -= frameDeleted
		f.stats.NativeSize.DeadFrameReservationBytes = 0
	}
	if holeDeleted != 0 && f.stats != nil {
		f.stats.NativeSize.BranchFoldHoleBytes -= holeDeleted
	}
	return internalOff, nil
}

type amd64FinalizeResult struct {
	Code    []byte
	Offsets shared.WideOffsetMap
}

func (f *fn) finalizeFrameAdjustments() (amd64FinalizeResult, int, int, error) {
	identity := func(reason string) (amd64FinalizeResult, int, int, error) {
		if reason != "" {
			f.stats.setFinalizerFallback(reason)
		}
		offsets, err := shared.NewWideOffsetMap(len(f.a.B), nil)
		if err != nil {
			return amd64FinalizeResult{}, 0, 0, fmt.Errorf("amd64 finalizer: %w", err)
		}
		return amd64FinalizeResult{Code: f.a.B, Offsets: offsets}, 0, 0, nil
	}
	if !f.compactNative() {
		return identity("")
	}
	if f.hasLoop && !loopCompactionEnabled {
		return identity("loop-disabled")
	}
	if f.hasLoop && len(f.a.B) > loopCompactionLimit(f.policy) {
		return identity("loop-function-size")
	}
	if f.hasJumpTableData && !jumpTableCompactionEnabled {
		return identity("jump-table-disabled")
	}
	if len(f.customInstructions) != 0 {
		return identity("plugin-fragment")
	}
	if f.a.Rel32Overflow {
		return identity("rel32-overflow")
	}
	frameSize := f.frameSize()
	compactFrame := frameSize <= 127
	for i := range f.a.Rel32Sites {
		f.a.Rel32Sites[i].SetShort(false)
	}

	var storage [shared.MaxWideOffsetMapDeletions]shared.DeletedRange
	deletions := storage[:0:f.finalizerDeletionLimit()]
	frameSites := 0
	if compactFrame {
		frameSites = len(f.sc.tailFrameSites) + 2
	}
	if frameSites > cap(deletions) {
		return identity("frame-site-budget")
	}
	holeBudget := cap(deletions) - frameSites
	holeSites := 0
	for _, over := range f.sc.brFoldSites {
		if over >= 0 && over+9 <= len(f.a.B) &&
			bytes.Equal(f.a.B[over+4:over+9], []byte{0x0f, 0x1f, 0x44, 0x00, 0x00}) {
			holeSites++
		}
	}
	if !partialHoleCompactionEnabled && holeSites > holeBudget {
		return identity("dead-hole-budget")
	}
	holeDeleted := 0
	frameDeleted := 0
	for _, over := range f.sc.brFoldSites {
		if holeBudget == 0 {
			break
		}
		if over >= 0 && over+9 <= len(f.a.B) &&
			bytes.Equal(f.a.B[over+4:over+9], []byte{0x0f, 0x1f, 0x44, 0x00, 0x00}) {
			deletions = append(deletions, shared.DeletedRange{Off: uint32(over + 4), Len: 5})
			holeDeleted += 5
			holeBudget--
		}
	}
	localBudget := cap(deletions) - len(deletions) - frameSites
	if localBudget > 0 && f.packLocalSlots(localBudget) != 0 {
		for _, site := range f.a.LocalRefs.Sites {
			local := int(site.Local)
			if local < 0 || local >= len(f.localSlot) {
				return amd64FinalizeResult{}, 0, 0, fmt.Errorf("amd64 finalizer: invalid local reference identity %d", site.Local)
			}
			newDisp := f.localOff(local)
			if newDisp == site.OldDisp {
				continue
			}
			modRMOff, dispOff := int(site.ModRMOff), int(site.DispOff)
			if modRMOff < 0 || modRMOff >= len(f.a.B) || dispOff < 0 || dispOff+4 > len(f.a.B) ||
				f.a.B[modRMOff]&0xc0 != 0x80 || int32(binary.LittleEndian.Uint32(f.a.B[dispOff:])) != site.OldDisp ||
				newDisp < 0 || newDisp > 127 {
				return amd64FinalizeResult{}, 0, 0, fmt.Errorf("amd64 finalizer: invalid local disp32 site %d/%d (%d -> %d)", modRMOff, dispOff, site.OldDisp, newDisp)
			}
			deletion := shared.DeletedRange{Off: uint32(dispOff + 1), Len: 3}
			f.a.B[modRMOff] = f.a.B[modRMOff]&0x3f | 0x40
			f.a.B[dispOff] = byte(newDisp)
			if newDisp == 0 {
				f.a.B[modRMOff] &= 0x3f
				deletion = shared.DeletedRange{Off: uint32(dispOff), Len: 4}
			}
			deletions = append(deletions, deletion)
			if s := f.stats; s != nil {
				s.Encoding.MemoryDisp32--
				s.Encoding.FrameDisp32--
				s.Encoding.LocalDisp32--
				if newDisp == 0 {
					s.Encoding.MemoryDisp0++
					s.Encoding.FrameDisp0++
					s.Encoding.LocalDisp0++
				} else {
					s.Encoding.MemoryDisp8++
					s.Encoding.FrameDisp8++
					s.Encoding.LocalDisp8++
				}
			}
		}
	}
	addFrameSite := func(site int, opcode byte) error {
		if site < 3 || site+4 > len(f.a.B) || f.a.B[site-3] != 0x48 ||
			f.a.B[site-2] != 0x81 || f.a.B[site-1] != opcode ||
			binary.LittleEndian.Uint32(f.a.B[site:]) != uint32(frameSize) {
			return fmt.Errorf("amd64 finalizer: invalid frame-adjustment site %d", site)
		}
		if frameSize == 0 {
			deletions = append(deletions, shared.DeletedRange{Off: uint32(site - 3), Len: 7})
			frameDeleted += 7
			return nil
		}
		// 48 81 /digit imm32 -> 48 83 /digit imm8. The low immediate byte is
		// already correct; delete only the trailing three bytes.
		f.a.B[site-2] = 0x83
		deletions = append(deletions, shared.DeletedRange{Off: uint32(site + 1), Len: 3})
		frameDeleted += 3
		return nil
	}
	if compactFrame {
		if err := addFrameSite(f.subRspAt, 0xec); err != nil {
			return amd64FinalizeResult{}, 0, 0, err
		}
		for _, site := range f.sc.tailFrameSites {
			if err := addFrameSite(site, 0xc4); err != nil {
				return amd64FinalizeResult{}, 0, 0, err
			}
		}
		if err := addFrameSite(f.addRspAt, 0xc4); err != nil {
			return amd64FinalizeResult{}, 0, 0, err
		}
	}

	sortDeletions := func(ranges []shared.DeletedRange) {
		for i := 1; i < len(ranges); i++ {
			value := ranges[i]
			j := i
			for j > 0 && ranges[j-1].Off > value.Off {
				ranges[j] = ranges[j-1]
				j--
			}
			ranges[j] = value
		}
	}
	sortDeletions(deletions)
	var deletionPrefix [shared.MaxWideOffsetMapDeletions]uint32
	rebuildDeletionPrefix := func(start int) {
		deleted := uint32(0)
		if start > 0 {
			deleted = deletionPrefix[start-1]
		}
		for i := start; i < len(deletions); i++ {
			deleted += deletions[i].Len
			deletionPrefix[i] = deleted
		}
	}
	rebuildDeletionPrefix(0)
	mapCurrent := func(off int) (int, bool) {
		lo, hi := 0, len(deletions)
		for lo < hi {
			mid := int(uint(lo+hi) >> 1)
			if int(deletions[mid].Off) <= off {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		i := lo - 1
		if i < 0 {
			return off, true
		}
		deletion := deletions[i]
		start := int(deletion.Off)
		end := start + int(deletion.Len)
		if off > start && off < end {
			return 0, false
		}
		deleted := deletionPrefix[i]
		if off == start {
			deleted -= deletion.Len
		}
		return off - int(deleted), true
	}
	mapWithExtra := func(off int, extra shared.DeletedRange) (int, bool) {
		mapped, ok := mapCurrent(off)
		if !ok {
			return 0, false
		}
		start := int(extra.Off)
		end := start + int(extra.Len)
		if off > start && off < end {
			return 0, false
		}
		if off >= end {
			mapped -= int(extra.Len)
		}
		return mapped, true
	}
	insertDeletion := func(deletion shared.DeletedRange) bool {
		if len(deletions) == cap(deletions) {
			return false
		}
		at := len(deletions)
		for i, existing := range deletions {
			if deletion.Off < existing.Off {
				at = i
				break
			}
		}
		end := deletion.Off + deletion.Len
		if at > 0 {
			previous := deletions[at-1]
			if previous.Off+previous.Len > deletion.Off {
				return false
			}
		}
		if at < len(deletions) && end > deletions[at].Off {
			return false
		}
		deletions = append(deletions, shared.DeletedRange{})
		copy(deletions[at+1:], deletions[at:])
		deletions[at] = deletion
		rebuildDeletionPrefix(at)
		return true
	}

	// Branch relaxation consumes the same fixed deletion budget as frames and
	// dead holes. Each admitted site changes only long -> short; reconsidering
	// rejected sites after every round finds cascades while bounding work by the
	// eight-range offset map.
	rel32Target := func(site encoderamd64.Rel32Site) (int, bool) {
		at := site.At()
		if at < 0 || at+4 > len(f.a.B) {
			return 0, false
		}
		target := at + 4 + int(int32(binary.LittleEndian.Uint32(f.a.B[at:])))
		return target, target >= 0 && target <= len(f.a.B)
	}
	branchForm := func(site encoderamd64.Rel32Site) (start, shortLen int, deletion shared.DeletedRange, ok bool) {
		at := site.At()
		switch site.Kind() {
		case encoderamd64.Rel32Jmp:
			if at < 1 || at+4 > len(f.a.B) || f.a.B[at-1] != 0xe9 {
				return 0, 0, shared.DeletedRange{}, false
			}
			return at - 1, 2, shared.DeletedRange{Off: uint32(at + 1), Len: 3}, true
		case encoderamd64.Rel32Jcc:
			if at < 2 || at+4 > len(f.a.B) || f.a.B[at-2] != 0x0f || f.a.B[at-1]&0xf0 != 0x80 {
				return 0, 0, shared.DeletedRange{}, false
			}
			return at - 2, 2, shared.DeletedRange{Off: uint32(at), Len: 4}, true
		default:
			return 0, 0, shared.DeletedRange{}, false
		}
	}
	// Exact recorded JMP/Jcc sites need no disassembly pass. Calls are Rel32Other
	// and retain their return-address side effect.
	var deletedBranches [(maxAMD64FinalizerRel32Sites + 63) / 64]uint64
	// Compact target-ID tables address a fixed-width rel32 jump vector, and the
	// large switch functions admitted by explicit fragments made full branch
	// relaxation exceed the Size compile-time gate. Keep every branch in a jump-
	// table function at its emitted width while still remapping it around frame
	// and dead-hole deletions.
	if !f.hasJumpTableData {
		for i, site := range f.a.Rel32Sites {
			start, _, _, ok := branchForm(site)
			if !ok {
				continue
			}
			target, okTarget := rel32Target(site)
			longLen := site.At() + 4 - start
			if !okTarget || target != site.At()+4 {
				continue
			}
			if !insertDeletion(shared.DeletedRange{Off: uint32(start), Len: uint32(longLen)}) {
				continue
			}
			deletedBranches[i>>6] |= uint64(1) << uint(i&63)
		}
		for round := 0; round < f.finalizerRelaxIterationLimit() && len(deletions) < cap(deletions); round++ {
			changed := false
			for i := range f.a.Rel32Sites {
				site := &f.a.Rel32Sites[i]
				if deletedBranches[i>>6]&(uint64(1)<<uint(i&63)) != 0 || site.Short() || len(deletions) == cap(deletions) {
					continue
				}
				start, shortLen, deletion, ok := branchForm(*site)
				if !ok {
					continue
				}
				target, okTarget := rel32Target(*site)
				if !okTarget {
					continue
				}
				mappedStart, okStart := mapWithExtra(start, deletion)
				mappedTarget, okTarget := mapWithExtra(target, deletion)
				disp := mappedTarget - (mappedStart + shortLen)
				if !okStart || !okTarget || disp < -128 || disp > 127 {
					continue
				}
				if !insertDeletion(deletion) {
					continue
				}
				site.SetShort(true)
				changed = true
			}
			if !changed {
				break
			}
		}
	}
	offsets, err := shared.NewWideOffsetMap(len(f.a.B), deletions)
	if err != nil {
		return amd64FinalizeResult{}, 0, 0, fmt.Errorf("amd64 finalizer: %w", err)
	}
	// Jump-table data has an explicit fragment owner. Compact target-ID bytes are
	// opaque and move unchanged; signed i32 entries are relative to the table
	// base and must follow both the base and their code targets.
	for _, fragment := range f.sc.jumpTableFragments {
		if fragment.start < 0 || fragment.end < fragment.start || fragment.end > len(f.a.B) {
			return amd64FinalizeResult{}, 0, 0, fmt.Errorf("amd64 finalizer: invalid jump-table fragment [%d,%d)", fragment.start, fragment.end)
		}
		for _, deletion := range deletions {
			deletionStart := int(deletion.Off)
			deletionEnd := deletionStart + int(deletion.Len)
			if deletionStart < fragment.end && fragment.start < deletionEnd {
				return amd64FinalizeResult{}, 0, 0, fmt.Errorf("amd64 finalizer: deletion [%d,%d) intersects jump-table fragment [%d,%d)", deletionStart, deletionEnd, fragment.start, fragment.end)
			}
		}
		newBase, baseOK := offsets.Map(fragment.start)
		newEnd, endOK := offsets.Map(fragment.end)
		if !baseOK || !endOK || newEnd-newBase != fragment.end-fragment.start {
			return amd64FinalizeResult{}, 0, 0, fmt.Errorf("amd64 finalizer: jump-table fragment [%d,%d) does not map intact", fragment.start, fragment.end)
		}
		switch fragment.kind {
		case jumpTableFragmentIDs:
			continue
		case jumpTableFragmentDeltas:
			if (fragment.end-fragment.start)&3 != 0 {
				return amd64FinalizeResult{}, 0, 0, fmt.Errorf("amd64 finalizer: unaligned jump-table fragment [%d,%d)", fragment.start, fragment.end)
			}
			for at := fragment.start; at < fragment.end; at += 4 {
				oldTarget := fragment.start + int(int32(binary.LittleEndian.Uint32(f.a.B[at:])))
				newTarget, targetOK := offsets.Map(oldTarget)
				if !targetOK {
					return amd64FinalizeResult{}, 0, 0, fmt.Errorf("amd64 finalizer: jump-table target %d intersects deleted code", oldTarget)
				}
				delta := int64(newTarget - newBase)
				if delta < -(1<<31) || delta >= 1<<31 {
					return amd64FinalizeResult{}, 0, 0, fmt.Errorf("amd64 finalizer: jump-table delta %d exceeds i32", delta)
				}
				binary.LittleEndian.PutUint32(f.a.B[at:], uint32(int32(delta)))
			}
		default:
			return amd64FinalizeResult{}, 0, 0, fmt.Errorf("amd64 finalizer: unknown jump-table fragment kind %d", fragment.kind)
		}
	}
	// Patch maximal-encoding source fields with their final displacements before
	// the left-to-right copy. Exact recorded fields supply their old targets; no
	// finalized instruction stream is decoded.
	for i, site := range f.a.Rel32Sites {
		if deletedBranches[i>>6]&(uint64(1)<<uint(i&63)) != 0 {
			continue
		}
		targetOld, okTargetOld := rel32Target(site)
		if !okTargetOld {
			return amd64FinalizeResult{}, 0, 0, fmt.Errorf("amd64 finalizer: invalid rel32 target at %d", site.At())
		}
		target, okTarget := offsets.Map(targetOld)
		if site.Short() {
			start, shortLen := site.At()-1, 2
			if site.Kind() == encoderamd64.Rel32Jcc {
				start = site.At() - 2
			}
			at, okAt := offsets.Map(start)
			if !okAt || !okTarget || at < 0 || at+shortLen > offsets.FinalLen() || target < 0 || target > offsets.FinalLen() {
				return amd64FinalizeResult{}, 0, 0, fmt.Errorf("amd64 finalizer: invalid rel8 remap %d -> %d", site.At(), targetOld)
			}
			disp := target - (at + shortLen)
			if disp < -128 || disp > 127 {
				return amd64FinalizeResult{}, 0, 0, fmt.Errorf("amd64 finalizer: rel8 overflow %d -> %d", site.At(), targetOld)
			}
			if site.Kind() == encoderamd64.Rel32Jmp {
				f.a.B[start] = 0xeb
			} else {
				f.a.B[start] = 0x70 | (f.a.B[start+1] & 0x0f)
			}
			f.a.B[start+1] = byte(int8(disp))
			continue
		}
		atOld := site.At()
		at, okAt := offsets.Map(atOld)
		if !okAt || !okTarget || at < 0 || at+4 > offsets.FinalLen() || target < 0 || target > offsets.FinalLen() {
			return amd64FinalizeResult{}, 0, 0, fmt.Errorf("amd64 finalizer: invalid rel32 remap %d -> %d", atOld, targetOld)
		}
		binary.LittleEndian.PutUint32(f.a.B[atOld:], uint32(int32(target-(at+4))))
	}
	src, dst := 0, 0
	for _, deletion := range deletions {
		off := int(deletion.Off)
		copy(f.a.B[dst:], f.a.B[src:off])
		dst += off - src
		src = off + int(deletion.Len)
	}
	copy(f.a.B[dst:], f.a.B[src:])
	code := f.a.B[:offsets.FinalLen()]
	if holeDeleted < holeSites*5 {
		f.stats.setFinalizerFallback("dead-hole-budget-partial")
	}
	return amd64FinalizeResult{Code: code, Offsets: offsets}, frameDeleted, holeDeleted, nil
}

func mapAMD64FinalOffset(offsets *shared.WideOffsetMap, old, codeLen int, kind string) (int, error) {
	mapped, ok := offsets.Map(old)
	if !ok || mapped < 0 || mapped > codeLen {
		return 0, fmt.Errorf("amd64 finalizer: invalid %s offset %d", kind, old)
	}
	return mapped, nil
}

//go:build arm64

package arm64

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

func (f *fn) recordFinalizerMarker(off int, marker finalizerMarker) {
	if !nativeFinalizerEnabled || !nativeFinalizerValidate {
		return
	}
	sc := f.scratchState()
	if sc.finalizerMarkers == nil {
		sc.finalizerMarkers = make(map[int]bool, 16)
	}
	sc.finalizerMarkers[finalizerMarkerKey(off, marker)] = true
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

func (f *fn) recordFinalizerFragment(start, end int, kind finalizerFragmentKind) {
	if !nativeFinalizerEnabled || !f.compactNative() || end <= start {
		return
	}
	sc := f.scratchState()
	if start < 0 || uint64(end) > uint64(^uint32(0)) {
		sc.fragmentOverflow = true
		return
	}
	sc.finalFragments = append(sc.finalFragments, finalizerFragment{start: uint32(start), end: uint32(end), kind: kind})
}

func (f *fn) recordPCRelative(off int) {
	if nativeFinalizerEnabled {
		f.scratchState().hasPCRelative = true
	}
	f.recordFinalizerMarker(off, markerPCRelative)
}

func (f *fn) compactNative() bool {
	return !nativeCompactionDisabled && (nativeCompactionEnabled || f.policy.CompactNative)
}

func (f *fn) finalizeNativeCode(internalOff int) (int, error) {
	// Mandatory branch patches also occur after body lowering, in return, trap,
	// and adapter code. Reject failures before any optional finalization path.
	if f.representationLimit != functionRepresentationOK {
		return 0, f.representationError()
	}
	if !nativeFinalizerEnabled {
		return internalOff, nil
	}
	if f.scratchState().fragmentOverflow {
		return 0, fmt.Errorf("arm64 finalizer: fragment offset exceeds 32-bit function domain")
	}
	if nativeFinalizerValidate {
		if err := f.validateFinalizerInventory(internalOff); err != nil {
			return 0, err
		}
	}
	oldLen := len(f.a.B)
	code := f.a.B
	if uint64(oldLen) > uint64(^uint32(0)) {
		return 0, fmt.Errorf("arm64 finalizer: invalid %d-byte function length", oldLen)
	}
	// Nil is the checked identity map. Only a shrinking finalizer needs the
	// fixed deletion/prefix table, which stays in compact-only worker storage.
	var offsets *shared.OffsetMap
	frameDeleted := 0
	if f.compactNative() {
		offsets = &f.scratchState().ensureCompactScratch().offsetMap
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
	}
	f.a.B = code
	if profileEnabled && f.stats != nil && f.stats.RecordSources {
		var profileOffsets interface {
			MapRange(int, int) (int, int, bool)
		} = identityProfileOffsets(oldLen)
		if offsets != nil {
			profileOffsets = offsets
		}
		sites, err := shared.RemapNativeCodeSites(f.stats.CodeSites, profileOffsets)
		if err != nil {
			return 0, err
		}
		f.stats.CodeSites = sites
		mapped, err := shared.RemapNativeSources(f.stats.SourceRanges, profileOffsets)
		if err != nil {
			return 0, err
		}
		f.stats.SourceRanges = mapped
		start, _, ok := profileOffsets.MapRange(internalOff, internalOff)
		if !ok {
			return 0, fmt.Errorf("invalid profile internal entry")
		}
		f.stats.SourceInternalOffset = start
	}

	mappedInternal, err := mapFinalOffset(offsets, internalOff, len(code), "internal entry")
	if err != nil {
		return 0, err
	}
	internalOff = mappedInternal
	if f.nativeImportRouter > 0 {
		mapped, err := mapFinalOffset(offsets, f.nativeImportRouter, len(code), "native import router")
		if err != nil {
			return 0, err
		}
		f.nativeImportRouter = mapped
	}
	for i := range f.relocs {
		mapped, err := mapFinalOffset(offsets, int(f.relocs[i].at), len(code), "call relocation")
		if err != nil {
			return 0, err
		}
		if uint64(mapped) >= uint64(invalidCallRelocField) {
			return 0, fmt.Errorf("arm64 finalizer: call relocation offset %#x exceeds compact domain", mapped)
		}
		f.relocs[i].at = uint32(mapped)
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
		var callsiteErr error
		if !plan.VisitCallsites(func(_ int, callsite shared.GCFrameCallsite) bool {
			var mapped int
			mapped, callsiteErr = mapFinalOffset(offsets, int(callsite.ReturnOffset()), len(code), "GC call return")
			if callsiteErr != nil {
				return false
			}
			callsite.SetReturnOffset(uint32(mapped))
			return true
		}) {
			if callsiteErr != nil {
				return 0, callsiteErr
			}
			return 0, fmt.Errorf("arm64: malformed GC callsite stream")
		}
	}
	if len(code) != oldLen {
		f.remapNativeSizeStats(offsets, internalOff, frameDeleted)
	}
	return internalOff, nil
}

func mapFinalOffset(offsets *shared.OffsetMap, off, codeLen int, what string) (int, error) {
	mapped, ok := off, off >= 0 && off <= codeLen
	if offsets != nil {
		mapped, ok = offsets.Map(off)
	}
	if !ok {
		return 0, fmt.Errorf("arm64 identity finalizer: %s offset %d is outside %d-byte function", what, off, codeLen)
	}
	return mapped, nil
}

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

func (f *fn) finalizeNativeCode(internalOff int) (int, error) {
	if !nativeFinalizerEnabled {
		return internalOff, nil
	}
	if nativeFinalizerValidate {
		if err := f.validateFinalizerInventory(internalOff); err != nil {
			return 0, err
		}
	}
	result, err := shared.FinalizeIdentity(f.a.B, nil, nil, nil)
	if err != nil {
		return 0, fmt.Errorf("arm64 identity finalizer: %w", err)
	}
	f.a.B = result.Code

	if internalOff, err = mapFinalOffset(result.Offsets, internalOff, len(result.Code), "internal entry"); err != nil {
		return 0, err
	}
	for i := range f.relocs {
		mapped, err := mapFinalOffset(result.Offsets, f.relocs[i].at, len(result.Code), "call relocation")
		if err != nil {
			return 0, err
		}
		f.relocs[i].at = mapped
	}
	if f.adapterReturnOff != 0 {
		mapped, err := mapFinalOffset(result.Offsets, f.adapterReturnOff, len(result.Code), "adapter return")
		if err != nil {
			return 0, err
		}
		f.adapterReturnOff = mapped
	}
	if plan := f.gcFrameRoots; plan != nil {
		if plan.AdapterReturnOffset != 0 {
			mapped, err := mapFinalOffset(result.Offsets, int(plan.AdapterReturnOffset), len(result.Code), "GC adapter return")
			if err != nil {
				return 0, err
			}
			plan.AdapterReturnOffset = uint32(mapped)
		}
		for i := range plan.Callsites {
			mapped, err := mapFinalOffset(result.Offsets, int(plan.Callsites[i].ReturnOffset), len(result.Code), "GC call return")
			if err != nil {
				return 0, err
			}
			plan.Callsites[i].ReturnOffset = uint32(mapped)
		}
	}
	return internalOff, nil
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
	for encoded := range sc.branchTargets {
		if encoded < 0 {
			off := -encoded - 1
			if err := shared.ValidateRelaxSite(len(f.a.B), shared.RelaxSite{Off: uint32(off), Kind: shared.RelaxDeadHole, LongLen: 4}); err != nil {
				return fmt.Errorf("arm64 identity finalizer: %w", err)
			}
		}
	}

	_, err := shared.FinalizeIdentity(f.a.B, nil, labels, nil)
	if err != nil {
		return fmt.Errorf("arm64 identity finalizer: %w", err)
	}
	return nil
}

func mapFinalOffset(offsets shared.OffsetMap, off, codeLen int, what string) (int, error) {
	mapped, ok := offsets.Map(off)
	if !ok {
		return 0, fmt.Errorf("arm64 identity finalizer: %s offset %d is outside %d-byte function", what, off, codeLen)
	}
	return mapped, nil
}

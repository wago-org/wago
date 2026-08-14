//go:build amd64

package amd64

import (
	"fmt"
	"os"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

// WAGO_FINALIZE=0 retains the pre-finalizer path as a rollout oracle. The
// identity finalizer changes no bytes; it establishes one owner for every
// function-relative metadata offset before AMD64 relaxation can shrink code.
var nativeFinalizerEnabled = os.Getenv("WAGO_FINALIZE") != "0"

func (f *fn) finalizeNativeCode(internalOff int) (int, error) {
	if !nativeFinalizerEnabled {
		return internalOff, nil
	}
	result, err := shared.FinalizeIdentity(f.a.B, nil, nil, nil)
	if err != nil {
		return 0, fmt.Errorf("amd64 finalizer: %w", err)
	}
	f.a.B = result.Code

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
	return internalOff, nil
}

func mapAMD64FinalOffset(offsets *shared.OffsetMap, old, codeLen int, kind string) (int, error) {
	mapped, ok := offsets.Map(old)
	if !ok || mapped < 0 || mapped > codeLen {
		return 0, fmt.Errorf("amd64 finalizer: invalid %s offset %d", kind, old)
	}
	return mapped, nil
}

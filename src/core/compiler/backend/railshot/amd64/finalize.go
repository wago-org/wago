//go:build amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

// WAGO_FINALIZE=0 retains the pre-finalizer path as a rollout oracle. The
// identity finalizer changes no bytes; it establishes one owner for every
// function-relative metadata offset before AMD64 relaxation can shrink code.
var nativeFinalizerEnabled = os.Getenv("WAGO_FINALIZE") != "0"
var nativeCompactionEnabled = os.Getenv("WAGO_COMPACT") == "1"

const maxAMD64FinalizerRel32Sites = 256

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

func (f *fn) finalizeFrameAdjustments() (shared.FinalizeResult, int, int, error) {
	identity := func() (shared.FinalizeResult, int, int, error) {
		result, err := shared.FinalizeIdentity(f.a.B, nil, nil, nil)
		if err != nil {
			return shared.FinalizeResult{}, 0, 0, fmt.Errorf("amd64 finalizer: %w", err)
		}
		return result, 0, 0, nil
	}
	if !nativeCompactionEnabled || f.hasLoop || f.hasJumpTableData ||
		len(f.customInstructions) != 0 || f.a.Rel32Overflow {
		return identity()
	}
	frameSize := f.frameSize()
	if frameSize > 127 {
		return identity()
	}

	var storage [shared.MaxOffsetMapDeletions]shared.DeletedRange
	deletions := storage[:0]
	frameSites := len(f.sc.tailFrameSites) + 2
	holeSites := 0
	for _, over := range f.sc.brFoldSites {
		if over >= 0 && over+9 <= len(f.a.B) &&
			bytes.Equal(f.a.B[over+4:over+9], []byte{0x0f, 0x1f, 0x44, 0x00, 0x00}) {
			holeSites++
		}
	}
	if frameSites+holeSites > cap(deletions) {
		return identity()
	}
	holeDeleted := 0
	for _, over := range f.sc.brFoldSites {
		if over >= 0 && over+9 <= len(f.a.B) &&
			bytes.Equal(f.a.B[over+4:over+9], []byte{0x0f, 0x1f, 0x44, 0x00, 0x00}) {
			deletions = append(deletions, shared.DeletedRange{Off: uint32(over + 4), Len: 5})
			holeDeleted += 5
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
			return nil
		}
		// 48 81 /digit imm32 -> 48 83 /digit imm8. The low immediate byte is
		// already correct; delete only the trailing three bytes.
		f.a.B[site-2] = 0x83
		deletions = append(deletions, shared.DeletedRange{Off: uint32(site + 1), Len: 3})
		return nil
	}
	if err := addFrameSite(f.subRspAt, 0xec); err != nil {
		return shared.FinalizeResult{}, 0, 0, err
	}
	for _, site := range f.sc.tailFrameSites {
		if err := addFrameSite(site, 0xc4); err != nil {
			return shared.FinalizeResult{}, 0, 0, err
		}
	}
	if err := addFrameSite(f.addRspAt, 0xc4); err != nil {
		return shared.FinalizeResult{}, 0, 0, err
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
	offsets, err := shared.NewOffsetMap(len(f.a.B), deletions)
	if err != nil {
		return shared.FinalizeResult{}, 0, 0, fmt.Errorf("amd64 finalizer: %w", err)
	}
	oldLen := len(f.a.B)
	src, dst := 0, 0
	for _, deletion := range deletions {
		off := int(deletion.Off)
		copy(f.a.B[dst:], f.a.B[src:off])
		dst += off - src
		src = off + int(deletion.Len)
	}
	copy(f.a.B[dst:], f.a.B[src:])
	code := f.a.B[:offsets.FinalLen()]
	for _, site := range f.a.Rel32Sites {
		at, okAt := offsets.Map(site.At)
		target, okTarget := offsets.Map(site.Target)
		if !okAt || !okTarget || at < 0 || at+4 > len(code) || target < 0 || target > len(code) {
			return shared.FinalizeResult{}, 0, 0, fmt.Errorf("amd64 finalizer: invalid rel32 remap %d -> %d", site.At, site.Target)
		}
		binary.LittleEndian.PutUint32(code[at:], uint32(int32(target-(at+4))))
	}
	return shared.FinalizeResult{Code: code, Offsets: offsets}, oldLen - len(code) - holeDeleted, holeDeleted, nil
}

func mapAMD64FinalOffset(offsets *shared.OffsetMap, old, codeLen int, kind string) (int, error) {
	mapped, ok := offsets.Map(old)
	if !ok || mapped < 0 || mapped > codeLen {
		return 0, fmt.Errorf("amd64 finalizer: invalid %s offset %d", kind, old)
	}
	return mapped, nil
}

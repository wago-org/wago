//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"sort"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

func (c *Compiled) rv64ExecutionContract() *rv64ExecutionContract {
	if indexes := c.loadCompileIndexes(); indexes != nil {
		return indexes.railshotTail.executionContract
	}
	return nil
}

func (c *Compiled) setRV64ExecutionContract(contract *rv64ExecutionContract) {
	c.ensureCompileIndexes().railshotTail.executionContract = contract
}

func rv64ArtifactContractSource(c *Compiled) bool {
	return rv64ArtifactContractValid(c) || rv64FreshRootedGC(c) && rv64ArtifactFrameOwnership(c)
}

func rv64ArtifactRootedContract(c *Compiled) bool { return rv64FreshRootedGC(c) }

// Older hand-built codec fixtures describe metadata without declaring a
// native execution ABI. They can be inspected after decode; Instantiate still
// requires Compiled.validate and cannot execute their unqualified code.
func rv64StructuralCodecMetadata(c *Compiled) bool {
	return c != nil && !c.nativeContextTrapABI && !c.nativeReferenceCallABI &&
		!c.nativeReferenceControlABI && !c.nativeReferenceFunctionABI &&
		!c.nativeTableContextABI && !c.nativeTableReferenceABI && c.nativeTable64ABI == 0 &&
		c.nativeGCABIRequirement() == 0 && c.genericGCFrameRoots() == nil &&
		c.stagedGCStructProduct() == 0 && c.stagedGCArrayProduct() == 0 && c.stagedGCI31Product() == 0 &&
		c.rv64ExecutionContract() == nil && rv64NativeTailModule(c) == nil
}

func rv64ArtifactLinkingCall(code []byte, returnOffset uint32) bool {
	if returnOffset < 4 || returnOffset&3 != 0 || uint64(returnOffset) > uint64(len(code)) {
		return false
	}
	w := binary.LittleEndian.Uint32(code[returnOffset-4:])
	return w>>7&31 == 1 && (w&127 == 0x6f || w&127 == 0x67 && w>>12&7 == 0)
}

func rv64ArtifactFrameOwnership(c *Compiled) bool {
	roots := c.genericGCFrameRoots()
	if len(c.Entry) != len(c.Funcs) || len(c.Entry) != len(c.InternalEntry) ||
		len(c.code) > 1<<31-1 || roots != nil && len(c.Entry) > railshot.MaxFunctions {
		return false
	}
	if len(c.Entry) == 0 {
		if len(c.code) != 0 {
			return false
		}
		if roots == nil {
			return true
		}
		// Exception storage can need a collector without a native function.
		// Its complete typed/state schema is checked by execution admission;
		// there is no native PC or frame to describe here.
		return (c.hasNoFunctionCode() || c.NumImports > 0 && c.hasCollectorReferenceCallBoundary()) && c.nativeGCABIRequirement() == gc.NativeABIVersion &&
			(c.requiredFeatures.IsEnabled(CoreFeatureExceptionHandling) || c.requiredFeatures.IsEnabled(CoreFeatureGC) && (c.usesGenericGCExecution() || c.hasCollectorReferenceCallBoundary())) &&
			len(roots.safepoints) == 0 && len(roots.callsites) == 0 && len(roots.adapterReturnOffsets) == 0
	}
	if roots == nil {
		if c.needsExactNativeGCRoots() && !rv64FixedRootlessGCProduct(c) {
			return false
		}
		roots = &importOnlyGCFrameRoots
	}
	var frames []int
	if len(roots.safepoints) != 0 || len(roots.callsites) != 0 {
		frames = make([]int, len(c.Entry))
	}
	for i, entry := range c.Entry {
		end := len(c.code)
		if i+1 < len(c.Entry) {
			end = c.Entry[i+1]
		}
		internal := c.InternalEntry[i]
		adapter := internalEntryOffset(internal) != entry && rv64DirectIntegerAdapter(c, i)
		if !rv64DirectIntegerMarkerShape(internal, adapter) || internalEntryOffset(internal) != entry && !adapter || entry < 0 || entry >= end || end > len(c.code) || i > 0 && entry <= c.Entry[i-1] {
			return false
		}
		frame, err := railshot.NativeFrameBytes(c.code[:end], entry)
		if err != nil {
			return false
		}
		if frames != nil {
			frames[i] = frame
		}
		mainEnd, err := railshot.NativeFrameBodyEnd(c.code[:end], entry)
		if err != nil {
			return false
		}
		if c.genericGCFrameRoots() != nil && len(roots.safepoints) == 0 && len(roots.callsites) == 0 {
			// With no retained map, no local body may make a collecting call.
			// Wrapper guard veneers use x0, so only actual linking calls count.
			for at := entry + 4; at <= mainEnd; at += 4 {
				if rv64ArtifactLinkingCall(c.code, uint32(at)) && !railshot.NativeNonCollectingGCCall(c.code, uint32(at), c.syncHostSlots) {
					return false
				}
			}
		}
	}
	for _, point := range roots.safepoints {
		found := false
		for _, frame := range frames {
			if uint32(frame) == point.frameBytes {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	for _, call := range roots.callsites {
		if !rv64ArtifactLinkingCall(c.code, call.returnOffset) {
			return false
		}
		owner := sort.Search(len(c.Entry), func(i int) bool { return c.Entry[i] >= int(call.returnOffset) }) - 1
		if owner < 0 || uint32(frames[owner]) != call.frameBytes {
			return false
		}
	}
	for _, offset := range roots.adapterReturnOffsets {
		if !rv64ArtifactLinkingCall(c.code, offset) {
			return false
		}
	}
	return true
}

// Fixed GC products predate generic safepoint maps. Their product validators
// authenticate the complete code and metadata shape, including the helper ABI.
// A decoded artifact uses generic product markers, so frame ownership must use
// the same exact validators that selected its persisted execution flags.
func rv64FixedRootlessGCProduct(c *Compiled) bool {
	return c != nil && c.genericGCFrameRoots() == nil &&
		(rv64OfficialGCRefEq(c) || rv64GCRefTestTable(c) || rv64GCRefTestConcrete(c) ||
			rv64GCRefTestAbstract(c) || rv64GCExtern(c) || rv64GCRefCastAbstract(c) ||
			rv64GCRefCastConcrete(c) || rv64GCBrOnCastAbstract(c) || rv64GCBrOnCastFailAbstract(c) ||
			rv64GCBrOnCastConcrete(c) || rv64GCBrOnCastFailConcrete(c) || rv64GCBrOnCastNullability(c) ||
			rv64GCBrOnCastFailNullability(c) || rv64GCStructGet(c) || rv64GCStructMutation(c) ||
			rv64GCStructNull(c) || rv64GCStructPacked(c) || rv64GCStructBasic(c) ||
			rv64GCArrayLocal(c) || rv64GCArrayUniform(c) || rv64GCArrayFixedLocal(c) ||
			rv64GCArrayFixedOfficial(c) || rv64GCArrayDefaultOfficial(c) || rv64GCArrayPackedData(c) ||
			rv64GCArrayReference(c) || rv64GCArrayInitElem(c) || rv64GCArrayInitData(c) ||
			rv64GCArrayFill(c) || rv64GCArrayCopy(c))
}

// Each register entry must target its own canonical framed wrapper. Marker
// flags alone cannot grant a foreign entry or bypass frame ownership checks.
func rv64DirectIntegerAdapter(c *Compiled, i int) bool {
	if c == nil || i < 0 || i >= len(c.Entry) || i >= len(c.InternalEntry) || i >= len(c.Funcs) {
		return false
	}
	sig := c.Funcs[i]
	if len(sig.Params) > 4 || len(sig.Results) > 1 {
		return false
	}
	for _, ts := range [][]ValType{sig.Params, sig.Results} {
		for _, t := range ts {
			if t != ValI32 && t != ValI64 {
				return false
			}
		}
	}
	end := len(c.code)
	if i+1 < len(c.Entry) {
		end = c.Entry[i+1]
	}
	if end < 0 || end > len(c.code) {
		return false
	}
	return railshot.NativeDirectIntegerEntry(c.code[:end], internalEntryOffset(c.InternalEntry[i]), c.Entry[i], len(sig.Params), len(sig.Results))
}

func rv64DirectIntegerMarkerShape(entry int, adapter bool) bool {
	// These shared flags authorize host ABIs that RV64 does not implement.
	// The canonical codec removes them, so reject them before digest admission.
	foreignHost := directHostSegmentsMask | nativeScalarLeafMask | nativeScalarBoundedMask | goHostDispatchTagMask
	direct, bounded := directPreparedEntry(entry), directPreparedBoundedEntry(entry)
	return uint(entry)&foreignHost == 0 && !directPreparedLightEntry(entry) && (!direct && !bounded || adapter && direct)
}

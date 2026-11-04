//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
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

// Older hand-built codec fixtures describe metadata without declaring a
// native execution ABI. They can be inspected after decode; Instantiate still
// requires Compiled.validate and cannot execute their unqualified code.
func rv64StructuralMetadata(c *Compiled) bool {
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

func rv64SourceFrameOwnership(c *Compiled) bool {
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

func rv64PopulateSourceReceipt(memo *validateMemo, c *Compiled) bool {
	var ok bool
	if memo.rv64FreshRootedGCTypeDigest, ok = rv64RootedTypeDigest(c); !ok {
		return false
	}
	if memo.rv64FreshRootedGCImportDigest, ok = rv64RootedImportDigest(c); !ok {
		return false
	}
	if memo.rv64FreshRootedGCDataDigest, ok = rv64RootedActiveDataDigest(c); !ok {
		return false
	}
	if memo.rv64FreshRootedGCExportDigest, ok = rv64RootedMemoryExportDigest(c); !ok {
		return false
	}
	if memo.rv64FreshRootedGCGlobalDigest, ok = rv64RootedGlobalDigest(c); !ok {
		return false
	}
	if memo.rv64FreshRootedGCTableDigest, ok = rv64RootedTableDigest(c); !ok {
		return false
	}
	memo.rv64FreshRootedGC = true
	memo.rv64FreshRootedGCDigest = sha256.Sum256(c.code)
	memo.rv64FreshRootedGCSyncHostSlots = c.syncHostSlots
	memo.rv64FreshRootedGCReserveMax = c.MemMaxPages
	memo.rv64FreshRootedGCHasStart = c.HasStart
	if c.HasStart {
		memo.rv64FreshRootedGCStartImport = c.StartIsImport
		if c.StartIsImport {
			memo.rv64FreshRootedGCStartImportIndex = uint32(c.StartImportIdx)
		} else {
			memo.rv64FreshRootedGCStartLocal = uint32(c.StartLocalFunc)
		}
	}
	if directory := c.memoryDir; directory != nil {
		memo.rv64FreshRootedGCMemories = uint8(len(directory.defs))
		memo.rv64FreshRootedGCMemoryStaged = directory.staged
		if len(directory.defs) != 0 {
			memo.rv64FreshRootedGCMemMin, memo.rv64FreshRootedGCMemMax = directory.defs[0].Min, directory.defs[0].Max
		}
	}
	return true
}

// rv64CanonicalMetadata preserves the representation used by source-admission
// fingerprints. These bytes are internal consistency checks, not a module format.
func rv64CanonicalMetadata(c *Compiled) ([]byte, error) {
	metadata, err := compiledMetadataBytes(c, true)
	memo := c.loadValidateMemo()
	if err != nil || memo == nil || c.rv64ExecutionContract() == nil && !memo.rv64FreshRootedGC {
		return metadata, err
	}
	if !rv64SourceContractValid(c) && !(rv64FreshRootedGC(c) && rv64SourceFrameOwnership(c)) {
		return nil, fmt.Errorf("RV64 execution contract lacks admitted source metadata")
	}
	capacities := c.rv64ExecutionTableCapacities()
	if len(capacities) > maxRV64ExecutionTables || len(capacities) != 0 && len(capacities) != c.tableCount() {
		return nil, fmt.Errorf("invalid RV64 execution table capacity count")
	}
	storageFree := rv64StorageFreeProof(c)
	digest := rv64ExecutionDigestProof(c.code, metadata, c.syncHostSlots, storageFree, capacities...)
	metadata = append(metadata, 'R', 'V', '6', '4', 'E', 'X', 'E', 'C')
	version := byte(1)
	if len(capacities) != 0 {
		version = 2
	}
	if storageFree {
		version += 2
	}
	metadata = append(metadata, version)
	if storageFree {
		metadata = append(metadata, 1)
	}
	metadata = append(metadata, byte(c.syncHostSlots), byte(c.syncHostSlots>>8))
	if len(capacities) != 0 {
		metadata = binary.LittleEndian.AppendUint32(metadata, uint32(len(capacities)))
		for _, capacity := range capacities {
			metadata = binary.LittleEndian.AppendUint32(metadata, capacity)
		}
	}
	return append(metadata, digest[:]...), nil
}

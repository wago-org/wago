//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"
)

const rv64GCTypeSubtypingRuntimeCallCastCodeLen = 19628

var rv64GCTypeSubtypingRuntimeCallCastCodeDigest = [sha256.Size]byte{0xab, 0x8a, 0x6c, 0xc4, 0xde, 0xb2, 0x04, 0x2d, 0x6a, 0xeb, 0x26, 0x2b, 0xc8, 0x29, 0x01, 0xb6, 0x4b, 0x61, 0xb2, 0xd6, 0x7b, 0x41, 0x75, 0xf5, 0xd1, 0x00, 0x52, 0x04, 0xdf, 0xe7, 0x48, 0x18}

const rv64GCTypeSubtypingRuntimeCallCastMetadataLen = 501

var rv64GCTypeSubtypingRuntimeCallCastMetadataDigest = [sha256.Size]byte{0xec, 0xaf, 0xfa, 0x00, 0xf3, 0x51, 0xec, 0x2a, 0x2f, 0x49, 0x60, 0x24, 0xc8, 0xdd, 0x06, 0xac, 0x02, 0xff, 0x70, 0xb0, 0xeb, 0x85, 0x01, 0x0e, 0x0a, 0xb0, 0xee, 0x3d, 0x32, 0x1f, 0x3e, 0xeb}

func rv64GCTypeSubtypingRuntimeCallCastProfileWithCode(c *Compiled, allowUnrestored, requireCode bool) bool {
	if c == nil || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled ||
		c.nativeTable64ABI != nativeTable64NoABI || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata || !c.NeedsFuncRefDescs || !c.dynamicFuncrefEscape ||
		!c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) || c.usesGCStructHelpers() || c.usesGCArrayHelpers() ||
		c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 10 || len(c.FuncTypeID) != 10 ||
		len(c.Exports) != 7 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 || c.tableCount() != 1 || len(c.extraTables) != 0 ||
		len(c.tableExports) != 0 || c.memoryCount() != 0 || c.tagImportCount() != 0 || c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 ||
		c.Names != nil || len(c.customInstructions) != 0 || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 1 || len(c.passiveElems) != 0 ||
		!c.HasTable || c.HasMemory || c.HasTableInitFunc || c.TableInitFunc != 0 || c.tableImport != "" || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 ||
		c.stagedGCStructProduct() != 0 || c.stagedGCArrayProduct() != 0 || c.genericGCFrameRoots() != nil || c.nativeGCABIRequirement() != 0 || c.requiredAMD64Features != 0 ||
		c.requiredFeatures != CoreFeatureReferenceTypes|CoreFeatureTypedFunctionReferences|CoreFeatureGC {
		return false
	}
	product := c.stagedGCTypeSubtypingProduct()
	if product != stagedGCTypeSubtypingRuntimeCallCast && (!allowUnrestored || product != 0) {
		return false
	}
	staged := c.stagedFeatures()
	if staged != CoreFeatureTypedFunctionReferences|CoreFeatureGC && (!allowUnrestored || staged != 0) {
		return false
	}
	wantEntries := []int{0, 608, 1216, 1824, 11792, 13216, 14528, 15840, 17136, 18384}
	if !slices.Equal(c.Entry, wantEntries) || !slices.Equal(c.InternalEntry, wantEntries) ||
		requireCode && (len(c.code) != rv64GCTypeSubtypingRuntimeCallCastCodeLen || sha256.Sum256(c.code) != rv64GCTypeSubtypingRuntimeCallCastCodeDigest) {
		return false
	}
	metadata, err := marshalCompiledMetadata(c)
	return err == nil && len(metadata) == rv64GCTypeSubtypingRuntimeCallCastMetadataLen && sha256.Sum256(metadata) == rv64GCTypeSubtypingRuntimeCallCastMetadataDigest
}

func rv64GCTypeSubtypingRuntimeCallCast(c *Compiled) bool {
	return rv64GCTypeSubtypingRuntimeCallCastProfileWithCode(c, false, true)
}

func rv64GCTypeSubtypingRuntimeCallCastExecutionCode(c *Compiled) bool {
	return c != nil && len(c.code) == rv64GCTypeSubtypingRuntimeCallCastCodeLen && sha256.Sum256(c.code) == rv64GCTypeSubtypingRuntimeCallCastCodeDigest
}

func railshotRestoreGCTypeSubtypingRuntimeCallCastArtifact(c *Compiled) {
	if !rv64GCTypeSubtypingRuntimeCallCastProfileWithCode(c, true, true) {
		return
	}
	c.ensureCodeCache()
	c.codeCache.stagedFeatures |= CoreFeatureTypedFunctionReferences | CoreFeatureGC
	c.codeCache.gcTypeSubtypingProduct = stagedGCTypeSubtypingRuntimeCallCast
}

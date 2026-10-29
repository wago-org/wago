//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"
)

const rv64GCTypeSubtypingRuntimeFinalityCallCastCodeLen = 6228

var rv64GCTypeSubtypingRuntimeFinalityCallCastCodeDigest = [sha256.Size]byte{0x6d, 0x81, 0xe1, 0x50, 0x2a, 0xed, 0xdf, 0x1b, 0xa8, 0xe8, 0xf7, 0x55, 0x44, 0x65, 0x36, 0xcf, 0x6e, 0x1b, 0x46, 0xf4, 0xb2, 0x5d, 0x42, 0x4a, 0xd6, 0xe8, 0x9f, 0x5f, 0x39, 0x66, 0x38, 0x6d}

const rv64GCTypeSubtypingRuntimeFinalityCallCastMetadataLen = 292

var rv64GCTypeSubtypingRuntimeFinalityCallCastMetadataDigest = [sha256.Size]byte{0x5d, 0x4b, 0x4c, 0x32, 0x9e, 0x15, 0x5a, 0xaf, 0x1a, 0xd7, 0xda, 0xbb, 0x58, 0x35, 0xac, 0xc1, 0x8a, 0x30, 0xaa, 0x18, 0x15, 0x82, 0x7c, 0xd2, 0xbc, 0xa3, 0xdb, 0x40, 0x81, 0x8e, 0x75, 0x66}

func rv64GCTypeSubtypingRuntimeFinalityCallCastProfileWithCode(c *Compiled, allowUnrestored, requireCode bool) bool {
	if c == nil || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled ||
		c.nativeTable64ABI != nativeTable64NoABI || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata || !c.NeedsFuncRefDescs || c.dynamicFuncrefEscape ||
		!c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) || c.usesGCStructHelpers() || c.usesGCArrayHelpers() ||
		c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 6 || len(c.FuncTypeID) != 6 ||
		len(c.Exports) != 4 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 || c.tableCount() != 1 || len(c.extraTables) != 0 ||
		len(c.tableExports) != 0 || c.memoryCount() != 0 || c.tagImportCount() != 0 || c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 ||
		c.Names != nil || len(c.customInstructions) != 0 || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 1 || len(c.passiveElems) != 0 ||
		!c.HasTable || c.HasMemory || c.HasTableInitFunc || c.TableInitFunc != 0 || c.tableImport != "" || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 ||
		c.stagedGCStructProduct() != 0 || c.stagedGCArrayProduct() != 0 || c.genericGCFrameRoots() != nil || c.nativeGCABIRequirement() != 0 || c.requiredAMD64Features != 0 ||
		c.requiredFeatures != CoreFeatureReferenceTypes|CoreFeatureGC {
		return false
	}
	product := c.stagedGCTypeSubtypingProduct()
	if product != stagedGCTypeSubtypingRuntimeFinalityCallCast && (!allowUnrestored || product != 0) {
		return false
	}
	staged := c.stagedFeatures()
	if staged != CoreFeatureTypedFunctionReferences|CoreFeatureGC && (!allowUnrestored || staged != 0) {
		return false
	}
	wantEntries := []int{0, 576, 1152, 2448, 3744, 4992}
	if !slices.Equal(c.Entry, wantEntries) || !slices.Equal(c.InternalEntry, wantEntries) ||
		requireCode && (len(c.code) != rv64GCTypeSubtypingRuntimeFinalityCallCastCodeLen || sha256.Sum256(c.code) != rv64GCTypeSubtypingRuntimeFinalityCallCastCodeDigest) {
		return false
	}
	metadata, err := rv64CanonicalMetadata(c)
	return err == nil && len(metadata) == rv64GCTypeSubtypingRuntimeFinalityCallCastMetadataLen && sha256.Sum256(metadata) == rv64GCTypeSubtypingRuntimeFinalityCallCastMetadataDigest
}

func rv64GCTypeSubtypingRuntimeFinalityCallCast(c *Compiled) bool {
	return rv64GCTypeSubtypingRuntimeFinalityCallCastProfileWithCode(c, false, true)
}

func rv64GCTypeSubtypingRuntimeFinalityCallCastExecutionCode(c *Compiled) bool {
	return c != nil && len(c.code) == rv64GCTypeSubtypingRuntimeFinalityCallCastCodeLen && sha256.Sum256(c.code) == rv64GCTypeSubtypingRuntimeFinalityCallCastCodeDigest
}

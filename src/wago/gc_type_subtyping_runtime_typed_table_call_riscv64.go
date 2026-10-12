//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"
)

const rv64GCTypeSubtypingRuntimeTypedTableCallCodeLen = 8444

var rv64GCTypeSubtypingRuntimeTypedTableCallCodeDigest = [sha256.Size]byte{0x37, 0x59, 0x93, 0x51, 0xb7, 0x27, 0xf8, 0x32, 0xc8, 0xf2, 0x87, 0x7d, 0x04, 0x2e, 0xef, 0x58, 0xf9, 0xfe, 0x47, 0x7d, 0x4f, 0xb3, 0x39, 0x7b, 0xce, 0xce, 0x78, 0x92, 0x90, 0xa2, 0xbc, 0x3e}

const rv64GCTypeSubtypingRuntimeTypedTableCallMetadataLen = 353

var rv64GCTypeSubtypingRuntimeTypedTableCallMetadataDigest = [sha256.Size]byte{0xb8, 0xaf, 0xaf, 0xcb, 0x97, 0xcf, 0x11, 0xfc, 0x4c, 0xbc, 0x9d, 0x6e, 0xec, 0x9f, 0x99, 0xb6, 0xe5, 0xb5, 0x23, 0xc0, 0x50, 0x23, 0xa4, 0xa5, 0x21, 0x54, 0xb5, 0x27, 0x05, 0xf2, 0xc6, 0x09}

func rv64GCTypeSubtypingRuntimeTypedTableCallProfileWithCode(c *Compiled, allowUnrestored, requireCode bool) bool {
	if c == nil || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled ||
		c.nativeTable64ABI != nativeTable64NoABI || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata || !c.NeedsFuncRefDescs || c.dynamicFuncrefEscape ||
		!c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) || c.usesGCStructHelpers() || c.usesGCArrayHelpers() ||
		c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 5 || len(c.FuncTypeID) != 5 ||
		len(c.Exports) != 3 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 || c.tableCount() != 1 || len(c.extraTables) != 0 ||
		len(c.tableExports) != 0 || c.memoryCount() != 0 || c.tagImportCount() != 0 || c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 ||
		c.Names != nil || len(c.customInstructions) != 0 || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 1 || len(c.passiveElems) != 0 ||
		!c.HasTable || c.HasMemory || c.HasTableInitFunc || c.TableInitFunc != 0 || c.tableImport != "" || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 ||
		c.stagedGCStructProduct() != 0 || c.stagedGCArrayProduct() != 0 || c.genericGCFrameRoots() != nil || c.nativeGCABIRequirement() != 0 || c.requiredAMD64Features != 0 ||
		c.requiredFeatures != CoreFeatureReferenceTypes|CoreFeatureTypedFunctionReferences|CoreFeatureGC {
		return false
	}
	product := c.stagedGCTypeSubtypingProduct()
	if product != stagedGCTypeSubtypingRuntimeTypedTableCall && (!allowUnrestored || product != 0) {
		return false
	}
	staged := c.stagedFeatures()
	if staged != CoreFeatureTypedFunctionReferences|CoreFeatureGC && (!allowUnrestored || staged != 0) {
		return false
	}
	wantEntries := []int{0, 576, 1152, 5856, 7152}
	if !slices.Equal(c.Entry, wantEntries) || !slices.Equal(c.InternalEntry, wantEntries) ||
		requireCode && (len(c.code) != rv64GCTypeSubtypingRuntimeTypedTableCallCodeLen || sha256.Sum256(c.code) != rv64GCTypeSubtypingRuntimeTypedTableCallCodeDigest) {
		return false
	}
	metadata, err := marshalCompiledMetadata(c)
	return err == nil && len(metadata) == rv64GCTypeSubtypingRuntimeTypedTableCallMetadataLen && sha256.Sum256(metadata) == rv64GCTypeSubtypingRuntimeTypedTableCallMetadataDigest
}

func rv64GCTypeSubtypingRuntimeTypedTableCall(c *Compiled) bool {
	return rv64GCTypeSubtypingRuntimeTypedTableCallProfileWithCode(c, false, true)
}

func rv64GCTypeSubtypingRuntimeTypedTableCallExecutionCode(c *Compiled) bool {
	return c != nil && len(c.code) == rv64GCTypeSubtypingRuntimeTypedTableCallCodeLen && sha256.Sum256(c.code) == rv64GCTypeSubtypingRuntimeTypedTableCallCodeDigest
}

func railshotRestoreGCTypeSubtypingRuntimeTypedTableCallArtifact(c *Compiled) {
	if !rv64GCTypeSubtypingRuntimeTypedTableCallProfileWithCode(c, true, true) {
		return
	}
	c.ensureCodeCache()
	c.codeCache.stagedFeatures |= CoreFeatureTypedFunctionReferences | CoreFeatureGC
	c.codeCache.gcTypeSubtypingProduct = stagedGCTypeSubtypingRuntimeTypedTableCall
}

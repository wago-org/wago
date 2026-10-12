//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"
)

const rv64GCTypeSubtypingRefTestSingleCodeLen = 1660

var rv64GCTypeSubtypingRefTestSingleCodeDigest = [sha256.Size]byte{0x14, 0x74, 0x44, 0xa8, 0x87, 0x0e, 0xaa, 0xe7, 0x7f, 0x6d, 0x79, 0xf4, 0x83, 0x53, 0x0e, 0x51, 0x0a, 0x73, 0x07, 0x38, 0xa0, 0xf7, 0xc2, 0xab, 0xc8, 0xff, 0x94, 0x8d, 0xb6, 0x0e, 0x04, 0x71}

const rv64GCTypeSubtypingRefTestSingleMetadataLen = 458

var rv64GCTypeSubtypingRefTestSingleMetadataDigest = [sha256.Size]byte{0xd6, 0xe4, 0x78, 0x1c, 0x95, 0x99, 0x8f, 0x79, 0x18, 0xe2, 0x31, 0x03, 0x78, 0x6c, 0xe6, 0xb9, 0xbf, 0x3e, 0xd1, 0x02, 0xab, 0x79, 0x91, 0xd4, 0x32, 0x0f, 0x65, 0x2c, 0x36, 0xdb, 0xff, 0x1a}

func rv64GCTypeSubtypingRefTestSingleProfileWithCode(c *Compiled, allowUnrestored, requireCode bool) bool {
	if c == nil || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled ||
		c.nativeTable64ABI != nativeTable64NoABI || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata || !c.NeedsFuncRefDescs || c.dynamicFuncrefEscape ||
		!c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) || c.usesGCStructHelpers() || c.usesGCArrayHelpers() ||
		c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 2 || len(c.FuncTypeID) != 2 ||
		len(c.Exports) != 1 || c.Exports["run"] != 1 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 || c.tableCount() != 0 || len(c.extraTables) != 0 ||
		len(c.tableExports) != 0 || c.memoryCount() != 0 || c.tagImportCount() != 0 || c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 ||
		c.Names != nil || len(c.customInstructions) != 0 || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 ||
		c.HasTable || c.HasMemory || c.HasTableInitFunc || c.TableInitFunc != 0 || c.tableImport != "" || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 ||
		c.stagedGCStructProduct() != 0 || c.stagedGCArrayProduct() != 0 || c.genericGCFrameRoots() != nil || c.nativeGCABIRequirement() != 0 || c.requiredAMD64Features != 0 ||
		c.requiredFeatures != CoreFeatureBulkMemoryOperations|CoreFeatureReferenceTypes|CoreFeatureTypedFunctionReferences|CoreFeatureGC {
		return false
	}
	product := c.stagedGCTypeSubtypingProduct()
	if product != stagedGCTypeSubtypingRefTestSingle && (!allowUnrestored || product != 0) {
		return false
	}
	staged := c.stagedFeatures()
	if staged != CoreFeatureTypedFunctionReferences|CoreFeatureGC && (!allowUnrestored || staged != 0) {
		return false
	}
	wantEntries := []int{0, 576}
	if !slices.Equal(c.Entry, wantEntries) || !slices.Equal(c.InternalEntry, wantEntries) ||
		requireCode && (len(c.code) != rv64GCTypeSubtypingRefTestSingleCodeLen || sha256.Sum256(c.code) != rv64GCTypeSubtypingRefTestSingleCodeDigest) {
		return false
	}
	d := c.memoryDir
	if d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 || len(d.gcStructGlobals) != 0 ||
		len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
		return false
	}
	metadata, err := marshalCompiledMetadata(c)
	return err == nil && len(metadata) == rv64GCTypeSubtypingRefTestSingleMetadataLen && sha256.Sum256(metadata) == rv64GCTypeSubtypingRefTestSingleMetadataDigest
}

func rv64GCTypeSubtypingRefTestSingle(c *Compiled) bool {
	return rv64GCTypeSubtypingRefTestSingleProfileWithCode(c, false, true)
}

func rv64GCTypeSubtypingRefTestSingleExecutionCode(c *Compiled) bool {
	return c != nil && len(c.code) == rv64GCTypeSubtypingRefTestSingleCodeLen && sha256.Sum256(c.code) == rv64GCTypeSubtypingRefTestSingleCodeDigest
}

func railshotRestoreGCTypeSubtypingRefTestSingleArtifact(c *Compiled) {
	if !rv64GCTypeSubtypingRefTestSingleProfileWithCode(c, true, true) {
		return
	}
	c.ensureCodeCache()
	c.codeCache.stagedFeatures |= CoreFeatureTypedFunctionReferences | CoreFeatureGC
	c.codeCache.gcTypeSubtypingProduct = stagedGCTypeSubtypingRefTestSingle
}

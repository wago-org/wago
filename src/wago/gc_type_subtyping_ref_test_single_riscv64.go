//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"
)

var rv64GCTypeSubtypingRefTestSingleCodeDigests = [...][sha256.Size]byte{
	{0x14, 0x74, 0x44, 0xa8, 0x87, 0x0e, 0xaa, 0xe7, 0x7f, 0x6d, 0x79, 0xf4, 0x83, 0x53, 0x0e, 0x51, 0x0a, 0x73, 0x07, 0x38, 0xa0, 0xf7, 0xc2, 0xab, 0xc8, 0xff, 0x94, 0x8d, 0xb6, 0x0e, 0x04, 0x71},
	{0x8d, 0x3a, 0xcf, 0x80, 0xff, 0x9b, 0xb6, 0xff, 0xb4, 0x5e, 0x93, 0x87, 0x84, 0x0a, 0x91, 0x41, 0x41, 0xe2, 0xbf, 0x98, 0x7d, 0x00, 0x75, 0x6a, 0x7d, 0xc4, 0x29, 0x45, 0xe4, 0xfc, 0x8c, 0x8b},
	{0xc1, 0xf5, 0xcd, 0x7c, 0x93, 0xb8, 0x82, 0x7b, 0xd1, 0x7c, 0xa8, 0x62, 0xa8, 0x6c, 0x0f, 0x72, 0x11, 0xd5, 0x52, 0x7b, 0x23, 0xcf, 0x89, 0xb5, 0x1f, 0xdf, 0xa3, 0x23, 0x90, 0xa2, 0x8f, 0x7d},
	// .22 and .23 deliberately share lowering: each compares against type 4.
	// Their retained graphs decide whether the ref.func carrier is type 6 or 4.
	{0xc1, 0xf5, 0xcd, 0x7c, 0x93, 0xb8, 0x82, 0x7b, 0xd1, 0x7c, 0xa8, 0x62, 0xa8, 0x6c, 0x0f, 0x72, 0x11, 0xd5, 0x52, 0x7b, 0x23, 0xcf, 0x89, 0xb5, 0x1f, 0xdf, 0xa3, 0x23, 0x90, 0xa2, 0x8f, 0x7d},
	{0xf1, 0xf0, 0xec, 0x44, 0x47, 0xae, 0x54, 0x72, 0x27, 0x12, 0x01, 0xca, 0xba, 0x8d, 0xba, 0x53, 0xe3, 0x5d, 0x4a, 0x95, 0xdb, 0x91, 0x2e, 0x73, 0xf1, 0xd4, 0xfa, 0x05, 0xbb, 0x78, 0xd8, 0x1d},
}

var rv64GCTypeSubtypingRefTestSingleCodeLens = [...]int{1660, 1660, 1660, 1660, 2152}

var rv64GCTypeSubtypingRefTestSingleMetadataLens = [...]int{458, 616, 458, 379, 696}

var rv64GCTypeSubtypingRefTestSingleMetadataDigests = [...][sha256.Size]byte{
	{0xd6, 0xe4, 0x78, 0x1c, 0x95, 0x99, 0x8f, 0x79, 0x18, 0xe2, 0x31, 0x03, 0x78, 0x6c, 0xe6, 0xb9, 0xbf, 0x3e, 0xd1, 0x02, 0xab, 0x79, 0x91, 0xd4, 0x32, 0x0f, 0x65, 0x2c, 0x36, 0xdb, 0xff, 0x1a},
	{0xa0, 0xea, 0x0b, 0xfc, 0x11, 0x78, 0x4e, 0xc5, 0xa9, 0xf6, 0x10, 0x83, 0x5a, 0xe0, 0x32, 0x48, 0xa1, 0x68, 0x66, 0xbb, 0x10, 0x31, 0xdd, 0x18, 0xa4, 0x3b, 0xdf, 0xd1, 0xdc, 0x02, 0x98, 0x01},
	{0x2a, 0xef, 0xc0, 0x10, 0x45, 0x99, 0xa2, 0xe7, 0xb2, 0xf9, 0x08, 0xe6, 0xc4, 0x08, 0x6b, 0x3e, 0xe5, 0x4c, 0xe5, 0x56, 0x79, 0x58, 0xbd, 0x46, 0xed, 0x8e, 0xfa, 0xea, 0x56, 0x7f, 0xe1, 0x69},
	{0x64, 0x10, 0xbc, 0xe0, 0xda, 0x1c, 0x0e, 0x90, 0x2d, 0x0b, 0xec, 0xf9, 0xae, 0xaf, 0x23, 0x16, 0x8e, 0x02, 0x34, 0x15, 0x5a, 0x5d, 0x1e, 0x3e, 0x38, 0xb2, 0x15, 0x16, 0x9d, 0x93, 0x80, 0x32},
	{0x5f, 0xf5, 0xcf, 0x74, 0x1e, 0x20, 0x10, 0x08, 0x86, 0x3e, 0x22, 0xee, 0xe2, 0xf9, 0x04, 0x84, 0x3e, 0xab, 0xe9, 0x28, 0x0d, 0x6b, 0xab, 0x19, 0xbe, 0x01, 0xc6, 0x0b, 0x44, 0x9a, 0x75, 0x94},
}

func rv64GCTypeSubtypingRefTestSingleProfileVariantWithCode(c *Compiled, allowUnrestored, requireCode bool) uint8 {
	baseFeatures := CoreFeatureBulkMemoryOperations | CoreFeatureReferenceTypes | CoreFeatureTypedFunctionReferences | CoreFeatureGC
	if c == nil || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled ||
		c.nativeTable64ABI != nativeTable64NoABI || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata || !c.NeedsFuncRefDescs || c.dynamicFuncrefEscape ||
		!c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) || c.usesGCStructHelpers() || c.usesGCArrayHelpers() ||
		c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 2 || len(c.FuncTypeID) != 2 ||
		len(c.Exports) != 1 || c.Exports["run"] != 1 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 || c.tableCount() != 0 || len(c.extraTables) != 0 ||
		len(c.tableExports) != 0 || c.memoryCount() != 0 || c.tagImportCount() != 0 || c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 ||
		c.Names != nil || len(c.customInstructions) != 0 || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 ||
		c.HasTable || c.HasMemory || c.HasTableInitFunc || c.TableInitFunc != 0 || c.tableImport != "" || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 ||
		c.stagedGCStructProduct() != 0 || c.stagedGCArrayProduct() != 0 || c.genericGCFrameRoots() != nil || c.nativeGCABIRequirement() != 0 || c.requiredAMD64Features != 0 ||
		c.requiredFeatures != baseFeatures && c.requiredFeatures != baseFeatures|CoreFeatureMultiValue {
		return 0
	}
	product := c.stagedGCTypeSubtypingProduct()
	if product != stagedGCTypeSubtypingRefTestSingle && product != stagedGCTypeSubtypingRefTestMulti && (!allowUnrestored || product != 0) {
		return 0
	}
	staged := c.stagedFeatures()
	if staged != CoreFeatureTypedFunctionReferences|CoreFeatureGC && (!allowUnrestored || staged != 0) {
		return 0
	}
	wantEntries := []int{0, 576}
	if !slices.Equal(c.Entry, wantEntries) || !slices.Equal(c.InternalEntry, wantEntries) {
		return 0
	}
	d := c.memoryDir
	if d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 || len(d.gcStructGlobals) != 0 ||
		len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
		return 0
	}
	metadata, err := marshalCompiledMetadata(c)
	if err != nil {
		return 0
	}
	metadataDigest := sha256.Sum256(metadata)
	codeDigest := sha256.Sum256(c.code)
	for i := range rv64GCTypeSubtypingRefTestSingleMetadataDigests {
		if len(metadata) == rv64GCTypeSubtypingRefTestSingleMetadataLens[i] && metadataDigest == rv64GCTypeSubtypingRefTestSingleMetadataDigests[i] &&
			(!requireCode || len(c.code) == rv64GCTypeSubtypingRefTestSingleCodeLens[i] && codeDigest == rv64GCTypeSubtypingRefTestSingleCodeDigests[i]) {
			variant := uint8(i + 1)
			wantProduct := stagedGCTypeSubtypingRefTestSingle
			if variant >= 5 {
				wantProduct = stagedGCTypeSubtypingRefTestMulti
			}
			wantFeatures := baseFeatures
			if variant >= 5 {
				wantFeatures |= CoreFeatureMultiValue
			}
			if c.requiredFeatures == wantFeatures && (product == wantProduct || allowUnrestored && product == 0) {
				return variant
			}
		}
	}
	return 0
}

func rv64GCTypeSubtypingRefTestSingleProfileWithCode(c *Compiled, allowUnrestored, requireCode bool) bool {
	return rv64GCTypeSubtypingRefTestSingleProfileVariantWithCode(c, allowUnrestored, requireCode) != 0
}

func rv64GCTypeSubtypingRefTestSingle(c *Compiled) bool {
	return rv64GCTypeSubtypingRefTestSingleProfileWithCode(c, false, true)
}

func rv64GCTypeSubtypingRefTestSingleExecutionCode(c *Compiled) bool {
	if c == nil {
		return false
	}
	digest := sha256.Sum256(c.code)
	for i := range rv64GCTypeSubtypingRefTestSingleCodeDigests {
		if len(c.code) == rv64GCTypeSubtypingRefTestSingleCodeLens[i] && digest == rv64GCTypeSubtypingRefTestSingleCodeDigests[i] {
			return true
		}
	}
	return false
}

func railshotRestoreGCTypeSubtypingRefTestSingleArtifact(c *Compiled) {
	variant := rv64GCTypeSubtypingRefTestSingleProfileVariantWithCode(c, true, true)
	if variant == 0 {
		return
	}
	c.ensureCodeCache()
	c.codeCache.stagedFeatures |= CoreFeatureTypedFunctionReferences | CoreFeatureGC
	if variant >= 5 {
		c.codeCache.gcTypeSubtypingProduct = stagedGCTypeSubtypingRefTestMulti
	} else {
		c.codeCache.gcTypeSubtypingProduct = stagedGCTypeSubtypingRefTestSingle
	}
}

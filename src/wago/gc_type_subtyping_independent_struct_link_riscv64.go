//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"
)

const rv64GCTypeSubtypingIndependentStructLinkProviderCodeLen = 576

var rv64GCTypeSubtypingIndependentStructLinkProviderCodeDigest = [sha256.Size]byte{0x1d, 0xf4, 0x1d, 0xb6, 0xda, 0x6c, 0xf7, 0x49, 0x1e, 0x95, 0x1a, 0x23, 0xe0, 0x52, 0x51, 0xba, 0xc1, 0x48, 0x0b, 0xe5, 0xaa, 0x36, 0xe1, 0x01, 0x5d, 0x3c, 0xd9, 0x75, 0x4c, 0x4d, 0x65, 0x03}
var rv64GCTypeSubtypingIndependentStructLinkProviderEntries = []int{0}

var rv64GCTypeSubtypingIndependentStructLinkMetadataLens = [...]int{321, 322}
var rv64GCTypeSubtypingIndependentStructLinkMetadataDigests = [...][sha256.Size]byte{
	{0xaa, 0xe7, 0x54, 0xc3, 0x73, 0x74, 0x2b, 0x98, 0xfe, 0x09, 0x3b, 0x2a, 0x28, 0xa8, 0xa4, 0x62, 0xc7, 0x3c, 0x07, 0xa5, 0xc3, 0xd2, 0x40, 0xb7, 0x67, 0x98, 0x29, 0x0f, 0xbb, 0x74, 0x69, 0xa8},
	{0xbe, 0x89, 0x64, 0xc1, 0xa1, 0xf1, 0xac, 0xb0, 0x93, 0x6b, 0x99, 0xf4, 0xc4, 0x09, 0x73, 0x84, 0xf8, 0x20, 0x66, 0x1b, 0x47, 0xb3, 0xc8, 0x69, 0xf5, 0x1c, 0xbf, 0x8e, 0x3f, 0x85, 0xb8, 0xc1},
}

func rv64GCTypeSubtypingIndependentStructLinkConsumer(c *Compiled) bool {
	return c != nil && c.NumImports == 1 && len(c.Imports) == 1 && c.Imports[0] == "M6.g" && len(c.importFuncSigs) == 1 && c.importFuncSigs[0].HasTypeIndex && c.importFuncSigs[0].TypeIndex == 0
}

func rv64GCTypeSubtypingIndependentStructLinkProfileWithCode(c *Compiled, allowUnrestored, requireCode bool) uint8 {
	if c == nil || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled ||
		c.nativeTable64ABI != nativeTable64NoABI || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata || !c.NeedsFuncRefDescs || c.dynamicFuncrefEscape ||
		c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) || c.usesGCStructHelpers() || c.usesGCArrayHelpers() ||
		len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 || len(c.Globals) != 0 || c.tableCount() != 0 || len(c.extraTables) != 0 || len(c.tableExports) != 0 ||
		c.memoryCount() != 0 || c.tagImportCount() != 0 || c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 || c.Names != nil ||
		len(c.customInstructions) != 0 || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.HasTable || c.HasMemory ||
		c.HasTableInitFunc || c.TableInitFunc != 0 || c.tableImport != "" || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 ||
		c.stagedGCStructProduct() != 0 || c.stagedGCArrayProduct() != 0 || c.genericGCFrameRoots() != nil || c.nativeGCABIRequirement() != 0 || c.requiredAMD64Features != 0 {
		return 0
	}
	product := c.stagedGCTypeSubtypingProduct()
	var variant uint8
	switch product {
	case stagedGCTypeSubtypingIndependentStructLinkProvider:
		variant = 1
	case stagedGCTypeSubtypingIndependentStructLinkConsumer:
		if !rv64GCTypeSubtypingIndependentStructLinkConsumer(c) {
			return 0
		}
		variant = 2
	case 0:
		if !allowUnrestored {
			return 0
		}
	default:
		return 0
	}
	if c.stagedFeatures() != CoreFeatureTypedFunctionReferences|CoreFeatureGC && (!allowUnrestored || c.stagedFeatures() != 0) || c.requiredFeatures != CoreFeatureReferenceTypes|CoreFeatureTypedFunctionReferences|CoreFeatureGC {
		return 0
	}
	if variant == 0 {
		if c.NumImports == 0 {
			variant = 1
		} else if rv64GCTypeSubtypingIndependentStructLinkConsumer(c) {
			variant = 2
		} else {
			return 0
		}
	}
	switch variant {
	case 1:
		if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 1 || len(c.FuncTypeID) != 1 || len(c.Exports) != 1 || c.Exports["g"] != 0 ||
			!slices.Equal(c.Entry, rv64GCTypeSubtypingIndependentStructLinkProviderEntries) || !slices.Equal(c.InternalEntry, rv64GCTypeSubtypingIndependentStructLinkProviderEntries) ||
			requireCode && (len(c.code) != rv64GCTypeSubtypingIndependentStructLinkProviderCodeLen || sha256.Sum256(c.code) != rv64GCTypeSubtypingIndependentStructLinkProviderCodeDigest) {
			return 0
		}
	case 2:
		if !c.dynamicImports || len(c.Funcs) != 0 || len(c.FuncTypeID) != 1 || len(c.Exports) != 0 || len(c.Entry) != 0 || len(c.InternalEntry) != 0 || len(c.code) != 0 {
			return 0
		}
	default:
		return 0
	}
	metadata, err := marshalCompiledMetadata(c)
	if err != nil || len(metadata) != rv64GCTypeSubtypingIndependentStructLinkMetadataLens[variant-1] || sha256.Sum256(metadata) != rv64GCTypeSubtypingIndependentStructLinkMetadataDigests[variant-1] {
		return 0
	}
	return variant
}

func rv64GCTypeSubtypingIndependentStructLink(c *Compiled) bool {
	return rv64GCTypeSubtypingIndependentStructLinkProfileWithCode(c, false, true) != 0
}

func rv64GCTypeSubtypingIndependentStructLinkExecutionCode(c *Compiled) bool {
	return c != nil && (len(c.code) == rv64GCTypeSubtypingIndependentStructLinkProviderCodeLen && sha256.Sum256(c.code) == rv64GCTypeSubtypingIndependentStructLinkProviderCodeDigest || len(c.code) == 0)
}

func railshotRestoreGCTypeSubtypingIndependentStructLinkArtifact(c *Compiled) {
	variant := rv64GCTypeSubtypingIndependentStructLinkProfileWithCode(c, true, true)
	if variant == 0 {
		return
	}
	c.ensureCodeCache()
	c.codeCache.stagedFeatures |= CoreFeatureTypedFunctionReferences | CoreFeatureGC
	if variant == 1 {
		c.codeCache.gcTypeSubtypingProduct = stagedGCTypeSubtypingIndependentStructLinkProvider
	} else {
		c.codeCache.gcTypeSubtypingProduct = stagedGCTypeSubtypingIndependentStructLinkConsumer
	}
}

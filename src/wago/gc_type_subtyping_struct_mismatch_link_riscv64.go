//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"
)

const rv64GCTypeSubtypingStructMismatchLinkProviderCodeLen = 576

var rv64GCTypeSubtypingStructMismatchLinkProviderCodeDigest = [sha256.Size]byte{0x1d, 0xf4, 0x1d, 0xb6, 0xda, 0x6c, 0xf7, 0x49, 0x1e, 0x95, 0x1a, 0x23, 0xe0, 0x52, 0x51, 0xba, 0xc1, 0x48, 0x0b, 0xe5, 0xaa, 0x36, 0xe1, 0x01, 0x5d, 0x3c, 0xd9, 0x75, 0x4c, 0x4d, 0x65, 0x03}
var rv64GCTypeSubtypingStructMismatchLinkProviderEntries = []int{0}

var rv64GCTypeSubtypingStructMismatchLinkMetadataLens = [...]int{321, 232}
var rv64GCTypeSubtypingStructMismatchLinkMetadataDigests = [...][sha256.Size]byte{
	{0x6f, 0x8d, 0xce, 0x56, 0xb5, 0x46, 0x71, 0x50, 0xad, 0x99, 0x0e, 0x89, 0x3c, 0x50, 0x95, 0xab, 0xa8, 0x64, 0xc2, 0xa0, 0xf7, 0x52, 0x7c, 0x29, 0xc4, 0x78, 0x03, 0x81, 0xc1, 0x1c, 0xb4, 0xdd},
	{0x0a, 0xb5, 0x0d, 0x8a, 0x1d, 0x2e, 0xe2, 0x2f, 0x84, 0xf9, 0x6e, 0x4d, 0x14, 0x94, 0xe1, 0x07, 0xec, 0x27, 0x37, 0x03, 0x7f, 0x76, 0xe2, 0xb7, 0x14, 0x6e, 0x57, 0x06, 0x5a, 0xce, 0x33, 0x3d},
}

func rv64GCTypeSubtypingStructMismatchLinkConsumer(c *Compiled) bool {
	return c != nil && c.NumImports == 1 && len(c.Imports) == 1 && c.Imports[0] == "M5.g" && len(c.importFuncSigs) == 1 && c.importFuncSigs[0].HasTypeIndex && c.importFuncSigs[0].TypeIndex == 2
}

func rv64GCTypeSubtypingStructMismatchLinkProfileWithCode(c *Compiled, allowUnrestored, requireCode bool) uint8 {
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
	case stagedGCTypeSubtypingStructMismatchLinkProvider:
		variant = 1
	case stagedGCTypeSubtypingStructMismatchLinkConsumer:
		if !rv64GCTypeSubtypingStructMismatchLinkConsumer(c) {
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
	if c.stagedFeatures() != CoreFeatureTypedFunctionReferences|CoreFeatureGC && (!allowUnrestored || c.stagedFeatures() != 0) ||
		c.requiredFeatures != CoreFeatureReferenceTypes|CoreFeatureTypedFunctionReferences|CoreFeatureGC {
		return 0
	}
	if variant == 0 {
		if c.NumImports == 0 {
			variant = 1
		} else if rv64GCTypeSubtypingStructMismatchLinkConsumer(c) {
			variant = 2
		} else {
			return 0
		}
	}
	switch variant {
	case 1:
		if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 1 || len(c.FuncTypeID) != 1 || len(c.Exports) != 1 || c.Exports["g"] != 0 ||
			!slices.Equal(c.Entry, rv64GCTypeSubtypingStructMismatchLinkProviderEntries) || !slices.Equal(c.InternalEntry, rv64GCTypeSubtypingStructMismatchLinkProviderEntries) ||
			requireCode && (len(c.code) != rv64GCTypeSubtypingStructMismatchLinkProviderCodeLen || sha256.Sum256(c.code) != rv64GCTypeSubtypingStructMismatchLinkProviderCodeDigest) {
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
	if err != nil || len(metadata) != rv64GCTypeSubtypingStructMismatchLinkMetadataLens[variant-1] || sha256.Sum256(metadata) != rv64GCTypeSubtypingStructMismatchLinkMetadataDigests[variant-1] {
		return 0
	}
	return variant
}

func rv64GCTypeSubtypingStructMismatchLink(c *Compiled) bool {
	return rv64GCTypeSubtypingStructMismatchLinkProfileWithCode(c, false, true) != 0
}

func rv64GCTypeSubtypingStructMismatchLinkExecutionCode(c *Compiled) bool {
	return c != nil && (len(c.code) == rv64GCTypeSubtypingStructMismatchLinkProviderCodeLen && sha256.Sum256(c.code) == rv64GCTypeSubtypingStructMismatchLinkProviderCodeDigest || len(c.code) == 0)
}

func railshotRestoreGCTypeSubtypingStructMismatchLinkArtifact(c *Compiled) {
	variant := rv64GCTypeSubtypingStructMismatchLinkProfileWithCode(c, true, true)
	if variant == 0 {
		return
	}
	c.ensureCodeCache()
	c.codeCache.stagedFeatures |= CoreFeatureTypedFunctionReferences | CoreFeatureGC
	if variant == 1 {
		c.codeCache.gcTypeSubtypingProduct = stagedGCTypeSubtypingStructMismatchLinkProvider
	} else {
		c.codeCache.gcTypeSubtypingProduct = stagedGCTypeSubtypingStructMismatchLinkConsumer
	}
}

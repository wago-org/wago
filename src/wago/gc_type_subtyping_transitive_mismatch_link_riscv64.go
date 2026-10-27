//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"
)

const rv64GCTypeSubtypingTransitiveMismatchLinkProviderCodeLen = 576

var rv64GCTypeSubtypingTransitiveMismatchLinkProviderCodeDigest = [sha256.Size]byte{0x1d, 0xf4, 0x1d, 0xb6, 0xda, 0x6c, 0xf7, 0x49, 0x1e, 0x95, 0x1a, 0x23, 0xe0, 0x52, 0x51, 0xba, 0xc1, 0x48, 0x0b, 0xe5, 0xaa, 0x36, 0xe1, 0x01, 0x5d, 0x3c, 0xd9, 0x75, 0x4c, 0x4d, 0x65, 0x03}
var rv64GCTypeSubtypingTransitiveMismatchLinkProviderEntries = []int{0}

var rv64GCTypeSubtypingTransitiveMismatchLinkMetadataLens = [...]int{302, 224}
var rv64GCTypeSubtypingTransitiveMismatchLinkMetadataDigests = [...][sha256.Size]byte{
	{0x50, 0x3a, 0x25, 0xb2, 0x66, 0xd0, 0xa8, 0xbe, 0xe8, 0x76, 0x03, 0x88, 0x51, 0xee, 0x8f, 0xa3, 0x40, 0x9e, 0x3a, 0xd0, 0x44, 0x25, 0x5b, 0x1f, 0x9e, 0x2c, 0x39, 0x6d, 0x78, 0x9f, 0xf4, 0xcc},
	{0xdf, 0x0d, 0xa9, 0xa6, 0x7c, 0xae, 0xbf, 0xe8, 0x35, 0xb4, 0xb6, 0xb0, 0x7a, 0x9a, 0xe6, 0xd2, 0x2b, 0x5f, 0xda, 0x60, 0x6e, 0x9a, 0x2b, 0x86, 0x3f, 0x91, 0x39, 0xec, 0xd0, 0xe6, 0xff, 0x8e},
}

func rv64GCTypeSubtypingTransitiveMismatchLinkConsumer(c *Compiled) bool {
	return c != nil && c.NumImports == 1 && len(c.Imports) == 1 && c.Imports[0] == "M11.f" && len(c.importFuncSigs) == 1 && c.importFuncSigs[0].HasTypeIndex && c.importFuncSigs[0].TypeIndex == 2
}

func rv64GCTypeSubtypingTransitiveMismatchLinkProfileWithCode(c *Compiled, allowUnrestored, requireCode bool) uint8 {
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
	case stagedGCTypeSubtypingTransitiveMismatchLinkProvider:
		variant = 1
	case stagedGCTypeSubtypingTransitiveMismatchLinkConsumer:
		if !rv64GCTypeSubtypingTransitiveMismatchLinkConsumer(c) {
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
		c.requiredFeatures != CoreFeatureTypedFunctionReferences|CoreFeatureGC {
		return 0
	}
	if variant == 0 {
		if c.NumImports == 0 {
			variant = 1
		} else if rv64GCTypeSubtypingTransitiveMismatchLinkConsumer(c) {
			variant = 2
		} else {
			return 0
		}
	}
	switch variant {
	case 1:
		if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 1 || len(c.FuncTypeID) != 1 || len(c.Exports) != 1 || c.Exports["f"] != 0 ||
			!slices.Equal(c.Entry, rv64GCTypeSubtypingTransitiveMismatchLinkProviderEntries) || !slices.Equal(c.InternalEntry, rv64GCTypeSubtypingTransitiveMismatchLinkProviderEntries) ||
			requireCode && (len(c.code) != rv64GCTypeSubtypingTransitiveMismatchLinkProviderCodeLen || sha256.Sum256(c.code) != rv64GCTypeSubtypingTransitiveMismatchLinkProviderCodeDigest) {
			return 0
		}
	case 2:
		if !c.dynamicImports || len(c.Funcs) != 0 || len(c.FuncTypeID) != 1 || len(c.Exports) != 0 || len(c.Entry) != 0 || len(c.InternalEntry) != 0 || len(c.code) != 0 {
			return 0
		}
	default:
		return 0
	}
	metadata, err := rv64CanonicalMetadata(c)
	if err != nil || len(metadata) != rv64GCTypeSubtypingTransitiveMismatchLinkMetadataLens[variant-1] || sha256.Sum256(metadata) != rv64GCTypeSubtypingTransitiveMismatchLinkMetadataDigests[variant-1] {
		return 0
	}
	return variant
}

func rv64GCTypeSubtypingTransitiveMismatchLink(c *Compiled) bool {
	return rv64GCTypeSubtypingTransitiveMismatchLinkProfileWithCode(c, false, true) != 0
}

func rv64GCTypeSubtypingTransitiveMismatchLinkExecutionCode(c *Compiled) bool {
	return c != nil && (len(c.code) == rv64GCTypeSubtypingTransitiveMismatchLinkProviderCodeLen && sha256.Sum256(c.code) == rv64GCTypeSubtypingTransitiveMismatchLinkProviderCodeDigest || len(c.code) == 0)
}

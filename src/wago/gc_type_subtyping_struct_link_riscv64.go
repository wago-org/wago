//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"
)

const rv64GCTypeSubtypingStructLinkProviderCodeLen = 576

var rv64GCTypeSubtypingStructLinkProviderCodeDigest = [sha256.Size]byte{0x1d, 0xf4, 0x1d, 0xb6, 0xda, 0x6c, 0xf7, 0x49, 0x1e, 0x95, 0x1a, 0x23, 0xe0, 0x52, 0x51, 0xba, 0xc1, 0x48, 0x0b, 0xe5, 0xaa, 0x36, 0xe1, 0x01, 0x5d, 0x3c, 0xd9, 0x75, 0x4c, 0x4d, 0x65, 0x03}
var rv64GCTypeSubtypingStructLinkProviderEntries = []int{0}

var rv64GCTypeSubtypingStructLinkMetadataLens = [...]int{231, 232}
var rv64GCTypeSubtypingStructLinkMetadataDigests = [...][sha256.Size]byte{
	{0x1d, 0x1c, 0xea, 0xae, 0x89, 0x14, 0xa5, 0x71, 0xc6, 0xf3, 0xf6, 0x23, 0xdf, 0x54, 0x33, 0x6e, 0xe0, 0xd8, 0xa3, 0x1c, 0xf1, 0x07, 0x35, 0x8e, 0x40, 0x9d, 0xff, 0xa2, 0x9b, 0x82, 0xf0, 0x0e},
	{0x0c, 0x3d, 0x9c, 0xf9, 0x53, 0x8e, 0x83, 0x0f, 0x61, 0x4f, 0x90, 0xe6, 0x55, 0x7a, 0xc8, 0x6f, 0x44, 0x80, 0x48, 0xef, 0xbd, 0x08, 0xbd, 0xb0, 0x39, 0xca, 0xee, 0xfe, 0xc6, 0x69, 0x09, 0x58},
}

func rv64GCTypeSubtypingStructLinkConsumer(c *Compiled) bool {
	return c != nil && c.NumImports == 1 && len(c.Imports) == 1 && c.Imports[0] == "M3.g" && len(c.importFuncSigs) == 1 && c.importFuncSigs[0].HasTypeIndex && c.importFuncSigs[0].TypeIndex == 2
}

func rv64GCTypeSubtypingStructLinkProfileWithCode(c *Compiled, allowUnrestored, requireCode bool) uint8 {
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
	variant := uint8(0)
	switch product {
	case stagedGCTypeSubtypingStructLinkProvider:
		variant = 1
	case stagedGCTypeSubtypingStructLinkConsumer:
		if !rv64GCTypeSubtypingStructLinkConsumer(c) {
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
		} else if rv64GCTypeSubtypingStructLinkConsumer(c) {
			variant = 2
		} else {
			return 0
		}
	}
	switch variant {
	case 1:
		if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 1 || len(c.FuncTypeID) != 1 || len(c.Exports) != 1 || c.Exports["g"] != 0 ||
			!slices.Equal(c.Entry, rv64GCTypeSubtypingStructLinkProviderEntries) || !slices.Equal(c.InternalEntry, rv64GCTypeSubtypingStructLinkProviderEntries) ||
			requireCode && (len(c.code) != rv64GCTypeSubtypingStructLinkProviderCodeLen || sha256.Sum256(c.code) != rv64GCTypeSubtypingStructLinkProviderCodeDigest) {
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
	if err != nil || len(metadata) != rv64GCTypeSubtypingStructLinkMetadataLens[variant-1] || sha256.Sum256(metadata) != rv64GCTypeSubtypingStructLinkMetadataDigests[variant-1] {
		return 0
	}
	return variant
}

func rv64GCTypeSubtypingStructLink(c *Compiled) bool {
	return rv64GCTypeSubtypingStructLinkProfileWithCode(c, false, true) != 0
}

func rv64GCTypeSubtypingStructLinkExecutionCode(c *Compiled) bool {
	return c != nil && (len(c.code) == rv64GCTypeSubtypingStructLinkProviderCodeLen && sha256.Sum256(c.code) == rv64GCTypeSubtypingStructLinkProviderCodeDigest || len(c.code) == 0)
}

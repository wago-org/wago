//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"
)

const rv64GCTypeSubtypingStructProjectionLinkProviderCodeLen = 576

var rv64GCTypeSubtypingStructProjectionLinkProviderCodeDigest = [sha256.Size]byte{0x1d, 0xf4, 0x1d, 0xb6, 0xda, 0x6c, 0xf7, 0x49, 0x1e, 0x95, 0x1a, 0x23, 0xe0, 0x52, 0x51, 0xba, 0xc1, 0x48, 0x0b, 0xe5, 0xaa, 0x36, 0xe1, 0x01, 0x5d, 0x3c, 0xd9, 0x75, 0x4c, 0x4d, 0x65, 0x03}
var rv64GCTypeSubtypingStructProjectionLinkProviderEntries = []int{0}

var rv64GCTypeSubtypingStructProjectionLinkMetadataLens = [...]int{400, 401}
var rv64GCTypeSubtypingStructProjectionLinkMetadataDigests = [...][sha256.Size]byte{
	{0xff, 0x3d, 0xa2, 0x3e, 0x68, 0xa3, 0x3d, 0x27, 0xee, 0xe0, 0x94, 0x00, 0x2b, 0x8e, 0xfb, 0x93, 0x71, 0x80, 0xe4, 0xe6, 0x0c, 0x82, 0xc5, 0x35, 0x36, 0x3a, 0x2d, 0x57, 0x65, 0xa4, 0x91, 0x1f},
	{0x66, 0x85, 0x7d, 0x07, 0x10, 0xb1, 0xc6, 0x25, 0xef, 0x95, 0x8f, 0x79, 0x27, 0x5d, 0x7c, 0xb8, 0xde, 0xb7, 0xe0, 0x46, 0x11, 0x93, 0xed, 0x92, 0xbb, 0xd4, 0x16, 0xbb, 0x03, 0x4c, 0xed, 0xdd},
}

func rv64GCTypeSubtypingStructProjectionLinkConsumer(c *Compiled) bool {
	return c != nil && c.NumImports == 1 && len(c.Imports) == 1 && c.Imports[0] == "M4.g" && len(c.importFuncSigs) == 1 && c.importFuncSigs[0].HasTypeIndex && c.importFuncSigs[0].TypeIndex == 4
}

func rv64GCTypeSubtypingStructProjectionLinkProfileWithCode(c *Compiled, allowUnrestored, requireCode bool) uint8 {
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
	case stagedGCTypeSubtypingStructProjectionLinkProvider:
		variant = 1
	case stagedGCTypeSubtypingStructProjectionLinkConsumer:
		if !rv64GCTypeSubtypingStructProjectionLinkConsumer(c) {
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
		} else if rv64GCTypeSubtypingStructProjectionLinkConsumer(c) {
			variant = 2
		} else {
			return 0
		}
	}
	switch variant {
	case 1:
		if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 1 || len(c.FuncTypeID) != 1 || len(c.Exports) != 1 || c.Exports["g"] != 0 ||
			!slices.Equal(c.Entry, rv64GCTypeSubtypingStructProjectionLinkProviderEntries) || !slices.Equal(c.InternalEntry, rv64GCTypeSubtypingStructProjectionLinkProviderEntries) ||
			requireCode && (len(c.code) != rv64GCTypeSubtypingStructProjectionLinkProviderCodeLen || sha256.Sum256(c.code) != rv64GCTypeSubtypingStructProjectionLinkProviderCodeDigest) {
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
	if err != nil || len(metadata) != rv64GCTypeSubtypingStructProjectionLinkMetadataLens[variant-1] || sha256.Sum256(metadata) != rv64GCTypeSubtypingStructProjectionLinkMetadataDigests[variant-1] {
		return 0
	}
	return variant
}

func rv64GCTypeSubtypingStructProjectionLink(c *Compiled) bool {
	return rv64GCTypeSubtypingStructProjectionLinkProfileWithCode(c, false, true) != 0
}

func rv64GCTypeSubtypingStructProjectionLinkExecutionCode(c *Compiled) bool {
	return c != nil && (len(c.code) == rv64GCTypeSubtypingStructProjectionLinkProviderCodeLen && sha256.Sum256(c.code) == rv64GCTypeSubtypingStructProjectionLinkProviderCodeDigest || len(c.code) == 0)
}

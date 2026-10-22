//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"
)

const rv64GCTypeSubtypingExtendedProjectionLinkProviderCodeLen = 576

var rv64GCTypeSubtypingExtendedProjectionLinkProviderCodeDigest = [sha256.Size]byte{0x1d, 0xf4, 0x1d, 0xb6, 0xda, 0x6c, 0xf7, 0x49, 0x1e, 0x95, 0x1a, 0x23, 0xe0, 0x52, 0x51, 0xba, 0xc1, 0x48, 0x0b, 0xe5, 0xaa, 0x36, 0xe1, 0x01, 0x5d, 0x3c, 0xd9, 0x75, 0x4c, 0x4d, 0x65, 0x03}
var rv64GCTypeSubtypingExtendedProjectionLinkProviderEntries = []int{0}

var rv64GCTypeSubtypingExtendedProjectionLinkMetadataLens = [...]int{479, 499}
var rv64GCTypeSubtypingExtendedProjectionLinkMetadataDigests = [...][sha256.Size]byte{
	{0xd0, 0xcc, 0x1b, 0xb4, 0xa7, 0xb4, 0xa5, 0x9b, 0xc0, 0x97, 0x5c, 0xdc, 0xf0, 0x72, 0x82, 0x68, 0x10, 0x36, 0xcf, 0x0f, 0x99, 0x72, 0x30, 0x8d, 0x41, 0x03, 0x12, 0x14, 0x81, 0x6e, 0xca, 0xf8},
	{0xf5, 0xf9, 0x67, 0x27, 0x6e, 0x7f, 0x3e, 0x79, 0xcd, 0x03, 0xbb, 0x44, 0xc3, 0xf3, 0xa6, 0x33, 0xf2, 0x27, 0xa9, 0x02, 0xf8, 0x19, 0x5a, 0xdd, 0x9e, 0xd7, 0xd6, 0x0f, 0xc3, 0x67, 0x02, 0x66},
}

func rv64GCTypeSubtypingExtendedProjectionLinkConsumer(c *Compiled) bool {
	return c != nil && c.NumImports == 2 && len(c.Imports) == 2 && c.Imports[0] == "M7.h" && c.Imports[1] == "M7.h" && len(c.importFuncSigs) == 2 &&
		c.importFuncSigs[0].HasTypeIndex && c.importFuncSigs[0].TypeIndex == 0 && c.importFuncSigs[1].HasTypeIndex && c.importFuncSigs[1].TypeIndex == 4
}

func rv64GCTypeSubtypingExtendedProjectionLinkProfileWithCode(c *Compiled, allowUnrestored, requireCode bool) uint8 {
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
	case stagedGCTypeSubtypingExtendedProjectionLinkProvider:
		variant = 1
	case stagedGCTypeSubtypingExtendedProjectionLinkConsumer:
		if !rv64GCTypeSubtypingExtendedProjectionLinkConsumer(c) {
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
		} else if rv64GCTypeSubtypingExtendedProjectionLinkConsumer(c) {
			variant = 2
		} else {
			return 0
		}
	}
	switch variant {
	case 1:
		if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 1 || len(c.FuncTypeID) != 1 || len(c.Exports) != 1 || c.Exports["h"] != 0 ||
			!slices.Equal(c.Entry, rv64GCTypeSubtypingExtendedProjectionLinkProviderEntries) || !slices.Equal(c.InternalEntry, rv64GCTypeSubtypingExtendedProjectionLinkProviderEntries) ||
			requireCode && (len(c.code) != rv64GCTypeSubtypingExtendedProjectionLinkProviderCodeLen || sha256.Sum256(c.code) != rv64GCTypeSubtypingExtendedProjectionLinkProviderCodeDigest) {
			return 0
		}
	case 2:
		if !c.dynamicImports || len(c.Funcs) != 0 || len(c.FuncTypeID) != 2 || len(c.Exports) != 0 || len(c.Entry) != 0 || len(c.InternalEntry) != 0 || len(c.code) != 0 {
			return 0
		}
	default:
		return 0
	}
	metadata, err := marshalCompiledMetadata(c)
	if err != nil || len(metadata) != rv64GCTypeSubtypingExtendedProjectionLinkMetadataLens[variant-1] || sha256.Sum256(metadata) != rv64GCTypeSubtypingExtendedProjectionLinkMetadataDigests[variant-1] {
		return 0
	}
	return variant
}

func rv64GCTypeSubtypingExtendedProjectionLink(c *Compiled) bool {
	return rv64GCTypeSubtypingExtendedProjectionLinkProfileWithCode(c, false, true) != 0
}

func rv64GCTypeSubtypingExtendedProjectionLinkExecutionCode(c *Compiled) bool {
	return c != nil && (len(c.code) == rv64GCTypeSubtypingExtendedProjectionLinkProviderCodeLen && sha256.Sum256(c.code) == rv64GCTypeSubtypingExtendedProjectionLinkProviderCodeDigest || len(c.code) == 0)
}

func railshotRestoreGCTypeSubtypingExtendedProjectionLinkArtifact(c *Compiled) {
	variant := rv64GCTypeSubtypingExtendedProjectionLinkProfileWithCode(c, true, true)
	if variant == 0 {
		return
	}
	c.ensureCodeCache()
	c.codeCache.stagedFeatures |= CoreFeatureTypedFunctionReferences | CoreFeatureGC
	if variant == 1 {
		c.codeCache.gcTypeSubtypingProduct = stagedGCTypeSubtypingExtendedProjectionLinkProvider
	} else {
		c.codeCache.gcTypeSubtypingProduct = stagedGCTypeSubtypingExtendedProjectionLinkConsumer
	}
}

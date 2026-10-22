//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"
)

const rv64GCTypeSubtypingCrossGroupMismatchLinkProviderCodeLen = 576

var rv64GCTypeSubtypingCrossGroupMismatchLinkProviderCodeDigest = [sha256.Size]byte{0x1d, 0xf4, 0x1d, 0xb6, 0xda, 0x6c, 0xf7, 0x49, 0x1e, 0x95, 0x1a, 0x23, 0xe0, 0x52, 0x51, 0xba, 0xc1, 0x48, 0x0b, 0xe5, 0xaa, 0x36, 0xe1, 0x01, 0x5d, 0x3c, 0xd9, 0x75, 0x4c, 0x4d, 0x65, 0x03}
var rv64GCTypeSubtypingCrossGroupMismatchLinkProviderEntries = []int{0}

var rv64GCTypeSubtypingCrossGroupMismatchLinkMetadataLens = [...]int{222, 144}
var rv64GCTypeSubtypingCrossGroupMismatchLinkMetadataDigests = [...][sha256.Size]byte{
	{0x9f, 0x55, 0xb4, 0x8d, 0x16, 0x94, 0xca, 0x48, 0xd5, 0x48, 0xcb, 0x16, 0x49, 0x5d, 0xbd, 0xc9, 0xae, 0x37, 0xbc, 0x35, 0x57, 0xd4, 0x98, 0x76, 0x67, 0x83, 0x9f, 0x27, 0xe2, 0xe4, 0x60, 0x78},
	{0x37, 0xa6, 0x7d, 0xca, 0xa0, 0x79, 0x1a, 0xec, 0x2f, 0xb6, 0x18, 0x34, 0x9b, 0x7a, 0x8e, 0xa0, 0x9d, 0x74, 0xad, 0xae, 0xec, 0xda, 0x98, 0xe2, 0xd4, 0xc9, 0x8e, 0xfa, 0x81, 0x06, 0xa9, 0xfb},
}

func rv64GCTypeSubtypingCrossGroupMismatchLinkConsumer(c *Compiled) bool {
	return c != nil && c.NumImports == 1 && len(c.Imports) == 1 && c.Imports[0] == "M10.f" && len(c.importFuncSigs) == 1 && c.importFuncSigs[0].HasTypeIndex && c.importFuncSigs[0].TypeIndex == 0
}

func rv64GCTypeSubtypingCrossGroupMismatchLinkProfileWithCode(c *Compiled, allowUnrestored, requireCode bool) uint8 {
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
	case stagedGCTypeSubtypingCrossGroupMismatchLinkProvider:
		variant = 1
	case stagedGCTypeSubtypingCrossGroupMismatchLinkConsumer:
		if !rv64GCTypeSubtypingCrossGroupMismatchLinkConsumer(c) {
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
		} else if rv64GCTypeSubtypingCrossGroupMismatchLinkConsumer(c) {
			variant = 2
		} else {
			return 0
		}
	}
	switch variant {
	case 1:
		if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 1 || len(c.FuncTypeID) != 1 || len(c.Exports) != 1 || c.Exports["f"] != 0 ||
			!slices.Equal(c.Entry, rv64GCTypeSubtypingCrossGroupMismatchLinkProviderEntries) || !slices.Equal(c.InternalEntry, rv64GCTypeSubtypingCrossGroupMismatchLinkProviderEntries) ||
			requireCode && (len(c.code) != rv64GCTypeSubtypingCrossGroupMismatchLinkProviderCodeLen || sha256.Sum256(c.code) != rv64GCTypeSubtypingCrossGroupMismatchLinkProviderCodeDigest) {
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
	if err != nil || len(metadata) != rv64GCTypeSubtypingCrossGroupMismatchLinkMetadataLens[variant-1] || sha256.Sum256(metadata) != rv64GCTypeSubtypingCrossGroupMismatchLinkMetadataDigests[variant-1] {
		return 0
	}
	return variant
}

func rv64GCTypeSubtypingCrossGroupMismatchLink(c *Compiled) bool {
	return rv64GCTypeSubtypingCrossGroupMismatchLinkProfileWithCode(c, false, true) != 0
}

func rv64GCTypeSubtypingCrossGroupMismatchLinkExecutionCode(c *Compiled) bool {
	return c != nil && (len(c.code) == rv64GCTypeSubtypingCrossGroupMismatchLinkProviderCodeLen && sha256.Sum256(c.code) == rv64GCTypeSubtypingCrossGroupMismatchLinkProviderCodeDigest || len(c.code) == 0)
}

func railshotRestoreGCTypeSubtypingCrossGroupMismatchLinkArtifact(c *Compiled) {
	variant := rv64GCTypeSubtypingCrossGroupMismatchLinkProfileWithCode(c, true, true)
	if variant == 0 {
		return
	}
	c.ensureCodeCache()
	c.codeCache.stagedFeatures |= CoreFeatureTypedFunctionReferences | CoreFeatureGC
	if variant == 1 {
		c.codeCache.gcTypeSubtypingProduct = stagedGCTypeSubtypingCrossGroupMismatchLinkProvider
	} else {
		c.codeCache.gcTypeSubtypingProduct = stagedGCTypeSubtypingCrossGroupMismatchLinkConsumer
	}
}

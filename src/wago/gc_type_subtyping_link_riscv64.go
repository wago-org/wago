//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"
)

const rv64GCTypeSubtypingLinkProviderCodeLen = 1812

var rv64GCTypeSubtypingLinkProviderCodeDigest = [sha256.Size]byte{0x83, 0x09, 0x02, 0xc6, 0xd1, 0x4e, 0xb8, 0x41, 0xe6, 0xaf, 0xd9, 0x88, 0x4b, 0x43, 0xe8, 0xbe, 0x55, 0x2d, 0x18, 0xc9, 0xca, 0xda, 0xdc, 0x89, 0xab, 0x99, 0xe4, 0x03, 0xd5, 0x5a, 0xad, 0x48}
var rv64GCTypeSubtypingLinkProviderEntries = []int{0, 608, 1216}
var rv64GCTypeSubtypingLinkMetadataLens = [...]int{248, 301}
var rv64GCTypeSubtypingLinkMetadataDigests = [...][sha256.Size]byte{
	{0x81, 0x0e, 0xef, 0xa4, 0x6d, 0x60, 0x9a, 0x1b, 0x19, 0x1e, 0xd6, 0x4c, 0x4f, 0x3b, 0xc1, 0x97, 0x4e, 0x04, 0x89, 0x5c, 0xb6, 0xda, 0x47, 0x0d, 0x0a, 0x48, 0xd7, 0xa8, 0xfe, 0x9e, 0x37, 0xf7},
	{0x4e, 0xb2, 0xef, 0xbb, 0xec, 0x4d, 0xf6, 0x70, 0x01, 0x4d, 0x0f, 0xa1, 0x4b, 0x20, 0x73, 0xc6, 0x1a, 0x71, 0xdd, 0x02, 0x5c, 0x70, 0x8f, 0xa0, 0xa1, 0x35, 0x71, 0x9d, 0x31, 0xf7, 0xee, 0x3d},
}

func rv64GCTypeSubtypingLinkProfileWithCode(c *Compiled, allowUnrestored, requireCode bool) uint8 {
	if c == nil || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled ||
		c.nativeTable64ABI != nativeTable64NoABI || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata || !c.NeedsFuncRefDescs || c.dynamicFuncrefEscape ||
		!c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) || c.usesGCStructHelpers() || c.usesGCArrayHelpers() ||
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
	case stagedGCTypeSubtypingLinkProvider:
		variant = 1
	case stagedGCTypeSubtypingLinkConsumer:
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
		} else {
			variant = 2
		}
	}
	switch variant {
	case 1:
		if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 3 || len(c.FuncTypeID) != 3 || len(c.Exports) != 3 ||
			!slices.Equal(c.Entry, rv64GCTypeSubtypingLinkProviderEntries) || !slices.Equal(c.InternalEntry, rv64GCTypeSubtypingLinkProviderEntries) ||
			requireCode && (len(c.code) != rv64GCTypeSubtypingLinkProviderCodeLen || sha256.Sum256(c.code) != rv64GCTypeSubtypingLinkProviderCodeDigest) {
			return 0
		}
	case 2:
		if c.NumImports != 6 || !c.dynamicImports || len(c.Imports) != 6 || len(c.importFuncSigs) != 6 || len(c.Funcs) != 0 || len(c.FuncTypeID) != 6 || len(c.Exports) != 0 ||
			len(c.Entry) != 0 || len(c.InternalEntry) != 0 || len(c.code) != 0 {
			return 0
		}
	default:
		return 0
	}
	metadata, err := marshalCompiledMetadata(c)
	if err != nil || len(metadata) != rv64GCTypeSubtypingLinkMetadataLens[variant-1] || sha256.Sum256(metadata) != rv64GCTypeSubtypingLinkMetadataDigests[variant-1] {
		return 0
	}
	return variant
}

func rv64GCTypeSubtypingLink(c *Compiled) bool {
	return rv64GCTypeSubtypingLinkProfileWithCode(c, false, true) != 0
}

func rv64GCTypeSubtypingLinkExecutionCode(c *Compiled) bool {
	return c != nil && (len(c.code) == rv64GCTypeSubtypingLinkProviderCodeLen && sha256.Sum256(c.code) == rv64GCTypeSubtypingLinkProviderCodeDigest || len(c.code) == 0)
}

func railshotRestoreGCTypeSubtypingLinkArtifact(c *Compiled) {
	variant := rv64GCTypeSubtypingLinkProfileWithCode(c, true, true)
	if variant == 0 {
		return
	}
	c.ensureCodeCache()
	c.codeCache.stagedFeatures |= CoreFeatureTypedFunctionReferences | CoreFeatureGC
	if variant == 1 {
		c.codeCache.gcTypeSubtypingProduct = stagedGCTypeSubtypingLinkProvider
	} else {
		c.codeCache.gcTypeSubtypingProduct = stagedGCTypeSubtypingLinkConsumer
	}
}

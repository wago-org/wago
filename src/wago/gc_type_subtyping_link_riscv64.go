//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"
)

const rv64GCTypeSubtypingLinkProviderCodeLen = 1812

var rv64GCTypeSubtypingLinkProviderCodeDigest = [sha256.Size]byte{0x83, 0x09, 0x02, 0xc6, 0xd1, 0x4e, 0xb8, 0x41, 0xe6, 0xaf, 0xd9, 0x88, 0x4b, 0x43, 0xe8, 0xbe, 0x55, 0x2d, 0x18, 0xc9, 0xca, 0xda, 0xdc, 0x89, 0xab, 0x99, 0xe4, 0x03, 0xd5, 0x5a, 0xad, 0x48}
var rv64GCTypeSubtypingLinkProviderEntries = []int{0, 608, 1216}
var rv64GCTypeSubtypingLinkMetadataLens = [...]int{248, 301, 206, 206, 206}
var rv64GCTypeSubtypingLinkMetadataDigests = [...][sha256.Size]byte{
	{0x81, 0x0e, 0xef, 0xa4, 0x6d, 0x60, 0x9a, 0x1b, 0x19, 0x1e, 0xd6, 0x4c, 0x4f, 0x3b, 0xc1, 0x97, 0x4e, 0x04, 0x89, 0x5c, 0xb6, 0xda, 0x47, 0x0d, 0x0a, 0x48, 0xd7, 0xa8, 0xfe, 0x9e, 0x37, 0xf7},
	{0x4e, 0xb2, 0xef, 0xbb, 0xec, 0x4d, 0xf6, 0x70, 0x01, 0x4d, 0x0f, 0xa1, 0x4b, 0x20, 0x73, 0xc6, 0x1a, 0x71, 0xdd, 0x02, 0x5c, 0x70, 0x8f, 0xa0, 0xa1, 0x35, 0x71, 0x9d, 0x31, 0xf7, 0xee, 0x3d},
	{0x3f, 0xe2, 0x19, 0xa0, 0x79, 0x42, 0x28, 0x1f, 0x6e, 0x89, 0xdb, 0x1e, 0x48, 0x10, 0xf0, 0x02, 0xdc, 0xca, 0x47, 0x92, 0xf1, 0xac, 0xac, 0x1f, 0xda, 0xea, 0xab, 0x11, 0x13, 0xad, 0x9f, 0x6d},
	{0x17, 0x54, 0x34, 0x82, 0xc3, 0x90, 0x7f, 0x6b, 0x24, 0xea, 0xc4, 0xf9, 0x40, 0x3f, 0xfe, 0x45, 0x2c, 0xbc, 0x6c, 0x70, 0xb2, 0x56, 0x5f, 0x07, 0xb3, 0xa9, 0xda, 0xab, 0x07, 0x96, 0x03, 0xcb},
	{0xf9, 0x00, 0x5c, 0x72, 0xea, 0xa3, 0x95, 0x9f, 0xc6, 0xcd, 0xbe, 0xf8, 0xc2, 0xd7, 0x46, 0x59, 0x0d, 0xf6, 0x1b, 0x48, 0x0b, 0x0d, 0x27, 0x9b, 0x65, 0x46, 0xfc, 0x5a, 0x7f, 0x73, 0xda, 0x47},
}

func rv64GCTypeSubtypingLinkConsumerVariant(c *Compiled) uint8 {
	if c == nil || len(c.Imports) != c.NumImports || len(c.importFuncSigs) != c.NumImports {
		return 0
	}
	if c.NumImports == 6 {
		return 2
	}
	if c.NumImports != 1 || !c.importFuncSigs[0].HasTypeIndex {
		return 0
	}
	switch {
	case c.Imports[0] == "M.f0" && c.importFuncSigs[0].TypeIndex == 1:
		return 3
	case c.Imports[0] == "M.f0" && c.importFuncSigs[0].TypeIndex == 2:
		return 4
	case c.Imports[0] == "M.f1" && c.importFuncSigs[0].TypeIndex == 2:
		return 5
	default:
		return 0
	}
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
		variant = rv64GCTypeSubtypingLinkConsumerVariant(c)
		if variant == 0 {
			return 0
		}
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
			variant = rv64GCTypeSubtypingLinkConsumerVariant(c)
			if variant == 0 {
				return 0
			}
		}
	}
	switch variant {
	case 1:
		if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 3 || len(c.FuncTypeID) != 3 || len(c.Exports) != 3 ||
			!slices.Equal(c.Entry, rv64GCTypeSubtypingLinkProviderEntries) || !slices.Equal(c.InternalEntry, rv64GCTypeSubtypingLinkProviderEntries) ||
			requireCode && (len(c.code) != rv64GCTypeSubtypingLinkProviderCodeLen || sha256.Sum256(c.code) != rv64GCTypeSubtypingLinkProviderCodeDigest) {
			return 0
		}
	case 2, 3, 4, 5:
		wantImports := 6
		if variant >= 3 {
			wantImports = 1
		}
		if c.NumImports != wantImports || !c.dynamicImports || len(c.Imports) != wantImports || len(c.importFuncSigs) != wantImports || len(c.Funcs) != 0 || len(c.FuncTypeID) != wantImports || len(c.Exports) != 0 ||
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

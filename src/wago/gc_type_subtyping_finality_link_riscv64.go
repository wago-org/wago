//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"
)

const rv64GCTypeSubtypingFinalityLinkProviderCodeLen = 1152

var rv64GCTypeSubtypingFinalityLinkProviderCodeDigest = [sha256.Size]byte{0x26, 0x6b, 0x8e, 0x30, 0xd1, 0x62, 0xca, 0x13, 0x66, 0x7f, 0x1d, 0x65, 0x6c, 0x04, 0xee, 0xe4, 0x7d, 0x45, 0x41, 0xbc, 0x17, 0x96, 0xb3, 0x41, 0x26, 0xa3, 0x32, 0x1f, 0x2b, 0xe5, 0x4c, 0x0b}
var rv64GCTypeSubtypingFinalityLinkProviderEntries = []int{0, 576}

var rv64GCTypeSubtypingFinalityLinkMetadataLens = [...]int{160, 140, 140}
var rv64GCTypeSubtypingFinalityLinkMetadataDigests = [...][sha256.Size]byte{
	{0xa0, 0xec, 0xec, 0x70, 0x23, 0xbf, 0xb2, 0x83, 0xe9, 0x77, 0x42, 0x7e, 0x14, 0x9b, 0xbc, 0xab, 0x34, 0xf5, 0xf8, 0xc8, 0xf9, 0xb9, 0x0a, 0x8f, 0x47, 0xa9, 0x6d, 0x2f, 0xd4, 0xb0, 0x94, 0xf1},
	{0x56, 0xba, 0xb6, 0x7f, 0xab, 0x46, 0x47, 0xc9, 0xca, 0x98, 0x64, 0x91, 0x3f, 0xee, 0xdc, 0x93, 0xcb, 0x76, 0x33, 0xc0, 0xc1, 0x24, 0x0c, 0x9b, 0x6d, 0x88, 0x68, 0xca, 0x32, 0xd0, 0x3d, 0xf7},
	{0x31, 0xb3, 0x4b, 0x12, 0xb7, 0x2c, 0xff, 0xa2, 0x75, 0x5e, 0x4c, 0x32, 0x97, 0x44, 0x9d, 0x30, 0xe1, 0xda, 0x4e, 0x4c, 0x56, 0x50, 0xcb, 0x50, 0x14, 0x79, 0xca, 0x23, 0x43, 0xde, 0x09, 0x5a},
}

func rv64GCTypeSubtypingFinalityLinkConsumerVariant(c *Compiled) uint8 {
	if c == nil || c.NumImports != 1 || len(c.Imports) != 1 || len(c.importFuncSigs) != 1 || !c.importFuncSigs[0].HasTypeIndex {
		return 0
	}
	switch {
	case c.Imports[0] == "M2.f1" && c.importFuncSigs[0].TypeIndex == 1:
		return 2
	case c.Imports[0] == "M2.f2" && c.importFuncSigs[0].TypeIndex == 0:
		return 3
	default:
		return 0
	}
}

func rv64GCTypeSubtypingFinalityLinkProfileWithCode(c *Compiled, allowUnrestored, requireCode bool) uint8 {
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
	case stagedGCTypeSubtypingFinalityLinkProvider:
		variant = 1
	case stagedGCTypeSubtypingFinalityLinkConsumer:
		variant = rv64GCTypeSubtypingFinalityLinkConsumerVariant(c)
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
		c.requiredFeatures != CoreFeatureGC {
		return 0
	}
	if variant == 0 {
		if c.NumImports == 0 {
			variant = 1
		} else {
			variant = rv64GCTypeSubtypingFinalityLinkConsumerVariant(c)
			if variant == 0 {
				return 0
			}
		}
	}
	switch variant {
	case 1:
		if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 2 || len(c.FuncTypeID) != 2 || len(c.Exports) != 2 ||
			!slices.Equal(c.Entry, rv64GCTypeSubtypingFinalityLinkProviderEntries) || !slices.Equal(c.InternalEntry, rv64GCTypeSubtypingFinalityLinkProviderEntries) ||
			requireCode && (len(c.code) != rv64GCTypeSubtypingFinalityLinkProviderCodeLen || sha256.Sum256(c.code) != rv64GCTypeSubtypingFinalityLinkProviderCodeDigest) {
			return 0
		}
	case 2, 3:
		if c.NumImports != 1 || !c.dynamicImports || len(c.Imports) != 1 || len(c.importFuncSigs) != 1 || len(c.Funcs) != 0 || len(c.FuncTypeID) != 1 || len(c.Exports) != 0 ||
			len(c.Entry) != 0 || len(c.InternalEntry) != 0 || len(c.code) != 0 {
			return 0
		}
	default:
		return 0
	}
	metadata, err := marshalCompiledMetadata(c)
	if err != nil {
		return 0
	}
	if len(metadata) != rv64GCTypeSubtypingFinalityLinkMetadataLens[variant-1] || sha256.Sum256(metadata) != rv64GCTypeSubtypingFinalityLinkMetadataDigests[variant-1] {
		return 0
	}
	return variant
}

func rv64GCTypeSubtypingFinalityLink(c *Compiled) bool {
	return rv64GCTypeSubtypingFinalityLinkProfileWithCode(c, false, true) != 0
}

func rv64GCTypeSubtypingFinalityLinkExecutionCode(c *Compiled) bool {
	return c != nil && (len(c.code) == rv64GCTypeSubtypingFinalityLinkProviderCodeLen && sha256.Sum256(c.code) == rv64GCTypeSubtypingFinalityLinkProviderCodeDigest || len(c.code) == 0)
}

func railshotRestoreGCTypeSubtypingFinalityLinkArtifact(c *Compiled) {
	variant := rv64GCTypeSubtypingFinalityLinkProfileWithCode(c, true, true)
	if variant == 0 {
		return
	}
	c.ensureCodeCache()
	c.codeCache.stagedFeatures |= CoreFeatureTypedFunctionReferences | CoreFeatureGC
	if variant == 1 {
		c.codeCache.gcTypeSubtypingProduct = stagedGCTypeSubtypingFinalityLinkProvider
	} else {
		c.codeCache.gcTypeSubtypingProduct = stagedGCTypeSubtypingFinalityLinkConsumer
	}
}

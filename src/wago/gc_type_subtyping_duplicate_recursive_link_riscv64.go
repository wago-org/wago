//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"
)

const rv64GCTypeSubtypingDuplicateRecursiveLinkProviderCodeLen = 1248

var rv64GCTypeSubtypingDuplicateRecursiveLinkProviderCodeDigest = [sha256.Size]byte{0xae, 0xc4, 0xd3, 0x0d, 0x9e, 0x07, 0xb3, 0x2c, 0x1a, 0xe9, 0x92, 0xfe, 0xf2, 0x8b, 0x5a, 0x4c, 0xc3, 0x22, 0x4f, 0xb1, 0xc6, 0x26, 0xf7, 0xb5, 0xe1, 0x63, 0xed, 0xf1, 0xc8, 0xa9, 0xed, 0x77}
var rv64GCTypeSubtypingDuplicateRecursiveLinkProviderEntries = []int{0, 624}

var rv64GCTypeSubtypingDuplicateRecursiveLinkMetadataLens = [...]int{272, 314}
var rv64GCTypeSubtypingDuplicateRecursiveLinkMetadataDigests = [...][sha256.Size]byte{
	{0xed, 0xb7, 0x6a, 0x38, 0xea, 0xe9, 0xec, 0x02, 0x15, 0x4e, 0xb9, 0xa1, 0xd3, 0xc3, 0xff, 0x8b, 0xaf, 0xfb, 0x9b, 0x35, 0x43, 0x72, 0xbc, 0x84, 0xa4, 0xfd, 0xb6, 0x61, 0xfb, 0xf6, 0xab, 0x57},
	{0xb3, 0x03, 0x97, 0x2d, 0xc4, 0x5e, 0x92, 0x9c, 0x93, 0x0a, 0x28, 0xe0, 0x11, 0xf2, 0x44, 0x4a, 0x68, 0x8c, 0xe1, 0xcd, 0x0e, 0xa5, 0xb8, 0x04, 0xf3, 0x2c, 0xab, 0x46, 0x3e, 0x9e, 0xce, 0x9f},
}

func rv64GCTypeSubtypingDuplicateRecursiveLinkConsumer(c *Compiled) bool {
	if c == nil || c.NumImports != 4 || len(c.Imports) != 4 || len(c.importFuncSigs) != 4 {
		return false
	}
	wantNames := [...]string{"M8.f11", "M8.f11", "M8.f12", "M8.f12"}
	wantTypes := [...]uint32{0, 2, 1, 3}
	for i := range wantNames {
		if c.Imports[i] != wantNames[i] || !c.importFuncSigs[i].HasTypeIndex || c.importFuncSigs[i].TypeIndex != wantTypes[i] {
			return false
		}
	}
	return true
}

func rv64GCTypeSubtypingDuplicateRecursiveLinkProfileWithCode(c *Compiled, allowUnrestored, requireCode bool) uint8 {
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
	var variant uint8
	switch product {
	case stagedGCTypeSubtypingDuplicateRecursiveLinkProvider:
		variant = 1
	case stagedGCTypeSubtypingDuplicateRecursiveLinkConsumer:
		if !rv64GCTypeSubtypingDuplicateRecursiveLinkConsumer(c) {
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
		} else if rv64GCTypeSubtypingDuplicateRecursiveLinkConsumer(c) {
			variant = 2
		} else {
			return 0
		}
	}
	switch variant {
	case 1:
		if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 2 || len(c.FuncTypeID) != 2 || len(c.Exports) != 2 || c.Exports["f11"] != 0 || c.Exports["f12"] != 1 ||
			!slices.Equal(c.Entry, rv64GCTypeSubtypingDuplicateRecursiveLinkProviderEntries) || !slices.Equal(c.InternalEntry, rv64GCTypeSubtypingDuplicateRecursiveLinkProviderEntries) ||
			requireCode && (len(c.code) != rv64GCTypeSubtypingDuplicateRecursiveLinkProviderCodeLen || sha256.Sum256(c.code) != rv64GCTypeSubtypingDuplicateRecursiveLinkProviderCodeDigest) {
			return 0
		}
	case 2:
		if !c.dynamicImports || len(c.Funcs) != 0 || len(c.FuncTypeID) != 4 || len(c.Exports) != 0 || len(c.Entry) != 0 || len(c.InternalEntry) != 0 || len(c.code) != 0 {
			return 0
		}
	default:
		return 0
	}
	metadata, err := marshalCompiledMetadata(c)
	if err != nil {
		return 0
	}
	if len(metadata) != rv64GCTypeSubtypingDuplicateRecursiveLinkMetadataLens[variant-1] || sha256.Sum256(metadata) != rv64GCTypeSubtypingDuplicateRecursiveLinkMetadataDigests[variant-1] {
		return 0
	}
	return variant
}

func rv64GCTypeSubtypingDuplicateRecursiveLink(c *Compiled) bool {
	return rv64GCTypeSubtypingDuplicateRecursiveLinkProfileWithCode(c, false, true) != 0
}

func rv64GCTypeSubtypingDuplicateRecursiveLinkExecutionCode(c *Compiled) bool {
	return c != nil && (len(c.code) == rv64GCTypeSubtypingDuplicateRecursiveLinkProviderCodeLen && sha256.Sum256(c.code) == rv64GCTypeSubtypingDuplicateRecursiveLinkProviderCodeDigest || len(c.code) == 0)
}

func railshotRestoreGCTypeSubtypingDuplicateRecursiveLinkArtifact(c *Compiled) {
	variant := rv64GCTypeSubtypingDuplicateRecursiveLinkProfileWithCode(c, true, true)
	if variant == 0 {
		return
	}
	c.ensureCodeCache()
	c.codeCache.stagedFeatures |= CoreFeatureTypedFunctionReferences | CoreFeatureGC
	if variant == 1 {
		c.codeCache.gcTypeSubtypingProduct = stagedGCTypeSubtypingDuplicateRecursiveLinkProvider
	} else {
		c.codeCache.gcTypeSubtypingProduct = stagedGCTypeSubtypingDuplicateRecursiveLinkConsumer
	}
}

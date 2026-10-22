//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"
)

const rv64GCTypeSubtypingExtendedRecursiveLinkProviderCodeLen = 1248

var rv64GCTypeSubtypingExtendedRecursiveLinkProviderCodeDigest = [sha256.Size]byte{0xae, 0xc4, 0xd3, 0x0d, 0x9e, 0x07, 0xb3, 0x2c, 0x1a, 0xe9, 0x92, 0xfe, 0xf2, 0x8b, 0x5a, 0x4c, 0xc3, 0x22, 0x4f, 0xb1, 0xc6, 0x26, 0xf7, 0xb5, 0xe1, 0x63, 0xed, 0xf1, 0xc8, 0xa9, 0xed, 0x77}
var rv64GCTypeSubtypingExtendedRecursiveLinkProviderEntries = []int{0, 624}

var rv64GCTypeSubtypingExtendedRecursiveLinkMetadataLens = [...]int{466, 592}
var rv64GCTypeSubtypingExtendedRecursiveLinkMetadataDigests = [...][sha256.Size]byte{
	{0x95, 0xce, 0x09, 0x4f, 0x28, 0x19, 0x06, 0xdf, 0x02, 0xea, 0x75, 0xfb, 0x46, 0xbf, 0xcd, 0x3a, 0x5d, 0xfd, 0x24, 0x75, 0x51, 0xae, 0x73, 0xfd, 0xce, 0xa8, 0x67, 0xd2, 0xe8, 0xc4, 0x51, 0x37},
	{0x2b, 0xf3, 0xb2, 0xba, 0x93, 0xae, 0x3c, 0xd1, 0xac, 0xfb, 0x0b, 0x7a, 0x09, 0x04, 0x4c, 0x44, 0x50, 0xb5, 0x7a, 0x2c, 0xa6, 0x29, 0xc6, 0x5e, 0xbe, 0xaf, 0x19, 0x83, 0x89, 0xa2, 0xc1, 0x15},
}

func rv64GCTypeSubtypingExtendedRecursiveLinkConsumer(c *Compiled) bool {
	if c == nil || c.NumImports != 8 || len(c.Imports) != 8 || len(c.importFuncSigs) != 8 {
		return false
	}
	wantNames := [...]string{"M9.g11", "M9.g11", "M9.g12", "M9.g12", "M9.g11", "M9.g11", "M9.g12", "M9.g12"}
	wantTypes := [...]uint32{0, 2, 0, 2, 4, 6, 5, 7}
	for i := range wantNames {
		if c.Imports[i] != wantNames[i] || !c.importFuncSigs[i].HasTypeIndex || c.importFuncSigs[i].TypeIndex != wantTypes[i] {
			return false
		}
	}
	return true
}

func rv64GCTypeSubtypingExtendedRecursiveLinkProfileWithCode(c *Compiled, allowUnrestored, requireCode bool) uint8 {
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
	case stagedGCTypeSubtypingExtendedRecursiveLinkProvider:
		variant = 1
	case stagedGCTypeSubtypingExtendedRecursiveLinkConsumer:
		if !rv64GCTypeSubtypingExtendedRecursiveLinkConsumer(c) {
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
		} else if rv64GCTypeSubtypingExtendedRecursiveLinkConsumer(c) {
			variant = 2
		} else {
			return 0
		}
	}
	switch variant {
	case 1:
		if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 2 || len(c.FuncTypeID) != 2 || len(c.Exports) != 2 || c.Exports["g11"] != 0 || c.Exports["g12"] != 1 ||
			!slices.Equal(c.Entry, rv64GCTypeSubtypingExtendedRecursiveLinkProviderEntries) || !slices.Equal(c.InternalEntry, rv64GCTypeSubtypingExtendedRecursiveLinkProviderEntries) ||
			requireCode && (len(c.code) != rv64GCTypeSubtypingExtendedRecursiveLinkProviderCodeLen || sha256.Sum256(c.code) != rv64GCTypeSubtypingExtendedRecursiveLinkProviderCodeDigest) {
			return 0
		}
	case 2:
		if !c.dynamicImports || len(c.Funcs) != 0 || len(c.FuncTypeID) != 8 || len(c.Exports) != 0 || len(c.Entry) != 0 || len(c.InternalEntry) != 0 || len(c.code) != 0 {
			return 0
		}
	default:
		return 0
	}
	metadata, err := marshalCompiledMetadata(c)
	if err != nil {
		return 0
	}
	if len(metadata) != rv64GCTypeSubtypingExtendedRecursiveLinkMetadataLens[variant-1] || sha256.Sum256(metadata) != rv64GCTypeSubtypingExtendedRecursiveLinkMetadataDigests[variant-1] {
		return 0
	}
	return variant
}

func rv64GCTypeSubtypingExtendedRecursiveLink(c *Compiled) bool {
	return rv64GCTypeSubtypingExtendedRecursiveLinkProfileWithCode(c, false, true) != 0
}

func rv64GCTypeSubtypingExtendedRecursiveLinkExecutionCode(c *Compiled) bool {
	return c != nil && (len(c.code) == rv64GCTypeSubtypingExtendedRecursiveLinkProviderCodeLen && sha256.Sum256(c.code) == rv64GCTypeSubtypingExtendedRecursiveLinkProviderCodeDigest || len(c.code) == 0)
}

func railshotRestoreGCTypeSubtypingExtendedRecursiveLinkArtifact(c *Compiled) {
	variant := rv64GCTypeSubtypingExtendedRecursiveLinkProfileWithCode(c, true, true)
	if variant == 0 {
		return
	}
	c.ensureCodeCache()
	c.codeCache.stagedFeatures |= CoreFeatureTypedFunctionReferences | CoreFeatureGC
	if variant == 1 {
		c.codeCache.gcTypeSubtypingProduct = stagedGCTypeSubtypingExtendedRecursiveLinkProvider
	} else {
		c.codeCache.gcTypeSubtypingProduct = stagedGCTypeSubtypingExtendedRecursiveLinkConsumer
	}
}

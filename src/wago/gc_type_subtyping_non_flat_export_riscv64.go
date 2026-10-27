//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"fmt"
	"slices"
)

const rv64GCTypeSubtypingNonFlatExportCodeLen = 3640

const rv64GCTypeSubtypingNonFlatExportMetadataLen = 414

var rv64GCTypeSubtypingNonFlatExportCodeDigest = [sha256.Size]byte{0x49, 0x83, 0x4c, 0x03, 0xd2, 0xd6, 0x3d, 0xba, 0x99, 0x28, 0x39, 0x30, 0xf2, 0x9a, 0x13, 0x46, 0xa6, 0x2b, 0x91, 0x82, 0x00, 0xe6, 0x5e, 0x80, 0x03, 0x74, 0xde, 0x44, 0x82, 0x3c, 0x6d, 0xbd}
var rv64GCTypeSubtypingNonFlatExportMetadataDigest = [sha256.Size]byte{0x67, 0xe6, 0x3a, 0x3b, 0xa1, 0x21, 0x2d, 0x9f, 0x3a, 0x37, 0xd7, 0x07, 0x40, 0xc6, 0x5f, 0x38, 0xc2, 0x84, 0x9e, 0xf7, 0x2f, 0x3e, 0x41, 0xbc, 0x27, 0x37, 0xce, 0xc9, 0xda, 0x04, 0x6f, 0x8c}
var rv64GCTypeSubtypingNonFlatExportEntries = []int{0, 608, 1216, 1824, 2432, 3040}

func rv64GCTypeSubtypingNonFlatExportProfileWithCode(c *Compiled, allowUnrestored, requireCode bool) bool {
	if c == nil || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled ||
		c.nativeTable64ABI != nativeTable64NoABI || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata || c.NeedsFuncRefDescs || c.dynamicFuncrefEscape ||
		c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) || c.usesGCStructHelpers() || c.usesGCArrayHelpers() ||
		c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 6 || len(c.FuncTypeID) != 6 ||
		len(c.ValueTypes) != 0 || len(c.Exports) != 6 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 ||
		c.tableCount() != 0 || len(c.extraTables) != 0 || len(c.tableExports) != 0 || c.memoryCount() != 0 || c.tagImportCount() != 0 ||
		c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 || c.Names != nil || len(c.customInstructions) != 0 ||
		c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.HasTable || c.HasMemory ||
		c.HasTableInitFunc || c.TableInitFunc != 0 || c.tableImport != "" || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 ||
		c.stagedGCStructProduct() != 0 || c.stagedGCArrayProduct() != 0 || c.genericGCFrameRoots() != nil || c.nativeGCABIRequirement() != 0 || c.requiredAMD64Features != 0 {
		return false
	}
	product := c.stagedGCTypeSubtypingProduct()
	if product != stagedGCTypeSubtypingNonFlatExport && (!allowUnrestored || product != 0) ||
		c.stagedFeatures() != CoreFeatureTypedFunctionReferences|CoreFeatureGC && (!allowUnrestored || c.stagedFeatures() != 0) ||
		c.requiredFeatures != CoreFeatureTypedFunctionReferences|CoreFeatureGC || len(c.Types) != 6 || len(c.GCTypeDescs) != 6 ||
		!slices.Equal(c.Entry, rv64GCTypeSubtypingNonFlatExportEntries) || !slices.Equal(c.InternalEntry, rv64GCTypeSubtypingNonFlatExportEntries) ||
		requireCode && (len(c.code) != rv64GCTypeSubtypingNonFlatExportCodeLen || sha256.Sum256(c.code) != rv64GCTypeSubtypingNonFlatExportCodeDigest) {
		return false
	}
	for i := 0; i < 6; i++ {
		fn := c.Funcs[i]
		if len(fn.Params) != 0 || !slices.Equal(fn.Results, []ValType{ValF32}) || !fn.HasTypeIndex || fn.TypeIndex != uint32(i) || fn.unsafeCrossTail || c.Exports[fmt.Sprintf("f%d", i+1)] != i {
			return false
		}
	}
	if d := c.memoryDir; d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 || len(d.gcStructGlobals) != 0 || len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
		return false
	}
	metadata, err := rv64CanonicalMetadata(c)
	if err != nil || len(metadata) != rv64GCTypeSubtypingNonFlatExportMetadataLen || sha256.Sum256(metadata) != rv64GCTypeSubtypingNonFlatExportMetadataDigest {
		return false
	}
	return true
}

func rv64GCTypeSubtypingNonFlatExport(c *Compiled) bool {
	return rv64GCTypeSubtypingNonFlatExportProfileWithCode(c, false, true)
}

func rv64GCTypeSubtypingNonFlatExportExecutionCode(c *Compiled) bool {
	return c != nil && len(c.code) == rv64GCTypeSubtypingNonFlatExportCodeLen && sha256.Sum256(c.code) == rv64GCTypeSubtypingNonFlatExportCodeDigest
}

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"
)

var rv64GCTypeSubtypingRefFuncGlobalCode = []struct {
	length int
	digest [sha256.Size]byte
	entry  []int
}{
	{576, [sha256.Size]byte{0x1d, 0xf4, 0x1d, 0xb6, 0xda, 0x6c, 0xf7, 0x49, 0x1e, 0x95, 0x1a, 0x23, 0xe0, 0x52, 0x51, 0xba, 0xc1, 0x48, 0x0b, 0xe5, 0xaa, 0x36, 0xe1, 0x01, 0x5d, 0x3c, 0xd9, 0x75, 0x4c, 0x4d, 0x65, 0x03}, []int{0}},
	{1248, [sha256.Size]byte{0xae, 0xc4, 0xd3, 0x0d, 0x9e, 0x07, 0xb3, 0x2c, 0x1a, 0xe9, 0x92, 0xfe, 0xf2, 0x8b, 0x5a, 0x4c, 0xc3, 0x22, 0x4f, 0xb1, 0xc6, 0x26, 0xf7, 0xb5, 0xe1, 0x63, 0xed, 0xf1, 0xc8, 0xa9, 0xed, 0x77}, []int{0, 624}},
}

var rv64GCTypeSubtypingRefFuncGlobalMetadata = []struct {
	length int
	digest [sha256.Size]byte
}{
	{416, [sha256.Size]byte{0x4e, 0x7b, 0xa6, 0x1f, 0x97, 0xaa, 0x0d, 0x65, 0xf3, 0x00, 0x4a, 0x5e, 0x7d, 0x1c, 0x99, 0x70, 0x11, 0x3c, 0x07, 0x15, 0x1c, 0x9f, 0xf4, 0x56, 0x0f, 0x18, 0x21, 0x3b, 0x5b, 0xe3, 0xd7, 0x16}},
	{574, [sha256.Size]byte{0x81, 0x4e, 0x0c, 0xe0, 0x2a, 0xd0, 0x8b, 0x6a, 0x64, 0x09, 0x9c, 0x5b, 0xd3, 0x7a, 0x01, 0x45, 0x63, 0xf3, 0x3d, 0x7a, 0xaf, 0xb4, 0x62, 0x1b, 0xa9, 0x29, 0x8e, 0x9b, 0x82, 0xda, 0xca, 0xe6}},
	{337, [sha256.Size]byte{0xc5, 0x8b, 0x31, 0x78, 0x54, 0xad, 0xf8, 0xc7, 0x80, 0x07, 0x63, 0x10, 0x9b, 0x68, 0x86, 0x7f, 0x02, 0x88, 0xc6, 0xc2, 0x6b, 0xfc, 0x43, 0xfd, 0x19, 0xb3, 0xd4, 0x67, 0x67, 0x1a, 0x87, 0x13}},
	{672, [sha256.Size]byte{0xfb, 0x9d, 0x2c, 0xc5, 0x56, 0xfd, 0x6d, 0x97, 0xdf, 0xa6, 0xd2, 0x82, 0xd1, 0x50, 0x69, 0x62, 0xa8, 0x64, 0x12, 0x18, 0x82, 0x4f, 0xbb, 0xd0, 0xd7, 0xd7, 0xa8, 0xc5, 0xa1, 0x03, 0xe5, 0x05}},
	{338, [sha256.Size]byte{0x04, 0x8e, 0xb2, 0x9c, 0xcf, 0xe4, 0x1e, 0xa3, 0x1b, 0x70, 0xc3, 0xa6, 0x3b, 0xce, 0x47, 0x21, 0x46, 0xa0, 0x90, 0x7d, 0xc8, 0x2f, 0x11, 0xbb, 0x5a, 0xd6, 0x74, 0x3d, 0xc5, 0x58, 0xc8, 0x06}},
	{592, [sha256.Size]byte{0xb9, 0x1b, 0x79, 0x9a, 0x13, 0xa9, 0x07, 0x79, 0xe9, 0x75, 0x52, 0xb6, 0xda, 0x13, 0x55, 0x70, 0xfa, 0x05, 0x2e, 0x65, 0x6a, 0x8c, 0x7e, 0xd0, 0xb2, 0x25, 0xfe, 0x6c, 0x4f, 0x92, 0x9f, 0x32}},
}

func rv64GCTypeSubtypingRefFuncGlobalsProfileWithCode(c *Compiled, allowUnrestored, requireCode bool) uint8 {
	if c == nil || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled ||
		c.nativeTable64ABI != nativeTable64NoABI || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata || !c.NeedsFuncRefDescs || c.dynamicFuncrefEscape ||
		!c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) || c.usesGCStructHelpers() || c.usesGCArrayHelpers() ||
		c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) < 1 || len(c.Funcs) > 2 || len(c.FuncTypeID) != len(c.Funcs) ||
		len(c.Exports) != 0 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 || c.tableCount() != 0 || len(c.extraTables) != 0 || len(c.tableExports) != 0 ||
		c.memoryCount() != 0 || c.tagImportCount() != 0 || c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 || c.Names != nil ||
		len(c.customInstructions) != 0 || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.HasTable || c.HasMemory ||
		c.HasTableInitFunc || c.TableInitFunc != 0 || c.tableImport != "" || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 ||
		c.stagedGCStructProduct() != 0 || c.stagedGCArrayProduct() != 0 || c.genericGCFrameRoots() != nil || c.nativeGCABIRequirement() != 0 || c.requiredAMD64Features != 0 {
		return 0
	}
	product := c.stagedGCTypeSubtypingProduct()
	if product != stagedGCTypeSubtypingRefFuncGlobals && (!allowUnrestored || product != 0) {
		return 0
	}
	staged := c.stagedFeatures()
	if staged != CoreFeatureTypedFunctionReferences|CoreFeatureGC && (!allowUnrestored || staged != 0) {
		return 0
	}
	if c.requiredFeatures != CoreFeatureReferenceTypes|CoreFeatureTypedFunctionReferences|CoreFeatureGC {
		return 0
	}
	d := c.memoryDir
	if d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 || len(d.gcStructGlobals) != 0 ||
		len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
		return 0
	}
	for _, g := range c.Globals {
		if g.Type != ValFuncRef || g.Mutable || !g.HasValueType || !g.HasInitFunc || int(g.InitFunc) >= len(c.Funcs) || g.Bits != 0 || g.V128 != (V128{}) ||
			g.HasInitGlobal || g.InitGlobal != 0 || len(g.InitExpr) != 0 {
			return 0
		}
	}
	codeProfile := 0
	for i, code := range rv64GCTypeSubtypingRefFuncGlobalCode {
		if len(c.Funcs) != len(code.entry) || !slices.Equal(c.Entry, code.entry) || !slices.Equal(c.InternalEntry, code.entry) ||
			requireCode && (len(c.code) != code.length || sha256.Sum256(c.code) != code.digest) {
			continue
		}
		if i == 0 && (len(c.Globals) != 1 && len(c.Globals) != 2) || i == 1 && (len(c.Globals) != 4 && len(c.Globals) != 8) {
			continue
		}
		codeProfile = i + 1
		break
	}
	if codeProfile == 0 {
		return 0
	}
	metadata, err := rv64CanonicalMetadata(c)
	if err != nil {
		return 0
	}
	for i, profile := range rv64GCTypeSubtypingRefFuncGlobalMetadata {
		if len(metadata) == profile.length && sha256.Sum256(metadata) == profile.digest {
			if codeProfile == 1 && i < 4 || codeProfile == 2 && i >= 4 {
				return uint8(i + 1)
			}
			return 0
		}
	}
	return 0
}

func rv64GCTypeSubtypingRefFuncGlobals(c *Compiled) bool {
	return rv64GCTypeSubtypingRefFuncGlobalsProfileWithCode(c, false, true) != 0
}

func rv64GCTypeSubtypingRefFuncGlobalsExecutionCode(c *Compiled) bool {
	if c == nil {
		return false
	}
	for _, code := range rv64GCTypeSubtypingRefFuncGlobalCode {
		if len(c.code) == code.length && sha256.Sum256(c.code) == code.digest {
			return true
		}
	}
	return false
}

// A fixed instruction image is not a unique module identity: the smallest
// ref.func-global product lowers to the same empty framed function as ordinary
// non-GC modules. Treat the digest as a hostile retained-image signal only
// while metadata still claims some part of the GC product contract. If every
// claim is removed, the ordinary validators below must independently admit or
// reject the remaining metadata.
func rv64GCTypeSubtypingRefFuncGlobalsExecutionClaim(c *Compiled) bool {
	return c != nil && (c.requiredFeatures.IsEnabled(CoreFeatureGC) ||
		c.stagedFeatures().IsEnabled(CoreFeatureGC) ||
		c.stagedGCTypeSubtypingProduct() == stagedGCTypeSubtypingRefFuncGlobals)
}

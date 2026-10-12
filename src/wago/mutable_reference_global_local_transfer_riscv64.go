//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"fmt"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc"
)

// Kept separate from the metadata witness so the exact native image can be
// pinned after native qualification without weakening structural admission.
var rv64MutableFuncrefLocalTransferCodeDigest = [sha256.Size]byte{0x23, 0x8e, 0x2e, 0xca, 0x4f, 0x71, 0xe2, 0x0f, 0x02, 0x35, 0xae, 0xc6, 0x14, 0x8b, 0x5b, 0x6f, 0x59, 0x4e, 0xbb, 0xdb, 0x58, 0x81, 0xe4, 0xa4, 0x51, 0x16, 0xee, 0x91, 0x2c, 0x24, 0xce, 0x48}

func rv64MutableFuncrefGlobalLocalTransferExecutionCode(c *Compiled) bool {
	return c != nil && len(c.code) == 1100 && sha256.Sum256(c.code) == rv64MutableFuncrefLocalTransferCodeDigest
}

// rv64MutableFuncrefGlobalLocalTransfer authenticates a bounded private
// container that can capture a descriptor from imported source zero, publish
// it to imported target one, and clear itself. Every write crosses the parked
// ownership bridge and the private cell receives an internal Global owner.
func rv64MutableFuncrefGlobalLocalTransfer(c *Compiled) bool {
	if c == nil || c.hasNoFunctionCode() || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled || c.nativeTable64ABI != nativeTable64NoABI || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata || c.NeedsFuncRefDescs || c.dynamicFuncrefEscape || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) {
		return false
	}
	if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 3 || len(c.FuncTypeID) != 3 || len(c.Exports) != 3 || len(c.Types) != 1 || len(c.GCTypeDescs) != 1 {
		return false
	}
	for name, index := range map[string]int{"capture": 0, "publish": 1, "clear_local": 2} {
		if got, ok := c.Exports[name]; !ok || got != index {
			return false
		}
	}
	if len(c.GlobalImports) != 2 || len(c.Globals) != 3 || len(c.GlobalExports) != 0 || len(c.ValueTypes) != 1 || c.tableCount() != 0 || len(c.extraTables) != 0 || len(c.tableExports) != 0 || c.memoryCount() != 0 || len(c.memoryExportMap()) != 0 || c.tagImportCount() != 0 || c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || len(c.customInstructions) != 0 || c.Names != nil || c.HasTable || c.HasMemory || c.tableImport != "" || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 {
		return false
	}
	if d := c.memoryDir; d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 || len(d.gcStructGlobals) != 0 || len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
		return false
	}
	funcType := &wasm.CompType{Kind: wasm.CompFunc}
	wantKey := wasm.StructuralFuncTypeKey(funcType)
	for i, sig := range c.Funcs {
		if len(sig.Params) != 0 || len(sig.Results) != 0 || !sig.HasTypeIndex || sig.TypeIndex != 0 || sig.unsafeCrossTail || c.FuncTypeID[i] != wantKey {
			return false
		}
	}
	typ := c.Types[0]
	if typ.RecGroup != 0 || !typ.Final || len(typ.Supers) != 0 || typ.HasDescribes || typ.Describes != 0 || typ.HasDescriptor || typ.Descriptor != 0 || typ.Kind != CompositeTypeFunction || len(typ.Params) != 0 || len(typ.Results) != 0 || len(typ.Fields) != 0 || typ.Array != (FieldTypeDescriptor{}) {
		return false
	}
	desc := c.GCTypeDescs[0]
	if desc.ID != 0 || desc.Kind != corergc.KindFunc || len(desc.Fields) != 0 || desc.Elem != 0 || desc.Size != 0 || desc.ElemSize != 0 || desc.Align != 0 || desc.HasRefs || !desc.Final || desc.Super != 0 || desc.HasSuper {
		return false
	}
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	for i, name := range []string{"source", "target"} {
		imp, global := c.GlobalImports[i], c.Globals[i]
		exact, err := c.globalExactType(i)
		if err != nil || exact != plain || imp.Module != "env" || imp.Name != name || !imp.HasValueType || imp.ValueTypeIndex != 0 || imp.Type != ValFuncRef || !imp.Mutable || global.Type != ValFuncRef || !global.Mutable || !global.HasValueType || global.ValueTypeIndex != 0 {
			return false
		}
		if global.HasInitFunc || global.InitFunc != 0 || global.HasInitGlobal || global.InitGlobal != 0 || len(global.InitExpr) != 0 || global.Bits != 0 || global.V128 != (V128{}) {
			return false
		}
	}
	local := c.Globals[2]
	localExact, err := c.globalExactType(2)
	if err != nil || localExact != plain || local.Type != ValFuncRef || !local.Mutable || !local.HasValueType || local.ValueTypeIndex != 0 || local.HasInitFunc || local.InitFunc != 0 || local.HasInitGlobal || local.InitGlobal != 0 || len(local.InitExpr) != 0 || local.Bits != 0 || local.V128 != (V128{}) {
		return false
	}
	required := CoreFeatureReferenceTypes | CoreFeatureMutableGlobal
	return c.requiredFeatures == required && c.requiredAMD64Features == 0 && compiledStructuralRequiredFeatures(c) == required && slices.Equal(c.Entry, []int{0, 288, 576}) && slices.Equal(c.InternalEntry, []int{0, 288, 576}) && rv64MutableFuncrefGlobalLocalTransferExecutionCode(c)
}

// ensureGuestLocalFuncrefGlobalOwner gives the admitted private cell the same
// ownership bookkeeping as an exported reference global without making the
// cell public. The bridge calls this only while guest execution is parked.
func (in *Instance) ensureGuestLocalFuncrefGlobalOwner(index int) error {
	if in == nil || !rv64MutableFuncrefGlobalLocalTransfer(in.c) || index != 2 || len(in.globalCells) != 3 || in.refStore == nil {
		return fmt.Errorf("invalid RV64 private funcref owner request")
	}
	g := in.globalCells[index]
	if g == nil || g.Type != ValFuncRef || !g.Mutable {
		return fmt.Errorf("invalid RV64 private funcref cell")
	}
	exact, err := in.c.globalExactType(index)
	if err != nil {
		return fmt.Errorf("RV64 private funcref exact type: %w", err)
	}
	in.lifeMu.Lock()
	if g.owner == nil {
		owner := &globalOwner{store: in.refStore, instance: in, typ: g.Type, mutable: g.Mutable, valueType: exact, types: in.c.Types, hasValueType: true}
		railshotLinkOwnedGlobalDescriptorHolderLocked(in, owner)
		g.owner = owner
	}
	owner := g.owner
	in.lifeMu.Unlock()
	owner.mu.Lock()
	valid := !owner.closed && owner.instance == in && owner.store == in.refStore && owner.typ == ValFuncRef && owner.mutable && owner.hasValueType && owner.valueType == exact
	owner.mu.Unlock()
	if !valid {
		return fmt.Errorf("invalid RV64 private funcref owner metadata")
	}
	in.markNativeControlShared()
	return nil
}

// copyGuestFuncrefDescriptorToLocal runs inside the already-admitted guest
// invocation. Starting a second invocation on the same instance would make a
// concurrent logical Close reject this otherwise valid in-flight write. The
// parked bridge has released native execution, so acquire only the native-state
// mutex and retain the outer invocation through the complete root transaction.
func (in *Instance) copyGuestFuncrefDescriptorToLocal(descriptor uint64) (err error) {
	if err := in.ensureGuestLocalFuncrefGlobalOwner(2); err != nil {
		return err
	}
	nativeMu := in.acquireInstanceNativeStateForHostAccess()
	var roots globalValueRoots
	var cleanup resourceCleanup
	defer func() {
		cleanup.run(nativeMu.Unlock)
		cleanup.run(func() { railshotFlushGlobalDescriptorDependencies(&roots.descriptorDependencies) })
		cleanup.run(roots.release)
		cleanup.finish()
	}()
	cleanup.run(func() { err = in.globalCells[2].copyGuestFuncrefDescriptorNoLease(in, descriptor, &roots) })
	return err
}

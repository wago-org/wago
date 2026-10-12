//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"fmt"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc"
)

var rv64MutableFuncrefStoreSelfCodeDigest = [sha256.Size]byte{0xcd, 0x20, 0x5d, 0x4c, 0x75, 0xe4, 0xbf, 0x7b, 0x61, 0x17, 0x87, 0x4d, 0xa5, 0xc1, 0x1e, 0xc1, 0xdf, 0x92, 0x0a, 0x6a, 0x9c, 0x6b, 0xad, 0x8c, 0x9b, 0x5d, 0xcb, 0xe2, 0x36, 0x08, 0x14, 0x08}

func rv64MutableFuncrefGlobalStoreSelfExecutionCode(c *Compiled) bool {
	return c != nil && len(c.code) == 940 && sha256.Sum256(c.code) == rv64MutableFuncrefStoreSelfCodeDigest
}

// rv64MutableFuncrefGlobalStoreSelf authenticates the first non-null guest
// reference write product. Its sole function can only publish its own function
// zero into imported mutable global zero through the parked ownership bridge.
func rv64MutableFuncrefGlobalStoreSelf(c *Compiled) bool {
	if c == nil || c.hasNoFunctionCode() || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled || c.nativeTable64ABI != nativeTable64NoABI || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata || !c.NeedsFuncRefDescs || c.dynamicFuncrefEscape || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) {
		return false
	}
	if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 1 || len(c.FuncTypeID) != 1 || len(c.Exports) != 1 || len(c.Types) != 1 || len(c.GCTypeDescs) != 1 {
		return false
	}
	if index, ok := c.Exports["store"]; !ok || index != 0 {
		return false
	}
	if len(c.GlobalImports) != 1 || len(c.Globals) != 1 || len(c.GlobalExports) != 1 || len(c.ValueTypes) != 1 || c.tableCount() != 0 || len(c.extraTables) != 0 || len(c.tableExports) != 0 || c.memoryCount() != 0 || len(c.memoryExportMap()) != 0 || c.tagImportCount() != 0 || c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 {
		return false
	}
	if index, ok := c.GlobalExports["cell"]; !ok || index != 0 {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || len(c.customInstructions) != 0 || c.Names != nil || c.HasTable || c.HasMemory || c.tableImport != "" || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 {
		return false
	}
	if d := c.memoryDir; d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 || len(d.gcStructGlobals) != 0 || len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
		return false
	}
	sig := c.Funcs[0]
	funcType := &wasm.CompType{Kind: wasm.CompFunc}
	if len(sig.Params) != 0 || len(sig.Results) != 0 || !sig.HasTypeIndex || sig.TypeIndex != 0 || sig.unsafeCrossTail || c.FuncTypeID[0] != wasm.StructuralFuncTypeKey(funcType) {
		return false
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
	imp, global := c.GlobalImports[0], c.Globals[0]
	exact, err := c.globalExactType(0)
	if err != nil || exact != plain || c.ValueTypes[0] != plain || !imp.HasValueType || imp.ValueTypeIndex != 0 || imp.Type != ValFuncRef || !imp.Mutable || global.Type != ValFuncRef || !global.Mutable || !global.HasValueType || global.ValueTypeIndex != 0 {
		return false
	}
	if global.HasInitFunc || global.InitFunc != 0 || global.HasInitGlobal || global.InitGlobal != 0 || len(global.InitExpr) != 0 || global.Bits != 0 || global.V128 != (V128{}) {
		return false
	}
	required := CoreFeatureReferenceTypes | CoreFeatureMutableGlobal
	return c.requiredFeatures == required && c.requiredAMD64Features == 0 && compiledStructuralRequiredFeatures(c) == required && slices.Equal(c.Entry, []int{0}) && slices.Equal(c.InternalEntry, []int{0}) && rv64MutableFuncrefGlobalStoreSelfExecutionCode(c)
}

// setGuestFuncrefDescriptor performs the same container transaction as
// SetValue without manufacturing a persistent public token for an already
// authenticated guest descriptor.
func (g *Global) setGuestFuncrefDescriptor(source *Instance, descriptor uint64) (err error) {
	if g == nil || g.owner == nil || source == nil {
		return fmt.Errorf("global has no compatible reference owner")
	}
	owner := g.owner.instance
	if owner != nil {
		if err := owner.beginInvocation(); err != nil {
			return fmt.Errorf("global owner instance is closed")
		}
	}
	nativeMu := &nativeExecutionMu
	if owner != nil {
		nativeMu = owner.acquireInstanceNativeStateForHostAccess()
	} else {
		nativeMu.Lock()
	}
	var roots globalValueRoots
	var cleanup resourceCleanup
	defer func() { finishGlobalValueAccess(nativeMu, owner, &roots, &cleanup) }()
	cleanup.run(func() { err = g.setGuestFuncrefDescriptorNoLease(source, descriptor, &roots) })
	return err
}

func (g *Global) setGuestFuncrefDescriptorNoLease(source *Instance, descriptor uint64, roots *globalValueRoots) error {
	o := g.owner
	o.mu.Lock()
	typ, mutable, store, containerOwner, closed := o.typ, o.mutable, o.store, o.instance, o.closed
	exact, exactTypes, hasExact := o.valueType, o.types, o.hasValueType
	consistent := g.Type == typ && g.Mutable == mutable
	o.mu.Unlock()
	if closed || !consistent || typ != ValFuncRef || !mutable || store == nil || source.refStore != store || descriptor == 0 || !rv64MutableFuncrefGlobalStoreSelf(source.c) {
		return fmt.Errorf("invalid RV64 guest funcref global store")
	}
	index, indexed := source.funcrefDescriptorIndex(descriptor)
	if !indexed || index != 0 || source.c.NumImports != 0 {
		return fmt.Errorf("RV64 guest funcref store descriptor is not local function zero")
	}
	if _, valid := rv64PublishedDescriptor(source, descriptor); !valid {
		return fmt.Errorf("RV64 guest funcref store descriptor is not published")
	}
	actual, actualTypes, valid := instanceFuncrefExactType(source, descriptor)
	if !valid || hasExact && !valueTypeSubtype(actual, actualTypes, exact, exactTypes) {
		return fmt.Errorf("RV64 guest funcref store descriptor type mismatch")
	}
	store.mu.Lock()
	retainedOwner, canonical, valid := store.canonicalFuncrefOwnerLocked(source, descriptor)
	valid = valid && retainedOwner == source && canonical == descriptor && store.hasInstanceResourcesLocked(source) && source.retainResourceRootForFinalization()
	store.mu.Unlock()
	if !valid {
		return fmt.Errorf("RV64 guest funcref store descriptor owner is unavailable")
	}
	bootstrap := roots.record(source)
	if retainedOwner == containerOwner {
		retainedOwner = nil
	}
	return g.publishReferenceValueNoLease(ValFuncRef, true, descriptor, retainedOwner, bootstrap, roots)
}

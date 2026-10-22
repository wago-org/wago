//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"fmt"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc"
)

var rv64MutableFuncrefCopyCodeDigest = [sha256.Size]byte{0x40, 0xe0, 0x3e, 0x1a, 0xca, 0x7a, 0x24, 0x6f, 0xa5, 0x36, 0xdc, 0x6f, 0x6b, 0x44, 0xa3, 0x70, 0x1b, 0x8f, 0x2b, 0x0f, 0xe3, 0xa1, 0x05, 0x63, 0xd1, 0x6a, 0x50, 0x39, 0x05, 0x00, 0xf4, 0x14}

func rv64MutableFuncrefGlobalCopyExecutionCode(c *Compiled) bool {
	return c != nil && len(c.code) == 876 && sha256.Sum256(c.code) == rv64MutableFuncrefCopyCodeDigest
}

// rv64MutableFuncrefGlobalCopy authenticates the first cross-container guest
// reference write product. Its sole function copies imported mutable funcref
// global zero into imported mutable funcref global one through the parked
// ownership bridge; no reference value escapes through a callable signature.
func rv64MutableFuncrefGlobalCopy(c *Compiled) bool {
	return rv64MutableFuncrefGlobalTransferMetadata(c, "copy") && rv64MutableFuncrefGlobalCopyExecutionCode(c)
}

func rv64MutableFuncrefGlobalTransferMetadata(c *Compiled, export string) bool {
	if c == nil || c.hasNoFunctionCode() || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled || c.nativeTable64ABI != nativeTable64NoABI || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata || c.NeedsFuncRefDescs || c.dynamicFuncrefEscape || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) {
		return false
	}
	if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 1 || len(c.FuncTypeID) != 1 || len(c.Exports) != 1 || len(c.Types) != 1 || len(c.GCTypeDescs) != 1 {
		return false
	}
	if index, ok := c.Exports[export]; !ok || index != 0 {
		return false
	}
	if len(c.GlobalImports) != 2 || len(c.Globals) != 2 || len(c.GlobalExports) != 0 || len(c.ValueTypes) != 1 || c.tableCount() != 0 || len(c.extraTables) != 0 || len(c.tableExports) != 0 || c.memoryCount() != 0 || len(c.memoryExportMap()) != 0 || c.tagImportCount() != 0 || c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 {
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
	required := CoreFeatureReferenceTypes | CoreFeatureMutableGlobal
	return c.requiredFeatures == required && c.requiredAMD64Features == 0 && compiledStructuralRequiredFeatures(c) == required && slices.Equal(c.Entry, []int{0}) && slices.Equal(c.InternalEntry, []int{0})
}

// copyGuestFuncrefDescriptor authenticates a descriptor already loaded by the
// exact copy image and transfers one canonical owner root into the target. The
// store lookup acquires the root before any source container can retire it.
func (g *Global) copyGuestFuncrefDescriptor(source *Instance, descriptor uint64) (err error) {
	return g.copyGuestFuncrefDescriptorWithOwner(source, descriptor, nil)
}

func (g *Global) copyGuestFuncrefDescriptorWithOwner(source *Instance, descriptor uint64, descriptorOwner *Instance) (err error) {
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
	cleanup.run(func() { err = g.copyGuestFuncrefDescriptorNoLease(source, descriptor, descriptorOwner, &roots) })
	return err
}

func (g *Global) copyGuestFuncrefDescriptorNoLease(source *Instance, descriptor uint64, descriptorOwner *Instance, roots *globalValueRoots) error {
	o := g.owner
	o.mu.Lock()
	typ, mutable, store, containerOwner, closed := o.typ, o.mutable, o.store, o.instance, o.closed
	exact, exactTypes, hasExact := o.valueType, o.types, o.hasValueType
	consistent := g.Type == typ && g.Mutable == mutable
	o.mu.Unlock()
	if closed || !consistent || typ != ValFuncRef || !mutable || store == nil || source.refStore != store || (!rv64PrimitiveGuestGlobalTarget(source, g) && !rv64MutableFuncrefGlobalCopy(source.c) && !rv64MutableFuncrefGlobalMove(source.c) && !rv64MutableFuncrefGlobalBidirectionalCopy(source.c) && !rv64MutableFuncrefGlobalLocalTransfer(source.c) && !rv64MutableFuncrefGlobalLocalExport(source.c) && !rv64MutableFuncrefGlobalLocalObserve(source.c) && !rv64MutableFuncrefGlobalLocalCall(source.c)) {
		return fmt.Errorf("invalid RV64 guest funcref global copy")
	}
	if descriptor == 0 {
		return g.publishReferenceValueNoLease(ValFuncRef, true, 0, nil, -1, roots)
	}
	retainedOwner := store.retainDescriptorOwnerForFinalization(descriptor)
	if retainedOwner == nil {
		if descriptorOwner == nil {
			return fmt.Errorf("RV64 guest funcref copy descriptor owner is unavailable")
		}
		if _, valid := rv64PublishedDescriptor(descriptorOwner, descriptor); !valid {
			return fmt.Errorf("RV64 guest funcref copy descriptor is not published")
		}
		actual, actualTypes, valid := instanceFuncrefExactType(descriptorOwner, descriptor)
		if !valid || hasExact && !valueTypeSubtype(actual, actualTypes, exact, exactTypes) {
			return fmt.Errorf("RV64 guest funcref copy descriptor type mismatch")
		}
		return g.publishGuestFuncrefProxyNoLease(source, descriptor, containerOwner, roots)
	}
	bootstrap := roots.record(retainedOwner)
	if _, valid := rv64PublishedDescriptor(retainedOwner, descriptor); !valid {
		return fmt.Errorf("RV64 guest funcref copy descriptor is not published")
	}
	actual, actualTypes, valid := instanceFuncrefExactType(retainedOwner, descriptor)
	if !valid || hasExact && !valueTypeSubtype(actual, actualTypes, exact, exactTypes) {
		return fmt.Errorf("RV64 guest funcref copy descriptor type mismatch")
	}
	if retainedOwner == containerOwner {
		retainedOwner = nil
	}
	return g.publishReferenceValueNoLease(ValFuncRef, true, descriptor, retainedOwner, bootstrap, roots)
}

// publishGuestFuncrefProxyNoLease commits a descriptor authenticated by the
// active native scope. The writer remains its bounded proxy until close-time
// dependency adoption can replace it with the cross-store producer.
func (g *Global) publishGuestFuncrefProxyNoLease(source *Instance, descriptor uint64, containerOwner *Instance, roots *globalValueRoots) error {
	if source == containerOwner || !source.retainResourceRootForFinalization() {
		return fmt.Errorf("RV64 guest funcref copy proxy is unavailable")
	}
	bootstrap := roots.record(source)
	o := g.owner
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed || len(g.cell) < 8 || o.typ != ValFuncRef || !o.mutable || g.Type != ValFuncRef || !g.Mutable {
		return fmt.Errorf("global reference owner metadata is invalid")
	}
	writeGlobalObject(g, ValFuncRef, descriptor)
	for root, state := range o.retained {
		if root == source {
			continue
		}
		delete(o.retained, root)
		railshotRetireGlobalDescriptorDependencyLocked(o, root, state, &roots.descriptorDependencies)
	}
	if o.retained == nil {
		o.retained = make(map[*Instance]*retainedInstanceRoot)
	}
	state := o.retained[source]
	if state == nil {
		state = &retainedInstanceRoot{}
		o.retained[source] = state
		railshotRecordGlobalDescriptorDependencyLocked(o, source, state, &roots.descriptorDependencies)
		roots.transfer(bootstrap)
		source.markNativeControlShared()
	}
	state.precise = false
	state.proxyDescriptors = map[uint64]struct{}{descriptor: {}}
	railshotPublishGlobalDescriptorDependencyLocked(o, state, &roots.descriptorDependencies)
	return nil
}

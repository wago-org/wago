//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"fmt"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc"
)

var rv64MutableFuncrefClearCodeDigest = [sha256.Size]byte{0xa5, 0xcd, 0x4d, 0x1f, 0x87, 0x04, 0xf5, 0x0d, 0x07, 0xac, 0x6a, 0x1c, 0x84, 0xd2, 0x6b, 0xc8, 0x88, 0x2f, 0x10, 0x8e, 0x50, 0x6f, 0x70, 0x8b, 0x35, 0x60, 0x3e, 0x6b, 0xb2, 0xa5, 0x6c, 0x4c}

func rv64MutableFuncrefGlobalClearExecutionCode(c *Compiled) bool {
	return c != nil && len(c.code) == 844 && sha256.Sum256(c.code) == rv64MutableFuncrefClearCodeDigest
}

// rv64MutableFuncrefGlobalClear authenticates the null guest reference write
// product. Its function can only store null into imported mutable global zero,
// and the image-local bridge executes the existing container transaction while
// native execution is parked.
func rv64MutableFuncrefGlobalClear(c *Compiled) bool {
	if c == nil || c.hasNoFunctionCode() || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled || c.nativeTable64ABI != nativeTable64NoABI || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata || c.NeedsFuncRefDescs || c.dynamicFuncrefEscape || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) {
		return false
	}
	if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 1 || len(c.FuncTypeID) != 1 || len(c.Exports) != 1 || len(c.Types) != 1 || len(c.GCTypeDescs) != 1 {
		return false
	}
	if index, ok := c.Exports["clear"]; !ok || index != 0 {
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
	return c.requiredFeatures == required && c.requiredAMD64Features == 0 && compiledStructuralRequiredFeatures(c) == required && slices.Equal(c.Entry, []int{0}) && slices.Equal(c.InternalEntry, []int{0}) && rv64MutableFuncrefGlobalClearExecutionCode(c)
}

func railshotUsesGuestGlobalStoreHelper(c *Compiled) bool {
	return rv64PrimitiveFunctionGlobalStores(c) || rv64MutableFuncrefGlobalClear(c) || rv64MutableFuncrefGlobalStoreSelf(c) || rv64MutableFuncrefGlobalCopy(c) || rv64MutableFuncrefGlobalMove(c) || rv64MutableFuncrefGlobalBidirectionalCopy(c) || rv64MutableFuncrefGlobalLocalCopy(c) || rv64MutableFuncrefGlobalLocalPublish(c) || rv64MutableFuncrefGlobalLocalTransfer(c) || rv64MutableFuncrefGlobalLocalExport(c) || rv64MutableFuncrefGlobalLocalObserve(c) || rv64MutableFuncrefGlobalLocalCall(c)
}

// retainGuestGlobalStoreDescriptor closes the raw-load/park ownership window
// for exact guest-global helper products. It runs while native execution is
// still leased, before a competing host writer can retire the container root.
// The caller owns the returned physical root and releases it while still
// parked, before native execution resumes; destination publication acquires its
// own independent unit.
func (in *Instance) retainGuestGlobalStoreDescriptor(dispatch uint32, args, results []uint64) (*Instance, error) {
	if dispatch != hostFuncRefDispatchBit || in == nil || !railshotUsesGuestGlobalStoreHelper(in.c) || len(results) != 0 {
		return nil, nil
	}
	var descriptor uint64
	switch {
	case len(args) == 2 && args[1] < uint64(len(in.globalCells)) && rv64PrimitiveMutableFunctionGlobal(in.c, args[1]):
		descriptor = args[0]
	case rv64MutableFuncrefGlobalClear(in.c) && len(args) == 0 && len(in.globalCells) == 1:
		return nil, nil
	case rv64MutableFuncrefGlobalStoreSelf(in.c) && len(args) == 1 && len(in.globalCells) == 1:
		descriptor = args[0]
	case rv64MutableFuncrefGlobalCopy(in.c) && len(args) == 1 && len(in.globalCells) == 2:
		descriptor = args[0]
	case rv64MutableFuncrefGlobalMove(in.c) && len(args) == 1 && len(in.globalCells) == 2:
		descriptor = args[0]
	case rv64MutableFuncrefGlobalMove(in.c) && len(args) == 0 && len(in.globalCells) == 2:
		return nil, nil
	case rv64MutableFuncrefGlobalBidirectionalCopy(in.c) && len(args) == 2 && len(in.globalCells) == 2 && args[1] < 2:
		descriptor = args[0]
	case rv64MutableFuncrefGlobalLocalCopy(in.c) && len(args) == 1 && len(in.globalCells) == 2:
		descriptor = args[0]
	case rv64MutableFuncrefGlobalLocalPublish(in.c) && len(args) == 1 && len(in.globalCells) == 2:
		descriptor = args[0]
	case rv64MutableFuncrefGlobalLocalTransfer(in.c) && len(args) == 2 && len(in.globalCells) == 3 && (args[1] == 1 || args[1] == 2):
		descriptor = args[0]
	case rv64MutableFuncrefGlobalLocalExport(in.c) && len(args) == 1 && len(in.globalCells) == 1:
		descriptor = args[0]
	case rv64MutableFuncrefGlobalLocalObserve(in.c) && len(args) == 1 && len(in.globalCells) == 1:
		descriptor = args[0]
	case rv64MutableFuncrefGlobalLocalCall(in.c) && len(args) == 1 && len(in.globalCells) == 1:
		descriptor = args[0]
	default:
		return nil, fmt.Errorf("invalid RV64 guest global store bridge")
	}
	if descriptor == 0 {
		return nil, nil
	}
	if in.refStore != nil {
		if owner := in.refStore.retainDescriptorOwnerForFinalization(descriptor); owner != nil {
			return owner, nil
		}
	}
	lease := railshotCurrentNativeTableTargets(in)
	if lease.targets == nil {
		return nil, fmt.Errorf("RV64 guest global store descriptor owner is unavailable")
	}
	var retained *Instance
	for _, candidate := range lease.targets.owners[:lease.targets.count] {
		if _, valid := rv64PublishedDescriptor(candidate, descriptor); !valid {
			continue
		}
		if retained != nil && retained != candidate {
			return nil, fmt.Errorf("RV64 guest global store descriptor owner is ambiguous")
		}
		retained = candidate
	}
	if retained == nil || !retained.retainResourceRootForFinalization() {
		return nil, fmt.Errorf("RV64 guest global store descriptor owner is unavailable")
	}
	return retained, nil
}

// The zero-index host-funcref dispatch encoding is permanently invalid for a
// public HostFuncRef. The exact authenticated image uses it as a private bridge
// while retaining the ordinary parked-host lease and context restoration path.
func (in *Instance) dispatchGuestGlobalStore(dispatch uint32, args, results []uint64) bool {
	return in.dispatchGuestGlobalStoreWithOwner(dispatch, args, results, nil)
}

func (in *Instance) dispatchGuestGlobalStoreWithOwner(dispatch uint32, args, results []uint64, descriptorOwner *Instance) bool {
	if dispatch != hostFuncRefDispatchBit {
		return false
	}
	if in == nil || len(results) != 0 {
		panic(invalidHostReference{err: fmt.Errorf("invalid RV64 guest global store bridge")})
	}
	var err error
	switch {
	case len(args) == 2 && args[1] < uint64(len(in.globalCells)) && rv64PrimitiveMutableFunctionGlobal(in.c, args[1]):
		index := int(args[1])
		if index < len(in.c.GlobalImports) {
			err = in.globalCells[index].copyGuestFuncrefDescriptorWithOwner(in, args[0], descriptorOwner)
		} else {
			err = in.copyGuestFuncrefDescriptorToLocal(index, args[0])
		}
	case rv64MutableFuncrefGlobalClear(in.c) && len(args) == 0 && len(in.globalCells) == 1:
		err = in.globalCells[0].SetValue(ValueFuncRef(NullFuncRef()))
	case rv64MutableFuncrefGlobalStoreSelf(in.c) && len(args) == 1 && len(in.globalCells) == 1:
		err = in.globalCells[0].setGuestFuncrefDescriptor(in, args[0])
	case rv64MutableFuncrefGlobalCopy(in.c) && len(args) == 1 && len(in.globalCells) == 2:
		err = in.globalCells[1].copyGuestFuncrefDescriptorWithOwner(in, args[0], descriptorOwner)
	case rv64MutableFuncrefGlobalMove(in.c) && len(args) == 1 && len(in.globalCells) == 2:
		err = in.globalCells[1].copyGuestFuncrefDescriptorWithOwner(in, args[0], descriptorOwner)
	case rv64MutableFuncrefGlobalMove(in.c) && len(args) == 0 && len(in.globalCells) == 2:
		err = in.globalCells[0].SetValue(ValueFuncRef(NullFuncRef()))
	case rv64MutableFuncrefGlobalBidirectionalCopy(in.c) && len(args) == 2 && len(in.globalCells) == 2 && args[1] < 2:
		err = in.globalCells[args[1]].copyGuestFuncrefDescriptorWithOwner(in, args[0], descriptorOwner)
	case rv64MutableFuncrefGlobalLocalCopy(in.c) && len(args) == 1 && len(in.globalCells) == 2:
		err = in.globalCells[0].setGuestFuncrefDescriptor(in, args[0])
	case rv64MutableFuncrefGlobalLocalPublish(in.c) && len(args) == 1 && len(in.globalCells) == 2:
		if args[0] == 0 {
			err = in.globalCells[0].SetValue(ValueFuncRef(NullFuncRef()))
		} else {
			err = in.globalCells[0].setGuestFuncrefDescriptor(in, args[0])
		}
	case rv64MutableFuncrefGlobalLocalTransfer(in.c) && len(args) == 2 && len(in.globalCells) == 3 && (args[1] == 1 || args[1] == 2):
		if args[1] == 2 {
			err = in.copyGuestFuncrefDescriptorToLocal(2, args[0])
		} else {
			err = in.globalCells[args[1]].copyGuestFuncrefDescriptorWithOwner(in, args[0], descriptorOwner)
		}
	case rv64MutableFuncrefGlobalLocalExport(in.c) && len(args) == 1 && len(in.globalCells) == 1:
		err = in.copyGuestFuncrefDescriptorToLocal(0, args[0])
	case rv64MutableFuncrefGlobalLocalObserve(in.c) && len(args) == 1 && len(in.globalCells) == 1:
		err = in.copyGuestFuncrefDescriptorToLocal(0, args[0])
	case rv64MutableFuncrefGlobalLocalCall(in.c) && len(args) == 1 && len(in.globalCells) == 1:
		err = in.copyGuestFuncrefDescriptorToLocal(0, args[0])
	default:
		panic(invalidHostReference{err: fmt.Errorf("invalid RV64 guest global store bridge")})
	}
	if err != nil {
		panic(invalidHostReference{err: fmt.Errorf("RV64 guest global store: %w", err)})
	}
	return true
}

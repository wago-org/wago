//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
	rv64runtime "github.com/wago-org/wago/src/core/runtime/riscv64"
)

// rv64StateOnlyExternrefGlobals is the persisted-artifact witness for a
// bounded directory containing at least one immutable null externref global
// plus optional immutable literal scalar globals in an otherwise empty module.
// It must remain declaration-only: executable reference carriers and every
// imported or initializer-dependent owner path require separate qualification.
func rv64StateOnlyExternrefGlobals(c *Compiled) bool {
	if c == nil || !c.hasNoFunctionCode() || c.NumImports != 0 || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Types) != 0 || len(c.GCTypeDescs) != 0 || len(c.FuncTypeID) != 0 || len(c.Exports) != 0 {
		return false
	}
	if len(c.Globals) == 0 || len(c.Globals) > 256 || len(c.GlobalImports) != 0 || len(c.GlobalExports) > 256 || len(c.ValueTypes) == 0 || len(c.ValueTypes) > 5 || c.tableCount() != 0 || c.memoryCount() != 0 || c.tagImportCount() != 0 || c.HasStart {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 {
		return false
	}
	for _, index := range c.GlobalExports {
		if index < 0 || index >= len(c.Globals) {
			return false
		}
	}
	plainExternref, _ := valueTypeDescriptorFromValType(ValExternRef)
	var kinds, used uint8
	for _, exact := range c.ValueTypes {
		if exact.Kind > ValueTypeF64 && exact != plainExternref || exact.Kind <= ValueTypeF64 && exact.Ref != (ReferenceTypeDescriptor{}) || kinds&(1<<exact.Kind) != 0 {
			return false
		}
		kinds |= 1 << exact.Kind
	}
	required := CoreFeatureReferenceTypes
	hasExternref := false
	for i, g := range c.Globals {
		exact, err := c.globalExactType(i)
		plain, ok := valueTypeDescriptorFromValType(g.Type)
		if err != nil || !ok || exact != plain || !g.HasValueType || uint64(g.ValueTypeIndex) >= uint64(len(c.ValueTypes)) || g.Mutable || g.V128 != (V128{}) || g.HasInitFunc || g.InitFunc != 0 || len(g.InitExpr) != 0 {
			return false
		}
		used |= 1 << g.ValueTypeIndex
		if g.Type != ValExternRef {
			if g.Type != ValI32 && g.Type != ValI64 && g.Type != ValF32 && g.Type != ValF64 || g.HasInitGlobal || g.InitGlobal != 0 || (g.Type == ValI32 || g.Type == ValF32) && g.Bits>>32 != 0 {
				return false
			}
			continue
		}
		hasExternref = true
		if exact != plainExternref || g.Bits != 0 {
			return false
		}
		if g.HasInitGlobal {
			if g.InitGlobal < 0 || g.InitGlobal >= i || c.Globals[g.InitGlobal].Type != ValExternRef {
				return false
			}
			required |= CoreFeatureExtendedConstExpressions
		} else if g.InitGlobal != 0 {
			return false
		}
	}
	return hasExternref && used == uint8((1<<len(c.ValueTypes))-1) && c.requiredFeatures == required && compiledStructuralRequiredFeatures(c) == required
}

// Loading and hand-built metadata must enforce the same bounded public ABI as
// source compilation before any executable image or host thunk is published.
func railshotValidateCompiled(c *Compiled) error {
	reject := func(detail string) error { return fmt.Errorf("%w: public RV64 %s", railshot.ErrUnsupported, detail) }
	if !c.nativeContextTrapABI {
		return reject("native-context trap ABI is required")
	}
	if len(c.code) > railshot.MaxCodeBytes || len(c.Funcs) > railshot.MaxFunctions || len(c.Types) > railshot.MaxFunctions || len(c.GCTypeDescs) > railshot.MaxFunctions || len(c.Globals) > 256 {
		return reject("bounded artifact shape")
	}
	if len(c.Funcs) == 0 && c.NumImports == 0 && !c.hasNoFunctionCode() {
		return reject("state-only artifact contains code or function entries")
	}
	if c.activeDataCount() > rv64runtime.MaxDataSegments || len(c.PassiveData) > rv64runtime.MaxDataSegments {
		return reject("data descriptor count bound")
	}
	remaining := railshot.MaxModuleBytes
	for i := 0; i < c.activeDataCount(); i++ {
		data := c.activeDataAt(i)
		if len(data.Bytes) > remaining {
			return reject("data payload size bound")
		}
		remaining -= len(data.Bytes)
	}
	for _, data := range c.PassiveData {
		if len(data.Bytes) > remaining {
			return reject("data payload size bound")
		}
		remaining -= len(data.Bytes)
	}
	if c.NumImports < 0 || c.NumImports > railshot.MaxHostFunctionImports || len(c.Imports) != c.NumImports || len(c.importFuncSigs) != c.NumImports || c.NumImports+len(c.Funcs) > railshot.MaxFunctions || c.dynamicImports != (c.NumImports > 0) {
		return reject("dynamic function import shape")
	}
	if c.NumImports+len(c.GlobalImports)+c.memoryImportCount()+c.tableImportCount() > railshot.MaxProductionImports {
		return reject("aggregate import bound")
	}
	if err := railshotValidateReferenceCalls(c); err != nil {
		return reject(err.Error())
	}
	if err := rv64ValidateNativeTail(c); err != nil {
		return reject(err.Error())
	}
	if err := rv64ValidateReferenceFunctions(c); err != nil {
		return reject(err.Error())
	}
	if m := rv64NativeTailModule(c); m != nil {
		for _, p := range m.Plans {
			if p.Kind > 2 {
				return reject("dynamic tail requires a qualified native-owner plan")
			}
		}
	}
	vectorProfile := c.requiredFeatures.IsEnabled(CoreFeatureSIMD)
	if c.NumImports != 0 {
		if !c.needsFuncRefContextHeader {
			return reject("host imports require an instance context anchor")
		}
		maxImportArity := coreruntime.MaxHostArity
		if vectorProfile {
			maxImportArity = 128
		}
		for _, sig := range c.importFuncSigs {
			if len(sig.Params) > maxImportArity || len(sig.Results) > maxImportArity {
				return reject("function import signature arity")
			}
			for _, values := range [][]ValType{sig.Params, sig.Results} {
				for _, typ := range values {
					if typ != ValI32 && typ != ValI64 && typ != ValF32 && typ != ValF64 && (!vectorProfile || typ != ValV128) {
						return reject("unsupported function import signature")
					}
				}
			}
		}
	}
	if c.HasStart {
		if _, err := compiledStartIndex(c); err != nil {
			return reject(err.Error())
		}
	}

	if c.boundsMode == BoundsChecksSignalsBased || c.genericGCFrameRoots() != nil || c.usesDynamicFuncRefTest() || c.memoryCount() > railshot.MaxProductionMemories {
		return reject("resource/host/reference lowering is pending")
	}
	if len(c.GlobalImports) > len(c.Globals) {
		return reject("global import index shape")
	}
	for i, global := range c.GlobalImports {
		reference := global.Type == ValFuncRef && !global.Mutable && c.tableCount() <= 1
		if reference {
			plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
			exact, err := exactValueType(global.Type, global.HasValueType, global.ValueTypeIndex, c.ValueTypes, c.Types)
			if err != nil || exact != plain {
				return reject("reference global import exact type")
			}
		}
		if (!reference && global.Type != ValI32 && global.Type != ValI64 && global.Type != ValF32 && global.Type != ValF64 && (!vectorProfile || global.Type != ValV128)) || global.Type != c.Globals[i].Type || global.Mutable != c.Globals[i].Mutable {
			return reject("global import type/mutability shape")
		}
	}
	for _, desc := range c.GCTypeDescs {
		if desc.Kind != gc.KindFunc || !desc.Final || desc.HasSuper {
			return reject("GC descriptor lowering is pending")
		}
	}
	qualifiedFeatures := platformCoreFeatures() | CoreFeatureTailCall
	if c.nativeTable64ABI == nativeTable64SizeABI && !rv64OwnedTwoTable32Size(c) && (!c.requiredFeatures.IsEnabled(CoreFeatureTable64) || !rv64OwnedTable64Size(c) && !rv64OwnedTwoTable64Size(c)) {
		return reject("table.size compiler witness requires its exact owned or indexed two-table profile")
	}
	if c.nativeTable64ABI == nativeTable64GrowABI && !rv64OwnedTwoTable32Grow(c) && (!c.requiredFeatures.IsEnabled(CoreFeatureTable64) || !rv64OwnedTable64Grow(c) && !rv64OwnedTwoTable64Grow(c)) {
		return reject("table.grow compiler witness requires its exact owned or indexed two-table profile")
	}
	if c.nativeTable64ABI == nativeTable64GetSetABI && !rv64OwnedTwoTable32GetSet(c) && (!c.requiredFeatures.IsEnabled(CoreFeatureTable64) || !rv64OwnedTable64GetSet(c) && !rv64OwnedTwoTable64GetSet(c)) {
		return reject("table.get/set compiler witness requires its exact owned or indexed two-table profile")
	}
	if c.nativeTable64ABI == nativeTable64IndirectABI && !rv64OwnedTwoTable32Indirect(c) && (!c.requiredFeatures.IsEnabled(CoreFeatureTable64) || !rv64OwnedTable64Indirect(c) && !rv64OwnedTwoTable64Indirect(c) && !rv64ImportedTable64Indirect(c)) {
		return reject("table indirect-call compiler witness requires its exact owned, indexed two-table32/table64, or imported table64 profile")
	}
	if c.nativeTable64ABI == nativeTable64FillABI && !rv64OwnedTwoTable32Fill(c) && (!c.requiredFeatures.IsEnabled(CoreFeatureTable64) || !rv64OwnedTable64Fill(c) && !rv64OwnedTwoTable64Fill(c)) {
		return reject("table.fill compiler witness requires its exact owned or indexed two-table profile")
	}
	if c.nativeTable64ABI == nativeTable64CopyABI && !rv64OwnedTwoTable32Copy(c) && (!c.requiredFeatures.IsEnabled(CoreFeatureTable64) || !rv64OwnedTable64Copy(c) && !rv64OwnedTwoTable64Copy(c)) {
		return reject("table.copy compiler witness requires its exact owned or indexed two-table profile")
	}
	if c.nativeTable64ABI == nativeTable64InitDropABI && !rv64OwnedTwoTable32InitDrop(c) && (!c.requiredFeatures.IsEnabled(CoreFeatureTable64) || !rv64OwnedTable64InitDrop(c) && !rv64OwnedTwoTable64InitDrop(c)) {
		return reject("table.init/drop compiler witness requires its exact owned or indexed two-table profile")
	}
	if c.nativeTable64ABI == nativeTable64ImportedSizeABI && (!c.requiredFeatures.IsEnabled(CoreFeatureTable64) || !rv64ImportedTable64Size(c)) {
		return reject("table64.size compiler witness requires its exact imported table64 profile")
	}
	if c.nativeTable64ABI == nativeTable64ImportedGetSetABI && (!c.requiredFeatures.IsEnabled(CoreFeatureTable64) || !rv64ImportedTable64GetSet(c)) {
		return reject("table64.get/set compiler witness requires its exact imported table64 profile")
	}
	if c.nativeTable64ABI == nativeTable64ImportedGrowABI && (!c.requiredFeatures.IsEnabled(CoreFeatureTable64) || !rv64ImportedTable64Grow(c)) {
		return reject("table64.grow compiler witness requires its exact imported table64 profile")
	}
	if c.nativeTable64ABI == nativeTable64ImportedFillABI && (!c.requiredFeatures.IsEnabled(CoreFeatureTable64) || !rv64ImportedTable64Fill(c)) {
		return reject("table64.fill compiler witness requires its exact imported table64 profile")
	}
	if c.nativeTable64ABI == nativeTable64ImportedCopyABI && (!c.requiredFeatures.IsEnabled(CoreFeatureTable64) || !rv64ImportedTable64Copy(c)) {
		return reject("table64.copy compiler witness requires its exact imported table64 profile")
	}
	if c.nativeTable64ABI == nativeTable64ImportedInitDropABI && (!c.requiredFeatures.IsEnabled(CoreFeatureTable64) || !rv64ImportedTable64InitDrop(c)) {
		return reject("table64.init/drop compiler witness requires its exact imported table64 profile")
	}
	if c.nativeTable64ABI > nativeTable64ImportedInitDropABI {
		return reject("unknown table64 compiler witness")
	}
	if c.requiredFeatures.IsEnabled(CoreFeatureTable64) && !rv64QualifiedTable64(c) {
		return reject("table64 requires an exact qualified owned-table profile")
	}
	if rv64HasReferenceTail(c) || c.nativeReferenceCallABI || c.nativeReferenceControlABI {
		qualifiedFeatures |= CoreFeatureTypedFunctionReferences
	}
	declarativeTyped := false
	if c.hasNoFunctionCode() && c.requiredFeatures.IsEnabled(CoreFeatureTypedFunctionReferences) {
		var err error
		declarativeTyped, err = rv64ValidateDeclarativeControlTypes(c)
		if err != nil {
			return reject(err.Error())
		}
		if !declarativeTyped {
			return reject("declarative typed feature requires an exact type witness")
		}
		qualifiedFeatures |= CoreFeatureTypedFunctionReferences
	}
	if c.requiredFeatures & ^qualifiedFeatures != 0 || c.requiredAMD64Features != 0 || c.preparedIsolatedTables {
		return reject("feature or ISA contract")
	}
	for i, entry := range c.Entry {
		if entry&15 != 0 || i >= len(c.InternalEntry) || c.InternalEntry[i] != entry {
			return reject("entry is not an aligned framed wrapper")
		}
	}
	if vectorProfile {
		if len(c.GlobalExports) > railshot.MaxFunctions {
			return reject("global export count bound")
		}
		for _, index := range c.GlobalExports {
			if index < 0 || index >= len(c.Globals) {
				return reject("global export index bound")
			}
		}
	}
	maxArity := 256
	if vectorProfile {
		maxArity = 128
	}
	declarationExtern := rv64Core3ReferenceDeclarations(c)
	for _, sig := range c.Funcs {
		if len(sig.Params) > maxArity || len(sig.Results) > maxArity {
			return reject("signature arity")
		}
		for _, types := range [][]ValType{sig.Params, sig.Results} {
			for _, typ := range types {
				if typ != ValI32 && typ != ValI64 && typ != ValF32 && typ != ValF64 && (!vectorProfile || typ != ValV128) && (!c.nativeReferenceFunctionABI || typ != ValFuncRef) && (!declarationExtern || typ != ValExternRef) {
					return reject("unsupported signature")
				}
			}
		}
	}
	if !declarativeTyped {
		if err := rv64ValidateDeclaredControlTypes(c); err != nil {
			return reject(err.Error())
		}
	}
	if err := rv64ValidateOwnedTable(c); err != nil {
		return reject(err.Error())
	}
	localMutableReferences, err := rv64ValidateLocalMutableReferences(c)
	if err != nil {
		return reject(err.Error())
	}
	hasReferenceGlobal := false
	stateOnlyExternrefs := rv64StateOnlyExternrefGlobals(c)
	for i, global := range c.Globals {
		if global.Type == ValExternRef {
			if !stateOnlyExternrefs {
				return reject("externref globals require the exact state-only null directory")
			}
			continue
		}
		if global.Type == ValFuncRef {
			hasReferenceGlobal = true
			plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
			exact, err := c.globalExactType(i)
			if err != nil || exact != plain || global.Mutable && (!localMutableReferences || i < len(c.GlobalImports)) || len(global.InitExpr) != 0 || global.Bits != 0 || global.V128 != (V128{}) || c.tableCount() > 1 || !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes) || global.HasInitFunc && (uint64(global.InitFunc) >= uint64(c.NumImports+len(c.Funcs)) || !c.needsFuncRefDescs()) {
				return reject("immutable reference global and retained references required")
			}
			if i < len(c.GlobalImports) && (global.HasInitFunc || global.HasInitGlobal) {
				return reject("reference import initializer")
			}
			if global.HasInitGlobal && (global.HasInitFunc || !rv64InitializerGlobal(c, global.InitGlobal, ValFuncRef, i)) {
				return reject("reference initializer requires an earlier immutable global")
			}
			continue
		}
		if (global.Type != ValI32 && global.Type != ValI64 && global.Type != ValF32 && global.Type != ValF64 && (!vectorProfile || global.Type != ValV128)) || global.HasInitFunc {
			return reject("global initializer lowering is pending")
		}
		if len(global.InitExpr) != 0 {
			if i < len(c.GlobalImports) || global.HasInitGlobal {
				return reject("conflicting global initializer")
			}
			if err := rv64ValidateIntegerInitializer(c, global.InitExpr, global.Type, i); err != nil {
				return reject(err.Error())
			}
		}
		if global.HasInitGlobal && (i < len(c.GlobalImports) || !rv64InitializerGlobal(c, global.InitGlobal, global.Type, i)) {
			return reject("global initializer requires an available immutable global of the exact type")
		}
	}
	if hasReferenceGlobal {
		if len(c.GlobalExports) > railshot.MaxFunctions {
			return reject("reference-global export count bound")
		}
		for _, index := range c.GlobalExports {
			if index < 0 || index >= len(c.Globals) {
				return reject("reference global export index")
			}

		}
	}
	for i := 0; i < c.activeDataCount(); i++ {
		data := c.activeDataAt(i)
		if uint64(data.MemoryIndex) >= uint64(c.memoryCount()) {
			return reject("data initializer memory index")
		}
		offsetType := ValI32
		if c.memoryDef(int(data.MemoryIndex)).Addr64 {
			offsetType = ValI64
			if data.Offset.HasGlobal {
				return reject("memory64 global offset requires an i64 expression")
			}
		}
		if len(data.Offset.Expr) != 0 {
			if data.Offset.HasGlobal {
				return reject("conflicting data initializer")
			}
			if err := rv64ValidateIntegerInitializer(c, data.Offset.Expr, offsetType, len(c.Globals)); err != nil {
				return reject(err.Error())
			}
		}
		if data.Offset.HasGlobal && !rv64InitializerGlobal(c, data.Offset.Global, ValI32, len(c.Globals)) {
			return reject("data initializer lowering is pending")
		}
	}
	if c.memoryCount() > 1 && !c.requiredFeatures.IsEnabled(CoreFeatureMultiMemory) {
		return reject("memory directory requires multi-memory feature metadata")
	}
	for i := 0; i < c.memoryCount(); i++ {
		memory := c.memoryDef(i)
		maxTypePages := uint64(65536)
		if memory.Addr64 {
			maxTypePages = 1 << 48
			if !c.requiredFeatures.IsEnabled(CoreFeatureMemory64) {
				return reject("memory64 requires feature metadata")
			}
		}
		reservation, err := ownedMemoryReservationPages(memory)
		if memory.Shared || err != nil || (memory.HasMax && memory.Max > maxTypePages) {
			return reject("bounded unshared memory required")
		}
		if i == 0 && (!c.HasMemory || uint64(c.MemMinPages) != memory.Min || uint64(c.MemMaxPages) < memory.Min || uint64(c.MemMaxPages) > reservation) {
			return reject("primary memory reservation metadata mismatch")
		}
	}
	if len(c.memoryExportMap()) > railshot.MaxFunctions {
		return reject("memory export count bound")
	}
	for _, index := range c.memoryExportMap() {
		if index < 0 || index >= c.memoryCount() {
			return reject("memory export index bound")
		}
	}
	if c.memoryDir != nil && (len(c.memoryDir.ehTags) != 0 || len(c.memoryDir.gcStructGlobals) != 0 || len(c.memoryDir.gcArrayGlobals) != 0 || c.memoryDir.gcArrayElement != nil || c.memoryDir.gcI31TableInit != nil) {
		return reject("GC/EH initializer lowering is pending")
	}
	return nil
}

// Every callable RV64 producer has a canonical context anchor, including
// import-free scalar exports used as retained proper-tail targets.
func railshotNeedsImportContextHeader(count int) bool { return count > 0 }

// Binding policy runs before prepareCodeMapping, including trusted artifacts.
// Numeric/vector native bindings use retained framed bridges. Go callbacks and
// owned host handles retain their separately qualified scalar protocol.
func railshotValidateResolvedImports(c *Compiled, imports resolvedImports, store *referenceStore) error {
	c = c.executionView()
	if c.NumImports == 0 && len(c.Imports) == 0 && c.tableImportCount() == 0 && len(c.GlobalImports) == 0 {
		return nil
	}
	if err := railshotValidateCompiled(c); err != nil {
		return err
	}
	if err := rv64ValidateTailImports(c, imports); err != nil {
		return err
	}
	if err := rv64ValidateResolvedGlobals(c, imports, store); err != nil {
		return err
	}
	if err := rv64ValidateResolvedTable(c, imports, store); err != nil {
		return err
	}
	hasReferenceGlobal := false
	for _, global := range c.Globals {
		if global.Type == ValFuncRef {
			hasReferenceGlobal = true
			break
		}
	}
	if hasReferenceGlobal {
		// Immutable exported reference chains are bounded and validated above.
		// Reject unqualified token owners before mapping or attaching any import.
		for _, index := range c.GlobalExports {
			global := c.Globals[index]
			if global.Type != ValFuncRef {
				continue
			}
			for global.HasInitGlobal {
				global = c.Globals[global.InitGlobal]
			}
			if !global.HasInitFunc || int(global.InitFunc) >= c.NumImports {
				continue
			}
			binding := imports[c.functionImportBindingKey(int(global.InitFunc))]
			if host, ok := binding.(*HostFuncRef); ok {
				if err := host.validateImport(store, c.importFuncSigs[global.InitFunc]); err != nil {
					return err
				}
				continue // The scalar signature gate below also applies.
			}
			ex, ok := binding.(*InstanceExport)
			if !ok || ex == nil || ex.inst == nil || store == nil {
				return fmt.Errorf("%w: RV64 reference export requires a same-Runtime native function", railshot.ErrUnsupported)
			}
			ex.inst.lifeMu.Lock()
			sameStore := ex.inst.refStore == store
			ex.inst.lifeMu.Unlock()
			if !sameStore {
				return fmt.Errorf("%w: RV64 reference export requires the same Runtime", railshot.ErrUnsupported)
			}
		}
	}
	var reexported [railshot.MaxHostFunctionImports]bool
	for _, index := range c.Exports {
		if index < 0 || index >= c.NumImports+len(c.Funcs) {
			return fmt.Errorf("%w: public RV64 function export index", railshot.ErrUnsupported)
		}
		if index < c.NumImports {
			reexported[index] = true
		}
	}
	for i, key := range c.Imports {
		value := imports[c.functionImportBindingKey(i)]
		if reexported[i] {
			if _, native := value.(*InstanceExport); !native {
				return fmt.Errorf("%w: public RV64 import %q reexport requires a native instance export", railshot.ErrUnsupported, key)
			}
		}
		switch value := value.(type) {
		case *HostFuncRef:
			if err := rv64ScalarHostSignature(c.importFuncSigs[i]); err != nil {
				return fmt.Errorf("public RV64 import %q: %w", key, err)
			}
			// The shared attachment path repeats this check under its ownership
			// lock before retaining the handle; reject invalid input before mapping.
			if err := value.validateImport(store, c.importFuncSigs[i]); err != nil {
				return fmt.Errorf("public RV64 import %q: %w", key, err)
			}
			continue
		case *InstanceExport:
			if err := railshotValidateNativeExport(c, i, value); err != nil {
				return fmt.Errorf("public RV64 import %q: %w", key, err)
			}
			continue
		case I32HostEvent, gatedI32HostEvent:
			return fmt.Errorf("%w: public RV64 import %q target ownership/protocol is pending", railshot.ErrUnsupported, key)
		}
		if err := rv64ScalarHostSignature(c.importFuncSigs[i]); err != nil {
			return fmt.Errorf("public RV64 import %q: %w", key, err)
		}
		if _, err := bindSyncHostImport(value, c.importFuncSigs[i]); err != nil {
			return fmt.Errorf("public RV64 import %q: %w", key, err)
		}
	}
	return nil
}

func rv64ImportedInitializer(c *Compiled, index int, typ ValType) bool {
	return index >= 0 && index < len(c.GlobalImports) && !c.GlobalImports[index].Mutable && c.GlobalImports[index].Type == typ
}

// This binding-dependent gate runs for every host import, including unused
// signatures and trusted artifacts, before executable mapping or owner retention.
func rv64ScalarHostSignature(sig FuncSig) error {
	if len(sig.Params) > coreruntime.MaxHostArity || len(sig.Results) > coreruntime.MaxHostArity {
		return fmt.Errorf("%w: public RV64 scalar host signature arity", railshot.ErrUnsupported)
	}
	for _, types := range [][]ValType{sig.Params, sig.Results} {
		for _, typ := range types {
			if typ != ValI32 && typ != ValI64 && typ != ValF32 && typ != ValF64 {
				return fmt.Errorf("%w: public RV64 nonscalar host signature", railshot.ErrUnsupported)
			}
		}
	}
	return nil
}

// Reject incompatible storage/targets before executable mapping or element writes.
// A temporary physical lease makes row authentication safe even during retirement.
func rv64ValidateResolvedTable(c *Compiled, imports resolvedImports, store *referenceStore) error {
	def, imported := c.tableImportAt(0)
	if !imported {
		return nil
	}
	table, ok := imports.table(c.tableImportBindingKey(0))
	if !ok || table == nil {
		return fmt.Errorf("missing imported table %q", def.Key)
	}
	exact, err := c.tableExactType(0)
	if err != nil {
		return err
	}
	if err := table.validateImportWithCollector(ValFuncRef, exact, c.Types, store, nil, def.Addr64); err != nil {
		return err
	}
	// Frozen global roots must not create a backedge into an instance-owned
	// destination. Host-created destinations have no such instance owner.
	for _, global := range c.GlobalImports {
		if global.Type != ValFuncRef {
			continue
		}
		table.owner.mu.Lock()
		instanceOwned := table.owner.instance != nil
		table.owner.mu.Unlock()
		if instanceOwned {
			return fmt.Errorf("%w: reference global imports require a host-created borrowed table", railshot.ErrUnsupported)
		}
		break
	}
	// The descriptor itself also needs a lease: host tables have no instance root.
	if err := table.attachImporterWithCollector(ValFuncRef, exact, c.Types, store, nil, def.Addr64); err != nil {
		return err
	}
	defer table.detachImporter(store)
	var roots rv64NativeTableTargets
	// Final-root release can reenter native execution. Unlock first.
	defer roots.release()
	nativeExecutionMu.Lock()
	defer nativeExecutionMu.Unlock()
	edges := 0
	if err := roots.tableRoots(table, &edges); err != nil {
		return fmt.Errorf("%w: %v", railshot.ErrUnsupported, err)
	}
	table.owner.mu.Lock()
	table.mu.Lock()
	size, capacity := uint64(binary.LittleEndian.Uint32(table.desc)), uint64(binary.LittleEndian.Uint32(table.desc[4:]))
	valid := size >= def.Min && size <= capacity && capacity <= railshot.MaxOwnedTableEntries && 8+capacity*coreruntime.TableEntryBytes <= uint64(len(table.desc)) && (!def.HasMax || table.owner.declaredHasMax && capacity <= def.Max)
	table.mu.Unlock()
	table.owner.mu.Unlock()
	if !valid {
		return fmt.Errorf("%w: imported table capacity/limits/extent", railshot.ErrUnsupported)
	}
	if err := roots.validateReachable(); err != nil {
		return fmt.Errorf("%w: %v", railshot.ErrUnsupported, err)
	}
	if err := roots.validateTable(table.desc); err != nil {
		return fmt.Errorf("%w: %v", railshot.ErrUnsupported, err)
	}
	return nil
}

// Compact local references need their own feature witness; the codec's generic
// structural feature scan does not infer local scope from these indices.
func rv64InitializerGlobal(c *Compiled, index int, expected ValType, limit int) bool {
	if index < 0 || index >= limit || index >= len(c.Globals) {
		return false
	}
	if index < len(c.GlobalImports) {
		return rv64ImportedInitializer(c, index, expected)
	}
	g := c.Globals[index]
	return c.requiredFeatures.IsEnabled(CoreFeatureExtendedConstExpressions) && !g.Mutable && g.Type == expected
}

func rv64ValidateIntegerInitializer(c *Compiled, body []byte, expected ValType, limit int) error {
	if (expected != ValI32 && expected != ValI64) || (!c.requiredFeatures.IsEnabled(CoreFeatureExtendedConst) && !c.requiredFeatures.IsEnabled(CoreFeatureExtendedConstExpressions) && !rv64CoreIntegerInitializer(body)) {
		return fmt.Errorf("integer expression requires its numeric feature metadata")
	}
	return railshot.ValidateIntegerInitializer(body, rv64NumericWasmType(expected), func(index uint32) (wasm.ValType, bool) {
		if uint64(index) >= uint64(len(c.Globals)) {
			return wasm.ValType{}, false
		}
		g := c.Globals[index]
		return rv64NumericWasmType(g.Type), rv64InitializerGlobal(c, int(index), g.Type, limit)
	})
}

// Memory64 keeps even literal/global offsets as Expr to retain all 64 bits.
// This representation alone does not imply extended-constant instructions.
// The common validator below still proves the result type and global scope.
func rv64CoreIntegerInitializer(body []byte) bool {
	if len(body) > railshot.MaxIntegerInitializerBytes {
		return false
	}
	r := wasm.ReaderFrom(body)
	op, err := r.Byte()
	if err != nil {
		return false
	}
	switch op {
	case 0x41:
		_, err = r.I32()
	case 0x42:
		_, err = r.I64()
	case 0x23:
		_, err = r.U32()
	default:
		return false
	}
	if err != nil {
		return false
	}
	end, err := r.Byte()
	return err == nil && end == 0x0b && !r.HasNext()
}

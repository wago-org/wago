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

// Loading and hand-built metadata must enforce the same bounded public ABI as
// source compilation before any executable image or host thunk is published.
func railshotValidateCompiled(c *Compiled) error {
	reject := func(detail string) error { return fmt.Errorf("%w: public RV64 %s", railshot.ErrUnsupported, detail) }
	if !c.nativeContextTrapABI {
		return reject("native-context trap ABI is required")
	}
	if len(c.code) > railshot.MaxCodeBytes || len(c.Funcs) == 0 && c.NumImports == 0 || len(c.Funcs) > railshot.MaxFunctions || len(c.Types) > railshot.MaxFunctions || len(c.GCTypeDescs) > railshot.MaxFunctions || len(c.Globals) > 256 {
		return reject("bounded artifact shape")
	}
	if len(c.Data) > rv64runtime.MaxDataSegments || len(c.PassiveData) > rv64runtime.MaxDataSegments {
		return reject("data descriptor count bound")
	}
	remaining := railshot.MaxModuleBytes
	for _, data := range c.Data {
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

	if c.memoryImportCount() > 1 || c.boundsMode == BoundsChecksSignalsBased || c.genericGCFrameRoots() != nil || c.memoryCount() > 1 {
		return reject("resource/host/reference lowering is pending")
	}
	if len(c.GlobalImports) > len(c.Globals) {
		return reject("global import index shape")
	}
	for i, global := range c.GlobalImports {
		if (global.Type != ValI32 && global.Type != ValI64 && global.Type != ValF32 && global.Type != ValF64 && (!vectorProfile || global.Type != ValV128)) || global.Type != c.Globals[i].Type || global.Mutable != c.Globals[i].Mutable {
			return reject("global import type/mutability shape")
		}
	}
	for _, desc := range c.GCTypeDescs {
		if desc.Kind != gc.KindFunc || !desc.Final || desc.HasSuper {
			return reject("GC descriptor lowering is pending")
		}
	}
	if c.requiredFeatures & ^platformCoreFeatures() != 0 || c.requiredAMD64Features != 0 || c.preparedIsolatedTables {
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
	for _, sig := range c.Funcs {
		if len(sig.Params) > maxArity || len(sig.Results) > maxArity {
			return reject("signature arity")
		}
		for _, types := range [][]ValType{sig.Params, sig.Results} {
			for _, typ := range types {
				if typ != ValI32 && typ != ValI64 && typ != ValF32 && typ != ValF64 && (!vectorProfile || typ != ValV128) {
					return reject("unsupported signature")
				}
			}
		}
	}
	for i, typ := range c.Types {
		if len(typ.Params) > maxArity || len(typ.Results) > maxArity {
			return reject("type signature arity")
		}
		if i > 0 && c.Types[i-1].RecGroup == typ.RecGroup {
			return reject("recursive type group lowering is pending")
		}
		if typ.Kind != CompositeTypeFunction || !typ.Final || len(typ.Supers) != 0 || typ.HasDescribes || typ.HasDescriptor {
			return reject("structural type lowering is pending")
		}
		for _, values := range [][]ValueTypeDescriptor{typ.Params, typ.Results} {
			for _, value := range values {
				if value.Kind != ValueTypeI32 && value.Kind != ValueTypeI64 && value.Kind != ValueTypeF32 && value.Kind != ValueTypeF64 && (!vectorProfile || value.Kind != ValueTypeV128) {
					return reject("unsupported type")
				}
			}
		}
	}
	if err := rv64ValidateOwnedTable(c); err != nil {
		return reject(err.Error())
	}
	for i, global := range c.Globals {
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
	for _, data := range c.Data {
		if len(data.Offset.Expr) != 0 {
			if data.Offset.HasGlobal {
				return reject("conflicting data initializer")
			}
			if err := rv64ValidateIntegerInitializer(c, data.Offset.Expr, ValI32, len(c.Globals)); err != nil {
				return reject(err.Error())
			}
		}
		if c.memoryCount() != 1 || data.MemoryIndex != 0 || (data.Offset.HasGlobal && !rv64InitializerGlobal(c, data.Offset.Global, ValI32, len(c.Globals))) {
			return reject("data initializer lowering is pending")
		}
	}
	if c.memoryCount() == 1 {
		memory := c.memoryDef(0)
		if memory.Shared || memory.Addr64 || memory.Min > 65536 || (memory.HasMax && memory.Max > 65536) {
			return reject("memory lowering is pending")
		}
	}
	if c.memoryDir != nil && (len(c.memoryDir.ehTags) != 0 || len(c.memoryDir.gcStructGlobals) != 0 || len(c.memoryDir.gcArrayGlobals) != 0 || c.memoryDir.gcArrayElement != nil || c.memoryDir.gcI31TableInit != nil) {
		return reject("GC/EH initializer lowering is pending")
	}
	return nil
}

func railshotNeedsImportContextHeader(count int) bool { return count > 0 }

// Binding policy runs before prepareCodeMapping, including trusted artifacts.
// Numeric/vector native bindings use retained framed bridges. Go callbacks and
// owned host handles retain their separately qualified scalar protocol.
func railshotValidateResolvedImports(c *Compiled, imports resolvedImports, store *referenceStore) error {
	c = c.executionView()
	if c.NumImports == 0 && len(c.Imports) == 0 && c.tableImportCount() == 0 {
		return nil
	}
	if err := railshotValidateCompiled(c); err != nil {
		return err
	}
	if err := rv64ValidateResolvedTable(c, imports, store); err != nil {
		return err
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
	if err := table.validateImportWithCollector(ValFuncRef, exact, c.Types, store, nil, false); err != nil {
		return err
	}
	// The descriptor itself also needs a lease: host tables have no instance root.
	if err := table.attachImporterWithCollector(ValFuncRef, exact, c.Types, store, nil, false); err != nil {
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
	if (expected != ValI32 && expected != ValI64) || (!c.requiredFeatures.IsEnabled(CoreFeatureExtendedConst) && !c.requiredFeatures.IsEnabled(CoreFeatureExtendedConstExpressions)) {
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

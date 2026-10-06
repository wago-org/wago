//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"

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
	if len(c.code) > railshot.MaxCodeBytes || len(c.Funcs) == 0 || len(c.Funcs) > railshot.MaxFunctions || len(c.Types) > railshot.MaxFunctions || len(c.GCTypeDescs) > railshot.MaxFunctions || len(c.Globals) > 256 {
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
	if c.NumImports != 0 {
		if !c.needsFuncRefContextHeader || c.tableCount() != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 {
			return reject("host imports require an instance context anchor without tables")
		}
		for _, index := range c.Exports {
			if index < c.NumImports {
				return reject("imported-function reexport lowering is pending")
			}
		}
		for _, sig := range c.importFuncSigs {
			if len(sig.Params) > coreruntime.MaxHostArity || len(sig.Results) > coreruntime.MaxHostArity {
				return reject("host signature arity")
			}
			for _, values := range [][]ValType{sig.Params, sig.Results} {
				for _, typ := range values {
					if typ != ValI32 && typ != ValI64 && typ != ValF32 && typ != ValF64 {
						return reject("nonscalar host signature")
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
	vectorProfile := c.requiredFeatures.IsEnabled(CoreFeatureSIMD)
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
		if (global.Type != ValI32 && global.Type != ValI64 && global.Type != ValF32 && global.Type != ValF64 && (!vectorProfile || global.Type != ValV128)) || global.HasInitFunc || len(global.InitExpr) != 0 {
			return reject("global initializer lowering is pending")
		}
		if global.HasInitGlobal && (i < len(c.GlobalImports) || !rv64ImportedInitializer(c, global.InitGlobal, global.Type)) {
			return reject("global initializer requires an immutable scalar import of the exact type")
		}
		if vectorProfile && global.HasInitGlobal {
			return reject("SIMD global initializer must be a literal")
		}
	}
	for _, data := range c.Data {
		if vectorProfile && data.Offset.HasGlobal {
			return reject("SIMD data offset must be a literal")
		}
		if c.memoryCount() != 1 || data.MemoryIndex != 0 || len(data.Offset.Expr) != 0 || (data.Offset.HasGlobal && !rv64ImportedInitializer(c, data.Offset.Global, ValI32)) {
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
// This milestone admits synchronous scalar callbacks and owned host handles.
// Unsupported foreign targets and replay protocols cannot publish a thunk.
func railshotValidateResolvedImports(c *Compiled, imports resolvedImports, store *referenceStore) error {
	c = c.executionView()
	if c.NumImports == 0 && len(c.Imports) == 0 {
		return nil
	}
	if err := railshotValidateCompiled(c); err != nil {
		return err
	}
	for i, key := range c.Imports {
		value := imports[c.functionImportBindingKey(i)]
		switch value := value.(type) {
		case *HostFuncRef:
			// The shared attachment path repeats this check under its ownership
			// lock before retaining the handle; reject invalid input before mapping.
			if err := value.validateImport(store, c.importFuncSigs[i]); err != nil {
				return fmt.Errorf("public RV64 import %q: %w", key, err)
			}
			continue
		case *InstanceExport:
			if c.HasStart && c.StartIsImport && c.StartImportIdx == i {
				return fmt.Errorf("%w: public RV64 native imported start is pending", railshot.ErrUnsupported)
			}
			if err := railshotValidateNativeExport(c, i, value); err != nil {
				return fmt.Errorf("public RV64 import %q: %w", key, err)
			}
			continue
		case I32HostEvent, gatedI32HostEvent:
			return fmt.Errorf("%w: public RV64 import %q target ownership/protocol is pending", railshot.ErrUnsupported, key)
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

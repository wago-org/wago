//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
	rv64runtime "github.com/wago-org/wago/src/core/runtime/riscv64"
)

// Loading and hand-built metadata must enforce the same bounded public ABI as
// source compilation before any executable image or host thunk is published.
func railshotValidateCompiled(c *Compiled) error {
	reject := func(detail string) error { return fmt.Errorf("%w: public RV64 %s", railshot.ErrUnsupported, detail) }
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
	if c.NumImports != 0 || len(c.Imports) != 0 || len(c.GlobalImports) != 0 || c.memoryImportCount() != 0 || c.tableCount() != 0 || c.HasStart || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.NeedsFuncRefDescs || c.boundsMode == BoundsChecksSignalsBased || c.genericGCFrameRoots() != nil || c.memoryCount() > 1 {
		return reject("resource/host/reference lowering is pending")
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
	for _, sig := range c.Funcs {
		if len(sig.Params) > 256 || len(sig.Results) > 256 {
			return reject("signature arity")
		}
		for _, types := range [][]ValType{sig.Params, sig.Results} {
			for _, typ := range types {
				if typ != ValI32 && typ != ValI64 && typ != ValF32 && typ != ValF64 {
					return reject("nonscalar signature")
				}
			}
		}
	}
	for i, typ := range c.Types {
		if len(typ.Params) > 256 || len(typ.Results) > 256 {
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
				if value.Kind != ValueTypeI32 && value.Kind != ValueTypeI64 && value.Kind != ValueTypeF32 && value.Kind != ValueTypeF64 {
					return reject("nonscalar type")
				}
			}
		}
	}
	for _, global := range c.Globals {
		if (global.Type != ValI32 && global.Type != ValI64 && global.Type != ValF32 && global.Type != ValF64) || global.HasInitGlobal || global.HasInitFunc || len(global.InitExpr) != 0 {
			return reject("global initializer lowering is pending")
		}
	}
	for _, data := range c.Data {
		if data.MemoryIndex != 0 || data.Offset.HasGlobal || len(data.Offset.Expr) != 0 {
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

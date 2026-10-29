//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
)

func rv64PlainReferenceTable32Directory(c *Compiled) bool {
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	extern, _ := valueTypeDescriptorFromValType(ValExternRef)
	for index := 0; index < c.tableCount(); index++ {
		exact, err := c.tableExactType(index)
		if err != nil || exact != plain && exact != extern && !rv64PrimitiveReferenceValue(c, exact) || c.tableDef(index).Addr64 {
			return false
		}
	}
	return true
}

// Every table and segment uses the same retained descriptor ABI. Qualification
// depends on its exact types and resource bounds, not a fixed module topology.
func rv64ValidateFunctionTable32Directory(c *Compiled) error {
	return rv64ValidatePlainReferenceTableDirectory(c, false)
}

func rv64ValidatePlainReferenceTableDirectory(c *Compiled, allow64 bool) error {
	if rv64HasExternTable32(c) && !rv64SourceContractValid(c) {
		return fmt.Errorf("public compact table32 requires an authenticated execution contract")
	}
	if c.tableCount() > railshot.MaxFunctions || !c.hasTableExportMetadata || len(c.tableExports) > railshot.MaxFunctions || !c.registerABIDisabled || !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes) && !allow64 {
		return fmt.Errorf("public table32 directory requires bounded framed reference metadata")
	}
	for index := 0; index < c.tableCount(); index++ {
		definition := c.tableDef(index)
		if definition.Addr64 && (!allow64 || !c.requiredFeatures.IsEnabled(CoreFeatureTable64)) {
			return fmt.Errorf("public table %d requires its exact table64 feature and execution ABI", index)
		}
		if imported, ok := c.tableImportAt(index); ok {
			localLimits := definition.Size != 0 || definition.Max != 0 || definition.HasMax
			if index != 0 {
				localLimits = definition.Size < 0 || uint64(definition.Size) != imported.Min || definition.Max != imported.Max || definition.HasMax || definition.ImportHasMax != imported.HasMax
			}
			if localLimits || definition.HasInitFunc || !definition.Addr64 && (imported.Min > uint64(^uint32(0)) || imported.Max > uint64(^uint32(0))) || imported.HasMax && imported.Max < imported.Min {
				return fmt.Errorf("public table32 import %d has invalid limits or local initializer", index)
			}
			continue
		}
		capacity := c.tableRuntimeCapacity(index)
		if definition.Size < 0 || uint64(definition.Size) > uint64(^uint32(0)) || !definition.Addr64 && definition.Max > uint64(^uint32(0)) || definition.HasMax && definition.Max < uint64(definition.Size) || capacity < definition.Size || uint64(capacity) > uint64(^uint32(0)) {
			return fmt.Errorf("public table32 %d has invalid storage limits", index)
		}
		if definition.HasInitFunc && (definition.Type == ValExternRef || !c.needsFuncRefDescs() || uint64(definition.InitFunc) >= uint64(c.NumImports+len(c.Funcs))) {
			return fmt.Errorf("public table32 %d has an invalid initial function", index)
		}
	}
	for _, index := range c.tableExports {
		if index < 0 || index >= c.tableCount() {
			return fmt.Errorf("public table export index is invalid")
		}
	}
	// The indexed directory retains dropped placeholders for active segments;
	// their live initializers are already counted in Elems.
	segments := len(c.Elems)
	for _, segment := range c.passiveElems {
		if segment.Mode != ElemModeActive {
			segments++
		}
	}
	if len(c.passiveElems) > railshot.MaxOwnedElementSegments || segments > railshot.MaxOwnedElementSegments {
		return fmt.Errorf("public element directory exceeds its bound")
	}
	remaining := railshot.MaxOwnedTableEntries
	for _, segment := range c.Elems {
		if segment.Mode != ElemModeActive || uint64(segment.TableIndex) >= uint64(c.tableCount()) || len(segment.Values) > remaining || !rv64PlainTableElementType(segment.RefType) {
			return fmt.Errorf("public active element has an invalid table, type or count")
		}
		remaining -= len(segment.Values)
		destination, err := c.tableExactType(int(segment.TableIndex))
		payload, payloadErr := c.elemExactType(segment)
		if payloadErr != nil || err != nil || destination.Kind != ValueTypeReference || payload.Kind != ValueTypeReference || !valueTypeSubtype(payload, c.Types, destination, c.Types) {
			return fmt.Errorf("public active element type differs from its table")
		}
		offsetType := ValI32
		if c.tableDef(int(segment.TableIndex)).Addr64 {
			offsetType = ValI64
		}
		if len(segment.Offset.Expr) != 0 {
			if segment.Offset.HasGlobal {
				return fmt.Errorf("public active element has conflicting offset metadata")
			}
			if err := rv64ValidateIntegerInitializer(c, segment.Offset.Expr, offsetType, len(c.Globals)); err != nil {
				return err
			}
		}
		if segment.Offset.HasGlobal && (segment.Offset.Base != 0 || !rv64InitializerGlobal(c, segment.Offset.Global, offsetType, len(c.Globals))) {
			return fmt.Errorf("public active element has an invalid immutable offset global")
		}
		if err := rv64ValidatePlainTableElementPayload(c, segment); err != nil {
			return err
		}
	}
	if len(c.passiveElems) != 0 && !c.requiredFeatures.IsEnabled(CoreFeatureBulkMemoryOperations) {
		return fmt.Errorf("public passive elements require bulk memory")
	}
	for _, segment := range c.passiveElems {
		activePlaceholder := segment.Mode == ElemModeActive && uint64(segment.TableIndex) < uint64(c.tableCount()) && len(segment.Values) == 0
		if segment.Mode != ElemModePassive && segment.Mode != ElemModeDeclarative && !activePlaceholder || segment.Mode != ElemModeActive && segment.TableIndex != 0 || segment.Offset.Base != 0 || segment.Offset.HasGlobal || len(segment.Offset.Expr) != 0 || len(segment.Values) > remaining || !rv64PlainTableElementType(segment.RefType) || segment.Mode != ElemModePassive && len(segment.Values) != 0 {
			return fmt.Errorf("public passive element has invalid metadata")
		}
		remaining -= len(segment.Values)
		if err := rv64ValidatePlainTableElementPayload(c, segment); err != nil {
			return err
		}
	}
	return rv64ValidateReferenceFunctionKeys(c)
}

func rv64HasExternTable32(c *Compiled) bool {
	for index := 0; index < c.tableCount(); index++ {
		if c.tableDef(index).Type == ValExternRef {
			return true
		}
	}
	return false
}

func rv64PlainTableElementType(typ ValType) bool {
	return normalizedElemRefType(typ) == ValFuncRef || typ == ValExternRef
}

func rv64ValidatePlainTableElementPayload(c *Compiled, segment ElemInit) error {
	if segment.RefType != ValExternRef {
		return rv64ValidateElementPayload(c, segment)
	}
	plain, _ := valueTypeDescriptorFromValType(ValExternRef)
	exact, err := c.elemExactType(segment)
	if !segment.HasValueType || err != nil || exact != plain && !(rv64PrimitiveReferenceContract(c) && rv64PrimitiveReferenceValue(c, exact)) {
		return fmt.Errorf("external elements require exact nullable abstract metadata")
	}
	for _, value := range segment.Values {
		if value.HasGlobal {
			if value.Null || value.FuncIndex != 0 || !rv64InitializerGlobal(c, int(value.GlobalIndex), ValExternRef, len(c.Globals)) || value.I31Wrap || value.RepeatPrevious || len(value.Expr) != 0 {
				return fmt.Errorf("external element requires an immutable compatible global")
			}
			global, err := c.globalExactType(int(value.GlobalIndex))
			if err != nil || !valueTypeSubtype(global, c.Types, exact, c.Types) {
				return fmt.Errorf("external element global differs from its payload type")
			}
			continue
		}
		if !value.Null || !exact.Ref.Nullable || value.FuncIndex != 0 || value.GlobalIndex != 0 || value.I31Wrap || value.RepeatPrevious || len(value.Expr) != 0 {
			return fmt.Errorf("external elements require a structural nullable initializer")
		}
	}
	return nil
}

//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
)

// This permission belongs to the destination instance, never to artifact bytes
// or a source Compiled's cache. Trusted decoding retains unpublished RW staging.
func railshotValidateReferenceFunctionInstance(c *Compiled, opts InstantiateOptions) error {
	c = c.executionView()
	if c.nativeReferenceFunctionABI && !opts.railshotTailPolicy.referenceFunctions {
		return fmt.Errorf("%w: closed RV64 reference functions require private Runtime permission", railshot.ErrUnsupported)
	}
	if c.nativeReferenceFunctionABI {
		return c.validateCached()
	}
	return nil
}

// The closed profile preserves physical function indices, but internal
// reference-valued signatures are never canonical descriptor identities.
// Callers use frozen metadata; admission validates exact type bindings first.
func railshotNonCallableFunction(c *Compiled, function int) bool {
	if c == nil || !c.nativeReferenceFunctionABI {
		return false
	}
	local := function - c.NumImports
	if local < 0 || local >= len(c.Funcs) {
		return false
	}
	for _, list := range [][]ValType{c.Funcs[local].Params, c.Funcs[local].Results} {
		for _, v := range list {
			if v != ValI32 && v != ValI64 && v != ValF32 && v != ValF64 && v != ValV128 {
				return true
			}
		}
	}
	return false
}

// Source escape confinement is attested by the compiler-origin witness; Wasm
// bodies and dropped declarative targets are not retained in native artifacts.
// The retained shape and every binding still require independent admission.
func rv64ValidateReferenceFunctions(c *Compiled) error {
	if !c.nativeReferenceFunctionABI {
		return nil
	}
	reject := func(detail string) error { return fmt.Errorf("RV64 closed reference functions: %s", detail) }
	if c.NumImports < 0 || c.NumImports > railshot.MaxHostFunctionImports || len(c.Imports) != c.NumImports || len(c.importFuncSigs) != c.NumImports || c.dynamicImports != (c.NumImports > 0) {
		return reject("bounded numeric import directory required")
	}
	if !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes) || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled || c.nativeReferenceCallABI || len(c.Funcs) == 0 || len(c.Funcs) > railshot.MaxFunctions-c.NumImports || len(c.Types) > railshot.MaxFunctions || len(c.Exports) > railshot.MaxFunctions || len(c.FuncTypeID) != c.NumImports+len(c.Funcs) {
		return reject("bounded framed function ABI required")
	}
	if c.dynamicFuncrefEscape || c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 || c.tableCount() != 0 || c.tableImport != "" || c.tableImportMin != 0 || c.tableImportMax != 0 || c.tableImportHasMax || len(c.tableExports) != 0 || len(c.Elems) != 0 || len(c.customInstructions) != 0 {
		return reject("external state or callable escape")
	}
	if m := rv64NativeTailModule(c); m != nil && len(m.Plans) != 0 {
		return reject("only local direct tail calls are admitted")
	}
	if c.TableHasValueType || c.TableValueTypeIndex != 0 {
		return reject("unused exact value metadata")
	}
	if d := c.memoryDir; d != nil && (len(d.ehTags) != 0 || len(d.ehTagExports) != 0 || len(d.gcStructGlobals) != 0 || len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil) {
		return reject("resource initializer metadata")
	}
	// Bound resource directories before exact-name helpers scan their prefixes.
	if c.memoryCount() > railshot.MaxProductionMemories || len(c.Globals) > 256 || len(c.GlobalImports) > len(c.Globals) || len(c.ValueTypes) > 5 {
		return reject("bounded numeric resource directories required")
	}
	functions, _, memories, _, exact := c.importModuleEndSections()
	if !exact {
		return reject("exact import names required")
	}
	for _, ends := range [][]uint64{functions, memories} {
		for _, end := range ends {
			if end == 0 {
				return reject("exact import separator required")
			}
		}
	}
	if err := c.validateImportModuleEnds(); err != nil {
		return err
	}
	if err := rv64ValidateDeclaredControlTypes(c); err != nil {
		return err
	}
	remaining := railshot.MaxModuleBytes
	if err := rv64ValidateReferenceFunctionState(c, &remaining); err != nil {
		return err
	}
	if err := rv64ValidateReferenceFunctionExports(c, &remaining); err != nil {
		return err
	}
	for i, sig := range c.importFuncSigs {
		if !sig.HasTypeIndex || uint64(sig.TypeIndex) >= uint64(len(c.Types)) {
			return reject("exact imported function type binding required")
		}
		if err := validateFuncSignature(sig, c.Types); err != nil {
			return err
		}
		if _, err := rv64NativeTailSignatureValues(sig); err != nil {
			return err
		}
		if remaining < 5 || len(c.Imports[i]) > remaining-5 {
			return reject("import binding/name byte bound")
		}
		remaining -= 5 + len(c.Imports[i])
	}
	for _, typ := range c.Types {
		if typ.Describes != 0 || typ.Descriptor != 0 {
			return reject("inactive type metadata")
		}
		for _, values := range [][]ValueTypeDescriptor{typ.Params, typ.Results} {
			for _, v := range values {
				remaining--
				if v.Kind == ValueTypeReference {
					remaining -= 5
				}
				if remaining < 0 {
					return reject("aggregate declared carrier byte bound")
				}
			}
		}
	}
	hasReferences := false
	for i, sig := range c.Funcs {
		if !sig.HasTypeIndex || uint64(sig.TypeIndex) >= uint64(len(c.Types)) {
			return reject("exact actual function type binding required")
		}
		if err := validateFuncSignature(sig, c.Types); err != nil {
			return err
		}
		hasReferences = hasReferences || railshotNonCallableFunction(c, c.NumImports+i)
	}
	if !hasReferences {
		return reject("witness without a reference-valued function")
	}
	for _, index := range c.Exports {
		if index < 0 || index >= c.NumImports+len(c.Funcs) || railshotNonCallableFunction(c, index) {
			return reject("numeric exports only")
		}
	}
	if len(c.passiveElems) > railshot.MaxOwnedElementSegments {
		return reject("declarative element count bound")
	}
	for _, e := range c.passiveElems {
		if e.Mode != ElemModeDeclarative || e.TableIndex != 0 || e.RefType != 0 && e.RefType != ValFuncRef || e.HasValueType || e.ValueTypeIndex != 0 || len(e.Values) != 0 || e.Offset.Base != 0 || e.Offset.HasGlobal || e.Offset.Global != 0 || len(e.Offset.Expr) != 0 {
			return reject("only canonical dropped declarative state is retained")
		}
	}
	return nil
}

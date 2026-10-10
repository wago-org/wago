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
	if !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes) || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled || len(c.Funcs) == 0 || len(c.Funcs) > railshot.MaxFunctions-c.NumImports || len(c.Types) > railshot.MaxFunctions || len(c.Exports) > railshot.MaxFunctions || len(c.FuncTypeID) != c.NumImports+len(c.Funcs) {
		return reject("bounded framed function ABI required")
	}
	if c.dynamicFuncrefEscape || !c.HasStart && c.StartIsImport || (!c.HasStart || c.StartIsImport) && c.StartLocalFunc != 0 || (!c.HasStart || !c.StartIsImport) && c.StartImportIdx != 0 || c.tableImport != "" || c.tableImportMin != 0 || c.tableImportMax != 0 || c.tableImportHasMax || len(c.tableExports) != 0 || len(c.customInstructions) != 0 {
		return reject("external state or callable escape")
	}
	if c.HasStart {
		if _, err := compiledStartIndex(c); err != nil {
			return err
		}
	}
	if c.HasStart && !c.StartIsImport {
		local := c.StartLocalFunc
		entry, end := c.Entry[local], len(c.code)
		if local+1 < len(c.Entry) {
			end = c.Entry[local+1]
		}
		if local >= len(c.InternalEntry) || c.InternalEntry[local] != entry || entry < 0 || entry&15 != 0 || end <= entry || end > len(c.code) {
			return reject("defined start framed entry extent")
		}
		if _, err := railshot.NativeFrameBytes(c.code[:end], entry); err != nil {
			return reject("defined start actual frame packet")
		}
	}
	if d := c.memoryDir; d != nil && (len(d.ehTags) != 0 || len(d.ehTagExports) != 0 || len(d.gcStructGlobals) != 0 || len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil) {
		return reject("resource initializer metadata")
	}
	// Bound resource directories before exact-name helpers scan their prefixes.
	if c.memoryCount() > railshot.MaxProductionMemories || len(c.Globals) > 256 || len(c.GlobalImports) > len(c.Globals) || len(c.ValueTypes) > 6 {
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
	for _, sig := range c.importFuncSigs {
		if !sig.HasTypeIndex || uint64(sig.TypeIndex) >= uint64(len(c.Types)) {
			return reject("exact imported function type binding required")
		}
		if err := validateFuncSignature(sig, c.Types); err != nil {
			return err
		}
		if _, err := rv64NativeTailSignatureValues(sig); err != nil {
			return err
		}
	}
	// Exact declared/actual signatures are bounded before selected plans can
	// consult sparse source rows or compare their numeric result contracts.
	if m := rv64NativeTailModule(c); m != nil {
		for _, p := range m.Plans {
			if p.Kind == 0 {
				if p.TypeIndex != 0 || p.ReferenceBase != 0 || uint64(p.ImportIndex) >= uint64(c.NumImports) {
					return reject("canonical numeric direct imported-tail plan required")
				}
			} else if (p.Kind != 1 && p.Kind != 2) || p.ImportIndex != 0 {
				return reject("only canonical numeric proper-tail plans admitted")
			}
		}
		if err := rv64ValidateNativeTailPlans(c, m.Plans); err != nil {
			return err
		}
	}
	remaining := railshot.MaxModuleBytes
	if c.HasStart {
		remaining -= 5
	}
	if err := rv64ValidateReferenceFunctionTable(c, &remaining); err != nil {
		return err
	}
	if err := rv64ValidateReferenceFunctionState(c, &remaining); err != nil {
		return err
	}
	if err := rv64ValidateReferenceFunctionExports(c, &remaining); err != nil {
		return err
	}
	for i := range c.importFuncSigs {
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
	referenceExports := false
	for _, index := range c.Exports {
		if index < 0 || index >= c.NumImports+len(c.Funcs) {
			return reject("function export index")
		}
		referenceExports = referenceExports || railshotNonCallableFunction(c, index)
	}
	if referenceExports {
		if c.tableCount() != 0 || c.nativeReferenceCallABI || rv64HasReferenceTail(c) {
			return reject("public reference wrapper requires ephemeral owner-scoped transport")
		}
		for _, global := range c.Globals {
			if global.Type == ValFuncRef {
				return reject("public reference wrapper requires reference-global-free transport")
			}
		}
	}
	if c.tableCount() != 0 {
		// Full signatures are validated before the common numeric key checker
		// uses its fixed carrier arrays. Private ordinals remain sparse holes.
		return rv64ValidateOwnedTable(c)
	}
	return nil
}

//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	"github.com/wago-org/wago/src/core/nativeabi"
)

func exceptionReferenceType(typ wasm.ValType) bool {
	if typ.Kind() != wasm.ValRef {
		return false
	}
	heap := typ.Ref().Heap()
	return heap == wasm.AbsHeap(wasm.HeapExn) || heap == wasm.AbsHeap(wasm.HeapNoExn)
}

// The projection is used only by existing resource/type admission. Lowering
// and the public metadata retain the independently validated original module.
func exceptionAdmissionProjection(m *wasm.Module) *wasm.Module {
	projected := *m
	projected.Tags = nil
	projected.Imports = make([]wasm.Import, 0, len(m.Imports))
	for _, im := range m.Imports {
		if im.Type.Kind != wasm.ExternTag {
			if im.Type.Kind == wasm.ExternGlobal {
				global := im.Type.GlobalType()
				if exceptionReferenceType(global.Type) {
					global.Type = wasm.I64
					im.Type = wasm.NewGlobalExternType(global)
				}
			} else if im.Type.Kind == wasm.ExternTable {
				table := im.Type.TableType()
				if exceptionReferenceType(wasm.RefVal(table.Ref)) {
					table.Ref = wasm.ExternRef.Ref()
					im.Type = wasm.NewTableExternType(table)
				}
			}
			projected.Imports = append(projected.Imports, im)
		}
	}
	projected.Exports = make([]wasm.Export, 0, len(m.Exports))
	for _, ex := range m.Exports {
		if ex.Index.Kind != wasm.ExternTag {
			projected.Exports = append(projected.Exports, ex)
		}
	}
	projected.Types = slices.Clone(m.Types)
	for i := range projected.Types {
		projected.Types[i].SubTypes = slices.Clone(m.Types[i].SubTypes)
		for j := range projected.Types[i].SubTypes {
			comp := &projected.Types[i].SubTypes[j].Comp
			if comp.Kind == wasm.CompStruct {
				comp.Fields = slices.Clone(comp.Fields)
				for k := range comp.Fields {
					comp.Fields[k] = exceptionAdmissionField(comp.Fields[k])
				}
			} else if comp.Kind == wasm.CompArray {
				comp.Array = exceptionAdmissionField(comp.Array)
			}
			if comp.Kind != wasm.CompFunc {
				continue
			}
			comp.Params, comp.Results = slices.Clone(comp.Params), slices.Clone(comp.Results)
			for _, values := range [][]wasm.ValType{comp.Params, comp.Results} {
				for k, typ := range values {
					if typ.Kind() == wasm.ValRef {
						values[k] = wasm.I64
					}
				}
			}
		}
	}
	projected.Code = slices.Clone(m.Code)
	for i := range projected.Code {
		projected.Code[i].Locals.Runs = slices.Clone(m.Code[i].Locals.Runs)
		for j := range projected.Code[i].Locals.Runs {
			if exceptionReferenceType(projected.Code[i].Locals.Runs[j].Type) {
				projected.Code[i].Locals.Runs[j].Type = wasm.I64
			}
		}
	}
	projected.Globals = slices.Clone(m.Globals)
	for i := range projected.Globals {
		global := &projected.Globals[i]
		if global.Type.Type.Kind() == wasm.ValRef {
			global.Type.Type = wasm.I64
			global.Init = exceptionAdmissionInitializer(global.Init, true)
		}
	}
	projected.Tables = slices.Clone(m.Tables)
	for i := range projected.Tables {
		table := &projected.Tables[i]
		if exceptionReferenceType(wasm.RefVal(table.Type.Ref)) {
			table.Type.Ref = wasm.ExternRef.Ref()
			if table.Init != nil {
				init := exceptionAdmissionInitializer(*table.Init, false)
				table.Init = &init
			}
		}
	}
	projected.Elements = slices.Clone(m.Elements)
	for i := range projected.Elements {
		element := &projected.Elements[i]
		if element.Kind.Kind == wasm.ElemTypedExprs && exceptionReferenceType(wasm.RefVal(element.Kind.Ref)) {
			element.Kind.Ref = wasm.ExternRef.Ref()
			element.Kind.Exprs = slices.Clone(element.Kind.Exprs)
			for j := range element.Kind.Exprs {
				element.Kind.Exprs[j] = exceptionAdmissionInitializer(element.Kind.Exprs[j], false)
			}
		}
	}
	return &projected
}

func exceptionAdmissionInitializer(expr wasm.Expr, integer bool) wasm.Expr {
	// The original module was validated. The state projection preserves null
	// and immutable-global initializers, while changing only their carrier.
	if len(expr.BodyBytes) != 0 && expr.BodyBytes[0] == 0xd0 {
		if integer {
			expr.BodyBytes = []byte{0x42, 0, 0x0b}
		} else {
			expr.BodyBytes = []byte{0xd0, 0x6f, 0x0b}
		}
	}
	return expr
}

func exceptionAdmissionField(field wasm.FieldType) wasm.FieldType {
	storage := field.Storage()
	if !storage.Packed() && exceptionReferenceType(storage.Val()) {
		ref := storage.Val().Ref()
		return wasm.NewFieldType(wasm.StorageVal(wasm.RefVal(wasm.Ref(ref.Nullable(), wasm.AbsHeap(wasm.HeapI31), false))), field.Mut())
	}
	return field
}

// This is only a cheap candidate check. Any parse error falls through to the
// real decoder; no permission or type claim comes from this scan.
func exceptionWireCandidate(sections []byte) bool {
	r := wasm.ReaderFrom(sections)
	for r.HasNext() {
		id, err := r.Byte()
		if err != nil {
			return true
		}
		size, err := r.U32()
		if err != nil {
			return true
		}
		payload, err := r.Bytes(int(size))
		if err != nil {
			return true
		}
		if id == 13 {
			return true
		}
		if id == 1 {
			types := wasm.ReaderFrom(payload)
			// The public grammar excludes exn/noexn declarations. A failed
			// grammar check is only a candidate for the full EH validator.
			if admitPublicFunctionTypeWire(&types, true, true) != nil {
				return true
			}
		}
		if id == 6 {
			globals := wasm.ReaderFrom(payload)
			n, err := globals.U32()
			if err != nil || uint64(n) > uint64(globals.BytesLeft()) {
				return true
			}
			for range n {
				if exceptionWireValueCandidate(&globals) {
					return true
				}
				if _, err := globals.Byte(); err != nil {
					return true
				}
				for {
					op, err := globals.Byte()
					if err != nil {
						return true
					}
					if op == 0x0b {
						break
					}
					if err := wasm.SkipInstructionImmediate(&globals, op); err != nil {
						return true
					}
				}
			}
		}
		if id != 10 {
			continue
		}
		code := wasm.ReaderFrom(payload)
		n, err := code.U32()
		if err != nil {
			return true
		}
		for i := uint32(0); i < n; i++ {
			size, err := code.U32()
			if err != nil {
				return true
			}
			body, err := code.Bytes(int(size))
			if err != nil {
				return true
			}
			fn := wasm.ReaderFrom(body)
			runs, err := fn.U32()
			if err != nil {
				return true
			}
			for j := uint32(0); j < runs; j++ {
				if _, err := fn.U32(); err != nil {
					return true
				}
				typ, err := fn.Byte()
				if err != nil {
					return true
				}
				if typ == byte(wasm.HeapExn) || typ == byte(wasm.HeapNoExn) {
					return true
				}
				if typ == 0x63 || typ == 0x64 {
					heap, err := fn.S33()
					if err != nil || heap == -23 || heap == -12 {
						return true
					}
				}
			}
			for fn.HasNext() {
				op, err := fn.Byte()
				if err != nil {
					return true
				}
				if op == 0x08 || op == 0x0a || op == 0x1f {
					return true
				}
				var immediate wasm.InstructionImmediate
				if err := wasm.ClassifyInstructionImmediateInto(&fn, op, &immediate); err != nil {
					return true
				}
			}
		}
	}
	return false
}

// A malformed or exception value type is a candidate, never an admission.
func exceptionWireValueCandidate(r *wasm.Reader) bool {
	typ, err := r.Byte()
	if err != nil || typ == byte(wasm.HeapExn) || typ == byte(wasm.HeapNoExn) {
		return true
	}
	if typ == 0x63 || typ == 0x64 {
		heap, err := r.S33()
		return err != nil || heap == -23 || heap == -12
	}
	return false
}

func AdmitExceptionHandlingSource(source []byte) bool {
	if len(source) < 8 || len(source) > MaxModuleBytes || !bytes.Equal(source[:8], []byte{0, 'a', 's', 'm', 1, 0, 0, 0}) || !exceptionWireCandidate(source[8:]) {
		return false
	}
	m, err := wasm.DecodeModule(source)
	return err == nil && AdmitExceptionHandlingModule(m)
}

func AdmitExceptionHandlingInitializers(m *wasm.Module) error {
	if !AdmitExceptionHandlingModule(m) {
		return unsupported("exception initializer source")
	}
	return AdmitPublicInitializers(exceptionAdmissionProjection(m))
}

func AdmitExceptionHandlingModule(m *wasm.Module) bool {
	if m == nil || len(m.Code) > MaxFunctions || m.TagCount() > MaxFunctions {
		return false
	}
	hasException := m.TagCount() != 0 || exceptionReferenceDeclarations(m)
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	for i := range m.Code {
		if hasException {
			break
		}
		r := wasm.NewReader(m.Code[i].BodyBytes)
		for r.HasNext() {
			op, err := r.Byte()
			if err != nil {
				return false
			}
			var imm wasm.InstructionImmediate
			if err := classifier.ClassifyInto(r, op, &imm); err != nil {
				return false
			}
			hasException = hasException || op == 0x08 || op == 0x0a || op == 0x1f
		}
	}
	if !hasException {
		return false
	}
	features := wasm.ValidationFeatures{MultiMemory: true, ExtendedConstGlobals: true, GCConstExpr: true, CompactImports: true}
	if wasm.ValidateModuleWithConfig(m, features, 1, wasm.ValidationLimits{}) != nil {
		return false
	}
	for tag := 0; tag < m.TagCount(); tag++ {
		if _, err := exceptionTagSignature(m, uint32(tag)); err != nil {
			return false
		}
	}
	vector, err := UsesSWAR(m)
	if err != nil {
		return false
	}
	projection := exceptionAdmissionProjection(m)
	if vector {
		return admitSWARDecodedBudgetReferences(projection, true, true, true, admitRV64GCRootedScalarModule(projection)) == nil
	}
	return admitShapeProfileFunctions(projection, true, true, true, true, false) == nil || admitRV64GCRootedScalarModule(projection)
}

func restoreExceptionStateTypes(m *wasm.Module, state *moduleState) {
	for i := range state.globals {
		if global, ok := m.GlobalTypeByIndex(uint32(i)); ok && global.Type.Kind() == wasm.ValRef {
			state.globals[i].typ, state.globals[i].wide, state.globals[i].reference = global.Type, true, true
			state.localMutableReferences, state.importReferences = true, true
		}
	}
	for i := range state.tableCompactRef {
		if table, ok := m.TableType(uint32(i)); ok && exceptionReferenceType(wasm.RefVal(table.Ref)) {
			state.tableCompactRef[i] = true
		}
	}
	flat := 0
	for i := range m.Types {
		for j := range m.Types[i].SubTypes {
			if flat < len(state.types) {
				state.types[flat].signature = &m.Types[i].SubTypes[j].Comp
				state.types[flat].key, _ = m.StructuralTypeKeyChecked(uint32(flat))
			}
			flat++
		}
	}
	for i := range state.functions {
		if sig, ok := m.FuncSignature(uint32(i)); ok {
			state.functions[i].signature = sig
			if idx, ok := m.FuncTypeIndex(uint32(i)); ok {
				state.functions[i].key, _ = m.StructuralTypeKeyChecked(idx.Index)
			}
		}
	}
}

func compileExceptionHandlingModule(m *wasm.Module, opts CompileOptions) (*enc.CompiledModule, error) {
	if !AdmitExceptionHandlingModule(m) || !opts.Interruptible || !opts.InvocationContext || !opts.ForeignTableCalls {
		return nil, unsupported("exception source or invocation ABI")
	}
	if err := AdmitPublicInitializers(exceptionAdmissionProjection(m)); err != nil {
		return nil, err
	}
	if err := admitProductionOptions(m, &opts, true); err != nil {
		return nil, err
	}
	if opts.gcRootedScalar && !validateGCFrameRootPlansOwned(m, opts.GCFrameRoots, opts.OwnedExceptionReferences, opts.CustomInstructions) {
		return nil, unsupported("exception collector frame roots")
	}
	opts.production, opts.hostImports, opts.publicStarts, opts.ForceFallback = true, true, true, true
	if vector, err := UsesSWAR(m); err != nil {
		return nil, err
	} else if vector || opts.pluginDirectory.hasCustom() {
		return compileSWARProductionModule(m, opts)
	}
	c, err := compileDecodedModule(m, scalarRegisters[:], opts)
	if err != nil {
		return nil, err
	}
	return stageAdmittedProductionModule(c, opts)
}

// ManagedExceptionReferences identifies source that needs owned exception
// values. It is a resource classifier; full source validation stays separate.
func exceptionReferenceDeclarations(m *wasm.Module) bool {
	if m == nil {
		return false
	}
	for _, group := range m.Types {
		for _, sub := range group.SubTypes {
			for _, values := range [][]wasm.ValType{sub.Comp.Params, sub.Comp.Results} {
				for _, typ := range values {
					if exceptionReferenceType(typ) {
						return true
					}
				}
			}
			for _, field := range sub.Comp.Fields {
				if !field.Storage().Packed() && exceptionReferenceType(field.Storage().Val()) {
					return true
				}
			}
			if sub.Comp.Kind == wasm.CompArray && !sub.Comp.Array.Storage().Packed() && exceptionReferenceType(sub.Comp.Array.Storage().Val()) {
				return true
			}
		}
	}
	for _, global := range m.Globals {
		if exceptionReferenceType(global.Type.Type) {
			return true
		}
	}
	for i := 0; i < m.TableCount(); i++ {
		table, ok := m.TableType(uint32(i))
		if ok && exceptionReferenceType(wasm.RefVal(table.Ref)) {
			return true
		}
	}
	for _, im := range m.Imports {
		if im.Type.Kind == wasm.ExternGlobal && exceptionReferenceType(im.Type.GlobalType().Type) {
			return true
		}
	}
	return false
}

func ManagedExceptionReferences(m *wasm.Module) bool {
	if m == nil {
		return false
	}
	if exceptionReferenceDeclarations(m) {
		return true
	}
	for tag := 0; tag < m.TagCount(); tag++ {
		sig, err := exceptionTagSignature(m, uint32(tag))
		if err != nil {
			return true
		}
		for _, typ := range sig.Params {
			if kind, root := exceptionPayloadRootKind(m, typ); root && kind == nativeabi.RootGCRef {
				return true
			}
		}
	}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	for i := range m.Code {
		shape, err := shared.ScanEHFrameShape(&classifier, m.Code[i].BodyBytes)
		if err != nil || shape.RootRecords != 0 {
			return true
		}
	}
	return false
}

// ExceptionOwnedCopyCount is the static count of allocating catch-ref routes.
func ExceptionOwnedCopyCount(m *wasm.Module, function int) (int, error) {
	if m == nil || function < 0 || function >= len(m.Code) {
		return 0, unsupported("exception root function index")
	}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	shape, err := shared.ScanEHFrameShape(&classifier, m.Code[function].BodyBytes)
	return shape.RootRecords, err
}

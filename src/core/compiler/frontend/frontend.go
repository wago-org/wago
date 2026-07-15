// Package frontend contains compiler front-end passes shared by the CLI and API.
package frontend

import (
	"fmt"
	"strconv"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
)

// UnsupportedError reports a feature that decodes and validates as WebAssembly
// but is outside the subset currently executable by wago's runtime/backend.
type UnsupportedError struct {
	Category string
	Feature  string
	Context  string
}

func (e *UnsupportedError) Error() string {
	if e.Context != "" {
		return fmt.Sprintf("unsupported %s %s at %s", e.Category, e.Feature, e.Context)
	}
	return fmt.Sprintf("unsupported %s %s", e.Category, e.Feature)
}

// DecodeValidate decodes without materializing function-body instruction trees,
// validates, and runs wago's support pass over data.
func DecodeValidate(data []byte) (*wasm.Module, error) {
	m, err := wasm.DecodeModule(data)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		return nil, fmt.Errorf("validate: %w", err)
	}
	if err := RejectUnsupported(m); err != nil {
		return nil, err
	}
	return m, nil
}

// Features toggles the optional WebAssembly proposals the support pass accepts.
// It is a plain frontend-side set (no dependency on the public wago config) that
// callers map their feature configuration onto.
type Features struct {
	SignExtension           bool // i32/i64.extend{8,16,32}_s
	BulkMemory              bool // memory.copy/fill/init, passive data + data.drop, table.init/copy, elem.drop
	SaturatingTrunc         bool // i32/i64.trunc_sat_f32/f64_s/u (non-trapping float→int)
	ReferenceTypes          bool // executable funcref plus externref signatures/locals/host ABI
	TypedFunctionReferences bool // internal staged gate for indexed/non-null function refs and call_ref
	TailCalls               bool // internal staged gate for bounded direct/indirect tail-call contexts
	TypedTailCalls          bool // internal staged gate for bounded return_call_ref contexts
	MultiMemory             bool // internal staged gate for bounded indexed memory execution
	Memory64                bool // internal staged gate for bounded local 64-bit-address memory execution
	Table64                 bool // internal staged gate for bounded local 64-bit-index table execution
	SIMD                    bool // supported 0xfd v128 SIMD and relaxed-SIMD instructions
	ExtendedConst           bool // i32/i64 add/sub/mul and prior immutable global.get in const expressions
}

// AllFeatures is the full optional set wago's backend lowers today; it is the
// default applied by RejectUnsupported.
func AllFeatures() Features {
	return Features{SignExtension: true, BulkMemory: true, SaturatingTrunc: true, ReferenceTypes: true, SIMD: true, ExtendedConst: true}
}

// RejectUnsupported rejects modules that require features not explicitly wired
// through the current JIT/runtime, accepting wago's full optional feature set.
func RejectUnsupported(m *wasm.Module) error {
	return RejectUnsupportedWithFeatures(m, AllFeatures())
}

// RejectUnsupportedWithFeatures is RejectUnsupported with optional proposals
// gated by f: a disabled proposal makes its instructions unsupported. The pass
// is deliberately conservative: a construct must be listed here before it can
// reach code generation.
func RejectUnsupportedWithFeatures(m *wasm.Module, f Features) error {
	p := supportPass{m: m, feat: f}
	return p.run()
}

type supportPass struct {
	m    *wasm.Module
	feat Features
}

const (
	minOnlyTableGrowCapacity          uint64 = 64
	minOnlyExternrefTableGrowCapacity uint64 = 1024
	stagedTable64Max                  uint64 = 16384
)

// TableRuntimeShape is the instantiate-time size/capacity of one table.
type TableRuntimeShape struct {
	Size       int
	Capacity   int
	EntryBytes int
}

// StagedTable64Max is the finite entry ceiling for the first local table64
// execution slice. It is sized to keep a funcref table plus fixed instance state
// within the bounded instantiate arena.
func StagedTable64Max() uint64 { return stagedTable64Max }

// SupportedTableRuntimeShapes returns one runtime ABI shape per table. A
// declared zero-length table still has runtime presence: call_indirect needs a
// descriptor whose length is zero so it can trap before reading an entry.
// Min-only local tables reserve a small bounded growth window only when this
// module can grow or export a table; fixed-use tables retain their minimum-sized
// footprint. Imported table descriptors remain foreign-owned; local tables use
// the type-specific entry stride recorded in each returned shape.
// RequiresFuncRefDescriptors reports whether instantiation needs the canonical
// per-function descriptor arena. Funcref tables need it; externref-only tables do
// not. Table-free modules pay for it only when a global initializer or executable
// body contains ref.func.
func RequiresFuncRefDescriptors(m *wasm.Module) bool {
	if m == nil {
		return false
	}
	for tableIndex := 0; tableIndex < m.TableCount(); tableIndex++ {
		tt, ok := m.TableType(uint32(tableIndex))
		if !ok || !isExternRef(tt.Ref) {
			return true
		}
	}
	for i := range m.Globals {
		if exprUsesRefFunc(m.Globals[i].Init) {
			return true
		}
	}
	for i := range m.Elements {
		e := &m.Elements[i]
		if e.Kind.Kind == wasm.ElemFuncs && len(e.Kind.Funcs) != 0 {
			return true
		}
		for j := range e.Kind.Exprs {
			if exprUsesRefFunc(e.Kind.Exprs[j]) {
				return true
			}
		}
	}
	for i := range m.Code {
		body := wasm.Expr{Instrs: m.Code[i].Body.Instrs, BodyBytes: m.Code[i].BodyBytes}
		if exprUsesRefFunc(body) {
			return true
		}
	}
	return false
}

func exprUsesRefFunc(e wasm.Expr) bool {
	if len(e.BodyBytes) != 0 {
		r := wasm.NewReader(e.BodyBytes)
		for r.HasNext() {
			op, err := r.Byte()
			if err != nil {
				return true
			}
			if op == 0xd2 {
				return true
			}
			if _, err := wasm.ClassifyInstructionImmediate(r, op); err != nil {
				return true
			}
		}
		return false
	}
	return instrsUseRefFunc(e.Instrs)
}

func instrsUseRefFunc(instrs []wasm.Instruction) bool {
	for i := range instrs {
		in := &instrs[i]
		if in.Kind == wasm.InstrRefFunc {
			return true
		}
		if instrsUseRefFunc(in.Body().Instrs) || instrsUseRefFunc(in.Then()) || instrsUseRefFunc(in.Else()) {
			return true
		}
	}
	return false
}

func SupportedTableRuntimeShapes(m *wasm.Module) ([]TableRuntimeShape, error) {
	if m == nil {
		return nil, fmt.Errorf("nil module")
	}
	imported := m.ImportedTableCount()
	// Imports precede definitions in the Wasm table index space. Imported
	// descriptors belong to their exporting instances and consume no local table
	// arena bytes; every following shape describes one local table.
	shapes := make([]TableRuntimeShape, imported+len(m.Tables))
	for tableIndex := 0; tableIndex < imported; tableIndex++ {
		tt, ok := m.TableType(uint32(tableIndex))
		if !ok {
			return nil, fmt.Errorf("table %d type unavailable", tableIndex)
		}
		entryBytes := runtime.TableEntryBytes
		if isExternRef(tt.Ref) {
			entryBytes = 8
		}
		shapes[tableIndex].EntryBytes = entryBytes
	}
	for i := range m.Tables {
		tableIndex := imported + i
		min := m.Tables[i].Type.Limits.Min
		if min > uint64(maxInt()) {
			return nil, fmt.Errorf("table %d minimum %d overflows int", tableIndex, min)
		}
		entryBytes := runtime.TableEntryBytes
		if isExternRef(m.Tables[i].Type.Ref) {
			entryBytes = 8
		}
		max := min
		observableCapacity := moduleUsesTableGrow(m) || moduleExportsTable(m, uint32(tableIndex))
		if m.Tables[i].Type.Limits.Max != nil {
			max = *m.Tables[i].Type.Limits.Max
			// Preserve ordinary declared-capacity allocation. Only inert tables whose
			// spare capacity cannot be observed and cannot fit in the bounded arena are
			// represented at their minimum, admitting valid huge declarations without
			// changing grow/export semantics or common fixed-table footprints.
			if !observableCapacity && max > uint64((runtime.InstantiateArenaSize-8)/entryBytes) {
				max = min
			}
		} else if observableCapacity {
			reserve := minOnlyTableGrowCapacity
			if isExternRef(m.Tables[i].Type.Ref) {
				reserve = minOnlyExternrefTableGrowCapacity
			}
			if max < reserve {
				max = reserve
			}
		}
		if max > uint64(maxInt()) {
			return nil, fmt.Errorf("table %d maximum %d overflows int", tableIndex, max)
		}
		shapes[tableIndex] = TableRuntimeShape{Size: int(min), Capacity: int(max), EntryBytes: entryBytes}
	}
	return shapes, nil
}

// SupportedTableRuntimeShape preserves the single-table metadata API used by
// older callers. Multiple tables must use SupportedTableRuntimeShapes.
func SupportedTableRuntimeShape(m *wasm.Module) (hasTable bool, tableSize int, tableMax int, err error) {
	shapes, err := SupportedTableRuntimeShapes(m)
	if err != nil {
		return false, 0, 0, err
	}
	if len(shapes) > 1 {
		return false, 0, 0, fmt.Errorf("multiple tables require indexed runtime shapes")
	}
	if len(shapes) == 0 {
		return false, 0, 0, nil
	}
	return true, shapes[0].Size, shapes[0].Capacity, nil
}

func moduleUsesTableGrow(m *wasm.Module) bool {
	for i := range m.Code {
		fn := &m.Code[i]
		if len(fn.BodyBytes) != 0 {
			if bodyBytesUseTableGrow(fn.BodyBytes) {
				return true
			}
			continue
		}
		if instrsUseTableGrow(fn.Body.Instrs) {
			return true
		}
	}
	return false
}

func bodyBytesUseTableGrow(body []byte) bool {
	r := wasm.NewReader(body)
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return true
		}
		imm, err := wasm.ClassifyInstructionImmediate(r, op)
		if err != nil {
			return true
		}
		if imm.Kind == wasm.InstrTableGrow {
			return true
		}
	}
	return false
}

func instrsUseTableGrow(instrs []wasm.Instruction) bool {
	for i := range instrs {
		in := &instrs[i]
		if in.Kind == wasm.InstrTableGrow {
			return true
		}
		if instrsUseTableGrow(in.Body().Instrs) || instrsUseTableGrow(in.Then()) || instrsUseTableGrow(in.Else()) {
			return true
		}
	}
	return false
}

func moduleExportsTable(m *wasm.Module, tableIndex uint32) bool {
	for i := range m.Exports {
		if m.Exports[i].Index.Kind == wasm.ExternTable && uint32(m.Exports[i].Index.Index) == tableIndex {
			return true
		}
	}
	return false
}

func (p supportPass) unsupported(category, feature, context string) error {
	return &UnsupportedError{Category: category, Feature: feature, Context: context}
}

func (p supportPass) run() error {
	if err := p.types(); err != nil {
		return err
	}
	if err := p.imports(); err != nil {
		return err
	}
	if err := p.tables(); err != nil {
		return err
	}
	if err := p.memories(); err != nil {
		return err
	}
	if len(p.m.Tags) != 0 {
		return p.unsupported("exception handling", "tag section (exception-handling disabled)", "tag section")
	}
	if len(p.m.StringRefs) != 0 {
		return p.unsupported("stringref", "section", "stringrefs section")
	}
	// An imported start function is supported: Instantiate runs its host binding
	// (validation guarantees () -> ()).
	if err := p.globals(); err != nil {
		return err
	}
	if err := p.exports(); err != nil {
		return err
	}
	if err := p.elements(); err != nil {
		return err
	}
	if err := p.data(); err != nil {
		return err
	}
	if err := p.runtimeFootprint(); err != nil {
		return err
	}
	return p.funcs()
}

func (p supportPass) types() error {
	flat := 0
	for gi, rt := range p.m.Types {
		if len(rt.SubTypes) != 1 && !p.feat.TypedFunctionReferences {
			return p.unsupported("gc type", "recursive group (gc disabled)", fmt.Sprintf("type %d", gi))
		}
		for si := range rt.SubTypes {
			st := rt.SubTypes[si]
			typeIndex := flat
			ctx := fmt.Sprintf("type %d", typeIndex)
			flat++
			if st.HasPrefix || len(st.Supers) != 0 || st.Metadata.Describes != nil || st.Metadata.Descriptor != nil {
				return p.unsupported("gc type", "subtyping metadata (gc disabled)", ctx)
			}
			if st.Comp.Kind != wasm.CompFunc {
				return p.unsupported("gc type", compTypeName(st.Comp.Kind)+" (gc disabled)", ctx)
			}
			comp, ok := p.m.ResolvedTypeFunc(uint32(typeIndex))
			if !ok {
				return p.unsupported("gc type", "unresolved function type", ctx)
			}
			if !p.supportedValTypes(comp.Params) {
				if err := p.valTypes(comp.Params, ctx+" params"); err != nil {
					return err
				}
			}
			if !p.supportedValTypes(comp.Results) {
				if err := p.valTypes(comp.Results, ctx+" results"); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func compTypeName(k wasm.CompTypeKind) string {
	switch k {
	case wasm.CompArray:
		return "array type"
	case wasm.CompStruct:
		return "struct type"
	case wasm.CompFunc:
		return "function type"
	default:
		return "unknown type"
	}
}

func (p supportPass) imports() error {
	for i, im := range p.m.Imports {
		ctx := fmt.Sprintf("import %d %q.%q", i, im.Module, im.Name)
		switch im.Type.Kind {
		case wasm.ExternFunc:
			ft := p.funcType(im.Type.Type)
			if ft == nil {
				return p.unsupported("import", "function with unknown type", ctx)
			}
			// Reflection-free host imports admit externref handles and opaque funcref
			// tokens. Instantiation still requires explicit ownership before a host
			// descriptor itself may cross a public funcref boundary.
			for _, pt := range ft.Params {
				if !p.supportedValType(pt) {
					return p.valType(pt, ctx+" function signature")
				}
			}
			for _, rt := range ft.Results {
				if !p.supportedValType(rt) {
					return p.valType(rt, ctx+" function result")
				}
			}
		case wasm.ExternGlobal:
			// Imported reference globals are admitted structurally here; instantiation
			// requires an exact typed, mutable, compatible-store Global owner.
			if err := p.globalType(im.Type.Global.Type, ctx); err != nil {
				return err
			}
		case wasm.ExternTable:
			// Imported tables carry their exact reference type into the shared
			// runtime handle. Externref imports additionally require reference types
			// and a compatible store-bound owner at instantiation.
			if !isFuncRef(im.Type.Table.Ref) && !isExternRef(im.Type.Table.Ref) && !p.supportedTypedFuncRef(im.Type.Table.Ref) {
				return p.valType(wasm.RefVal(im.Type.Table.Ref), ctx+" table type")
			}
			if isExternRef(im.Type.Table.Ref) && !p.feat.ReferenceTypes {
				return p.unsupported("import", "externref table (reference-types disabled)", ctx)
			}
			if im.Type.Table.Limits.Addr64 {
				if !p.feat.Table64 {
					return p.unsupported("import", "64-bit table (table64 disabled)", ctx)
				}
				return p.unsupported("import", "64-bit table imports remain outside the staged table64 boundary", ctx)
			}
		case wasm.ExternMem:
			if err := p.checkMemType(im.Type.Mem, ctx); err != nil {
				return err
			}
		case wasm.ExternTag:
			return p.unsupported("import", "tag (exception-handling disabled)", ctx)
		default:
			return p.unsupported("import", "unknown external kind", ctx)
		}
	}
	return nil
}

func (p supportPass) tables() error {
	imported := p.m.ImportedTableCount()
	tableCount := imported + len(p.m.Tables)
	if tableCount > 1 && !p.feat.ReferenceTypes {
		return p.unsupported("table", "multiple tables (reference-types disabled)", "module")
	}
	for i, t := range p.m.Tables {
		tableIndex := imported + i
		ctx := fmt.Sprintf("table %d", tableIndex)
		if !isFuncRef(t.Type.Ref) && !isExternRef(t.Type.Ref) && !p.supportedTypedFuncRef(t.Type.Ref) {
			return p.valType(wasm.RefVal(t.Type.Ref), ctx)
		}
		if isExternRef(t.Type.Ref) && !p.feat.ReferenceTypes {
			return p.unsupported("reference type", "externref (reference-types disabled)", ctx)
		}
		if t.Type.Limits.Addr64 {
			if !p.feat.Table64 {
				return p.unsupported("table", "64-bit limits (table64 disabled)", ctx)
			}
			if t.Type.Limits.Max == nil {
				return p.unsupported("table", "table64 requires an explicit bounded maximum", ctx)
			}
			if *t.Type.Limits.Max > stagedTable64Max {
				return p.unsupported("table", fmt.Sprintf("table64 maximum %d exceeds staged ceiling %d", *t.Type.Limits.Max, stagedTable64Max), ctx)
			}
		}
		if t.Init != nil {
			if !p.feat.ReferenceTypes {
				return p.unsupported("table", "initializer expression (reference-types disabled)", ctx)
			}
			if err := p.elementExpr(*t.Init, ctx+" initializer"); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p supportPass) tableAddr64(index uint32) bool {
	tt, ok := p.m.TableType(index)
	return ok && tt.Limits.Addr64
}

func (p supportPass) memories() error {
	if p.m.ImportedMemCount()+len(p.m.Memories) > 1 && !p.feat.MultiMemory {
		return p.unsupported("memory", "multiple memories (multi-memory disabled)", "module")
	}
	for i, mem := range p.m.Memories {
		if err := p.checkMemType(mem, fmt.Sprintf("memory %d", i)); err != nil {
			return err
		}
	}
	return nil
}

// checkMemType rejects memory shapes outside wago's non-shared model. The staged
// memory64 path deliberately retains the existing 65535-page implementation
// reservation ceiling. A declaration without a maximum keeps its exact Wasm type
// and may fail growth at that finite resource ceiling; import/platform restrictions
// are enforced by the product compile boundary before allocation.
func (p supportPass) checkMemType(mem wasm.MemType, ctx string) error {
	if mem.Shared {
		return p.unsupported("memory", "shared", ctx)
	}
	if mem.Limits.Addr64 {
		if !p.feat.Memory64 {
			return p.unsupported("memory", "memory64 (memory64 disabled)", ctx)
		}
		if mem.Limits.Max != nil && *mem.Limits.Max > 65535 {
			return p.unsupported("memory", fmt.Sprintf("memory64 maximum %d pages exceeds staged ceiling 65535", *mem.Limits.Max), ctx)
		}
	}
	if mem.Limits.Min > 65535 {
		return p.unsupported("memory", fmt.Sprintf("minimum %d pages exceeds 65535", mem.Limits.Min), ctx)
	}
	return nil
}

func (p supportPass) globals() error {
	for i, g := range p.m.Globals {
		ctx := fmt.Sprintf("global %d", i)
		if err := p.globalType(g.Type.Type, ctx); err != nil {
			return err
		}
		if err := p.constExpr(g.Init, ctx+" initializer"); err != nil {
			return err
		}
	}
	return nil
}

func (p supportPass) exports() error {
	for i, ex := range p.m.Exports {
		switch ex.Index.Kind {
		case wasm.ExternFunc, wasm.ExternGlobal:
			// Supported metadata is serialized for function and numeric-global exports.
		case wasm.ExternTable:
			tt, ok := p.m.TableType(p.m.Exports[i].Index.Index)
			if !ok {
				return p.unsupported("export", "unknown table", fmt.Sprintf("export %d %q", i, ex.Name))
			}
			if isExternRef(tt.Ref) && !p.feat.ReferenceTypes {
				return p.unsupported("export", "externref table (reference-types disabled)", fmt.Sprintf("export %d %q", i, ex.Name))
			}
			// Table exports are resolved by their declared name and validated exact
			// type/store ownership at instantiation and public-handle boundaries.
		case wasm.ExternMem:
			// Memory exports are metadata-only for wago today; the instance exposes
			// linear memory directly, and preserving this keeps current MVP modules
			// that export memory runnable.
		case wasm.ExternTag:
			return p.unsupported("export", "tag (exception-handling disabled)", fmt.Sprintf("export %d %q", i, ex.Name))
		default:
			return p.unsupported("export", "unknown external kind", fmt.Sprintf("export %d %q", i, ex.Name))
		}
	}
	return nil
}

func (p supportPass) elements() error {
	for i, e := range p.m.Elements {
		ctx := fmt.Sprintf("element %d", i)
		switch e.Mode.Kind {
		case wasm.ElemActive:
			if err := p.constExpr(e.Mode.Offset, ctx+" offset"); err != nil {
				return err
			}
		case wasm.ElemPassive:
			if !p.feat.BulkMemory {
				return p.unsupported("element", "passive segment (bulk-memory-operations disabled)", ctx)
			}
		case wasm.ElemDeclarative:
			// Declarative segments only declare ref.func targets; no runtime storage.
		default:
			return p.unsupported("element", elemModeName(e.Mode.Kind), ctx)
		}
		switch e.Kind.Kind {
		case wasm.ElemFuncs:
		case wasm.ElemFuncExprs, wasm.ElemTypedExprs:
			if !p.feat.ReferenceTypes {
				return p.unsupported("reference type", elemKindName(e.Kind.Kind), ctx)
			}
			if e.Kind.Kind == wasm.ElemTypedExprs && !isFuncRef(e.Kind.Ref) && !isExternRef(e.Kind.Ref) && !p.supportedTypedFuncRef(e.Kind.Ref) {
				return p.valType(wasm.RefVal(e.Kind.Ref), ctx)
			}
			for j, ex := range e.Kind.Exprs {
				if err := p.elementExpr(ex, fmt.Sprintf("%s expression %d", ctx, j)); err != nil {
					return err
				}
			}
		default:
			return p.unsupported("reference type", elemKindName(e.Kind.Kind), ctx)
		}
	}
	return nil
}

func (p supportPass) elementExpr(e wasm.Expr, context string) error {
	if _, err := wasm.ParseElementExpr(e); err != nil {
		return p.unsupported("element expression", err.Error(), context)
	}
	return nil
}

func elemModeName(k wasm.ElemModeKind) string {
	switch k {
	case wasm.ElemPassive:
		return "passive segment"
	case wasm.ElemActive:
		return "active segment"
	case wasm.ElemDeclarative:
		return "declarative segment"
	default:
		return "unknown segment mode"
	}
}

func elemKindName(k wasm.ElemKindKind) string {
	switch k {
	case wasm.ElemFuncs:
		return "function index segment"
	case wasm.ElemFuncExprs:
		return "function expression segment"
	case wasm.ElemTypedExprs:
		return "typed expression segment"
	default:
		return "unknown segment kind"
	}
}

func (p supportPass) data() error {
	for i, d := range p.m.Data {
		switch d.Mode.Kind {
		case wasm.DataActive:
			ctx := fmt.Sprintf("data %d", i)
			if d.Mode.Mem != 0 && !p.feat.MultiMemory {
				return p.unsupported("data", fmt.Sprintf("memory index %d", d.Mode.Mem), ctx)
			}
			if err := p.constExpr(d.Mode.Offset, ""); err != nil {
				if unsupported, ok := err.(*UnsupportedError); ok {
					copy := *unsupported
					copy.Context = fmt.Sprintf("data %d offset%s", i, unsupported.Context)
					return &copy
				}
				return fmt.Errorf("data %d offset: %w", i, err)
			}
		case wasm.DataPassive:
			if !p.feat.BulkMemory {
				ctx := fmt.Sprintf("data %d", i)
				return p.unsupported("data", "passive segment (bulk-memory-operations disabled)", ctx)
			}
		default:
			ctx := fmt.Sprintf("data %d", i)
			return p.unsupported("data", "unknown segment mode", ctx)
		}
	}
	return nil
}

func (p supportPass) runtimeFootprint() error {
	tables, err := SupportedTableRuntimeShapes(p.m)
	if err != nil {
		return p.unsupported("runtime footprint", err.Error(), "instantiate arena")
	}
	maxParams, maxResults := p.maxLocalFuncSlots()
	funcRefCount := 0
	if RequiresFuncRefDescriptors(p.m) {
		funcRefCount = p.m.FuncCount() + 1
	}
	tableCaps := make([]int, len(tables))
	tableEntryBytes := make([]int, len(tables))
	for i := range tables {
		tableCaps[i] = tables[i].Capacity
		tableEntryBytes[i] = tables[i].EntryBytes
	}
	passiveElemBytes := 0
	for i := range p.m.Elements {
		e := &p.m.Elements[i]
		if e.Mode.Kind != wasm.ElemPassive {
			continue
		}
		stride := runtime.TableEntryBytes
		if e.Kind.Kind == wasm.ElemTypedExprs && isExternRef(e.Kind.Ref) {
			stride = 8
		}
		count := len(e.Kind.Funcs)
		if e.Kind.Kind != wasm.ElemFuncs {
			count = len(e.Kind.Exprs)
		}
		if count > (maxInt()-passiveElemBytes)/stride {
			return p.unsupported("runtime footprint", fmt.Sprintf("passive element %d payload overflows arena allocation", i), "instantiate arena")
		}
		passiveElemBytes += count * stride
	}
	hostCallBytes := 0
	if needsPublicFuncrefHostReentry(p.m, tables) {
		hostCallBytes = runtime.HostCtrlFrameBytes
	}
	need, err := runtime.InstantiateArenaNeed(runtime.InstantiateFootprint{
		FuncImportCount:    p.m.ImportedFuncCount(),
		HostCallBytes:      hostCallBytes,
		FuncRefCount:       funcRefCount,
		GlobalCount:        p.m.GlobalCount(),
		MemoryCount:        p.m.MemCount(),
		HasTable:           len(tables) != 0,
		TableCapacities:    tableCaps,
		TableEntryBytes:    tableEntryBytes,
		ImportedTableCount: p.m.ImportedTableCount(),
		ElemCount:          len(p.m.Elements),
		PassiveElemCount:   len(p.m.Elements),
		PassiveElemBytes:   passiveElemBytes,
		PassiveDataCount:   passiveDataDescriptorCount(p.m),
		MaxParamSlots:      maxParams,
		MaxResultSlots:     maxResults,
	})
	if err != nil {
		return p.unsupported("runtime footprint", err.Error(), "instantiate arena")
	}
	if need > runtime.InstantiateArenaSize {
		return p.unsupported("runtime footprint", fmt.Sprintf("instantiate arena need %d > limit %d", need, runtime.InstantiateArenaSize), "instantiate arena")
	}
	return nil
}

func needsPublicFuncrefHostReentry(m *wasm.Module, tables []TableRuntimeShape) bool {
	hasFuncrefTable := false
	for _, table := range tables {
		if table.EntryBytes == runtime.TableEntryBytes {
			hasFuncrefTable = true
			break
		}
	}
	if !hasFuncrefTable {
		return false
	}
	for li := range m.FuncTypes {
		ft, ok := m.LocalFuncType(li)
		if !ok {
			continue
		}
		for _, param := range ft.Params {
			if param.Kind == wasm.ValRef && isFuncRef(param.Ref) {
				return true
			}
		}
	}
	return false
}

func passiveDataDescriptorCount(m *wasm.Module) int {
	maxIdx := -1
	for i := range m.Data {
		if m.Data[i].Mode.Kind == wasm.DataPassive {
			maxIdx = i
		}
	}
	return maxIdx + 1
}

func (p supportPass) maxLocalFuncSlots() (params, results int) {
	for li := range p.m.FuncTypes {
		ft, ok := p.m.LocalFuncType(li)
		if !ok {
			continue
		}
		if len(ft.Params) > params {
			params = len(ft.Params)
		}
		if len(ft.Results) > results {
			results = len(ft.Results)
		}
	}
	return params, results
}

func (p supportPass) funcs() error {
	importedFuncs := p.m.ImportedFuncCount()
	for i, fn := range p.m.Code {
		ctx := "function " + strconv.Itoa(importedFuncs+i)
		for j, run := range fn.Locals.Runs {
			if p.supportedValType(run.Type) {
				continue
			}
			if err := p.valType(run.Type, fmt.Sprintf("%s local run %d", ctx, j)); err != nil {
				return err
			}
		}
		body := wasm.Expr{BodyBytes: fn.BodyBytes}
		if len(fn.BodyBytes) == 0 {
			body = fn.Body
		}
		if err := p.expr(body, ctx); err != nil {
			return err
		}
	}
	return nil
}

func (p supportPass) expr(e wasm.Expr, context string) error {
	if len(e.Instrs) == 0 && len(e.BodyBytes) != 0 {
		return p.exprBytes(e.BodyBytes, context)
	}
	for i, in := range e.Instrs {
		if err := p.instr(in, instructionContext(context, i)); err != nil {
			return err
		}
	}
	return nil
}

func instructionContext(context string, instr int) string {
	return context + " instruction " + strconv.Itoa(instr)
}

func (p supportPass) exprBytes(body []byte, context string) error {
	r := wasm.NewReader(body)
	for instr := 0; r.HasNext(); instr++ {
		op, err := r.Byte()
		if err != nil {
			return err
		}
		sawEnd, err := p.instrByte(r, op, context, instr)
		if err != nil {
			return err
		}
		if sawEnd && !r.HasNext() {
			return nil
		}
	}
	return nil
}

func (p supportPass) instrByte(r *wasm.Reader, op byte, context string, instr int) (bool, error) {
	ctx := func() string { return instructionContext(context, instr) }
	skipBlockType := func() error {
		b, err := r.Byte()
		if err != nil {
			return err
		}
		if b == 0x7b {
			if !p.feat.SIMD {
				return p.unsupported("value type", "v128 (simd disabled)", ctx())
			}
			return nil
		}
		if b == 0x40 || b == 0x7f || b == 0x7e || b == 0x7d || b == 0x7c {
			return nil
		}
		if isRefTypeLeadByte(b) {
			if b == 0x63 || b == 0x64 {
				heap, err := r.S33()
				if err != nil {
					return err
				}
				if !p.feat.ReferenceTypes || !p.feat.TypedFunctionReferences || !p.supportedTypedFuncHeap(heap) {
					return p.unsupported("value type", fmt.Sprintf("ref heap %d (typed-function-references disabled or unsupported)", heap), ctx())
				}
				return nil
			}
			if p.feat.ReferenceTypes {
				return nil
			}
			return p.unsupported("value type", fmt.Sprintf("0x%02x", b), ctx())
		}
		// Multi-value block type: the first byte was part of a signed LEB. The
		// validator has already checked that it resolves to a valid type.
		for b&0x80 != 0 {
			var err error
			b, err = r.Byte()
			if err != nil {
				return err
			}
		}
		return nil
	}
	skipValType := func() error {
		b, err := r.Byte()
		if err != nil {
			return err
		}
		if b == 0x7b {
			if !p.feat.SIMD {
				return p.unsupported("value type", "v128 (simd disabled)", ctx())
			}
			return nil
		}
		if b == 0x7f || b == 0x7e || b == 0x7d || b == 0x7c {
			return nil
		}
		if isRefTypeLeadByte(b) && p.feat.ReferenceTypes {
			if b == 0x63 || b == 0x64 {
				heap, err := r.S33()
				if err != nil {
					return err
				}
				if !p.feat.TypedFunctionReferences || !p.supportedTypedFuncHeap(heap) {
					return p.unsupported("value type", fmt.Sprintf("ref heap %d (typed-function-references disabled or unsupported)", heap), ctx())
				}
			}
			return nil
		}
		return p.unsupported("value type", fmt.Sprintf("0x%02x", b), ctx())
	}
	switch op {
	case 0x00, 0x01, 0x05, 0x0f, 0x1a, 0x1b,
		0x45, 0x46, 0x47, 0x48, 0x49, 0x4a, 0x4b, 0x4c, 0x4d, 0x4e, 0x4f,
		0x50, 0x51, 0x52, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59, 0x5a,
		0x5b, 0x5c, 0x5d, 0x5e, 0x5f, 0x60, 0x61, 0x62, 0x63, 0x64, 0x65,
		0x66, 0x67, 0x68, 0x69, 0x6a, 0x6b, 0x6c, 0x6d, 0x6e, 0x6f, 0x70,
		0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78, 0x79, 0x7a, 0x7b,
		0x7c, 0x7d, 0x7e, 0x7f, 0x80, 0x81, 0x82, 0x83, 0x84, 0x85, 0x86,
		0x87, 0x88, 0x89, 0x8a, 0x8b, 0x8c, 0x8d, 0x8e, 0x8f, 0x90, 0x91,
		0x92, 0x93, 0x94, 0x95, 0x96, 0x97, 0x98, 0x99, 0x9a, 0x9b, 0x9c,
		0x9d, 0x9e, 0x9f, 0xa0, 0xa1, 0xa2, 0xa3, 0xa4, 0xa5, 0xa6, 0xa7,
		0xa8, 0xa9, 0xaa, 0xab, 0xac, 0xad, 0xae, 0xaf, 0xb0, 0xb1, 0xb2,
		0xb3, 0xb4, 0xb5, 0xb6, 0xb7, 0xb8, 0xb9, 0xba, 0xbb, 0xbc, 0xbd,
		0xbe, 0xbf:
		return false, nil
	case 0x0b:
		return true, nil
	case 0x02, 0x03, 0x04:
		return false, skipBlockType()
	case 0x0c, 0x0d, 0x10, 0x20, 0x21, 0x22, 0x23, 0x24, 0x0e:
		err := wasm.SkipInstructionImmediate(r, op)
		return false, err
	case 0x11:
		imm, err := wasm.ClassifyInstructionImmediate(r, op)
		if err != nil {
			return false, err
		}
		if p.tableAddr64(uint32(imm.Index2)) {
			return false, p.unsupported("table64 instruction", "call_indirect outside staged get/set/grow/size/fill family", ctx())
		}
		if imm.Index2 != 0 && !p.feat.ReferenceTypes {
			return false, p.unsupported("table", fmt.Sprintf("call_indirect table %d (reference-types disabled)", imm.Index2), ctx())
		}
		return false, nil
	case 0x12, 0x13, 0x14, 0x15:
		if err := wasm.SkipInstructionImmediate(r, op); err != nil {
			return false, err
		}
		switch op {
		case 0x12, 0x13:
			if p.feat.TailCalls {
				return false, nil
			}
			return false, p.unsupported("instruction", "tail-call disabled", ctx())
		case 0x14:
			if p.feat.TypedFunctionReferences {
				return false, nil
			}
			return false, p.unsupported("instruction", "typed-function-references disabled", ctx())
		case 0x15:
			if p.feat.TypedFunctionReferences && p.feat.TypedTailCalls {
				return false, nil
			}
			return false, p.unsupported("instruction", "typed reference tail calls disabled", ctx())
		default:
			return false, p.unsupported("instruction", "tail-call and typed-function-references disabled", ctx())
		}
	case 0x1c:
		n, err := r.U32()
		if err != nil {
			return false, err
		}
		for i := uint32(0); i < n; i++ {
			if err := skipValType(); err != nil {
				return false, err
			}
		}
		return false, nil
	case 0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f, 0x30, 0x31, 0x32,
		0x33, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x3a, 0x3b, 0x3c, 0x3d,
		0x3e:
		align, err := r.U32()
		if err != nil {
			return false, err
		}
		var memIndex uint32
		explicit := align >= 64 && align < 128
		if explicit {
			memIndex, err = r.U32()
			if err != nil {
				return false, err
			}
		}
		if explicit && !p.feat.MultiMemory {
			return false, p.unsupported("memory", fmt.Sprintf("explicit index %d", memIndex), ctx())
		}
		mt, ok := p.m.MemoryType(memIndex)
		if !ok {
			return false, p.unsupported("memory", fmt.Sprintf("unknown index %d", memIndex), ctx())
		}
		if mt.Limits.Addr64 {
			if _, err := r.U64(); err != nil {
				return false, err
			}
			if !stagedMemory64ScalarOpcode(op) {
				return false, p.unsupported("memory64 instruction", fmt.Sprintf("opcode 0x%02x outside staged scalar family", op), ctx())
			}
		} else if _, err := r.U32(); err != nil {
			return false, err
		}
		return false, nil
	case 0x3f, 0x40:
		index, err := r.U32()
		if err != nil {
			return false, err
		}
		if index != 0 && !p.feat.MultiMemory {
			return false, &wasm.DecodeError{Code: wasm.ErrInvalidInstruction, Offset: r.Offset() - 1}
		}
		return false, nil
	case 0x41, 0x42, 0x43, 0x44:
		err := wasm.SkipInstructionImmediate(r, op)
		return false, err
	case 0xc0, 0xc1, 0xc2, 0xc3, 0xc4:
		if !p.feat.SignExtension {
			return false, p.unsupported("instruction", "sign-extension-ops disabled", ctx())
		}
		return false, nil
	case 0x25, 0x26:
		imm, err := wasm.ClassifyInstructionImmediate(r, op)
		if err != nil {
			return false, err
		}
		if !p.feat.ReferenceTypes {
			name := "table.get"
			if op == 0x26 {
				name = "table.set"
			}
			return false, p.unsupported("instruction", name+" (reference-types disabled)", ctx())
		}
		if p.tableAddr64(uint32(imm.Index)) && !p.feat.Table64 {
			return false, p.unsupported("table64 instruction", imm.Kind.String()+" (table64 disabled)", ctx())
		}
		return false, nil
	case 0xd0:
		heap, err := r.S33()
		if err != nil {
			return false, err
		}
		if !p.feat.ReferenceTypes {
			return false, p.unsupported("reference instruction", "RefNull", ctx())
		}
		if heap != -17 && heap != -14 && heap != -16 && heap != -13 && (!p.feat.TypedFunctionReferences || !p.supportedTypedFuncHeap(heap)) {
			return false, p.unsupported("reference instruction", fmt.Sprintf("ref.null heap %d", heap), ctx())
		}
		return false, nil
	case 0xd2:
		if _, err := r.U32(); err != nil {
			return false, err
		}
		if !p.feat.ReferenceTypes {
			return false, p.unsupported("reference instruction", "RefFunc", ctx())
		}
		return false, nil
	case 0xd1, 0xd3:
		if !p.feat.ReferenceTypes {
			name := "RefIsNull"
			if op == 0xd3 {
				name = "RefEq"
			}
			return false, p.unsupported("reference instruction", name, ctx())
		}
		return false, nil
	case 0xd4:
		if !p.feat.ReferenceTypes || !p.feat.TypedFunctionReferences {
			return false, p.unsupported("reference instruction", "ref.as_non_null (typed-function-references disabled)", ctx())
		}
		return false, nil
	case 0xd5, 0xd6:
		if _, err := r.U32(); err != nil {
			return false, err
		}
		if !p.feat.ReferenceTypes || !p.feat.TypedFunctionReferences {
			name := "br_on_null"
			if op == 0xd6 {
				name = "br_on_non_null"
			}
			return false, p.unsupported("reference instruction", name+" (typed-function-references disabled)", ctx())
		}
		return false, nil
	case 0xfd:
		var imm wasm.InstructionImmediate
		memarg64 := false
		for i := 0; i < p.m.MemCount(); i++ {
			if mt, ok := p.m.MemoryType(uint32(i)); ok && mt.Limits.Addr64 {
				memarg64 = true
				break
			}
		}
		err := wasm.ClassifyInstructionImmediateIntoWithMemarg64(r, op, &imm, memarg64)
		if err != nil {
			return false, err
		}
		if !p.feat.SIMD {
			return false, p.unsupported("instruction", "simd disabled", ctx())
		}
		if imm.HasMemIndex && !p.feat.MultiMemory {
			return false, p.unsupported("memory", fmt.Sprintf("explicit index %d", imm.MemIndex), ctx())
		}
		if imm.TouchesMemory {
			if mt, ok := p.m.MemoryType(imm.MemIndex); ok && mt.Limits.Addr64 && !stagedMemory64SIMDInstruction(imm.Kind) {
				return false, p.unsupported("memory64 instruction", simdUnsupportedName(imm)+" outside staged SIMD family", ctx())
			}
		}
		if !supportedSIMDInstruction(imm) {
			return false, p.unsupported("instruction", simdUnsupportedName(imm), ctx())
		}
		return false, nil
	case 0xfb, 0xfe:
		if err := wasm.SkipInstructionImmediate(r, op); err != nil {
			return false, err
		}
		return false, p.unsupported("instruction", fmt.Sprintf("opcode 0x%02x", op), ctx())
	case 0xfc:
		return false, p.fcInstrByte(r, ctx)
	default:
		return false, p.unsupported("instruction", fmt.Sprintf("opcode 0x%02x", op), ctx())
	}
}

func simdUnsupportedName(imm wasm.InstructionImmediate) string {
	if imm.Kind == wasm.InstrInvalid {
		return fmt.Sprintf("0xFD opcode %d", imm.Subopcode)
	}
	return imm.Kind.String()
}

func supportedSIMDInstruction(imm wasm.InstructionImmediate) bool {
	if imm.Subopcode == 12 || imm.Subopcode == 13 { // classifyFDBytes skips the 16-byte v128.const/shuffle immediate without allocating payloads.
		return true
	}
	switch imm.Kind {
	case wasm.InstrV128Load, wasm.InstrV128Store,
		wasm.InstrI8x16Swizzle, wasm.InstrI8x16RelaxedSwizzle,
		wasm.InstrI32x4RelaxedTruncF32x4S, wasm.InstrI32x4RelaxedTruncF32x4U,
		wasm.InstrI32x4RelaxedTruncZeroF64x2S, wasm.InstrI32x4RelaxedTruncZeroF64x2U,
		wasm.InstrF32x4RelaxedMadd, wasm.InstrF32x4RelaxedNmadd, wasm.InstrF64x2RelaxedMadd, wasm.InstrF64x2RelaxedNmadd,
		wasm.InstrI8x16RelaxedLaneselect, wasm.InstrI16x8RelaxedLaneselect, wasm.InstrI32x4RelaxedLaneselect, wasm.InstrI64x2RelaxedLaneselect,
		wasm.InstrF32x4RelaxedMin, wasm.InstrF32x4RelaxedMax, wasm.InstrF64x2RelaxedMin, wasm.InstrF64x2RelaxedMax,
		wasm.InstrI16x8RelaxedQ15mulrS, wasm.InstrI16x8RelaxedDotI8x16I7x16S, wasm.InstrI32x4RelaxedDotI8x16I7x16AddS,
		wasm.InstrV128Load8x8S, wasm.InstrV128Load8x8U, wasm.InstrV128Load16x4S, wasm.InstrV128Load16x4U, wasm.InstrV128Load32x2S, wasm.InstrV128Load32x2U,
		wasm.InstrV128Load8Splat, wasm.InstrV128Load16Splat, wasm.InstrV128Load32Splat, wasm.InstrV128Load64Splat,
		wasm.InstrV128Load32Zero, wasm.InstrV128Load64Zero,
		wasm.InstrV128Load8Lane, wasm.InstrV128Load16Lane, wasm.InstrV128Load32Lane, wasm.InstrV128Load64Lane,
		wasm.InstrV128Store8Lane, wasm.InstrV128Store16Lane, wasm.InstrV128Store32Lane, wasm.InstrV128Store64Lane,
		wasm.InstrI8x16Splat, wasm.InstrI16x8Splat, wasm.InstrI32x4Splat, wasm.InstrI64x2Splat,
		wasm.InstrF32x4Splat, wasm.InstrF64x2Splat,
		wasm.InstrI8x16ExtractLaneS, wasm.InstrI8x16ExtractLaneU, wasm.InstrI8x16ReplaceLane,
		wasm.InstrI16x8ExtractLaneS, wasm.InstrI16x8ExtractLaneU, wasm.InstrI16x8ReplaceLane,
		wasm.InstrI32x4ExtractLane, wasm.InstrI32x4ReplaceLane,
		wasm.InstrI64x2ExtractLane, wasm.InstrI64x2ReplaceLane,
		wasm.InstrF32x4ExtractLane, wasm.InstrF32x4ReplaceLane,
		wasm.InstrF64x2ExtractLane, wasm.InstrF64x2ReplaceLane,
		wasm.InstrI8x16Eq, wasm.InstrI8x16Ne, wasm.InstrI8x16LtS, wasm.InstrI8x16LtU, wasm.InstrI8x16GtS, wasm.InstrI8x16GtU, wasm.InstrI8x16LeS, wasm.InstrI8x16LeU, wasm.InstrI8x16GeS, wasm.InstrI8x16GeU, wasm.InstrI8x16NarrowI16x8S, wasm.InstrI8x16NarrowI16x8U, wasm.InstrI8x16Shl, wasm.InstrI8x16ShrS, wasm.InstrI8x16ShrU, wasm.InstrI8x16Add, wasm.InstrI8x16AddSatS, wasm.InstrI8x16AddSatU, wasm.InstrI8x16Sub, wasm.InstrI8x16SubSatS, wasm.InstrI8x16SubSatU, wasm.InstrI8x16MinS, wasm.InstrI8x16MinU, wasm.InstrI8x16MaxS, wasm.InstrI8x16MaxU, wasm.InstrI8x16AvgrU,
		wasm.InstrI16x8ExtaddPairwiseI8x16S, wasm.InstrI16x8ExtaddPairwiseI8x16U, wasm.InstrI32x4ExtaddPairwiseI16x8S, wasm.InstrI32x4ExtaddPairwiseI16x8U,
		wasm.InstrI16x8Eq, wasm.InstrI16x8Ne, wasm.InstrI16x8LtS, wasm.InstrI16x8LtU, wasm.InstrI16x8GtS, wasm.InstrI16x8GtU, wasm.InstrI16x8LeS, wasm.InstrI16x8LeU, wasm.InstrI16x8GeS, wasm.InstrI16x8GeU, wasm.InstrI16x8Q15mulrSatS, wasm.InstrI16x8NarrowI32x4S, wasm.InstrI16x8NarrowI32x4U, wasm.InstrI16x8ExtendLowI8x16S, wasm.InstrI16x8ExtendHighI8x16S, wasm.InstrI16x8ExtendLowI8x16U, wasm.InstrI16x8ExtendHighI8x16U, wasm.InstrI16x8Shl, wasm.InstrI16x8ShrS, wasm.InstrI16x8ShrU, wasm.InstrI16x8Add, wasm.InstrI16x8AddSatS, wasm.InstrI16x8AddSatU, wasm.InstrI16x8Sub, wasm.InstrI16x8SubSatS, wasm.InstrI16x8SubSatU, wasm.InstrI16x8Mul, wasm.InstrI16x8MinS, wasm.InstrI16x8MinU, wasm.InstrI16x8MaxS, wasm.InstrI16x8MaxU, wasm.InstrI16x8AvgrU, wasm.InstrI16x8ExtmulLowI8x16S, wasm.InstrI16x8ExtmulHighI8x16S, wasm.InstrI16x8ExtmulLowI8x16U, wasm.InstrI16x8ExtmulHighI8x16U,
		wasm.InstrI8x16Abs, wasm.InstrI8x16Neg, wasm.InstrI8x16Popcnt,
		wasm.InstrI16x8Abs, wasm.InstrI16x8Neg,
		wasm.InstrI32x4Abs, wasm.InstrI32x4Neg,
		wasm.InstrI32x4Eq, wasm.InstrI32x4Ne, wasm.InstrI32x4LtS, wasm.InstrI32x4LtU, wasm.InstrI32x4GtS, wasm.InstrI32x4GtU, wasm.InstrI32x4LeS, wasm.InstrI32x4LeU, wasm.InstrI32x4GeS, wasm.InstrI32x4GeU, wasm.InstrI32x4ExtendLowI16x8S, wasm.InstrI32x4ExtendHighI16x8S, wasm.InstrI32x4ExtendLowI16x8U, wasm.InstrI32x4ExtendHighI16x8U, wasm.InstrI32x4Shl, wasm.InstrI32x4ShrS, wasm.InstrI32x4ShrU, wasm.InstrI32x4Add, wasm.InstrI32x4Sub, wasm.InstrI32x4Mul, wasm.InstrI32x4MinS, wasm.InstrI32x4MinU, wasm.InstrI32x4MaxS, wasm.InstrI32x4MaxU, wasm.InstrI32x4DotI16x8S, wasm.InstrI32x4ExtmulLowI16x8S, wasm.InstrI32x4ExtmulHighI16x8S, wasm.InstrI32x4ExtmulLowI16x8U, wasm.InstrI32x4ExtmulHighI16x8U,
		wasm.InstrI64x2Abs, wasm.InstrI64x2Neg,
		wasm.InstrI64x2ExtendLowI32x4S, wasm.InstrI64x2ExtendHighI32x4S, wasm.InstrI64x2ExtendLowI32x4U, wasm.InstrI64x2ExtendHighI32x4U,
		wasm.InstrI64x2Shl, wasm.InstrI64x2ShrS, wasm.InstrI64x2ShrU,
		wasm.InstrI64x2Eq, wasm.InstrI64x2Ne, wasm.InstrI64x2LtS, wasm.InstrI64x2GtS, wasm.InstrI64x2LeS, wasm.InstrI64x2GeS, wasm.InstrI64x2Add, wasm.InstrI64x2Sub, wasm.InstrI64x2Mul, wasm.InstrI64x2ExtmulLowI32x4S, wasm.InstrI64x2ExtmulHighI32x4S, wasm.InstrI64x2ExtmulLowI32x4U, wasm.InstrI64x2ExtmulHighI32x4U,
		wasm.InstrF32x4Eq, wasm.InstrF32x4Ne, wasm.InstrF32x4Lt, wasm.InstrF32x4Gt, wasm.InstrF32x4Le, wasm.InstrF32x4Ge,
		wasm.InstrF64x2Eq, wasm.InstrF64x2Ne, wasm.InstrF64x2Lt, wasm.InstrF64x2Gt, wasm.InstrF64x2Le, wasm.InstrF64x2Ge,
		wasm.InstrF32x4Abs, wasm.InstrF32x4Neg, wasm.InstrF32x4Ceil, wasm.InstrF32x4Floor, wasm.InstrF32x4Trunc, wasm.InstrF32x4Nearest, wasm.InstrF32x4Sqrt, wasm.InstrF32x4Add, wasm.InstrF32x4Sub, wasm.InstrF32x4Mul, wasm.InstrF32x4Div,
		wasm.InstrF32x4Min, wasm.InstrF32x4Max, wasm.InstrF32x4Pmin, wasm.InstrF32x4Pmax,
		wasm.InstrF64x2Abs, wasm.InstrF64x2Neg, wasm.InstrF64x2Ceil, wasm.InstrF64x2Floor, wasm.InstrF64x2Trunc, wasm.InstrF64x2Nearest, wasm.InstrF64x2Sqrt, wasm.InstrF64x2Add, wasm.InstrF64x2Sub, wasm.InstrF64x2Mul, wasm.InstrF64x2Div,
		wasm.InstrF64x2Min, wasm.InstrF64x2Max, wasm.InstrF64x2Pmin, wasm.InstrF64x2Pmax,
		wasm.InstrF32x4DemoteF64x2Zero, wasm.InstrF64x2PromoteLowF32x4,
		wasm.InstrI32x4TruncSatF32x4S, wasm.InstrI32x4TruncSatF32x4U, wasm.InstrF32x4ConvertI32x4S, wasm.InstrF32x4ConvertI32x4U,
		wasm.InstrI32x4TruncSatF64x2SZero, wasm.InstrI32x4TruncSatF64x2UZero, wasm.InstrF64x2ConvertLowI32x4S, wasm.InstrF64x2ConvertLowI32x4U,
		wasm.InstrV128Not, wasm.InstrV128And, wasm.InstrV128Andnot, wasm.InstrV128Or, wasm.InstrV128Xor, wasm.InstrV128Bitselect,
		wasm.InstrV128AnyTrue,
		wasm.InstrI8x16AllTrue, wasm.InstrI8x16Bitmask,
		wasm.InstrI16x8AllTrue, wasm.InstrI16x8Bitmask,
		wasm.InstrI32x4AllTrue, wasm.InstrI32x4Bitmask,
		wasm.InstrI64x2AllTrue, wasm.InstrI64x2Bitmask:
		return true
	}
	return false
}

func (p supportPass) fcInstrByte(r *wasm.Reader, context func() string) error {
	var imm wasm.InstructionImmediate
	err := wasm.ClassifyInstructionImmediateInto(r, 0xfc, &imm)
	if err != nil {
		return err
	}
	switch imm.Subopcode {
	case 0, 1, 2, 3, 4, 5, 6, 7:
		if !p.feat.SaturatingTrunc {
			return p.unsupported("instruction", "nontrapping-float-to-int-conversion disabled", context())
		}
		return nil
	case 8:
		if mt, ok := p.m.MemoryType(uint32(imm.Index2)); ok && mt.Limits.Addr64 && !p.feat.Memory64 {
			return p.unsupported("memory64 instruction", "memory.init (memory64 disabled)", context())
		}
		if !p.feat.BulkMemory {
			return p.unsupported("instruction", "memory.init (bulk-memory-operations disabled)", context())
		}
		if imm.Index2 != 0 && !p.feat.MultiMemory {
			return p.unsupported("memory", fmt.Sprintf("init memory index %d", imm.Index2), context())
		}
		return nil
	case 9:
		if !p.feat.BulkMemory {
			return p.unsupported("instruction", "data.drop (bulk-memory-operations disabled)", context())
		}
		return nil
	case 12:
		if p.tableAddr64(uint32(imm.Index2)) {
			return p.unsupported("table64 instruction", "table.init outside staged get/set/grow/size/fill family", context())
		}
		if !p.feat.ReferenceTypes {
			return p.unsupported("instruction", "table.init (reference-types disabled)", context())
		}
		if !p.feat.BulkMemory {
			return p.unsupported("instruction", "table.init (bulk-memory-operations disabled)", context())
		}
		return nil
	case 13:
		if !p.feat.ReferenceTypes {
			return p.unsupported("instruction", "elem.drop (reference-types disabled)", context())
		}
		if !p.feat.BulkMemory {
			return p.unsupported("instruction", "elem.drop (bulk-memory-operations disabled)", context())
		}
		return nil
	case 14:
		if p.tableAddr64(uint32(imm.Index)) || p.tableAddr64(uint32(imm.Index2)) {
			return p.unsupported("table64 instruction", "table.copy outside staged get/set/grow/size/fill family", context())
		}
		if !p.feat.ReferenceTypes {
			return p.unsupported("instruction", "table.copy (reference-types disabled)", context())
		}
		if !p.feat.BulkMemory {
			return p.unsupported("instruction", "table.copy (bulk-memory-operations disabled)", context())
		}
		return nil
	case 15:
		if p.tableAddr64(uint32(imm.Index)) && !p.feat.Table64 {
			return p.unsupported("table64 instruction", "table.grow (table64 disabled)", context())
		}
		if !p.feat.ReferenceTypes {
			return p.unsupported("instruction", "table.grow (reference-types disabled)", context())
		}
		return nil
	case 16:
		if !p.feat.ReferenceTypes {
			return p.unsupported("instruction", "table.size (reference-types disabled)", context())
		}
		if p.tableAddr64(uint32(imm.Index)) && !p.feat.Table64 {
			return p.unsupported("table64 instruction", "table.size (table64 disabled)", context())
		}
		return nil
	case 17:
		if p.tableAddr64(uint32(imm.Index)) && !p.feat.Table64 {
			return p.unsupported("table64 instruction", "table.fill (table64 disabled)", context())
		}
		if !p.feat.ReferenceTypes {
			return p.unsupported("instruction", "table.fill (reference-types disabled)", context())
		}
		return nil
	case 10:
		if !p.feat.BulkMemory {
			return p.unsupported("instruction", "memory.copy (bulk-memory-operations disabled)", context())
		}
		if (imm.Index != 0 || imm.Index2 != 0) && !p.feat.MultiMemory {
			return p.unsupported("memory", fmt.Sprintf("copy indexes %d,%d", imm.Index, imm.Index2), context())
		}
		return nil
	case 11:
		if !p.feat.BulkMemory {
			return p.unsupported("instruction", "memory.fill (bulk-memory-operations disabled)", context())
		}
		if imm.Index != 0 && !p.feat.MultiMemory {
			return p.unsupported("memory", fmt.Sprintf("fill index %d", imm.Index), context())
		}
		return nil
	default:
		return p.unsupported("instruction", fmt.Sprintf("0xfc %d", imm.Subopcode), context())
	}
}

func isRefTypeLeadByte(b byte) bool {
	return b == 0x63 || b == 0x64 || b == 0x6f || b == 0x70 || b == 0x6e || b == 0x6d || b == 0x6c || b == 0x6b || b == 0x6a || b == 0x69 || b == 0x71 || b == 0x72 || b == 0x73 || b == 0x74
}

func (p supportPass) constExpr(e wasm.Expr, context string) error {
	if len(e.Instrs) == 0 && len(e.BodyBytes) != 0 {
		return p.constExprBytes(e.BodyBytes, context)
	}
	for i, in := range e.Instrs {
		switch in.Kind {
		case wasm.InstrI32Const, wasm.InstrI64Const, wasm.InstrF32Const, wasm.InstrF64Const:
		case wasm.InstrGlobalGet:
			if int(in.Index) >= p.m.ImportedGlobalCount() && !p.feat.ExtendedConst {
				return p.unsupported("const expression", "prior global.get (extended-const-expressions disabled)", instructionContext(context, i))
			}
		case wasm.InstrI32Add, wasm.InstrI32Sub, wasm.InstrI32Mul,
			wasm.InstrI64Add, wasm.InstrI64Sub, wasm.InstrI64Mul:
			if !p.feat.ExtendedConst {
				return p.unsupported("const expression", in.Kind.String()+" (extended-const-expressions disabled)", instructionContext(context, i))
			}
		case wasm.InstrV128Const:
			if !p.feat.SIMD {
				return p.unsupported("const expression", "v128.const (simd disabled)", instructionContext(context, i))
			}
		case wasm.InstrRefNull:
			if !p.feat.ReferenceTypes {
				return p.unsupported("const expression", "ref.null (reference-types disabled)", instructionContext(context, i))
			}
			if !isNullableAbsRef(in.RefType()) && !p.supportedTypedFuncRef(in.RefType()) {
				return p.unsupported("const expression", "ref.null "+refTypeName(in.RefType()), instructionContext(context, i))
			}
		case wasm.InstrRefFunc:
			if !p.feat.ReferenceTypes {
				return p.unsupported("const expression", "ref.func (reference-types disabled)", instructionContext(context, i))
			}
		default:
			return p.unsupported("const expression", in.Kind.String(), instructionContext(context, i))
		}
	}
	return nil
}

func (p supportPass) constExprBytes(body []byte, context string) error {
	r := wasm.ReaderFrom(body)
	for instr := 0; r.HasNext(); instr++ {
		op, err := r.Byte()
		if err != nil {
			return err
		}
		ctx := instructionContext(context, instr)
		switch op {
		case 0x0b:
			if r.BytesLeft() != 0 {
				return p.unsupported("const expression", "trailing bytes", context)
			}
			return nil
		case 0x23:
			idx, err := r.U32()
			if err != nil {
				return err
			}
			if int(idx) >= p.m.ImportedGlobalCount() && !p.feat.ExtendedConst {
				return p.unsupported("const expression", "prior global.get (extended-const-expressions disabled)", ctx)
			}
		case 0x41:
			if _, err := r.I32(); err != nil {
				return err
			}
		case 0x42:
			if _, err := r.I64(); err != nil {
				return err
			}
		case 0x43:
			if _, err := r.Bytes(4); err != nil {
				return err
			}
		case 0x44:
			if _, err := r.Bytes(8); err != nil {
				return err
			}
		case 0x6a, 0x6b, 0x6c, 0x7c, 0x7d, 0x7e:
			if !p.feat.ExtendedConst {
				return p.unsupported("const expression", "integer add/sub/mul (extended-const-expressions disabled)", ctx)
			}
		case 0xd0:
			heap, err := r.S33()
			if err != nil {
				return err
			}
			if !p.feat.ReferenceTypes {
				return p.unsupported("const expression", "ref.null (reference-types disabled)", ctx)
			}
			// Abstract heap types encoded as S33: func (-16) and extern (-17) plus
			// their bottoms nofunc (-13) / noextern (-14). Validation accepts the
			// bottom nulls as subtypes; indexed function heaps additionally require
			// the staged typed-reference gate.
			switch heap {
			case -16, -17, -13, -14:
			default:
				if !p.feat.TypedFunctionReferences || !p.supportedTypedFuncHeap(heap) {
					return p.unsupported("const expression", fmt.Sprintf("ref.null heap type %d", heap), ctx)
				}
			}
		case 0xd2:
			if _, err := r.U32(); err != nil {
				return err
			}
			if !p.feat.ReferenceTypes {
				return p.unsupported("const expression", "ref.func (reference-types disabled)", ctx)
			}
		case 0xfd:
			var imm wasm.InstructionImmediate
			if err := wasm.ClassifyInstructionImmediateInto(&r, op, &imm); err != nil {
				return err
			}
			if !p.feat.SIMD {
				return p.unsupported("const expression", "v128.const (simd disabled)", ctx)
			}
			if imm.Subopcode != 12 {
				return p.unsupported("const expression", simdUnsupportedName(imm), ctx)
			}
		default:
			return p.unsupported("const expression", fmt.Sprintf("opcode 0x%02x", op), ctx)
		}
	}
	return p.unsupported("const expression", "missing end", context)
}

func stagedMemory64ScalarOpcode(op byte) bool {
	return (op >= 0x28 && op <= 0x3e) && op != 0x3f && op != 0x40
}

func stagedMemory64ScalarInstruction(k wasm.InstrKind) bool {
	switch k {
	case wasm.InstrI32Load, wasm.InstrI64Load,
		wasm.InstrI32Load8S, wasm.InstrI32Load8U, wasm.InstrI32Load16S, wasm.InstrI32Load16U,
		wasm.InstrI64Load8S, wasm.InstrI64Load8U, wasm.InstrI64Load16S, wasm.InstrI64Load16U, wasm.InstrI64Load32S, wasm.InstrI64Load32U,
		wasm.InstrI32Store, wasm.InstrI64Store, wasm.InstrI32Store8, wasm.InstrI32Store16,
		wasm.InstrI64Store8, wasm.InstrI64Store16, wasm.InstrI64Store32,
		wasm.InstrF32Load, wasm.InstrF64Load, wasm.InstrF32Store, wasm.InstrF64Store:
		return true
	default:
		return false
	}
}

func stagedMemory64SIMDInstruction(k wasm.InstrKind) bool {
	switch k {
	case wasm.InstrV128Load, wasm.InstrV128Store,
		wasm.InstrV128Load8x8S, wasm.InstrV128Load8x8U, wasm.InstrV128Load16x4S, wasm.InstrV128Load16x4U,
		wasm.InstrV128Load32x2S, wasm.InstrV128Load32x2U,
		wasm.InstrV128Load8Splat, wasm.InstrV128Load16Splat, wasm.InstrV128Load32Splat, wasm.InstrV128Load64Splat,
		wasm.InstrV128Load32Zero, wasm.InstrV128Load64Zero,
		wasm.InstrV128Load8Lane, wasm.InstrV128Load16Lane, wasm.InstrV128Load32Lane, wasm.InstrV128Load64Lane,
		wasm.InstrV128Store8Lane, wasm.InstrV128Store16Lane, wasm.InstrV128Store32Lane, wasm.InstrV128Store64Lane:
		return true
	default:
		return false
	}
}

func (p supportPass) instr(in wasm.Instruction, context string) error {
	if err := p.blockType(in.BlockType(), context); err != nil {
		return err
	}
	if len(in.ValTypes()) != 0 {
		if err := p.valTypes(in.ValTypes(), context+" select types"); err != nil {
			return err
		}
	}
	if in.MemArg().Mem != nil && !p.feat.MultiMemory {
		return p.unsupported("memory", fmt.Sprintf("explicit index %d", *in.MemArg().Mem), context)
	}
	if (in.Kind == wasm.InstrMemorySize || in.Kind == wasm.InstrMemoryGrow) && in.Index != 0 && !p.feat.MultiMemory {
		return p.unsupported("memory", fmt.Sprintf("index %d", in.Index), context)
	}
	memoryIndex := uint32(0)
	if in.MemArg().Mem != nil {
		memoryIndex = uint32(*in.MemArg().Mem)
	} else if in.Kind == wasm.InstrMemorySize || in.Kind == wasm.InstrMemoryGrow {
		memoryIndex = in.Index
	}
	if mt, ok := p.m.MemoryType(memoryIndex); ok && mt.Limits.Addr64 {
		switch in.Kind {
		case wasm.InstrMemorySize, wasm.InstrMemoryGrow:
		default:
			if in.MemArg().Mem != nil && !stagedMemory64ScalarInstruction(in.Kind) && !stagedMemory64SIMDInstruction(in.Kind) {
				return p.unsupported("memory64 instruction", in.Kind.String()+" outside staged scalar/SIMD family", context)
			}
		}
	}
	if err := p.instructionKind(in.Kind, context); err != nil {
		return err
	}
	switch in.Kind {
	case wasm.InstrBlock, wasm.InstrLoop:
		return p.expr(in.Body(), context+" body")
	case wasm.InstrIf:
		if err := p.expr(wasm.Expr{Instrs: in.Then()}, context+" then"); err != nil {
			return err
		}
		return p.expr(wasm.Expr{Instrs: in.Else()}, context+" else")
	case wasm.InstrMemoryInit:
		if mt, ok := p.m.MemoryType(uint32(in.Index2)); ok && mt.Limits.Addr64 && !p.feat.Memory64 {
			return p.unsupported("memory64 instruction", in.Kind.String()+" (memory64 disabled)", context)
		}
		if in.Index2 != 0 && !p.feat.MultiMemory {
			return p.unsupported("memory", fmt.Sprintf("init memory index %d", in.Index2), context)
		}
	case wasm.InstrMemoryCopy:
		if (in.Index != 0 || in.Index2 != 0) && !p.feat.MultiMemory {
			return p.unsupported("memory", fmt.Sprintf("copy indexes %d,%d", in.Index, in.Index2), context)
		}
	case wasm.InstrMemoryFill:
		if in.Index != 0 && !p.feat.MultiMemory {
			return p.unsupported("memory", fmt.Sprintf("fill index %d", in.Index), context)
		}
	case wasm.InstrTableGet, wasm.InstrTableSet, wasm.InstrTableSize:
		if p.tableAddr64(in.Index) && !p.feat.Table64 {
			return p.unsupported("table64 instruction", in.Kind.String()+" (table64 disabled)", context)
		}
	case wasm.InstrTableGrow:
		if p.tableAddr64(in.Index) && !p.feat.Table64 {
			return p.unsupported("table64 instruction", in.Kind.String()+" (table64 disabled)", context)
		}
	case wasm.InstrTableFill:
		if p.tableAddr64(in.Index) && !p.feat.Table64 {
			return p.unsupported("table64 instruction", in.Kind.String()+" (table64 disabled)", context)
		}
	case wasm.InstrTableInit:
		if p.tableAddr64(in.Index2) {
			return p.unsupported("table64 instruction", in.Kind.String()+" outside staged get/set/grow/size/fill family", context)
		}
	case wasm.InstrTableCopy:
		if p.tableAddr64(in.Index) || p.tableAddr64(in.Index2) {
			return p.unsupported("table64 instruction", in.Kind.String()+" outside staged get/set/grow/size/fill family", context)
		}
	case wasm.InstrRefNull:
		if !isNullableAbsRef(in.RefType()) && !p.supportedTypedFuncRef(in.RefType()) {
			return p.unsupported("reference instruction", "ref.null "+refTypeName(in.RefType()), context)
		}
	case wasm.InstrCallIndirect:
		if p.tableAddr64(in.Index2) {
			return p.unsupported("table64 instruction", "call_indirect outside staged get/set/grow/size/fill family", context)
		}
		if in.Index2 != 0 && !p.feat.ReferenceTypes {
			return p.unsupported("table", fmt.Sprintf("call_indirect table %d (reference-types disabled)", in.Index2), context)
		}
	}
	return nil
}

func (p supportPass) instructionKind(k wasm.InstrKind, context string) error {
	// WebAssembly 3.0 families remain explicit frontend admission failures until
	// their runtime/backend lowering is complete. Do not let decoder support imply
	// executable support.
	switch k {
	case wasm.InstrReturnCall, wasm.InstrReturnCallIndirect:
		if !p.feat.TailCalls {
			return p.unsupported("instruction", k.String()+" (tail-call disabled)", context)
		}
		return nil
	case wasm.InstrCallRef:
		if !p.feat.TypedFunctionReferences {
			return p.unsupported("instruction", k.String()+" (typed-function-references disabled)", context)
		}
		return nil
	case wasm.InstrReturnCallRef:
		if !p.feat.TypedFunctionReferences || !p.feat.TypedTailCalls {
			return p.unsupported("instruction", k.String()+" (typed reference tail calls disabled)", context)
		}
		return nil
	}

	if stagedMemory64SIMDInstruction(k) {
		if !p.feat.SIMD {
			return p.unsupported("instruction", k.String()+" (simd disabled)", context)
		}
		return nil
	}

	// Proposals gated by the configured feature set.
	switch k {
	case wasm.InstrI32Extend8S, wasm.InstrI32Extend16S, wasm.InstrI64Extend8S, wasm.InstrI64Extend16S, wasm.InstrI64Extend32S:
		if !p.feat.SignExtension {
			return p.unsupported("instruction", k.String()+" (sign-extension-ops disabled)", context)
		}
		return nil
	case wasm.InstrMemoryInit, wasm.InstrMemoryCopy, wasm.InstrMemoryFill, wasm.InstrDataDrop,
		wasm.InstrTableInit, wasm.InstrElemDrop, wasm.InstrTableCopy:
		if !p.feat.BulkMemory {
			return p.unsupported("instruction", k.String()+" (bulk-memory-operations disabled)", context)
		}
		return nil
	case wasm.InstrTableGet, wasm.InstrTableSet, wasm.InstrTableGrow, wasm.InstrTableSize, wasm.InstrTableFill,
		wasm.InstrRefNull, wasm.InstrRefIsNull, wasm.InstrRefFunc, wasm.InstrRefEq:
		if !p.feat.ReferenceTypes {
			return p.unsupported("instruction", k.String()+" (reference-types disabled)", context)
		}
		return nil
	case wasm.InstrRefAsNonNull, wasm.InstrBrOnNull, wasm.InstrBrOnNonNull:
		if !p.feat.ReferenceTypes || !p.feat.TypedFunctionReferences {
			return p.unsupported("instruction", k.String()+" (typed-function-references disabled)", context)
		}
		return nil
	case wasm.InstrI32TruncSatF32S, wasm.InstrI32TruncSatF32U, wasm.InstrI32TruncSatF64S, wasm.InstrI32TruncSatF64U,
		wasm.InstrI64TruncSatF32S, wasm.InstrI64TruncSatF32U, wasm.InstrI64TruncSatF64S, wasm.InstrI64TruncSatF64U:
		if !p.feat.SaturatingTrunc {
			return p.unsupported("instruction", k.String()+" (nontrapping-float-to-int-conversion disabled)", context)
		}
		return nil
	}
	switch k {
	case wasm.InstrUnreachable, wasm.InstrNop,
		wasm.InstrBlock, wasm.InstrLoop, wasm.InstrIf,
		wasm.InstrBr, wasm.InstrBrIf, wasm.InstrBrTable, wasm.InstrReturn,
		wasm.InstrCall, wasm.InstrCallIndirect,
		wasm.InstrDrop, wasm.InstrSelect,
		wasm.InstrMemorySize, wasm.InstrMemoryGrow,
		wasm.InstrLocalGet, wasm.InstrLocalSet, wasm.InstrLocalTee,
		wasm.InstrGlobalGet, wasm.InstrGlobalSet,
		wasm.InstrI32Load, wasm.InstrI64Load, wasm.InstrF32Load, wasm.InstrF64Load,
		wasm.InstrI32Load8S, wasm.InstrI32Load8U, wasm.InstrI32Load16S, wasm.InstrI32Load16U,
		wasm.InstrI64Load8S, wasm.InstrI64Load8U, wasm.InstrI64Load16S, wasm.InstrI64Load16U, wasm.InstrI64Load32S, wasm.InstrI64Load32U,
		wasm.InstrI32Store, wasm.InstrI64Store, wasm.InstrF32Store, wasm.InstrF64Store,
		wasm.InstrI32Store8, wasm.InstrI32Store16, wasm.InstrI64Store8, wasm.InstrI64Store16, wasm.InstrI64Store32,
		wasm.InstrI32Const, wasm.InstrI64Const, wasm.InstrF32Const, wasm.InstrF64Const,
		wasm.InstrI32Eqz, wasm.InstrI32Eq, wasm.InstrI32Ne, wasm.InstrI32LtS, wasm.InstrI32LtU, wasm.InstrI32GtS, wasm.InstrI32GtU, wasm.InstrI32LeS, wasm.InstrI32LeU, wasm.InstrI32GeS, wasm.InstrI32GeU,
		wasm.InstrI64Eqz, wasm.InstrI64Eq, wasm.InstrI64Ne, wasm.InstrI64LtS, wasm.InstrI64LtU, wasm.InstrI64GtS, wasm.InstrI64GtU, wasm.InstrI64LeS, wasm.InstrI64LeU, wasm.InstrI64GeS, wasm.InstrI64GeU,
		wasm.InstrF32Eq, wasm.InstrF32Ne, wasm.InstrF32Lt, wasm.InstrF32Gt, wasm.InstrF32Le, wasm.InstrF32Ge,
		wasm.InstrF64Eq, wasm.InstrF64Ne, wasm.InstrF64Lt, wasm.InstrF64Gt, wasm.InstrF64Le, wasm.InstrF64Ge,
		wasm.InstrI32Clz, wasm.InstrI32Ctz, wasm.InstrI32Popcnt,
		wasm.InstrI32Add, wasm.InstrI32Sub, wasm.InstrI32Mul, wasm.InstrI32DivS, wasm.InstrI32DivU, wasm.InstrI32RemS, wasm.InstrI32RemU, wasm.InstrI32And, wasm.InstrI32Or, wasm.InstrI32Xor, wasm.InstrI32Shl, wasm.InstrI32ShrS, wasm.InstrI32ShrU, wasm.InstrI32Rotl, wasm.InstrI32Rotr,
		wasm.InstrI64Clz, wasm.InstrI64Ctz, wasm.InstrI64Popcnt,
		wasm.InstrI64Add, wasm.InstrI64Sub, wasm.InstrI64Mul, wasm.InstrI64DivS, wasm.InstrI64DivU, wasm.InstrI64RemS, wasm.InstrI64RemU, wasm.InstrI64And, wasm.InstrI64Or, wasm.InstrI64Xor, wasm.InstrI64Shl, wasm.InstrI64ShrS, wasm.InstrI64ShrU, wasm.InstrI64Rotl, wasm.InstrI64Rotr,
		wasm.InstrF32Abs, wasm.InstrF32Neg, wasm.InstrF32Sqrt, wasm.InstrF32Add, wasm.InstrF32Sub, wasm.InstrF32Mul, wasm.InstrF32Div, wasm.InstrF32Min, wasm.InstrF32Max,
		wasm.InstrF32Ceil, wasm.InstrF32Floor, wasm.InstrF32Trunc, wasm.InstrF32Nearest, wasm.InstrF32Copysign,
		wasm.InstrF64Abs, wasm.InstrF64Neg, wasm.InstrF64Sqrt, wasm.InstrF64Add, wasm.InstrF64Sub, wasm.InstrF64Mul, wasm.InstrF64Div, wasm.InstrF64Min, wasm.InstrF64Max,
		wasm.InstrF64Ceil, wasm.InstrF64Floor, wasm.InstrF64Trunc, wasm.InstrF64Nearest, wasm.InstrF64Copysign,
		wasm.InstrI32WrapI64, wasm.InstrI32TruncF32S, wasm.InstrI32TruncF32U, wasm.InstrI32TruncF64S, wasm.InstrI32TruncF64U,
		wasm.InstrI64ExtendI32S, wasm.InstrI64ExtendI32U, wasm.InstrI64TruncF32S, wasm.InstrI64TruncF32U, wasm.InstrI64TruncF64S, wasm.InstrI64TruncF64U,
		wasm.InstrF32ConvertI32S, wasm.InstrF32ConvertI32U, wasm.InstrF32ConvertI64S, wasm.InstrF32ConvertI64U, wasm.InstrF32DemoteF64,
		wasm.InstrF64ConvertI32S, wasm.InstrF64ConvertI32U, wasm.InstrF64ConvertI64S, wasm.InstrF64ConvertI64U, wasm.InstrF64PromoteF32,
		wasm.InstrI32ReinterpretF32, wasm.InstrI64ReinterpretF64, wasm.InstrF32ReinterpretI32, wasm.InstrF64ReinterpretI64:
		return nil
	}
	if isGCInstruction(k) {
		return p.unsupported("gc instruction", k.String()+" (gc disabled)", context)
	}
	if isReferenceInstruction(k) {
		return p.unsupported("reference instruction", k.String(), context)
	}
	return p.unsupported("instruction", k.String(), context)
}

func isReferenceInstruction(k wasm.InstrKind) bool {
	switch k {
	case wasm.InstrRefNull, wasm.InstrRefIsNull, wasm.InstrRefFunc, wasm.InstrRefEq, wasm.InstrRefAsNonNull,
		wasm.InstrBrOnNull, wasm.InstrBrOnNonNull, wasm.InstrRefTest, wasm.InstrRefCast,
		wasm.InstrBrOnCast, wasm.InstrBrOnCastFail, wasm.InstrAnyConvertExtern,
		wasm.InstrExternConvertAny, wasm.InstrRefI31, wasm.InstrI31GetS, wasm.InstrI31GetU,
		wasm.InstrCallRef, wasm.InstrReturnCallRef:
		return true
	default:
		return false
	}
}

func isGCInstruction(k wasm.InstrKind) bool {
	switch k {
	case wasm.InstrStructNew, wasm.InstrStructNewDefault, wasm.InstrStructNewDesc,
		wasm.InstrStructNewDefaultDesc, wasm.InstrStructGet, wasm.InstrStructGetS,
		wasm.InstrStructGetU, wasm.InstrStructAtomicGet, wasm.InstrStructAtomicGetS,
		wasm.InstrStructAtomicGetU, wasm.InstrStructSet, wasm.InstrArrayNew,
		wasm.InstrArrayNewDefault, wasm.InstrArrayNewFixed, wasm.InstrArrayNewData,
		wasm.InstrArrayNewElem, wasm.InstrArrayGet, wasm.InstrArrayGetS,
		wasm.InstrArrayGetU, wasm.InstrArraySet, wasm.InstrArrayLen,
		wasm.InstrArrayFill, wasm.InstrArrayCopy, wasm.InstrArrayInitData,
		wasm.InstrArrayInitElem, wasm.InstrRefGetDesc, wasm.InstrRefTestDesc,
		wasm.InstrRefCastDescEq:
		return true
	default:
		return false
	}
}

func (p supportPass) blockType(bt wasm.BlockType, context string) error {
	switch bt.Kind {
	case wasm.BlockVoid:
		return nil
	case wasm.BlockVal:
		return p.valType(bt.Val, context+" block type")
	case wasm.BlockTypeIndex:
		return nil
	default:
		return p.unsupported("block", "unknown type", context)
	}
}

func (p supportPass) valTypes(vs []wasm.ValType, context string) error {
	for i, vt := range vs {
		if p.supportedValType(vt) {
			continue
		}
		if err := p.valType(vt, fmt.Sprintf("%s[%d]", context, i)); err != nil {
			return err
		}
	}
	return nil
}

func (p supportPass) supportedValTypes(vs []wasm.ValType) bool {
	for _, v := range vs {
		if !p.supportedValType(v) {
			return false
		}
	}
	return true
}

func (p supportPass) supportedValType(v wasm.ValType) bool {
	if v.Kind == wasm.ValNum {
		switch v.Num {
		case wasm.NumI32, wasm.NumI64, wasm.NumF32, wasm.NumF64:
			return true
		}
	}
	if p.feat.SIMD && v.Kind == wasm.ValVec && wasm.EqualValType(v, wasm.V128) {
		return true
	}
	return p.feat.ReferenceTypes && v.Kind == wasm.ValRef && (isFuncRef(v.Ref) || isExternRef(v.Ref) || p.supportedTypedFuncRef(v.Ref))
}

func (p supportPass) valType(v wasm.ValType, context string) error {
	if p.supportedValType(v) {
		return nil
	}
	if v.Kind == wasm.ValVec && wasm.EqualValType(v, wasm.V128) && !p.feat.SIMD {
		return p.unsupported("value type", "v128 (simd disabled)", context)
	}
	if v.Kind == wasm.ValRef {
		feature := valTypeName(v)
		if !p.feat.ReferenceTypes {
			feature += " (reference-types disabled)"
		} else if p.isTypedFuncRef(v.Ref) && !p.feat.TypedFunctionReferences {
			feature += " (typed-function-references disabled)"
		}
		return p.unsupported("reference type", feature, context)
	}
	return p.unsupported("value type", valTypeName(v), context)
}

func (p supportPass) globalType(v wasm.ValType, context string) error {
	if v.Kind == wasm.ValRef {
		if p.feat.ReferenceTypes && (isFuncRef(v.Ref) || isExternRef(v.Ref) || p.supportedTypedFuncRef(v.Ref)) {
			return nil
		}
		feature := valTypeName(v)
		if !p.feat.ReferenceTypes {
			feature += " (reference-types disabled)"
		} else if p.isTypedFuncRef(v.Ref) && !p.feat.TypedFunctionReferences {
			feature += " (typed-function-references disabled)"
		}
		return p.unsupported("global type", feature, context)
	}
	if err := p.valType(v, context); err == nil {
		return nil
	}
	return p.unsupported("global type", valTypeName(v), context)
}

func valTypeName(v wasm.ValType) string {
	if v.Kind == wasm.ValRef {
		return refTypeName(v.Ref)
	}
	return v.String()
}

func refTypeName(rt wasm.RefType) string {
	if isFuncRef(rt) {
		return "funcref"
	}
	if rt.Nullable && rt.Bare && !rt.Exact && rt.Heap.Kind == wasm.HeapAbs && rt.Heap.Abs == wasm.HeapExtern {
		return "externref"
	}
	return wasm.RefVal(rt).String()
}

// ModuleRequiresSIMD reports whether a validated module's types, globals,
// locals, or instruction streams require the SIMD CPU baseline used by wago's
// generated amd64 code. It is intentionally a semantic scan rather than a byte
// substring search, so non-SIMD immediates that happen to contain 0xfd do not
// make a scalar module non-portable.
func ModuleRequiresSIMD(m *wasm.Module) bool {
	if m == nil {
		return false
	}
	for i := range m.Types {
		for j := range m.Types[i].SubTypes {
			comp := m.Types[i].SubTypes[j].Comp
			if compValTypesRequireSIMD(comp.Params) || compValTypesRequireSIMD(comp.Results) {
				return true
			}
		}
	}
	p := supportPass{m: m}
	for i := range m.Imports {
		im := &m.Imports[i]
		switch im.Type.Kind {
		case wasm.ExternFunc:
			if ft := p.funcType(im.Type.Type); ft != nil && (compValTypesRequireSIMD(ft.Params) || compValTypesRequireSIMD(ft.Results)) {
				return true
			}
		case wasm.ExternGlobal:
			if valTypeRequiresSIMD(im.Type.Global.Type) {
				return true
			}
		}
	}
	for i := range m.Globals {
		if valTypeRequiresSIMD(m.Globals[i].Type.Type) || exprRequiresSIMD(m.Globals[i].Init) {
			return true
		}
	}
	for i := range m.Tables {
		if m.Tables[i].Init != nil && exprRequiresSIMD(*m.Tables[i].Init) {
			return true
		}
	}
	for i := range m.Elements {
		if exprRequiresSIMD(m.Elements[i].Mode.Offset) {
			return true
		}
		for j := range m.Elements[i].Kind.Exprs {
			if exprRequiresSIMD(m.Elements[i].Kind.Exprs[j]) {
				return true
			}
		}
	}
	for i := range m.Data {
		if exprRequiresSIMD(m.Data[i].Mode.Offset) {
			return true
		}
	}
	for i := range m.Code {
		for _, run := range m.Code[i].Locals.Runs {
			if valTypeRequiresSIMD(run.Type) {
				return true
			}
		}
		if exprRequiresSIMD(wasm.Expr{Instrs: m.Code[i].Body.Instrs, BodyBytes: m.Code[i].BodyBytes}) {
			return true
		}
	}
	return false
}

func compValTypesRequireSIMD(vs []wasm.ValType) bool {
	for _, v := range vs {
		if valTypeRequiresSIMD(v) {
			return true
		}
	}
	return false
}

func valTypeRequiresSIMD(v wasm.ValType) bool {
	return v.Kind == wasm.ValVec && wasm.EqualValType(v, wasm.V128)
}

func exprRequiresSIMD(e wasm.Expr) bool {
	if len(e.BodyBytes) != 0 {
		return exprBytesRequireSIMD(e.BodyBytes)
	}
	return instrsRequireSIMD(e.Instrs)
}

func exprBytesRequireSIMD(body []byte) bool {
	r := wasm.NewReader(body)
	p := supportPass{feat: AllFeatures()}
	for instr := 0; r.HasNext(); instr++ {
		op, err := r.Byte()
		if err != nil {
			return false
		}
		switch op {
		case 0xfd:
			return true
		case 0x0b:
			continue
		case 0x02, 0x03, 0x04:
			if uses, ok := blockTypeBytesRequireSIMD(r); !ok {
				return false
			} else if uses {
				return true
			}
			continue
		case 0x1c:
			n, err := r.U32()
			if err != nil {
				return false
			}
			for i := uint32(0); i < n; i++ {
				b, err := r.Byte()
				if err != nil {
					return false
				}
				if b == 0x7b {
					return true
				}
			}
			continue
		}
		if _, err := p.instrByte(r, op, "simd scan", instr); err != nil {
			return false
		}
	}
	return false
}

func blockTypeBytesRequireSIMD(r *wasm.Reader) (uses bool, ok bool) {
	b, err := r.Byte()
	if err != nil {
		return false, false
	}
	if b == 0x7b {
		return true, true
	}
	if b == 0x63 || b == 0x64 {
		if _, err := r.S33(); err != nil {
			return false, false
		}
		return false, true
	}
	if b == 0x40 || b == 0x7f || b == 0x7e || b == 0x7d || b == 0x7c || isRefTypeLeadByte(b) {
		return false, true
	}
	for b&0x80 != 0 {
		b, err = r.Byte()
		if err != nil {
			return false, false
		}
	}
	return false, true
}

func instrsRequireSIMD(instrs []wasm.Instruction) bool {
	simdKinds := wasm.SIMDValidationInstructionKinds()
	for i := range instrs {
		in := &instrs[i]
		if _, ok := simdKinds[in.Kind]; ok {
			return true
		}
		if exprRequiresSIMD(in.Body()) || instrsRequireSIMD(in.Then()) || instrsRequireSIMD(in.Else()) {
			return true
		}
	}
	return false
}

func maxInt() int { return int(^uint(0) >> 1) }

func (p supportPass) funcType(idx wasm.TypeIdx) *wasm.CompType {
	if idx.Rec {
		return nil
	}
	ct, ok := p.m.ResolvedTypeFunc(idx.Index)
	if !ok {
		return nil
	}
	return ct
}

func (p supportPass) supportedTypedFuncRef(rt wasm.RefType) bool {
	return p.feat.TypedFunctionReferences && p.isTypedFuncRef(rt)
}

func (p supportPass) supportedTypedFuncHeap(heap int64) bool {
	if heap == -16 || heap == -13 { // func / nofunc
		return true
	}
	if heap < 0 || uint64(heap) > uint64(^uint32(0)) {
		return false
	}
	_, ok := p.m.TypeFunc(uint32(heap))
	return ok
}

func (p supportPass) isTypedFuncRef(rt wasm.RefType) bool {
	switch rt.Heap.Kind {
	case wasm.HeapAbs:
		return !rt.Bare && (rt.Heap.Abs == wasm.HeapFunc || rt.Heap.Abs == wasm.HeapNoFunc)
	case wasm.HeapTypeIndex:
		_, ok := p.m.TypeFunc(rt.Heap.Type.Index)
		return ok
	default:
		return false
	}
}

func isFuncRef(rt wasm.RefType) bool {
	return rt.Nullable && rt.Bare && !rt.Exact && rt.Heap.Kind == wasm.HeapAbs && rt.Heap.Abs == wasm.HeapFunc
}

func isExternRef(rt wasm.RefType) bool {
	return rt.Nullable && rt.Bare && !rt.Exact && rt.Heap.Kind == wasm.HeapAbs && rt.Heap.Abs == wasm.HeapExtern
}

// isNullableAbsRef reports whether rt is a bare nullable reference to one of the
// abstract heap types wago can lower as a null const value: the func and extern
// families, including their nofunc/noextern bottoms. Validation accepts a bottom
// null (e.g. ref.null nofunc) as a subtype of func/extern, so the const-expr
// support pass must accept it too or it rejects valid WebAssembly 2.0 modules.
func isNullableAbsRef(rt wasm.RefType) bool {
	if !(rt.Nullable && rt.Bare && !rt.Exact && rt.Heap.Kind == wasm.HeapAbs) {
		return false
	}
	switch rt.Heap.Abs {
	case wasm.HeapFunc, wasm.HeapExtern, wasm.HeapNoFunc, wasm.HeapNoExtern:
		return true
	}
	return false
}

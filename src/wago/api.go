package wago

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	goruntime "runtime"
	"sort"
	"sync/atomic"
	"unsafe"

	"github.com/wago-org/wago/internal/functionworkers"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	wruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/gc"
)

type GCConfig = gc.Config
type GCProfile = gc.Profile
type GCAllocatorKind = gc.AllocatorKind
type GCRuntimeKind = gc.RuntimeKind

const (
	GCAllocatorPagedSizeClass     = gc.AllocatorPagedSizeClass
	GCAllocatorTinyFixedBlock     = gc.AllocatorTinyFixedBlock
	GCProfileThroughput           = gc.ProfileThroughput
	GCProfileTiny                 = gc.ProfileTiny
	GCRuntimeGenerational         = gc.RuntimeGenerational
	GCRuntimeIncrementalMarkSweep = gc.RuntimeIncrementalMarkSweep
)

// Compile decodes, validates, and compiles a wasm module to native code.
//
// On success ownership of wasmBytes transfers to the returned Compiled. The
// caller must not mutate or reuse its backing array: decoded segment metadata
// may retain slices into that storage. This avoids an input-sized copy while the
// compiled artifact is live.
//
// It accepts both the current explicit form:
//
//	Compile(cfg, wasmBytes)
//
// and the original default-config shorthand:
//
//	Compile(wasmBytes)
//
// Pass nil as cfg, or omit it, to use NewRuntimeConfig.
func Compile(args ...any) (*Compiled, error) {
	cfg, wasmBytes, err := compileArgs(args)
	if err != nil {
		return nil, err
	}
	return compileWithConfig(cfg, wasmBytes)
}

// CompileWithConfig is the named compatibility form of Compile(cfg, wasmBytes).
// It has the same successful-call ownership transfer for wasmBytes. The
// config's feature set gates which modules are accepted and its bounds-check
// mode selects the code-generation strategy.
func CompileWithConfig(cfg *RuntimeConfig, wasmBytes []byte) (*Compiled, error) {
	return compileWithConfig(cfg, wasmBytes)
}

func compileArgs(args []any) (*RuntimeConfig, []byte, error) {
	switch len(args) {
	case 1:
		wasmBytes, ok := args[0].([]byte)
		if !ok {
			return nil, nil, fmt.Errorf("wago: Compile expects []byte or (*RuntimeConfig, []byte), got %T", args[0])
		}
		return nil, wasmBytes, nil
	case 2:
		var cfg *RuntimeConfig
		if args[0] != nil {
			var ok bool
			cfg, ok = args[0].(*RuntimeConfig)
			if !ok {
				return nil, nil, fmt.Errorf("wago: Compile config must be *RuntimeConfig or nil, got %T", args[0])
			}
		}
		wasmBytes, ok := args[1].([]byte)
		if !ok {
			return nil, nil, fmt.Errorf("wago: Compile wasm bytes must be []byte, got %T", args[1])
		}
		return cfg, wasmBytes, nil
	default:
		return nil, nil, fmt.Errorf("wago: Compile expects []byte or (*RuntimeConfig, []byte), got %d arguments", len(args))
	}
}

// functionWorkersForModule resolves the configured policy once so validation
// and codegen use the same bounded worker count for a module.
func functionWorkersForModule(m *wasm.Module, policy int) int {
	totalBody := 0
	for i := range m.Code {
		totalBody += len(m.Code[i].BodyBytes)
	}
	return functionworkers.Resolve(policy, len(m.Code), totalBody)
}

func compileWithConfig(cfg *RuntimeConfig, wasmBytes []byte) (*Compiled, error) {
	if cfg == nil {
		cfg = NewRuntimeConfig()
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return compileWithFrontendFeatures(cfg, wasmBytes, cfg.frontendFeatures())
}

func stagedTwoLocalTableOperation(k wasm.InstrKind) (allowed bool, tableOperation bool) {
	switch k {
	case wasm.InstrTableCopy, wasm.InstrTableGet, wasm.InstrTableSet, wasm.InstrTableSize,
		wasm.InstrTableGrow, wasm.InstrTableFill, wasm.InstrTableInit, wasm.InstrElemDrop:
		return true, true
	case wasm.InstrCallIndirect, wasm.InstrReturnCallIndirect:
		return false, true
	default:
		return false, false
	}
}

func stagedTableInstrsAllowed(instrs []wasm.Instruction, allowed func(wasm.InstrKind) bool) (bool, error) {
	found := false
	for i := range instrs {
		in := &instrs[i]
		if _, tableOperation := stagedTwoLocalTableOperation(in.Kind); tableOperation {
			if !allowed(in.Kind) {
				return false, fmt.Errorf("instruction %s is outside the exact table operation slice", in.Kind)
			}
			found = true
		}
		for _, nested := range [][]wasm.Instruction{in.Body().Instrs, in.Then(), in.Else()} {
			nestedFound, err := stagedTableInstrsAllowed(nested, allowed)
			if err != nil {
				return false, err
			}
			found = found || nestedFound
		}
	}
	return found, nil
}

func stagedTableBodyAllowed(body wasm.Expr, allowed func(wasm.InstrKind) bool) (bool, error) {
	if len(body.BodyBytes) == 0 {
		return stagedTableInstrsAllowed(body.Instrs, allowed)
	}
	found := false
	r := wasm.NewReader(body.BodyBytes)
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return false, err
		}
		imm, err := wasm.ClassifyInstructionImmediate(r, op)
		if err != nil {
			return false, err
		}
		if _, tableOperation := stagedTwoLocalTableOperation(imm.Kind); tableOperation {
			if !allowed(imm.Kind) {
				return false, fmt.Errorf("instruction %s is outside the exact table operation slice", imm.Kind)
			}
			found = true
		}
	}
	return found, nil
}

func stagedTwoLocalTableBody(body wasm.Expr) (bool, error) {
	return stagedTableBodyAllowed(body, func(k wasm.InstrKind) bool {
		allowed, _ := stagedTwoLocalTableOperation(k)
		return allowed
	})
}

func stagedTwoLocalExternrefReadWriteShape(m *wasm.Module) bool {
	return m.ImportedTableCount() == 0 && len(m.Tables) == 2 &&
		m.Tables[0].Type.Limits.Addr64 && m.Tables[1].Type.Limits.Addr64 &&
		m.Tables[0].Type.Limits.Max == nil && m.Tables[1].Type.Limits.Max == nil &&
		wasm.EqualValType(wasm.RefVal(m.Tables[0].Type.Ref), wasm.ExternRef) &&
		wasm.EqualValType(wasm.RefVal(m.Tables[1].Type.Ref), wasm.FuncRef)
}

func stagedTwoLocalExternrefFillShape(m *wasm.Module) bool {
	return m.ImportedTableCount() == 0 && len(m.Tables) == 2 &&
		!m.Tables[0].Type.Limits.Addr64 && m.Tables[1].Type.Limits.Addr64 &&
		m.Tables[0].Type.Limits.Max == nil && m.Tables[1].Type.Limits.Max == nil &&
		wasm.EqualValType(wasm.RefVal(m.Tables[0].Type.Ref), wasm.ExternRef) &&
		wasm.EqualValType(wasm.RefVal(m.Tables[1].Type.Ref), wasm.ExternRef)
}

func stagedExactTableOperationShape(m *wasm.Module, label string, allowed func(wasm.InstrKind) bool) error {
	found := false
	for i := range m.Code {
		bodyFound, err := stagedTableBodyAllowed(wasm.Expr{Instrs: m.Code[i].Body.Instrs, BodyBytes: m.Code[i].BodyBytes}, allowed)
		if err != nil {
			return fmt.Errorf("function %d: %w", i, err)
		}
		found = found || bodyFound
	}
	if !found {
		return fmt.Errorf("the %s requires a table operation", label)
	}
	return nil
}

func stagedSoleExternrefGrowShape(m *wasm.Module) bool {
	return m.ImportedTableCount() == 0 && len(m.Tables) == 1 &&
		m.Tables[0].Type.Limits.Addr64 && m.Tables[0].Type.Limits.Max == nil &&
		wasm.EqualValType(wasm.RefVal(m.Tables[0].Type.Ref), wasm.ExternRef)
}

func stagedTwoLocalNoMaxTable64DeclarationShape(m *wasm.Module) bool {
	if m.ImportedTableCount() != 0 || len(m.Tables) != 2 || len(m.Code) != 0 || len(m.Elements) != 0 || len(m.Exports) != 0 {
		return false
	}
	for i := range m.Tables {
		t := &m.Tables[i]
		if t.Init != nil || !t.Type.Limits.Addr64 || t.Type.Limits.Min != 0 || t.Type.Limits.Max != nil ||
			!wasm.EqualValType(wasm.RefVal(t.Type.Ref), wasm.FuncRef) {
			return false
		}
	}
	return true
}

func stagedImportedLocalNoMaxTable64DeclarationShape(m *wasm.Module) bool {
	if m.ImportedTableCount() != 1 || len(m.Tables) != 1 || m.TableCount() != 2 || len(m.Code) != 0 || len(m.Elements) != 0 || len(m.Exports) != 0 {
		return false
	}
	var imported *wasm.Import
	for i := range m.Imports {
		if m.Imports[i].Type.Kind == wasm.ExternTable {
			if imported != nil {
				return false
			}
			imported = &m.Imports[i]
		} else {
			return false
		}
	}
	if imported == nil || imported.Module != "spectest" || imported.Name != "table64" {
		return false
	}
	for _, tt := range []wasm.TableType{imported.Type.Table, m.Tables[0].Type} {
		if !tt.Limits.Addr64 || tt.Limits.Min != 0 || tt.Limits.Max != nil ||
			!wasm.EqualValType(wasm.RefVal(tt.Ref), wasm.FuncRef) {
			return false
		}
	}
	return m.Tables[0].Init == nil
}

func stagedInertOversizedTable64Shape(m *wasm.Module) bool {
	if m.ImportedTableCount() != 0 || len(m.Tables) != 1 || len(m.Code) != 0 || len(m.Elements) != 0 {
		return false
	}
	t := &m.Tables[0]
	if t.Init != nil || !t.Type.Limits.Addr64 || t.Type.Limits.Max == nil ||
		t.Type.Limits.Min > frontend.StagedTable64Max() || *t.Type.Limits.Max <= frontend.StagedTable64Max() ||
		!wasm.EqualValType(wasm.RefVal(t.Type.Ref), wasm.FuncRef) {
		return false
	}
	for i := range m.Exports {
		if m.Exports[i].Index.Kind == wasm.ExternTable {
			return false
		}
	}
	return true
}

func stagedFourLocalExternrefSizeGrowShape(m *wasm.Module) bool {
	if m.ImportedTableCount() != 0 || len(m.Tables) != 4 {
		return false
	}
	for i := range m.Tables {
		if !m.Tables[i].Type.Limits.Addr64 || !wasm.EqualValType(wasm.RefVal(m.Tables[i].Type.Ref), wasm.ExternRef) {
			return false
		}
	}
	return true
}

func stagedThreeLocalTableInit64Instruction(in wasm.Instruction) (bool, error) {
	switch in.Kind {
	case wasm.InstrTableInit:
		if in.Index2 != 2 {
			return false, fmt.Errorf("table.init targets table %d, want table64 index 2", in.Index2)
		}
		return true, nil
	case wasm.InstrTableCopy:
		if in.Index != 2 || in.Index2 != 2 {
			return false, fmt.Errorf("table.copy indexes %d,%d, want table64 index 2", in.Index, in.Index2)
		}
		return true, nil
	case wasm.InstrCallIndirect:
		if in.Index2 != 2 {
			return false, fmt.Errorf("call_indirect targets table %d, want table64 index 2", in.Index2)
		}
		return true, nil
	case wasm.InstrElemDrop:
		return true, nil
	default:
		if _, tableOperation := stagedTwoLocalTableOperation(in.Kind); tableOperation {
			return false, fmt.Errorf("instruction %s is outside the exact three-local table.init64 slice", in.Kind)
		}
		return false, nil
	}
}

func stagedThreeLocalTableInit64Instrs(instrs []wasm.Instruction) (foundInit, foundIndirect bool, err error) {
	for i := range instrs {
		in := &instrs[i]
		found, checkErr := stagedThreeLocalTableInit64Instruction(*in)
		if checkErr != nil {
			return false, false, checkErr
		}
		foundInit = foundInit || (found && in.Kind == wasm.InstrTableInit)
		foundIndirect = foundIndirect || (found && in.Kind == wasm.InstrCallIndirect)
		for _, nested := range [][]wasm.Instruction{in.Body().Instrs, in.Then(), in.Else()} {
			nestedInit, nestedIndirect, nestedErr := stagedThreeLocalTableInit64Instrs(nested)
			if nestedErr != nil {
				return false, false, nestedErr
			}
			foundInit = foundInit || nestedInit
			foundIndirect = foundIndirect || nestedIndirect
		}
	}
	return foundInit, foundIndirect, nil
}

func stagedThreeLocalTableInit64Body(body wasm.Expr) (foundInit, foundIndirect bool, err error) {
	if len(body.BodyBytes) == 0 {
		return stagedThreeLocalTableInit64Instrs(body.Instrs)
	}
	r := wasm.NewReader(body.BodyBytes)
	for r.HasNext() {
		op, readErr := r.Byte()
		if readErr != nil {
			return false, false, readErr
		}
		imm, classifyErr := wasm.ClassifyInstructionImmediate(r, op)
		if classifyErr != nil {
			return false, false, classifyErr
		}
		found, checkErr := stagedThreeLocalTableInit64Instruction(wasm.Instruction{Kind: imm.Kind, Index: uint32(imm.Index), Index2: uint32(imm.Index2)})
		if checkErr != nil {
			return false, false, checkErr
		}
		foundInit = foundInit || (found && imm.Kind == wasm.InstrTableInit)
		foundIndirect = foundIndirect || (found && imm.Kind == wasm.InstrCallIndirect)
	}
	return foundInit, foundIndirect, nil
}

func stagedThreeLocalTableInit64Shape(m *wasm.Module) error {
	if m.ImportedTableCount() != 0 || len(m.Tables) != 3 {
		return fmt.Errorf("the exact three-local table.init64 slice requires three local tables and no table imports")
	}
	if m.ImportedFuncCount() == 0 {
		return fmt.Errorf("the exact three-local table.init64 slice requires retained function imports")
	}
	for i := range m.Tables {
		t := &m.Tables[i]
		if !wasm.EqualValType(wasm.RefVal(t.Type.Ref), wasm.FuncRef) {
			return fmt.Errorf("table %d is not funcref in the exact three-local table.init64 slice", i)
		}
		if t.Init != nil {
			return fmt.Errorf("table %d initializer expression is outside the exact three-local table.init64 slice", i)
		}
		if t.Type.Limits.Max == nil {
			return fmt.Errorf("table %d requires an explicit maximum in the exact three-local table.init64 slice", i)
		}
		wantAddr64 := i == 2
		if t.Type.Limits.Addr64 != wantAddr64 {
			return fmt.Errorf("table %d address form does not match the exact table32/table32/table64 slice", i)
		}
	}
	for i := range m.Elements {
		e := &m.Elements[i]
		if e.Mode.Kind == wasm.ElemActive && e.Mode.Table != 2 {
			return fmt.Errorf("active element segment %d targets table %d, want table64 index 2", i, e.Mode.Table)
		}
	}
	foundInit, foundIndirect := false, false
	for i := range m.Code {
		bodyInit, bodyIndirect, err := stagedThreeLocalTableInit64Body(wasm.Expr{Instrs: m.Code[i].Body.Instrs, BodyBytes: m.Code[i].BodyBytes})
		if err != nil {
			return fmt.Errorf("function %d: %w", i, err)
		}
		foundInit = foundInit || bodyInit
		foundIndirect = foundIndirect || bodyIndirect
	}
	if !foundInit || !foundIndirect {
		return fmt.Errorf("the exact three-local table.init64 slice requires table.init and call_indirect on table 2")
	}
	return nil
}

func stagedTwoLocalTableShape(m *wasm.Module) error {
	if m.ImportedTableCount() != 0 || len(m.Tables) != 2 {
		return fmt.Errorf("the exact two-local-table slice requires two local tables and no table imports")
	}
	for i := range m.Tables {
		if m.Tables[i].Init != nil {
			return fmt.Errorf("table %d initializer expression is outside the exact two-local-table slice", i)
		}
	}
	exactExternrefReadWrite := stagedTwoLocalExternrefReadWriteShape(m)
	exactExternrefFill := stagedTwoLocalExternrefFillShape(m)
	if !exactExternrefReadWrite && !exactExternrefFill {
		for i := range m.Tables {
			if m.Tables[i].Type.Limits.Max == nil {
				return fmt.Errorf("table %d requires an explicit maximum in the exact two-local-table slice", i)
			}
		}
	}
	allowed := func(k wasm.InstrKind) bool {
		if exactExternrefReadWrite {
			return k == wasm.InstrTableGet || k == wasm.InstrTableSet
		}
		if exactExternrefFill {
			return k == wasm.InstrTableGet || k == wasm.InstrTableFill
		}
		ok, _ := stagedTwoLocalTableOperation(k)
		return ok
	}
	return stagedExactTableOperationShape(m, "exact two-local-table slice", allowed)
}

func stagedTagFuncType(m *wasm.Module, index uint32) (*wasm.CompType, bool) {
	for i := range m.Imports {
		im := &m.Imports[i]
		if im.Type.Kind != wasm.ExternTag {
			continue
		}
		if index == 0 {
			ft, ok := m.ResolvedTypeFunc(im.Type.Tag.Type.Index)
			return ft, ok
		}
		index--
	}
	if int(index) >= len(m.Tags) {
		return nil, false
	}
	ft, ok := m.ResolvedTypeFunc(m.Tags[index].Type.Index)
	return ft, ok
}

func stagedLocalFuncrefExceptionPayload(m *wasm.Module) (funcIndex uint32, typeIndex uint32, ok bool, err error) {
	for tag := uint32(0); tag < uint32(m.TagCount()); tag++ {
		ft, found := stagedTagFuncType(m, tag)
		if !found {
			return 0, 0, false, fmt.Errorf("bounded exception handling tag %d signature is unavailable", tag)
		}
		for _, typ := range ft.Params {
			if wasm.EqualValType(typ, wasm.I32) || wasm.EqualValType(typ, wasm.I64) || wasm.EqualValType(typ, wasm.F32) || wasm.EqualValType(typ, wasm.F64) {
				continue
			}
			if ok {
				return 0, 0, false, fmt.Errorf("bounded exception handling admits only one reference tag payload")
			}
			if m.TagCount() != 1 || len(ft.Params) != 1 || typ.Kind != wasm.ValRef || typ.Ref.Nullable || typ.Ref.Exact || typ.Ref.Heap.Kind != wasm.HeapTypeIndex {
				return 0, 0, false, fmt.Errorf("bounded exception handling admits only one local non-null indexed-function tag payload")
			}
			payloadFunc, found := m.ResolvedTypeFunc(typ.Ref.Heap.Type.Index)
			if !found || payloadFunc == nil || len(payloadFunc.Params) != 0 || len(payloadFunc.Results) != 0 {
				return 0, 0, false, fmt.Errorf("bounded exception handling indexed-function payload must have type () -> ()")
			}
			typeIndex, ok = typ.Ref.Heap.Type.Index, true
		}
	}
	if !ok {
		return 0, 0, false, nil
	}
	if m.ImportedFuncCount() != 0 || m.ImportedTagCount() != 0 || m.Start != nil {
		return 0, 0, false, fmt.Errorf("bounded exception handling reference payload requires local functions, a local tag, and no start")
	}
	if len(m.Elements) != 1 || m.Elements[0].Mode.Kind != wasm.ElemDeclarative || m.Elements[0].Kind.Kind != wasm.ElemFuncs || len(m.Elements[0].Kind.Funcs) != 1 {
		return 0, 0, false, fmt.Errorf("bounded exception handling reference payload requires one declarative local function element")
	}
	funcIndex = uint32(m.Elements[0].Kind.Funcs[0])
	if int(funcIndex) >= m.FuncCount() || int(funcIndex) < m.ImportedFuncCount() {
		return 0, 0, false, fmt.Errorf("bounded exception handling reference payload element must name one local function")
	}
	declaredType, found := m.FuncTypeIndex(funcIndex)
	if !found || declaredType.Index != typeIndex {
		return 0, 0, false, fmt.Errorf("bounded exception handling reference payload function must have the exact indexed type")
	}
	return funcIndex, typeIndex, true, nil
}

func stagedExceptionHandlingShape(m *wasm.Module, exceptionReferences, tailCalls bool) error {
	const maxLocalTags = 9
	const maxTryTables = 24
	const maxCatches = 8
	if m.TagCount() == 0 || m.TagCount() > maxLocalTags {
		return fmt.Errorf("bounded exception handling requires one to %d total tags", maxLocalTags)
	}
	payloadFunc, payloadType, hasFuncrefPayload, err := stagedLocalFuncrefExceptionPayload(m)
	if err != nil {
		return err
	}
	exactTailTable := tailCalls && m.TableCount() == 1 && m.ImportedTableCount() == 0 && len(m.Elements) == 1
	if m.MemCount() != 0 || m.GlobalCount() != 0 || len(m.Data) != 0 || (m.TableCount() != 0 && !exactTailTable) || (len(m.Elements) != 0 && !exactTailTable && !hasFuncrefPayload) {
		return fmt.Errorf("bounded exception handling requires functions without memory/global/data state and only the exact immutable local tail table")
	}
	for _, ex := range m.Exports {
		if ex.Index.Kind == wasm.ExternTable {
			return fmt.Errorf("bounded exception handling rejects exported tables")
		}
		if ex.Index.Kind == wasm.ExternFunc {
			ft, ok := m.FuncSignature(ex.Index.Index)
			if !ok {
				return fmt.Errorf("bounded exception handling export %q has no function signature", ex.Name)
			}
			if hasFuncrefPayload {
				if len(ft.Params) != 0 || len(ft.Results) > 1 {
					return fmt.Errorf("bounded exception handling reference-payload exports must be () -> () or return the sole nullable indexed function")
				}
				if len(ft.Results) == 1 {
					result := ft.Results[0]
					if result.Kind != wasm.ValRef || !result.Ref.Nullable || result.Ref.Exact || result.Ref.Heap.Kind != wasm.HeapTypeIndex || result.Ref.Heap.Type.Index != payloadType {
						return fmt.Errorf("bounded exception handling reference-payload export %q has an unsupported result", ex.Name)
					}
				}
			}
			for _, typ := range append(append([]wasm.ValType(nil), ft.Params...), ft.Results...) {
				if typ.Kind == wasm.ValRef && typ.Ref.Heap.Kind == wasm.HeapAbs && (typ.Ref.Heap.Abs == wasm.HeapExn || typ.Ref.Heap.Abs == wasm.HeapNoExn) {
					return fmt.Errorf("bounded exception handling rejects exported exception-reference ABI in %q", ex.Name)
				}
			}
		}
	}
	for i := 0; i < m.ImportedFuncCount(); i++ {
		ft, ok := m.FuncSignature(uint32(i))
		if !ok || len(ft.Params) != 0 || len(ft.Results) != 0 {
			return fmt.Errorf("bounded exception handling imported function %d requires the exact () -> () transfer ABI", i)
		}
	}
	for i := uint32(0); i < uint32(m.TagCount()); i++ {
		ft, ok := stagedTagFuncType(m, i)
		if !ok || len(ft.Results) != 0 || len(ft.Params) > 2 {
			return fmt.Errorf("bounded exception handling tag %d requires zero to two scalar payloads and no results", i)
		}
		for _, typ := range ft.Params {
			if !wasm.EqualValType(typ, wasm.I32) && !wasm.EqualValType(typ, wasm.I64) && !wasm.EqualValType(typ, wasm.F32) && !wasm.EqualValType(typ, wasm.F64) && !hasFuncrefPayload {
				return fmt.Errorf("bounded exception handling tag %d payloads must be i32/i64/f32/f64", i)
			}
		}
	}
	tryCount, throwCount, refFuncCount := 0, 0, 0
	for i := range m.Code {
		rootCount := 0
		body := m.Code[i].BodyBytes
		r := wasm.NewReader(body)
		for r.HasNext() {
			op, err := r.Byte()
			if err != nil {
				return err
			}
			switch op {
			case 0x08:
				tag, err := r.U32()
				if err != nil || int(tag) >= m.TagCount() || (hasFuncrefPayload && tag != 0) {
					return fmt.Errorf("bounded exception handling throw must target a declared tag")
				}
				throwCount++
			case 0xd2:
				function, err := r.U32()
				if err != nil {
					return err
				}
				if !hasFuncrefPayload || function != payloadFunc {
					return fmt.Errorf("bounded exception handling ref.func must name the exact local payload function")
				}
				refFuncCount++
			case 0x0a:
				if !exceptionReferences {
					return fmt.Errorf("bounded exception handling rejects throw_ref")
				}
			case 0x12:
				if hasFuncrefPayload {
					return fmt.Errorf("bounded exception handling reference payload rejects tail calls")
				}
				if !tailCalls {
					return fmt.Errorf("bounded exception handling rejects tail calls before handler transfer is proven")
				}
				if _, err := r.U32(); err != nil {
					return err
				}
			case 0x13:
				if hasFuncrefPayload {
					return fmt.Errorf("bounded exception handling reference payload rejects tail calls")
				}
				if !tailCalls {
					return fmt.Errorf("bounded exception handling rejects tail calls before handler transfer is proven")
				}
				if _, err := r.U32(); err != nil {
					return err
				}
				if _, err := r.U32(); err != nil {
					return err
				}
			case 0x14:
				if _, err := r.U32(); err != nil {
					return err
				}
				if hasFuncrefPayload {
					return fmt.Errorf("bounded exception handling reference payload rejects call_ref")
				}
			case 0x15:
				if _, err := r.U32(); err != nil {
					return err
				}
				return fmt.Errorf("bounded exception handling function %d rejects reference tail calls", i)
			case 0x1f:
				tryCount++
				if tryCount > maxTryTables {
					return fmt.Errorf("bounded exception handling admits at most %d try_table constructs per module", maxTryTables)
				}
				if _, err := r.S33(); err != nil {
					return err
				}
				n, err := r.U32()
				if err != nil || n > maxCatches {
					return fmt.Errorf("bounded exception handling admits at most %d catches per try_table", maxCatches)
				}
				for j := uint32(0); j < n; j++ {
					kind, err := r.Byte()
					if err != nil {
						return err
					}
					switch wasm.CatchKind(kind) {
					case wasm.CatchTag, wasm.CatchRef:
						tag, err := r.U32()
						if err != nil || int(tag) >= m.TagCount() {
							return fmt.Errorf("bounded exception handling catch must target a declared tag")
						}
						if wasm.CatchKind(kind) == wasm.CatchRef {
							if !exceptionReferences {
								return fmt.Errorf("bounded exception handling rejects exception-reference catches")
							}
							rootCount++
						}
					case wasm.CatchAll:
					case wasm.CatchAllRef:
						if !exceptionReferences {
							return fmt.Errorf("bounded exception handling rejects exception-reference catches")
						}
						rootCount++
					default:
						return fmt.Errorf("bounded exception handling rejects unknown catch kind %d", kind)
					}
					if rootCount > 4 {
						return fmt.Errorf("bounded exception handling admits at most 4 rooted exception values per function")
					}
					if _, err := r.U32(); err != nil {
						return err
					}
				}
			default:
				if _, err := wasm.ClassifyInstructionImmediate(r, op); err != nil {
					return err
				}
			}
		}
		if hasFuncrefPayload && rootCount != 0 {
			if rootCount != 1 || len(body) < 3 || body[len(body)-3] != 0x0b || body[len(body)-2] != 0x1a || body[len(body)-1] != 0x0b {
				return fmt.Errorf("bounded exception handling reference catches must expose one rooted exn value and drop it immediately")
			}
		}
	}
	if hasFuncrefPayload && (refFuncCount != 1 || throwCount != 1) {
		return fmt.Errorf("bounded exception handling reference payload requires one ref.func and one throw")
	}
	_ = throwCount // retained for bounded support-scan accounting and diagnostics
	return nil
}

// compileWithFrontendFeatures is the internal staged path used to prove an
// implementation family before SupportedFeatures admits its public bit. Public
// entry points always validate RuntimeConfig first and pass its exact mapping.
func compileWithFrontendFeatures(cfg *RuntimeConfig, wasmBytes []byte, features frontend.Features) (*Compiled, error) {
	m, err := wasm.DecodeModule(wasmBytes)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	workers := functionWorkersForModule(m, cfg.functionWorkers)
	validationFeatures := wasm.ValidationFeatures{CompactImports: features.MultiMemory, MultiMemory: features.MultiMemory}
	if err := wasm.ValidateModuleWithFeaturesAndWorkers(m, validationFeatures, workers); err != nil {
		return nil, fmt.Errorf("validate: %w", err)
	}
	if features.MultiMemory && m.MemCount() > 1 {
		if goruntime.GOOS != "linux" || goruntime.GOARCH != "amd64" {
			return nil, fmt.Errorf("compile: unsupported memory multi-memory staged execution on %s/%s", goruntime.GOOS, goruntime.GOARCH)
		}
		if cfg.boundsChecks == BoundsChecksSignalsBased {
			return nil, fmt.Errorf("compile: unsupported memory multi-memory with signals-based bounds checks")
		}
	}
	if features.TailCalls && moduleRequiredFeatures(m).IsEnabled(CoreFeatureTailCall) && (goruntime.GOOS != "linux" || goruntime.GOARCH != "amd64") {
		return nil, fmt.Errorf("compile: unsupported instruction tail-call staged execution on %s/%s", goruntime.GOOS, goruntime.GOARCH)
	}
	if features.ExceptionHandling {
		if goruntime.GOOS != "linux" || goruntime.GOARCH != "amd64" {
			return nil, fmt.Errorf("compile: unsupported exception handling staged execution on %s/%s", goruntime.GOOS, goruntime.GOARCH)
		}
		if cfg.boundsChecks == BoundsChecksSignalsBased {
			return nil, fmt.Errorf("compile: unsupported exception handling with signals-based bounds checks")
		}
		if err := stagedExceptionHandlingShape(m, features.ExceptionReferences, features.TailCalls); err != nil {
			return nil, fmt.Errorf("compile: staged exception handling: %w", err)
		}
	}
	usesMemory64 := false
	for i := uint32(0); i < uint32(m.MemCount()); i++ {
		if mt, ok := m.MemoryType(i); ok && mt.Limits.Addr64 {
			usesMemory64 = true
		}
	}
	usesTable64 := false
	for i := uint32(0); i < uint32(m.TableCount()); i++ {
		if tt, ok := m.TableType(i); ok && tt.Limits.Addr64 {
			usesTable64 = true
		}
	}
	if features.Table64 && usesTable64 {
		if goruntime.GOOS != "linux" || goruntime.GOARCH != "amd64" {
			return nil, fmt.Errorf("compile: unsupported table table64 staged execution on %s/%s", goruntime.GOOS, goruntime.GOARCH)
		}
		if cfg.boundsChecks == BoundsChecksSignalsBased {
			return nil, fmt.Errorf("compile: unsupported table table64 with signals-based bounds checks")
		}
		twoLocal := m.TableCount() == 2 && m.ImportedTableCount() == 0 && len(m.Tables) == 2
		twoLocalDeclaration := stagedTwoLocalNoMaxTable64DeclarationShape(m)
		importedLocalDeclaration := stagedImportedLocalNoMaxTable64DeclarationShape(m)
		threeLocalTableInit64 := m.TableCount() == 3 && m.ImportedTableCount() == 0 && len(m.Tables) == 3
		soleExternrefGrow := m.TableCount() == 1 && stagedSoleExternrefGrowShape(m)
		fourLocalExternrefSizeGrow := m.TableCount() == 4 && stagedFourLocalExternrefSizeGrowShape(m)
		if m.TableCount() != 1 && !twoLocal && !importedLocalDeclaration && !threeLocalTableInit64 && !fourLocalExternrefSizeGrow {
			return nil, fmt.Errorf("compile: staged table64 requires exactly one local/imported table or an exact bounded multi-table slice")
		}
		if m.TableCount() == 1 && m.ImportedTableCount() != 0 && len(m.Tables) != 0 {
			return nil, fmt.Errorf("compile: staged table64 rejects mixed imported/local table shapes")
		}
		if twoLocal && !twoLocalDeclaration {
			if err := stagedTwoLocalTableShape(m); err != nil {
				return nil, fmt.Errorf("compile: staged table64 %w", err)
			}
		}
		if threeLocalTableInit64 {
			if err := stagedThreeLocalTableInit64Shape(m); err != nil {
				return nil, fmt.Errorf("compile: staged table64 %w", err)
			}
		}
		if soleExternrefGrow || fourLocalExternrefSizeGrow {
			for i := range m.Tables {
				if m.Tables[i].Init != nil {
					return nil, fmt.Errorf("compile: staged table64 table %d initializer expression is outside the exact local externref size/grow slice", i)
				}
			}
			if len(m.Elements) != 0 {
				return nil, fmt.Errorf("compile: staged table64 element segments are outside the exact local externref size/grow slice")
			}
			allowed := func(k wasm.InstrKind) bool {
				if fourLocalExternrefSizeGrow {
					return k == wasm.InstrTableSize || k == wasm.InstrTableGrow
				}
				return k == wasm.InstrTableGet || k == wasm.InstrTableSet || k == wasm.InstrTableSize || k == wasm.InstrTableGrow
			}
			if err := stagedExactTableOperationShape(m, "exact local externref size/grow slice", allowed); err != nil {
				return nil, fmt.Errorf("compile: staged table64 %w", err)
			}
		}
		externrefLocal := (twoLocal && (stagedTwoLocalExternrefReadWriteShape(m) || stagedTwoLocalExternrefFillShape(m))) || soleExternrefGrow || fourLocalExternrefSizeGrow
		inertOversized := stagedInertOversizedTable64Shape(m)
		for tableIndex := 0; tableIndex < m.TableCount(); tableIndex++ {
			tt, ok := m.TableType(uint32(tableIndex))
			if !ok {
				return nil, fmt.Errorf("compile: staged table64 table %d type is unavailable", tableIndex)
			}
			if !wasm.EqualValType(wasm.RefVal(tt.Ref), wasm.FuncRef) && !(externrefLocal && wasm.EqualValType(wasm.RefVal(tt.Ref), wasm.ExternRef)) {
				return nil, fmt.Errorf("compile: staged table64 requires funcref table %d outside an exact local externref slice", tableIndex)
			}
			if tt.Limits.Min > frontend.StagedTable64Max() || (tt.Limits.Max != nil && *tt.Limits.Max > frontend.StagedTable64Max() && !inertOversized) {
				return nil, fmt.Errorf("compile: staged table64 table %d requires an executable runtime bound no greater than %d entries", tableIndex, frontend.StagedTable64Max())
			}
		}
		for i := range m.Elements {
			e := &m.Elements[i]
			if e.Mode.Kind == wasm.ElemActive && (int(e.Mode.Table) < 0 || int(e.Mode.Table) >= m.TableCount()) {
				return nil, fmt.Errorf("compile: staged table64 active element segment targets unavailable table %d", e.Mode.Table)
			}
			if !twoLocal && !importedLocalDeclaration && !threeLocalTableInit64 && e.Mode.Kind == wasm.ElemActive && e.Mode.Table != 0 {
				return nil, fmt.Errorf("compile: staged table64 active element segment targets table %d, want the sole table 0", e.Mode.Table)
			}
			if !twoLocal && !importedLocalDeclaration && !threeLocalTableInit64 && m.ImportedTableCount() != 0 && e.Mode.Kind != wasm.ElemActive {
				return nil, fmt.Errorf("compile: imported table64 passive/declarative lifecycle remains outside the sole-local staged boundary")
			}
		}
	}
	if features.Memory64 && usesMemory64 {
		if goruntime.GOOS != "linux" || goruntime.GOARCH != "amd64" {
			return nil, fmt.Errorf("compile: unsupported memory memory64 staged execution on %s/%s", goruntime.GOOS, goruntime.GOARCH)
		}
		if cfg.boundsChecks == BoundsChecksSignalsBased {
			return nil, fmt.Errorf("compile: unsupported memory memory64 with signals-based bounds checks")
		}
		if m.MemCount() != 1 || (m.ImportedMemCount() != 0 && len(m.Memories) != 0) {
			return nil, fmt.Errorf("compile: staged memory64 requires exactly one local or imported memory and rejects multi-memory shapes")
		}
		mt, ok := m.MemoryType(0)
		if !ok {
			return nil, fmt.Errorf("compile: staged memory64 memory type is unavailable")
		}
		if mt.Shared {
			return nil, fmt.Errorf("compile: staged memory64 rejects shared memory")
		}
	}
	gcDescs, err := frontend.BuildGCTypeDescs(m)
	if err != nil {
		return nil, fmt.Errorf("gc descriptors: %w", err)
	}
	if err := frontend.RejectUnsupportedWithFeatures(m, features); err != nil {
		return nil, fmt.Errorf("compile: %w", err)
	}
	// Architectures that always use the sync-host dispatcher can compile host
	// defaults up front; others defer returning imports until link time.
	boundsMode := effectiveCompileBoundsMode(cfg.boundsChecks, m)
	elide := boundsMode == BoundsChecksSignalsBased
	importedFuncs := m.ImportedFuncCount()
	dynamicBindings := make([]railshotImportBinding, importedFuncs)
	for i := range dynamicBindings {
		dynamicBindings[i] = railshotImportBinding{Dynamic: true, ImportIndex: uint32(i), EHTransfer: features.ExceptionHandling}
	}
	pressureAt, pressure := compileMemoryPressure(len(wasmBytes))
	cm, err := railshotCompileModuleWith(m, railshotCompileOptions{Workers: workers, ElideBoundsChecks: elide, NoBoundsFacts: cfg.noDeferBounds, ImportBindings: dynamicBindings, Interruptible: true, MemoryPressureAt: pressureAt, MemoryPressure: pressure})
	if err != nil {
		return nil, fmt.Errorf("compile: %w", err)
	}
	code, entry, internalEntry := cm.Code, cm.Entry, cm.InternalEntry

	types, err := typeDescriptorsFromWasm(m)
	if err != nil {
		return nil, fmt.Errorf("type metadata: %w", err)
	}
	c := &Compiled{Code: code, Entry: entry, InternalEntry: internalEntry, NumImports: importedFuncs, Types: types, Exports: map[string]int{}, Names: m.NameSec, GlobalExports: map[string]int{}, hasTableExportMetadata: true, memoryDir: &compiledMemoryDirectory{exports: map[string]int{}, exactExports: true, staged: features.MultiMemory && m.MemCount() > 1, stagedMemory64: features.Memory64 && usesMemory64}, boundsMode: boundsMode, stagedTable64: features.Table64 && usesTable64, GCTypeDescs: gcDescs, requiredFeatures: moduleRequiredFeatures(m), dynamicImports: importedFuncs > 0}
	if importedFuncs > 0 {
		c.importFuncSigs = make([]FuncSig, importedFuncs)
		for i := 0; i < importedFuncs; i++ {
			typeIdx, ok := m.FuncTypeIndex(uint32(i))
			if !ok {
				continue
			}
			ft, ok := m.ResolvedTypeFunc(typeIdx.Index)
			if !ok {
				continue
			}
			params, err := valTypesFromWasmInModule(m, ft.Params, c.Types)
			if err != nil {
				return nil, fmt.Errorf("imported function %d params: %w", i, err)
			}
			results, err := valTypesFromWasmInModule(m, ft.Results, c.Types)
			if err != nil {
				return nil, fmt.Errorf("imported function %d results: %w", i, err)
			}
			c.importFuncSigs[i] = FuncSig{Params: params, Results: results, TypeIndex: typeIdx.Index, HasTypeIndex: true}
		}
	}
	importedTables := m.ImportedTableCount()
	var additionalTableImports []tableImportDef
	if importedTables > 1 {
		additionalTableImports = make([]tableImportDef, 0, importedTables-1)
	}
	tableImportIndex := 0
	for i := range m.Imports {
		im := &m.Imports[i]
		switch im.Type.Kind {
		case wasm.ExternFunc:
			c.Imports = append(c.Imports, im.Module+"."+im.Name)
		case wasm.ExternGlobal:
			exact, err := valueTypeDescriptorInModule(m, im.Type.Global.Type)
			if err != nil {
				return nil, fmt.Errorf("global import %q.%q type: %w", im.Module, im.Name, err)
			}
			typeIndex := internValueType(&c.ValueTypes, exact)
			abiType, err := valTypeFromWasmInModule(m, im.Type.Global.Type, c.Types)
			if err != nil {
				return nil, fmt.Errorf("global import %q.%q ABI type: %w", im.Module, im.Name, err)
			}
			imp := GlobalImportDef{Module: im.Module, Name: im.Name, Type: abiType, ValueTypeIndex: typeIndex, HasValueType: true, Mutable: im.Type.Global.Mutable}
			c.GlobalImports = append(c.GlobalImports, imp)
			c.Globals = append(c.Globals, GlobalDef{Type: imp.Type, ValueTypeIndex: typeIndex, HasValueType: true, Mutable: imp.Mutable})
		case wasm.ExternMem:
			def := memoryDefFromWasm(im.Type.Mem)
			def.ImportKey = im.Module + "." + im.Name
			c.memoryDir.defs = append(c.memoryDir.defs, def)
			if c.memoryImport == "" {
				c.memoryImport = def.ImportKey
			}
		case wasm.ExternTable:
			exact, err := valueTypeDescriptorInModule(m, wasm.RefVal(im.Type.Table.Ref))
			if err != nil {
				return nil, fmt.Errorf("table import %q.%q type: %w", im.Module, im.Name, err)
			}
			abiType, err := valTypeFromWasmInModule(m, wasm.RefVal(im.Type.Table.Ref), c.Types)
			if err != nil {
				return nil, fmt.Errorf("table import %q.%q ABI type: %w", im.Module, im.Name, err)
			}
			def := tableImportDef{Key: im.Module + "." + im.Name, Type: abiType, ValueTypeIndex: internValueType(&c.ValueTypes, exact), HasValueType: true, Addr64: im.Type.Table.Limits.Addr64}
			min := im.Type.Table.Limits.Min
			if min > uint64(maxInt()) {
				return nil, fmt.Errorf("table import %q.%q minimum %d overflows int", im.Module, im.Name, min)
			}
			def.Min = int(min)
			if im.Type.Table.Limits.Max != nil {
				max := *im.Type.Table.Limits.Max
				if max > uint64(maxInt()) {
					return nil, fmt.Errorf("table import %q.%q maximum %d overflows int", im.Module, im.Name, max)
				}
				def.Max = int(max)
				def.HasMax = true
			}
			if tableImportIndex == 0 {
				c.tableImport = def.Key
				c.tableImportMin = def.Min
				c.tableImportMax = def.Max
				c.tableImportHasMax = def.HasMax
				c.TableAddr64 = def.Addr64
			} else {
				additionalTableImports = append(additionalTableImports, def)
			}
			tableImportIndex++
		case wasm.ExternTag:
			c.memoryDir.ehTags = append(c.memoryDir.ehTags, compiledTagDef{ImportKey: im.Module + "." + im.Name, TypeIndex: im.Type.Tag.Type.Index})
		}
	}
	if features.ExceptionHandling {
		for i := range m.Tags {
			c.memoryDir.ehTags = append(c.memoryDir.ehTags, compiledTagDef{TypeIndex: m.Tags[i].Type.Index})
		}
	}
	for li := range m.FuncTypes {
		ft, ok := m.ResolvedLocalFuncType(li)
		if !ok {
			return nil, fmt.Errorf("function %d: unknown type", li)
		}
		params, err := valTypesFromWasmInModule(m, ft.Params, c.Types)
		if err != nil {
			return nil, fmt.Errorf("function %d params: %w", li, err)
		}
		results, err := valTypesFromWasmInModule(m, ft.Results, c.Types)
		if err != nil {
			return nil, fmt.Errorf("function %d results: %w", li, err)
		}
		c.Funcs = append(c.Funcs, FuncSig{Params: params, Results: results, TypeIndex: m.FuncTypes[li].Index, HasTypeIndex: true})
	}
	for i := range m.Globals {
		v, err := evalConstExprWithModule(m.Globals[i].Init, m.Globals[i].Type.Type, m)
		if err != nil {
			return nil, fmt.Errorf("global %d initializer: %w", i, err)
		}
		exact, err := valueTypeDescriptorInModule(m, m.Globals[i].Type.Type)
		if err != nil {
			return nil, fmt.Errorf("global %d type: %w", i, err)
		}
		abiType, err := valTypeFromWasmInModule(m, m.Globals[i].Type.Type, c.Types)
		if err != nil {
			return nil, fmt.Errorf("global %d ABI type: %w", i, err)
		}
		g := GlobalDef{Type: abiType, ValueTypeIndex: internValueType(&c.ValueTypes, exact), HasValueType: true, Mutable: m.Globals[i].Type.Mutable}
		applyGlobalInit(&g, v.Init())
		c.Globals = append(c.Globals, g)
	}
	memoryExported := false
	for i := range m.Exports {
		switch m.Exports[i].Index.Kind {
		case wasm.ExternFunc:
			c.Exports[m.Exports[i].Name] = int(m.Exports[i].Index.Index)
		case wasm.ExternGlobal:
			c.GlobalExports[m.Exports[i].Name] = int(m.Exports[i].Index.Index)
		case wasm.ExternTable:
			if c.tableExports == nil {
				c.tableExports = make(map[string]int)
			}
			c.tableExports[m.Exports[i].Name] = int(m.Exports[i].Index.Index)
		case wasm.ExternMem:
			memoryExported = true
			c.memoryDir.exports[m.Exports[i].Name] = int(m.Exports[i].Index.Index)
		case wasm.ExternTag:
			if c.memoryDir.ehTagExports == nil {
				c.memoryDir.ehTagExports = make(map[string]int)
			}
			c.memoryDir.ehTagExports[m.Exports[i].Name] = int(m.Exports[i].Index.Index)
		}
	}

	tableShapes, err := frontend.SupportedTableRuntimeShapes(m)
	if err != nil {
		return nil, fmt.Errorf("compile: %w", err)
	}
	c.HasTable = len(tableShapes) != 0
	if len(tableShapes) != 0 {
		c.TableSize = tableShapes[0].Size
		tt, ok := m.TableType(0)
		if !ok {
			return nil, fmt.Errorf("table 0 type unavailable")
		}
		c.TableType, err = valTypeFromWasmInModule(m, wasm.RefVal(tt.Ref), c.Types)
		if err != nil {
			return nil, fmt.Errorf("table 0 ABI type: %w", err)
		}
		exact, err := valueTypeDescriptorInModule(m, wasm.RefVal(tt.Ref))
		if err != nil {
			return nil, fmt.Errorf("table 0 type: %w", err)
		}
		c.TableValueTypeIndex = internValueType(&c.ValueTypes, exact)
		c.TableHasValueType = true
		c.TableAddr64 = tt.Limits.Addr64
		if c.tableImport == "" {
			c.TableHasMax = tt.Limits.Max != nil
			c.TableMax = uint64(tableShapes[0].Capacity)
			if tt.Limits.Max != nil {
				c.TableMax = *tt.Limits.Max
			}
		}
	}
	if len(tableShapes) > 1 {
		c.extraTables = make([]tableDef, len(tableShapes)-1)
		for i := 1; i < len(tableShapes); i++ {
			tt, ok := m.TableType(uint32(i))
			if !ok {
				return nil, fmt.Errorf("table %d type unavailable", i)
			}
			exact, err := valueTypeDescriptorInModule(m, wasm.RefVal(tt.Ref))
			if err != nil {
				return nil, fmt.Errorf("table %d type: %w", i, err)
			}
			abiType, err := valTypeFromWasmInModule(m, wasm.RefVal(tt.Ref), c.Types)
			if err != nil {
				return nil, fmt.Errorf("table %d ABI type: %w", i, err)
			}
			persistedMax := uint64(tableShapes[i].Capacity)
			if tt.Limits.Max != nil {
				persistedMax = *tt.Limits.Max
			}
			c.extraTables[i-1] = tableDef{Size: tableShapes[i].Size, Max: persistedMax, Type: abiType, ValueTypeIndex: internValueType(&c.ValueTypes, exact), HasValueType: true, HasMax: tt.Limits.Max != nil, Addr64: tt.Limits.Addr64}
		}
		for i, def := range additionalTableImports {
			c.extraTables[i] = tableDef{ImportKey: def.Key, Size: def.Min, Max: uint64(def.Max), Type: def.Type, ValueTypeIndex: def.ValueTypeIndex, HasValueType: def.HasValueType, ImportHasMax: def.HasMax, Addr64: def.Addr64}
		}
	}
	c.NeedsFuncRefDescs = frontend.RequiresFuncRefDescriptors(m)
	for i := range m.Tables {
		tableIndex := importedTables + i
		if m.Tables[i].Init == nil {
			continue
		}
		payload, err := funcrefExprPayload(*m.Tables[i].Init)
		if err != nil {
			return nil, fmt.Errorf("table %d initializer: %w", tableIndex, err)
		}
		if payload == nullFuncRefIndex {
			continue
		}
		if tableIndex == 0 {
			c.HasTableInitFunc = true
			c.TableInitFunc = payload
		} else {
			c.extraTables[tableIndex-1].HasInitFunc = true
			c.extraTables[tableIndex-1].InitFunc = payload
		}
	}
	for i := range m.Memories {
		c.memoryDir.defs = append(c.memoryDir.defs, memoryDefFromWasm(m.Memories[i]))
	}
	if len(c.memoryDir.defs) > 0 {
		memory0 := c.memoryDir.defs[0]
		c.HasMemory = true
		if memory0.Min <= uint64(^uint32(0)) {
			c.MemMinPages = uint32(memory0.Min)
		}
		c.MemMaxPages = 65535 // default memory-0 reservation ceiling
		if memory0.HasMax && memory0.Max < uint64(c.MemMaxPages) {
			c.MemMaxPages = uint32(memory0.Max)
		}
		// Pin a local memory-0 reservation to its initial size only when this
		// module never grows or exports it. Exact declared limits remain in the
		// directory for inspection, policy, linking, and codec round trips.
		if memory0.ImportKey == "" && !moduleUsesMemoryGrow(m) && !memoryExported {
			c.MemMaxPages = c.MemMinPages
		}
	}
	if m.Start != nil {
		c.HasStart = true
		if int(*m.Start) < importedFuncs {
			// Imported start: run the imported function's host binding at instantiate
			// (validation guarantees () -> ()).
			c.StartIsImport = true
			c.StartImportIdx = int(*m.Start)
		} else {
			c.StartLocalFunc = int(*m.Start) - importedFuncs // validated local & () -> ()
		}
	}
	// Function descriptors back every executable funcref table. Table 0 keeps the
	// direct runtime slot; later table indexes use the bounded directory.
	for i := range m.Imports {
		if m.Imports[i].Type.Kind == wasm.ExternFunc {
			typeIndex := m.Imports[i].Type.Type.Index
			key, ok := m.StructuralTypeKeyChecked(typeIndex)
			if !ok {
				return nil, fmt.Errorf("import function type %d exceeds bounded native identity", typeIndex)
			}
			c.FuncTypeID = append(c.FuncTypeID, key)
		}
	}
	for li := range m.FuncTypes {
		typeIndex := m.FuncTypes[li].Index
		key, ok := m.StructuralTypeKeyChecked(typeIndex)
		if !ok {
			return nil, fmt.Errorf("function type %d exceeds bounded native identity", typeIndex)
		}
		c.FuncTypeID = append(c.FuncTypeID, key)
	}
	elemStateCount, dataStateCount := moduleSegmentStateCounts(m)
	if elemStateCount > 0 {
		// table.init/elem.drop immediates address the module's original element
		// index space. Active/declarative slots remain zero-length (dropped).
		c.passiveElems = make([]ElemInit, elemStateCount)
	}
	for i := range m.Elements {
		e := &m.Elements[i]
		refType, exactType, values, err := elementPayloads(m, c.Types, e)
		if err != nil {
			return nil, fmt.Errorf("element %d: %w", i, err)
		}
		init := ElemInit{TableIndex: uint32(e.Mode.Table), RefType: refType, ValueTypeIndex: internValueType(&c.ValueTypes, exactType), HasValueType: true, Mode: elemModeFromWasm(e.Mode.Kind), Values: values}
		if i < len(c.passiveElems) {
			state := init
			if e.Mode.Kind != wasm.ElemPassive {
				state.Values = nil // active/declarative segments start dropped
			}
			c.passiveElems[i] = state
		}
		switch e.Mode.Kind {
		case wasm.ElemPassive, wasm.ElemDeclarative:
			continue
		case wasm.ElemActive:
			want := wasm.I32
			table64 := false
			if tt, ok := m.TableType(uint32(e.Mode.Table)); ok && tt.Limits.Addr64 {
				want = wasm.I64
				table64 = true
			}
			base, err := evalConstExprWithModule(e.Mode.Offset, want, m)
			if err != nil {
				return nil, fmt.Errorf("element %d offset: %w", i, err)
			}
			if table64 {
				// OffsetInit's compact Base/HasGlobal forms are i32-only. Preserve the
				// validated i64 expression so codec v27 and instantiation retain every bit.
				if len(e.Mode.Offset.BodyBytes) != 0 {
					init.Offset.Expr = append([]byte(nil), e.Mode.Offset.BodyBytes...)
				} else {
					encoded, err := wasm.EncodeExpr(e.Mode.Offset)
					if err != nil {
						return nil, fmt.Errorf("element %d offset encode: %w", i, err)
					}
					init.Offset.Expr = encoded
				}
			} else {
				applyElemOffset(&init, base.Init())
			}
			// Preserve even empty active segments: the offset must still be bounds-
			// checked against the actual table length at instantiation time.
			c.Elems = append(c.Elems, init)
		}
	}
	if dataStateCount > 0 {
		// memory.init/data.drop immediates address the module's original data
		// index space. Active slots remain zero-length (dropped).
		c.PassiveData = make([]PassiveDataInit, dataStateCount)
	}
	// Active data dominates metadata in Go-produced modules (esbuild has tens of
	// thousands of segments). Reserve once instead of geometrically copying the
	// growing descriptor slice.
	activeData := 0
	for i := range m.Data {
		if m.Data[i].Mode.Kind != wasm.DataPassive {
			activeData++
		}
	}
	c.Data = make([]DataInit, 0, activeData)
	for i := range m.Data {
		d := &m.Data[i]
		if d.Mode.Kind == wasm.DataPassive {
			c.PassiveData[i] = PassiveDataInit{Bytes: append([]byte(nil), d.Init...)}
			continue
		}
		want := wasm.I32
		memory64 := false
		if mt, ok := m.MemoryType(uint32(d.Mode.Mem)); ok && mt.Limits.Addr64 {
			want = wasm.I64
			memory64 = true
		}
		off, err := evalConstExprWithModule(d.Mode.Offset, want, m)
		if err != nil {
			return nil, fmt.Errorf("data %d offset: %w", i, err)
		}
		init := DataInit{MemoryIndex: uint32(d.Mode.Mem), Bytes: d.Init}
		if memory64 {
			// OffsetInit's compact Base/HasGlobal forms are intentionally i32-only.
			// Preserve the already validated i64 program in the existing Expr field so
			// codec v27 retains the existing expression field while instantiation preserves all 64 address bits.
			if len(d.Mode.Offset.BodyBytes) != 0 {
				init.Offset.Expr = append([]byte(nil), d.Mode.Offset.BodyBytes...)
			} else {
				encoded, err := wasm.EncodeExpr(d.Mode.Offset)
				if err != nil {
					return nil, fmt.Errorf("data %d offset encode: %w", i, err)
				}
				init.Offset.Expr = encoded
			}
		} else {
			applyDataOffset(&init, off.Init())
		}
		c.Data = append(c.Data, init)
	}
	compiled := installCompiledFinalizer(c)
	if features.TypedFunctionReferences {
		compiled.codeCache.stagedFeatures |= CoreFeatureTypedFunctionReferences
	}
	if features.TailCalls || features.TypedTailCalls {
		compiled.codeCache.stagedFeatures |= CoreFeatureTailCall
	}
	return compiled, nil
}

// effectiveCompileBoundsMode keeps zero-minimum memories correct on ARM64.
// The current ARM64 guard entry places control words immediately below linMem;
// when linMem begins on the first inaccessible linear page, entry is not reliable
// across the supported Linux and Darwin signal trampolines. Compile those rare
// modules with explicit checks and classic growable memory instead.
func effectiveCompileBoundsMode(requested BoundsCheckMode, m *wasm.Module) BoundsCheckMode {
	if requested != BoundsChecksSignalsBased || goruntime.GOARCH != "arm64" {
		return requested
	}
	if min, ok := moduleInitialMemoryPages(m); ok && min == 0 {
		return BoundsChecksExplicit
	}
	return requested
}

func moduleInitialMemoryPages(m *wasm.Module) (uint64, bool) {
	for i := range m.Imports {
		if m.Imports[i].Type.Kind == wasm.ExternMem {
			return m.Imports[i].Type.Mem.Limits.Min, true
		}
	}
	if len(m.Memories) != 0 {
		return m.Memories[0].Limits.Min, true
	}
	return 0, false
}

func compileMemoryPressure(sourceBytes int) (int, func()) {
	// One checkpoint is enough to prevent dead per-function state from riding the
	// GC growth curve to the end of real-world modules. Small modules stay on the
	// zero-overhead path. runtime.GC does not modify GOGC or GOMEMLIMIT.
	if sourceBytes < 8<<20 {
		return 0, nil
	}
	return 0, goruntime.GC
}

func elemModeFromWasm(mode wasm.ElemModeKind) ElemMode {
	switch mode {
	case wasm.ElemPassive:
		return ElemModePassive
	case wasm.ElemDeclarative:
		return ElemModeDeclarative
	default:
		return ElemModeActive
	}
}

func elementPayloads(m *wasm.Module, types []DefinedTypeDescriptor, e *wasm.Elem) (ValType, ValueTypeDescriptor, []RefInit, error) {
	switch e.Kind.Kind {
	case wasm.ElemFuncs:
		out := make([]RefInit, len(e.Kind.Funcs))
		for i, fidx := range e.Kind.Funcs {
			out[i] = RefInit{FuncIndex: uint32(fidx)}
		}
		exact, _ := valueTypeDescriptorFromValType(ValFuncRef)
		return ValFuncRef, exact, out, nil
	case wasm.ElemFuncExprs, wasm.ElemTypedExprs:
		refType := ValFuncRef
		exact, _ := valueTypeDescriptorFromValType(refType)
		if e.Kind.Kind == wasm.ElemTypedExprs {
			var err error
			exact, err = valueTypeDescriptorInModule(m, wasm.RefVal(e.Kind.Ref))
			if err != nil {
				return 0, ValueTypeDescriptor{}, nil, err
			}
			refType, err = valTypeFromWasmInModule(m, wasm.RefVal(e.Kind.Ref), types)
			if err != nil {
				return 0, ValueTypeDescriptor{}, nil, err
			}
		}
		out := make([]RefInit, len(e.Kind.Exprs))
		for i, ex := range e.Kind.Exprs {
			payload, err := wasm.ParseElementExpr(ex)
			if err != nil {
				return 0, ValueTypeDescriptor{}, nil, fmt.Errorf("expression %d: %w", i, err)
			}
			payloadType, err := valueTypeDescriptorInModule(m, wasm.RefVal(payload.RefType))
			if err != nil {
				return 0, ValueTypeDescriptor{}, nil, fmt.Errorf("expression %d type: %w", i, err)
			}
			payloadABI, ok := payloadType.ABIType(types)
			if !ok || payloadABI != refType {
				return 0, ValueTypeDescriptor{}, nil, fmt.Errorf("expression %d type does not match segment ABI type %s", i, refType)
			}
			out[i] = RefInit{FuncIndex: payload.FuncIndex, Null: payload.Null}
		}
		return refType, exact, out, nil
	default:
		return 0, ValueTypeDescriptor{}, nil, fmt.Errorf("unsupported element kind %d", e.Kind.Kind)
	}
}

func funcrefExprPayload(e wasm.Expr) (uint32, error) {
	payload, err := wasm.ParseFuncrefElementExpr(e)
	if err != nil {
		return 0, err
	}
	if payload.Null {
		return nullFuncRefIndex, nil
	}
	return payload.FuncIndex, nil
}

func funcTypeUsesV128(ft *wasm.CompType) bool {
	if ft == nil {
		return false
	}
	for _, t := range ft.Params {
		if wasm.EqualValType(t, wasm.V128) {
			return true
		}
	}
	for _, t := range ft.Results {
		if wasm.EqualValType(t, wasm.V128) {
			return true
		}
	}
	return false
}

// asyncReplayable reports whether a host import's signature can be served by the
// async log-and-replay path, which captures a single i32 argument and no results.
// Every other signature must run through the synchronous host dispatcher.
func asyncReplayable(sig FuncSig) bool {
	return len(sig.Results) == 0 && len(sig.Params) <= 1 &&
		(len(sig.Params) == 0 || sig.Params[0] == ValI32)
}

func (c *Compiled) importsRequireSync(imports Imports, force bool) bool {
	if force || forceSyncHostImports || c.needsPublicFuncrefHostReentry() {
		return true
	}
	for _, key := range c.Imports {
		if export, cross := imports[key].(*InstanceExport); cross {
			// A cross-instance-only consumer needs the parked-host loop only when
			// its target can itself park. syncMode is immutable after the producer
			// is instantiated and already includes its transitive direct imports.
			if export == nil || export.inst == nil || export.inst.syncMode {
				return true
			}
			continue
		}
		// Every actual host binding remains synchronous. In particular, a legacy
		// replayable HostFunc may later be reached through an InstanceExport, where
		// logging into the callee's private buffer would be invisible to the public
		// root. Only host-free cross-instance links take the fast native path.
		return true
	}
	// An imported funcref table can be mutated to contain a host or
	// cross-instance descriptor after instantiation, so it remains conservative.
	for i := 0; i < c.tableImportCount(); i++ {
		if c.tableElementType(i) == ValFuncRef {
			return true
		}
	}
	return false
}

// validateImportBindings checks cross-instance signatures and reference-store
// compatibility. Imported calls are already compiled; instantiation only writes
// concrete targets into the per-instance dispatch table.
func (c *Compiled) validateImportBindings(imports Imports, store *referenceStore) error {
	ehNativeCalls := c.stagedFeatures().IsEnabled(CoreFeatureExceptionHandling) && len(c.Imports) != 0
	for i, key := range c.Imports {
		ex, ok := imports[key].(*InstanceExport)
		if !ok {
			if ehNativeCalls {
				return fmt.Errorf("exception-handler transfer import %q requires a retained InstanceExport", key)
			}
			continue
		}
		if ex == nil || ex.inst == nil {
			return fmt.Errorf("cross-instance import %q is nil", key)
		}
		if ex.localIdx < 0 || ex.localIdx >= len(ex.inst.c.Entry) {
			return fmt.Errorf("cross-instance import %q references an unavailable function", key)
		}
		if i >= len(c.importFuncSigs) {
			return fmt.Errorf("cross-instance import %q is missing its signature", key)
		}
		sig := c.importFuncSigs[i]
		if !sigMatches(sig, c.Types, ex) {
			return fmt.Errorf("cross-instance import %q signature mismatch", key)
		}
		if hasValType(sig.Params, ValExternRef) || hasValType(sig.Results, ValExternRef) {
			if store == nil || ex.inst.refStore != store {
				return fmt.Errorf("cross-instance externref import %q requires the same reference store", key)
			}
		}
		if ehNativeCalls {
			if !ex.inst.c.stagedFeatures().IsEnabled(CoreFeatureExceptionHandling) {
				return fmt.Errorf("exception-handler transfer import %q requires an exception-enabled producer", key)
			}
			if len(sig.Params) != 0 || len(sig.Results) != 0 {
				return fmt.Errorf("exception-handler transfer import %q requires the exact () -> () ABI", key)
			}
		}
	}
	return nil

}

func sigMatches(required FuncSig, requiredTypes []DefinedTypeDescriptor, ex *InstanceExport) bool {
	if ex == nil || ex.inst == nil || ex.localIdx < 0 || ex.localIdx >= len(ex.inst.c.Funcs) {
		return false
	}
	requiredParams, requiredResults, err := exactFuncSignature(required, requiredTypes)
	if err != nil {
		return false
	}
	actualParams, actualResults, err := exactFuncSignature(ex.inst.c.Funcs[ex.localIdx], ex.inst.c.Types)
	if err != nil || len(requiredParams) != len(actualParams) || len(requiredResults) != len(actualResults) {
		return false
	}
	for i := range requiredParams {
		if !valueTypeEquivalent(actualParams[i], ex.inst.c.Types, requiredParams[i], requiredTypes) {
			return false
		}
	}
	for i := range requiredResults {
		if !valueTypeEquivalent(actualResults[i], ex.inst.c.Types, requiredResults[i], requiredTypes) {
			return false
		}
	}
	return true
}

func moduleSegmentStateCounts(m *wasm.Module) (elemCount, dataCount int) {
	for i := range m.Elements {
		if m.Elements[i].Mode.Kind == wasm.ElemPassive {
			elemCount = i + 1
		}
	}
	for i := range m.Data {
		if m.Data[i].Mode.Kind == wasm.DataPassive {
			dataCount = i + 1
		}
	}
	if elemCount == len(m.Elements) && dataCount == len(m.Data) {
		// Every declared index already has a passive descriptor slot (including
		// the common zero-segment case), so no instruction walk is needed.
		return elemCount, dataCount
	}
	for i := range m.Code {
		fn := &m.Code[i]
		if len(fn.BodyBytes) != 0 {
			if !bodyBytesSegmentStateCounts(fn.BodyBytes, &elemCount, &dataCount) {
				// Validation already walked this body successfully. If a later walker
				// nevertheless disagrees, reserve every declared slot rather than emit
				// code that can index beyond the runtime descriptor arrays.
				elemCount = len(m.Elements)
				dataCount = len(m.Data)
			}
		} else {
			instrsSegmentStateCounts(fn.Body.Instrs, &elemCount, &dataCount)
		}
	}
	return elemCount, dataCount
}

func bodyBytesSegmentStateCounts(body []byte, elemCount, dataCount *int) bool {
	r := wasm.NewReader(body)
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return false
		}
		imm, err := wasm.ClassifyInstructionImmediate(r, op)
		if err != nil {
			return false
		}
		segmentStateCount(imm.Kind, imm.Index, elemCount, dataCount)
	}
	return true
}

func instrsSegmentStateCounts(instrs []wasm.Instruction, elemCount, dataCount *int) {
	for i := range instrs {
		in := &instrs[i]
		segmentStateCount(in.Kind, in.Index, elemCount, dataCount)
		instrsSegmentStateCounts(in.Body().Instrs, elemCount, dataCount)
		instrsSegmentStateCounts(in.Then(), elemCount, dataCount)
		instrsSegmentStateCounts(in.Else(), elemCount, dataCount)
	}
}

func segmentStateCount(kind wasm.InstrKind, index uint32, elemCount, dataCount *int) {
	count := int(index) + 1
	switch kind {
	case wasm.InstrTableInit, wasm.InstrElemDrop:
		if count > *elemCount {
			*elemCount = count
		}
	case wasm.InstrMemoryInit, wasm.InstrDataDrop:
		if count > *dataCount {
			*dataCount = count
		}
	}
}

func moduleUsesMemoryGrow(m *wasm.Module) bool {
	for i := range m.Code {
		fn := &m.Code[i]
		// Byte-backed decode keeps function bodies as raw bytecode and leaves
		// Body.Instrs empty, so walk the encoded stream when present and only fall
		// back to the instruction tree for programmatically built bodies.
		if len(fn.BodyBytes) != 0 {
			if bodyBytesUseMemoryGrow(fn.BodyBytes) {
				return true
			}
			continue
		}
		if instrsUseMemoryGrow(fn.Body.Instrs) {
			return true
		}
	}
	return false
}

// bodyBytesUseMemoryGrow reports whether a validated, byte-backed function body
// contains a memory.grow. The body is already validated, so a decode hiccup is
// not expected; if one occurs it conservatively returns true so the caller does
// not pin the memory reservation to its minimum size and break memory.grow.
func bodyBytesUseMemoryGrow(body []byte) bool {
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
		if imm.Kind == wasm.InstrMemoryGrow {
			return true
		}
	}
	return false
}

func instrsUseMemoryGrow(instrs []wasm.Instruction) bool {
	for i := range instrs {
		in := &instrs[i]
		if in.Kind == wasm.InstrMemoryGrow {
			return true
		}
		if instrsUseMemoryGrow(in.Body().Instrs) || instrsUseMemoryGrow(in.Then()) || instrsUseMemoryGrow(in.Else()) {
			return true
		}
	}
	return false
}

// MustCompile is like Compile with the default config but panics on error, for
// tests, examples, and package-level initialization.
func MustCompile(wasmBytes []byte) *Compiled {
	c, err := Compile(nil, wasmBytes)
	if err != nil {
		panic("wago: MustCompile: " + err.Error())
	}
	return c
}

// ExportedFunctions returns the names of the module's exported functions, sorted.
func (c *Compiled) ExportedFunctions() []string { return sortedKeys(c.Exports) }

// ExportedGlobals returns the names of the module's exported globals, sorted.
func (c *Compiled) ExportedGlobals() []string { return sortedKeys(c.GlobalExports) }

// MemoryImport returns the "module.name" key when the module imports exactly one
// memory. Modules with zero or multiple memory imports return false; use
// MemoryImports for the complete ordered list.
func (c *Compiled) MemoryImport() (string, bool) {
	if c == nil || c.memoryImportCount() != 1 {
		return "", false
	}
	def, _ := c.memoryImportAt(0)
	return def.ImportKey, true
}

// MemoryImports returns every imported memory key in Wasm memory-index order.
// Duplicate keys are preserved because distinct declarations may alias the same
// host memory once indexed execution is admitted.
func (c *Compiled) MemoryImports() []string {
	if c == nil {
		return nil
	}
	count := c.memoryImportCount()
	keys := make([]string, count)
	for i := range keys {
		def, _ := c.memoryImportAt(i)
		keys[i] = def.ImportKey
	}
	return keys
}

// TableImport returns the "module.name" key when the module imports exactly one
// table. Instantiate then requires a *Table for that key. Modules with zero or
// multiple table imports return false; use TableImports for the complete list.
func (c *Compiled) TableImport() (string, bool) {
	if c == nil {
		return "", false
	}
	if c.tableImportCount() != 1 {
		return "", false
	}
	return c.tableImport, true
}

// TableImports returns every imported table key in Wasm table-index order.
// Duplicate keys are preserved because two declarations may intentionally alias
// the same shared table object.
func (c *Compiled) TableImports() []string {
	if c == nil {
		return nil
	}
	count := c.tableImportCount()
	if count == 0 {
		return nil
	}
	keys := make([]string, count)
	for i := range keys {
		def, _ := c.tableImportAt(i)
		keys[i] = def.Key
	}
	return keys
}

func memoryDefFromWasm(mt wasm.MemType) memoryDef {
	def := memoryDef{Min: mt.Limits.Min, Addr64: mt.Limits.Addr64, Shared: mt.Shared}
	if mt.Limits.Max != nil {
		def.Max = *mt.Limits.Max
		def.HasMax = true
	}
	return def
}

func sortedKeys(m map[string]int) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Signature returns the parameter and result types of an exported function.
func (c *Compiled) Signature(export string) (params, results []ValType, err error) {
	if c == nil {
		return nil, nil, fmt.Errorf("compiled module is nil")
	}
	gfi, ok := c.Exports[export]
	if !ok {
		return nil, nil, fmt.Errorf("no exported function %q", export)
	}
	if gfi < 0 {
		return nil, nil, fmt.Errorf("export %q function index %d out of range", export, gfi)
	}
	if gfi < c.NumImports {
		if gfi >= len(c.importFuncSigs) {
			return nil, nil, fmt.Errorf("export %q imported function index %d has no signature", export, gfi)
		}
		sig := c.importFuncSigs[gfi]
		return sig.Params, sig.Results, nil
	}
	li := gfi - c.NumImports
	if li < 0 || li >= len(c.Funcs) {
		return nil, nil, fmt.Errorf("export %q function index %d out of range", export, gfi)
	}
	return c.Funcs[li].Params, c.Funcs[li].Results, nil
}

// SignatureDescriptor returns the exact structural parameter and result types
// of an exported function. Indexed references resolve against TypeDefinitions.
func (c *Compiled) SignatureDescriptor(export string) (params, results []ValueTypeDescriptor, err error) {
	if c == nil {
		return nil, nil, fmt.Errorf("compiled module is nil")
	}
	gfi, ok := c.Exports[export]
	if !ok {
		return nil, nil, fmt.Errorf("no exported function %q", export)
	}
	var sig FuncSig
	if gfi < 0 {
		return nil, nil, fmt.Errorf("export %q function index %d out of range", export, gfi)
	} else if gfi < c.NumImports {
		if gfi >= len(c.importFuncSigs) {
			return nil, nil, fmt.Errorf("export %q imported function index %d has no signature", export, gfi)
		}
		sig = c.importFuncSigs[gfi]
	} else {
		li := gfi - c.NumImports
		if li < 0 || li >= len(c.Funcs) {
			return nil, nil, fmt.Errorf("export %q function index %d out of range", export, gfi)
		}
		sig = c.Funcs[li]
	}
	params, results, err = exactFuncSignature(sig, c.Types)
	if err != nil {
		return nil, nil, fmt.Errorf("export %q signature: %w", export, err)
	}
	return params, results, nil
}

// TypeDefinitions returns a copy of the compiled module's flattened structural
// type graph.
func (c *Compiled) TypeDefinitions() []DefinedTypeDescriptor {
	if c == nil {
		return nil
	}
	return cloneDefinedTypeDescriptors(c.Types)
}

// FuncName returns the name-section name for a global function index.
func (c *Compiled) FuncName(funcIdx uint32) (string, bool) {
	if c == nil || c.Names == nil {
		return "", false
	}
	return c.Names.FuncName(funcIdx)
}

// LocalFuncName returns the name-section name for a locally-defined function
// index (that is, an index into Compiled.Funcs rather than wasm's global
// function-index space).
func (c *Compiled) LocalFuncName(localIdx int) (string, bool) {
	if c == nil || localIdx < 0 {
		return "", false
	}
	return c.FuncName(uint32(localIdx + c.NumImports))
}

// FuncDebugName returns a stable display name for a global function index,
// preferring the wasm name section and falling back to exports or funcN.
func (c *Compiled) FuncDebugName(funcIdx uint32) string {
	if name, ok := c.FuncName(funcIdx); ok && name != "" {
		return name
	}
	if c != nil {
		var exports []string
		for name, idx := range c.Exports {
			if idx == int(funcIdx) {
				exports = append(exports, name)
			}
		}
		if len(exports) > 0 {
			sort.Strings(exports)
			return exports[0]
		}
	}
	return fmt.Sprintf("func%d", funcIdx)
}

func (c *Compiled) validate() error {
	if c == nil {
		return fmt.Errorf("compiled module is nil")
	}
	if c.NumImports < 0 {
		return fmt.Errorf("compiled metadata invalid: negative NumImports %d", c.NumImports)
	}
	if len(c.Imports) != c.NumImports {
		return fmt.Errorf("compiled metadata invalid: Imports length %d != NumImports %d", len(c.Imports), c.NumImports)
	}
	if len(c.importFuncSigs) != c.NumImports {
		return fmt.Errorf("compiled metadata invalid: importFuncSigs length %d != NumImports %d", len(c.importFuncSigs), c.NumImports)
	}
	if c.dynamicImports != (c.NumImports > 0) {
		return fmt.Errorf("compiled metadata invalid: dynamic import dispatch=%v with %d function import(s)", c.dynamicImports, c.NumImports)
	}
	if c.NumImports > maxInt()-len(c.Funcs) {
		return fmt.Errorf("compiled metadata invalid: function count overflows int")
	}
	if err := validateDefinedTypeDescriptors(c.Types); err != nil {
		return err
	}
	if err := validateValueTypeDescriptors(c.Types, c.ValueTypes); err != nil {
		return err
	}
	if err := c.validateExactValueMetadata(); err != nil {
		return err
	}
	validateSigs := func(kind string, sigs []FuncSig) error {
		for i, sig := range sigs {
			if _, _, err := exactFuncSignature(sig, c.Types); err != nil {
				return fmt.Errorf("compiled metadata invalid: %s function %d signature: %w", kind, i, err)
			}
		}
		return nil
	}
	if err := validateSigs("imported", c.importFuncSigs); err != nil {
		return err
	}
	if err := validateSigs("local", c.Funcs); err != nil {
		return err
	}
	required := c.requiredFeatures
	unsupported := required &^ coreFeaturesWago
	var staged CoreFeatures
	if c.memoryDir != nil {
		if c.memoryDir.staged {
			staged |= CoreFeatureMultiMemory
		}
		if c.memoryDir.stagedMemory64 {
			staged |= CoreFeatureMemory64
		}
	}
	if c.stagedTable64 {
		staged |= CoreFeatureTable64
	}
	if c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		staged |= CoreFeatureExceptionHandling
		if len(c.memoryDir.ehTags) > 9 {
			return fmt.Errorf("compiled metadata invalid: staged exception tag directory")
		}
		importCount := 0
		for i, tag := range c.memoryDir.ehTags {
			if int(tag.TypeIndex) >= len(c.Types) || c.Types[tag.TypeIndex].Kind != CompositeTypeFunction || len(c.Types[tag.TypeIndex].Results) != 0 || len(c.Types[tag.TypeIndex].Params) > 2 {
				return fmt.Errorf("compiled metadata invalid: staged exception tag directory")
			}
			if tag.ImportKey != "" {
				if i != importCount {
					return fmt.Errorf("compiled metadata invalid: staged exception tag imports must precede locals")
				}
				importCount++
			}
		}
		for name, index := range c.memoryDir.ehTagExports {
			if name == "" || index < 0 || index >= len(c.memoryDir.ehTags) {
				return fmt.Errorf("compiled metadata invalid: staged exception tag export")
			}
		}
	}
	staged |= c.stagedFeatures()
	if unsupported&^staged != 0 {
		return fmt.Errorf("compiled metadata invalid: unknown required feature bits 0x%x", uint64(unsupported&^staged))
	}
	if err := c.validateMemoryMetadata(required); err != nil {
		return err
	}
	if c.TableSize < 0 {
		return fmt.Errorf("compiled metadata invalid: negative TableSize %d", c.TableSize)
	}
	if c.TableMax != 0 && c.TableMax < uint64(c.TableSize) {
		return fmt.Errorf("compiled metadata invalid: TableMax %d < TableSize %d", c.TableMax, c.TableSize)
	}
	if c.TableAddr64 && !required.IsEnabled(CoreFeatureTable64) {
		return fmt.Errorf("compiled metadata invalid: table 0 uses 64-bit indexes without table64 feature")
	}
	if c.TableMax > uint64(maxInt()) && !c.stagedInertOversizedTable64(0) {
		return fmt.Errorf("compiled metadata invalid: table 0 maximum %d overflows executable capacity", c.TableMax)
	}
	if len(c.extraTables) > 0 && !c.HasTable {
		return fmt.Errorf("compiled metadata invalid: %d extra table(s) without table 0", len(c.extraTables))
	}
	if c.HasTable && c.TableType != 0 && c.TableType != ValFuncRef && c.TableType != ValExternRef {
		return fmt.Errorf("compiled metadata invalid: table 0 element type %s is unsupported", c.TableType)
	}
	for i, table := range c.extraTables {
		if table.Size < 0 {
			return fmt.Errorf("compiled metadata invalid: negative table %d size", i+1)
		}
		if table.Max != 0 && table.Max < uint64(table.Size) {
			return fmt.Errorf("compiled metadata invalid: table %d maximum %d < size %d", i+1, table.Max, table.Size)
		}
		if table.Type != 0 && table.Type != ValFuncRef && table.Type != ValExternRef {
			return fmt.Errorf("compiled metadata invalid: table %d element type %s is unsupported", i+1, table.Type)
		}
		if table.Addr64 && !required.IsEnabled(CoreFeatureTable64) {
			return fmt.Errorf("compiled metadata invalid: table %d uses 64-bit indexes without table64 feature", i+1)
		}
		if table.Max > uint64(maxInt()) {
			return fmt.Errorf("compiled metadata invalid: table %d maximum %d overflows executable capacity", i+1, table.Max)
		}
	}
	if !c.HasTable && c.TableSize != 0 {
		return fmt.Errorf("compiled metadata invalid: TableSize %d without table", c.TableSize)
	}
	if !c.HasTable && c.TableMax != 0 {
		return fmt.Errorf("compiled metadata invalid: TableMax %d without table", c.TableMax)
	}
	if !c.HasTable && c.TableAddr64 {
		return fmt.Errorf("compiled metadata invalid: table64 address form without table")
	}
	if !c.HasTable && c.tableImport != "" {
		return fmt.Errorf("compiled metadata invalid: table import %q without table", c.tableImport)
	}
	if c.tableImport == "" {
		if c.tableImportMin != 0 || c.tableImportMax != 0 || c.tableImportHasMax {
			return fmt.Errorf("compiled metadata invalid: table import limits without table import")
		}
		if c.TableHasMax && !c.HasTable {
			return fmt.Errorf("compiled metadata invalid: table maximum without table")
		}
	} else {
		if c.TableHasMax {
			return fmt.Errorf("compiled metadata invalid: local table maximum flag on imported table 0")
		}
		if c.TableSize != 0 || c.TableMax != 0 {
			return fmt.Errorf("compiled metadata invalid: local table limits present on imported table")
		}
		if c.tableImportMin < 0 || c.tableImportMax < 0 {
			return fmt.Errorf("compiled metadata invalid: negative imported table limit")
		}
		if !c.tableImportHasMax && c.tableImportMax != 0 {
			return fmt.Errorf("compiled metadata invalid: imported table max without max flag")
		}
		if c.tableImportHasMax && c.tableImportMax < c.tableImportMin {
			return fmt.Errorf("compiled metadata invalid: imported table max %d < min %d", c.tableImportMax, c.tableImportMin)
		}
	}
	seenLocalTable := false
	for i, table := range c.extraTables {
		index := i + 1
		if table.ImportKey == "" {
			seenLocalTable = true
			if table.ImportHasMax {
				return fmt.Errorf("compiled metadata invalid: table %d import max flag without import key", index)
			}
			continue
		}
		if c.tableImport == "" {
			return fmt.Errorf("compiled metadata invalid: imported table %d without imported table 0", index)
		}
		if seenLocalTable {
			return fmt.Errorf("compiled metadata invalid: imported table %d follows a local table", index)
		}
		if table.HasInitFunc {
			return fmt.Errorf("compiled metadata invalid: initializer on imported table %d", index)
		}
		if table.HasMax {
			return fmt.Errorf("compiled metadata invalid: local max flag on imported table %d", index)
		}
		if !table.ImportHasMax && table.Max != 0 {
			return fmt.Errorf("compiled metadata invalid: imported table %d max without max flag", index)
		}
		if table.ImportHasMax && table.Max < uint64(table.Size) {
			return fmt.Errorf("compiled metadata invalid: imported table %d max %d < min %d", index, table.Max, table.Size)
		}
	}
	if len(c.Elems) > 0 && !c.HasTable {
		return fmt.Errorf("compiled metadata invalid: %d element segment(s) without table", len(c.Elems))
	}
	if len(c.Entry) != len(c.Funcs) {
		return fmt.Errorf("compiled metadata invalid: Entry length %d != Funcs length %d", len(c.Entry), len(c.Funcs))
	}
	for i, off := range c.Entry {
		if off < 0 || off >= len(c.Code) {
			return fmt.Errorf("compiled metadata invalid: Entry[%d] offset %d out of code range %d", i, off, len(c.Code))
		}
	}
	totalFuncs := c.NumImports + len(c.Funcs)
	if len(c.FuncTypeID) != totalFuncs {
		return fmt.Errorf("compiled metadata invalid: FuncTypeID length %d != function count %d", len(c.FuncTypeID), totalFuncs)
	}
	if c.HasTableInitFunc {
		if !c.HasTable {
			return fmt.Errorf("compiled metadata invalid: table initializer without table")
		}
		if c.tableImport != "" {
			return fmt.Errorf("compiled metadata invalid: table initializer on imported table")
		}
		if uint64(c.TableInitFunc) >= uint64(totalFuncs) {
			return fmt.Errorf("compiled metadata invalid: table initializer function index %d out of range", c.TableInitFunc)
		}
		actual, actualErr := c.functionRefExactType(c.TableInitFunc)
		required, requiredErr := c.tableExactType(0)
		if actualErr != nil || requiredErr != nil || !valueTypeSubtype(actual, c.Types, required, c.Types) {
			return fmt.Errorf("compiled metadata invalid: table 0 initializer function type mismatch")
		}
	}
	for i, table := range c.extraTables {
		if !table.HasInitFunc {
			continue
		}
		if uint64(table.InitFunc) >= uint64(totalFuncs) {
			return fmt.Errorf("compiled metadata invalid: table %d initializer function index %d out of range", i+1, table.InitFunc)
		}
		actual, actualErr := c.functionRefExactType(table.InitFunc)
		required, requiredErr := c.tableExactType(i + 1)
		if actualErr != nil || requiredErr != nil || !valueTypeSubtype(actual, c.Types, required, c.Types) {
			return fmt.Errorf("compiled metadata invalid: table %d initializer function type mismatch", i+1)
		}
	}
	for name, gfi := range c.Exports {
		if gfi < 0 || gfi >= totalFuncs {
			return fmt.Errorf("compiled metadata invalid: function export %q index %d out of range", name, gfi)
		}
	}
	if len(c.tableExports) != 0 && !c.hasTableExportMetadata {
		return fmt.Errorf("compiled metadata invalid: table exports without exact export metadata marker")
	}
	for name, tableIndex := range c.tableExports {
		if tableIndex < 0 || tableIndex >= c.tableCount() {
			return fmt.Errorf("compiled metadata invalid: table export %q index %d out of range", name, tableIndex)
		}
	}
	if err := c.validateRuntimeReferenceGlobalMetadata(); err != nil {
		return err
	}
	if len(c.GlobalImports) > len(c.Globals) {
		return fmt.Errorf("compiled metadata invalid: GlobalImports length %d > Globals length %d", len(c.GlobalImports), len(c.Globals))
	}
	for i, imp := range c.GlobalImports {
		g := c.Globals[i]
		if g.Type != imp.Type || g.Mutable != imp.Mutable {
			return fmt.Errorf("compiled metadata invalid: imported global %d metadata mismatch", i)
		}
		gt, gerr := exactValueType(g.Type, g.HasValueType, g.ValueTypeIndex, c.ValueTypes, c.Types)
		it, ierr := exactValueType(imp.Type, imp.HasValueType, imp.ValueTypeIndex, c.ValueTypes, c.Types)
		if gerr != nil || ierr != nil || gt != it {
			return fmt.Errorf("compiled metadata invalid: imported global %d structural type mismatch", i)
		}
	}
	for name, idx := range c.GlobalExports {
		if idx < 0 || idx >= len(c.Globals) {
			return fmt.Errorf("compiled metadata invalid: global export %q index %d out of range", name, idx)
		}
	}
	for i, g := range c.Globals {
		initKinds := 0
		if g.HasInitGlobal {
			initKinds++
		}
		if g.HasInitFunc {
			initKinds++
		}
		if len(g.InitExpr) != 0 {
			initKinds++
		}
		if initKinds > 1 {
			return fmt.Errorf("compiled metadata invalid: global %d has multiple initializer forms", i)
		}
		if i < len(c.GlobalImports) && initKinds != 0 {
			return fmt.Errorf("compiled metadata invalid: imported global %d has initializer metadata", i)
		}
		if g.HasInitGlobal {
			if g.InitGlobal < 0 || g.InitGlobal >= i || g.InitGlobal >= len(c.Globals) {
				return fmt.Errorf("compiled metadata invalid: global %d initializer references unavailable global %d", i, g.InitGlobal)
			}
			src := c.Globals[g.InitGlobal]
			if src.Mutable {
				return fmt.Errorf("compiled metadata invalid: global %d initializer references mutable global %d", i, g.InitGlobal)
			}
			if src.Type != g.Type {
				return fmt.Errorf("compiled metadata invalid: global %d initializer type %s != source global %d type %s", i, g.Type, g.InitGlobal, src.Type)
			}
			srcExact, srcErr := exactValueType(src.Type, src.HasValueType, src.ValueTypeIndex, c.ValueTypes, c.Types)
			dstExact, dstErr := exactValueType(g.Type, g.HasValueType, g.ValueTypeIndex, c.ValueTypes, c.Types)
			if srcErr != nil || dstErr != nil || !valueTypeSubtype(srcExact, c.Types, dstExact, c.Types) {
				return fmt.Errorf("compiled metadata invalid: global %d initializer structural type mismatch with source global %d", i, g.InitGlobal)
			}
		}
		if len(g.InitExpr) != 0 {
			if err := validateCompiledScalarConstExpr(g.InitExpr, g.Type, c.Globals, i); err != nil {
				return fmt.Errorf("compiled metadata invalid: global %d extended initializer: %w", i, err)
			}
		}
		if g.HasInitFunc {
			if g.Type != ValFuncRef {
				return fmt.Errorf("compiled metadata invalid: global %d ref.func initializer has type %s", i, g.Type)
			}
			if uint64(g.InitFunc) >= uint64(totalFuncs) {
				return fmt.Errorf("compiled metadata invalid: global %d ref.func initializer index %d out of range", i, g.InitFunc)
			}
			if !c.needsFuncRefDescs() {
				return fmt.Errorf("compiled metadata invalid: global %d ref.func initializer without descriptor arena", i)
			}
			actual, actualErr := c.functionRefExactType(g.InitFunc)
			required, requiredErr := c.globalExactType(i)
			if actualErr != nil || requiredErr != nil || !valueTypeSubtype(actual, c.Types, required, c.Types) {
				return fmt.Errorf("compiled metadata invalid: global %d ref.func initializer structural type mismatch", i)
			}
		}
	}
	validateOffset := func(kind string, seg int, offset OffsetInit, want ValType) error {
		if offset.HasGlobal && len(offset.Expr) != 0 {
			return fmt.Errorf("compiled metadata invalid: %s %d has multiple offset initializer forms", kind, seg)
		}
		if offset.HasGlobal {
			if want != ValI32 {
				return fmt.Errorf("compiled metadata invalid: %s %d uses compact i32 global offset for %s address", kind, seg, want)
			}
			return c.validateDeferredOffsetGlobal(kind, seg, offset.Global)
		}
		if len(offset.Expr) != 0 {
			if err := validateCompiledScalarConstExpr(offset.Expr, want, c.Globals, len(c.GlobalImports)); err != nil {
				return fmt.Errorf("compiled metadata invalid: %s %d extended %s offset: %w", kind, seg, want, err)
			}
		}
		return nil
	}
	validateElementValues := func(kind string, seg int, elem ElemInit) error {
		refType := normalizedElemRefType(elem.RefType)
		if refType != ValFuncRef && refType != ValExternRef {
			return fmt.Errorf("compiled metadata invalid: %s element %d has unsupported reference type %s", kind, seg, refType)
		}
		required, err := c.elemExactType(elem)
		if err != nil {
			return fmt.Errorf("compiled metadata invalid: %s element %d exact type: %w", kind, seg, err)
		}
		for k, value := range elem.Values {
			if value.Null {
				if required.Kind != ValueTypeReference || !required.Ref.Nullable {
					return fmt.Errorf("compiled metadata invalid: %s element %d value %d is null for a non-null type", kind, seg, k)
				}
				continue
			}
			if refType != ValFuncRef {
				return fmt.Errorf("compiled metadata invalid: %s element %d value %d is non-null %s", kind, seg, k, refType)
			}
			if int(value.FuncIndex) >= totalFuncs {
				return fmt.Errorf("compiled metadata invalid: %s element %d function %d index %d out of range", kind, seg, k, value.FuncIndex)
			}
			actual, actualErr := c.functionRefExactType(value.FuncIndex)
			if actualErr != nil || !valueTypeSubtype(actual, c.Types, required, c.Types) {
				return fmt.Errorf("compiled metadata invalid: %s element %d value %d function type mismatch", kind, seg, k)
			}
		}
		return nil
	}
	for seg, el := range c.Elems {
		refType := normalizedElemRefType(el.RefType)
		if el.Mode != ElemModeActive {
			return fmt.Errorf("compiled metadata invalid: active element %d has mode %d", seg, el.Mode)
		}
		if uint64(el.TableIndex) >= uint64(c.tableCount()) {
			return fmt.Errorf("compiled metadata invalid: active element %d table index %d out of range", seg, el.TableIndex)
		}
		if c.tableElementType(int(el.TableIndex)) != refType {
			return fmt.Errorf("compiled metadata invalid: active element %d type %s does not match table %d type %s", seg, refType, el.TableIndex, c.tableElementType(int(el.TableIndex)))
		}
		elemExact, elemErr := c.elemExactType(el)
		tableExact, tableErr := c.tableExactType(int(el.TableIndex))
		if elemErr != nil || tableErr != nil || !valueTypeSubtype(elemExact, c.Types, tableExact, c.Types) {
			return fmt.Errorf("compiled metadata invalid: active element %d structural type does not match table %d", seg, el.TableIndex)
		}
		offsetType := ValI32
		if c.tableDef(int(el.TableIndex)).Addr64 {
			offsetType = ValI64
		}
		if err := validateOffset("element", seg, el.Offset, offsetType); err != nil {
			return err
		}
		if err := validateElementValues("active", seg, el); err != nil {
			return err
		}
	}
	for seg, el := range c.passiveElems {
		mode := el.Mode
		if mode == ElemModeActive || mode == ElemModeDeclarative {
			if len(el.Values) != 0 {
				return fmt.Errorf("compiled metadata invalid: dropped element %d retains %d value(s)", seg, len(el.Values))
			}
			if mode == ElemModeActive && uint64(el.TableIndex) >= uint64(c.tableCount()) {
				return fmt.Errorf("compiled metadata invalid: dropped active element %d table index %d out of range", seg, el.TableIndex)
			}
		} else if mode != ElemModePassive {
			return fmt.Errorf("compiled metadata invalid: element-state slot %d has mode %d", seg, mode)
		}
		if err := validateElementValues("element-state", seg, el); err != nil {
			return err
		}
	}
	for seg, d := range c.Data {
		if count := c.memoryCount(); d.MemoryIndex != 0 || count != 0 {
			if uint64(d.MemoryIndex) >= uint64(count) {
				return fmt.Errorf("compiled metadata invalid: active data %d memory index %d out of range", seg, d.MemoryIndex)
			}
		}
		want := ValI32
		if count := c.memoryCount(); count != 0 && c.memoryDef(int(d.MemoryIndex)).Addr64 {
			want = ValI64
		}
		if err := validateOffset("data", seg, d.Offset, want); err != nil {
			return err
		}
	}
	for seg, d := range c.PassiveData {
		if uint64(len(d.Bytes)) > uint64(^uint32(0)) {
			return fmt.Errorf("compiled metadata invalid: passive data %d length %d overflows descriptor", seg, len(d.Bytes))
		}
	}
	if err := gc.ValidateTypeDescs(c.GCTypeDescs); err != nil {
		return fmt.Errorf("compiled metadata invalid: GCTypeDescs: %w", err)
	}
	if err := c.validateArenaFootprint(); err != nil {
		return err
	}
	return nil
}

func (c *Compiled) validateRuntimeReferenceGlobalMetadata() error {
	for i, g := range c.Globals {
		if g.Type == ValExternRef && g.Bits != 0 {
			return fmt.Errorf("compiled metadata invalid: non-null externref global initializer at global %d is unsupported", i)
		}
		if g.Type == ValFuncRef && g.Bits != 0 {
			return fmt.Errorf("compiled metadata invalid: non-structural funcref global initializer at global %d is unsupported", i)
		}
	}
	return nil
}

func (c *Compiled) validateExactValueMetadata() error {
	check := func(context string, legacy ValType, has bool, index uint32) error {
		if _, err := exactValueType(legacy, has, index, c.ValueTypes, c.Types); err != nil {
			return fmt.Errorf("compiled metadata invalid: %s: %w", context, err)
		}
		return nil
	}
	for i, g := range c.GlobalImports {
		if err := check(fmt.Sprintf("global import %d type", i), g.Type, g.HasValueType, g.ValueTypeIndex); err != nil {
			return err
		}
	}
	for i, g := range c.Globals {
		if err := check(fmt.Sprintf("global %d type", i), g.Type, g.HasValueType, g.ValueTypeIndex); err != nil {
			return err
		}
	}
	if c.HasTable {
		if err := check("table 0 type", c.tableElementType(0), c.TableHasValueType, c.TableValueTypeIndex); err != nil {
			return err
		}
		for i, table := range c.extraTables {
			if err := check(fmt.Sprintf("table %d type", i+1), c.tableElementType(i+1), table.HasValueType, table.ValueTypeIndex); err != nil {
				return err
			}
		}
	}
	for i, elem := range c.Elems {
		if err := check(fmt.Sprintf("active element %d type", i), normalizedElemRefType(elem.RefType), elem.HasValueType, elem.ValueTypeIndex); err != nil {
			return err
		}
	}
	for i, elem := range c.passiveElems {
		if err := check(fmt.Sprintf("element-state %d type", i), normalizedElemRefType(elem.RefType), elem.HasValueType, elem.ValueTypeIndex); err != nil {
			return err
		}
	}
	return nil
}

func (c *Compiled) validateCodecMetadata() error {
	if err := validateDefinedTypeDescriptors(c.Types); err != nil {
		return err
	}
	if err := validateValueTypeDescriptors(c.Types, c.ValueTypes); err != nil {
		return err
	}
	if err := c.validateExactValueMetadata(); err != nil {
		return err
	}
	for _, set := range []struct {
		kind string
		sigs []FuncSig
	}{{"imported", c.importFuncSigs}, {"local", c.Funcs}} {
		for i, sig := range set.sigs {
			if _, _, err := exactFuncSignature(sig, c.Types); err != nil {
				return fmt.Errorf("compiled metadata invalid: %s function %d signature: %w", set.kind, i, err)
			}
		}
	}
	structural := compiledStructuralRequiredFeatures(c)
	if unsupported := structural &^ CoreFeaturesV3; unsupported != 0 {
		return fmt.Errorf("compiled metadata invalid: unknown required feature bits 0x%x", uint64(unsupported))
	}
	if err := c.validateMemoryMetadata(structural); err != nil {
		return err
	}
	if err := c.validateRuntimeReferenceGlobalMetadata(); err != nil {
		return err
	}
	for i, g := range c.Globals {
		initKinds := 0
		if g.HasInitGlobal {
			initKinds++
		}
		if g.HasInitFunc {
			initKinds++
		}
		if len(g.InitExpr) != 0 {
			initKinds++
		}
		if initKinds > 1 {
			return fmt.Errorf("compiled metadata invalid: global %d has multiple initializer forms", i)
		}
		if len(g.InitExpr) != 0 {
			if err := validateCompiledScalarConstExpr(g.InitExpr, g.Type, c.Globals, i); err != nil {
				return fmt.Errorf("compiled metadata invalid: global %d extended initializer: %w", i, err)
			}
		}
		if g.HasInitFunc && g.Type != ValFuncRef {
			return fmt.Errorf("compiled metadata invalid: global %d ref.func initializer has type %s", i, g.Type)
		}
	}
	for name, tableIndex := range c.tableExports {
		if tableIndex < 0 || tableIndex >= c.tableCount() {
			return fmt.Errorf("compiled metadata invalid: table export %q index %d out of range", name, tableIndex)
		}
	}
	checkOffset := func(kind string, i int, offset OffsetInit, want ValType) error {
		if offset.HasGlobal && len(offset.Expr) != 0 {
			return fmt.Errorf("compiled metadata invalid: %s %d has multiple offset initializer forms", kind, i)
		}
		if offset.HasGlobal && want != ValI32 {
			return fmt.Errorf("compiled metadata invalid: %s %d uses compact i32 global offset for %s address", kind, i, want)
		}
		if len(offset.Expr) != 0 {
			if err := validateCompiledScalarConstExpr(offset.Expr, want, c.Globals, len(c.GlobalImports)); err != nil {
				return fmt.Errorf("compiled metadata invalid: %s %d extended %s offset: %w", kind, i, want, err)
			}
		}
		return nil
	}
	checkElems := func(kind string, elems []ElemInit, active bool) error {
		for i, elem := range elems {
			offsetType := ValI32
			if active && int(elem.TableIndex) < c.tableCount() && c.tableDef(int(elem.TableIndex)).Addr64 {
				offsetType = ValI64
			}
			if err := checkOffset(kind, i, elem.Offset, offsetType); err != nil {
				return err
			}
			refType := normalizedElemRefType(elem.RefType)
			for j, value := range elem.Values {
				if !value.Null && refType != ValFuncRef {
					return fmt.Errorf("compiled metadata invalid: %s element %d value %d is non-null %s", kind, i, j, refType)
				}
			}
		}
		return nil
	}
	if err := checkElems("active", c.Elems, true); err != nil {
		return err
	}
	if err := checkElems("element-state", c.passiveElems, false); err != nil {
		return err
	}
	for i, data := range c.Data {
		want := ValI32
		if c.memoryCount() != 0 && c.memoryDef(int(data.MemoryIndex)).Addr64 {
			want = ValI64
		}
		if err := checkOffset("data", i, data.Offset, want); err != nil {
			return err
		}
	}
	return nil
}

func (c *Compiled) validateSnapshotReferenceGlobals() error {
	for i, g := range c.GlobalImports {
		if isReferenceValType(g.Type) {
			return fmt.Errorf("snapshot reference global metadata at import %d is unsupported until a live-state resolver exists", i)
		}
	}
	for i, g := range c.Globals {
		if isReferenceValType(g.Type) {
			return fmt.Errorf("snapshot reference global metadata at global %d is unsupported until a live-state resolver exists", i)
		}
	}
	return nil
}

func maxInt() int { return int(^uint(0) >> 1) }

func valTypeSlots(t ValType) int {
	if t == ValV128 {
		return 2
	}
	return 1
}

func valTypesSlots(ts []ValType) (int, error) {
	n := 0
	for _, t := range ts {
		s := valTypeSlots(t)
		if n > maxInt()-s {
			return 0, fmt.Errorf("value slot count overflows int")
		}
		n += s
	}
	return n, nil
}

func (c *Compiled) needsFuncRefDescs() bool {
	return c.NeedsFuncRefDescs || c.hasFuncrefTable()
}

func normalizedElemRefType(t ValType) ValType {
	if t == ValExternRef {
		return ValExternRef
	}
	return ValFuncRef
}

func normalizedTableElementType(t ValType) ValType {
	if t == ValExternRef {
		return ValExternRef
	}
	return ValFuncRef
}

func (c *Compiled) tableElementType(index int) ValType {
	if index == 0 {
		return normalizedTableElementType(c.TableType)
	}
	return normalizedTableElementType(c.extraTables[index-1].Type)
}

func (c *Compiled) tableEntryBytes(index int) int {
	if c.tableElementType(index) == ValExternRef {
		return 8
	}
	return wruntime.TableEntryBytes
}

func (c *Compiled) hasFuncrefTable() bool {
	for i := 0; i < c.tableCount(); i++ {
		if c.tableElementType(i) == ValFuncRef {
			return true
		}
	}
	return false
}

func (c *Compiled) hasExternrefTable() bool {
	for i := 0; i < c.tableCount(); i++ {
		if c.tableElementType(i) == ValExternRef {
			return true
		}
	}
	return false
}

func (c *Compiled) memoryExportMap() map[string]int {
	if c == nil || c.memoryDir == nil {
		return nil
	}
	return c.memoryDir.exports
}

func (c *Compiled) hasExactMemoryExports() bool {
	return c != nil && c.memoryDir != nil && c.memoryDir.exactExports
}

func (c *Compiled) memoryCount() int {
	if c == nil {
		return 0
	}
	if c.memoryDir != nil && len(c.memoryDir.defs) != 0 {
		return len(c.memoryDir.defs)
	}
	if c.HasMemory || c.memoryImport != "" {
		return 1
	}
	return 0
}

func (c *Compiled) memoryImportCount() int {
	count := 0
	for i := 0; i < c.memoryCount(); i++ {
		def := c.memoryDef(i)
		if def.ImportKey == "" {
			break
		}
		count++
	}
	return count
}

func (c *Compiled) memoryImportAt(index int) (memoryDef, bool) {
	if c == nil || index < 0 || index >= c.memoryCount() {
		return memoryDef{}, false
	}
	def := c.memoryDef(index)
	return def, def.ImportKey != ""
}

func (c *Compiled) memoryDef(index int) memoryDef {
	if c.memoryDir != nil && len(c.memoryDir.defs) != 0 {
		return c.memoryDir.defs[index]
	}
	if index != 0 {
		return memoryDef{}
	}
	def := memoryDef{ImportKey: c.memoryImport, Min: uint64(c.MemMinPages), Max: uint64(c.MemMaxPages), HasMax: c.MemMaxPages != 0}
	return def
}

func (c *Compiled) validateMemoryMetadata(required CoreFeatures) error {
	seenLocal := false
	for i := 0; i < c.memoryCount(); i++ {
		memory := c.memoryDef(i)
		if memory.HasMax && memory.Max < memory.Min {
			return fmt.Errorf("compiled metadata invalid: memory %d maximum %d < minimum %d", i, memory.Max, memory.Min)
		}
		if memory.Shared && !memory.HasMax {
			return fmt.Errorf("compiled metadata invalid: shared memory %d has no maximum", i)
		}
		if memory.ImportKey == "" {
			seenLocal = true
		} else if seenLocal {
			return fmt.Errorf("compiled metadata invalid: imported memory %d follows a local memory", i)
		}
		if memory.Addr64 && !required.IsEnabled(CoreFeatureMemory64) {
			return fmt.Errorf("compiled metadata invalid: memory %d uses 64-bit addresses without memory64 feature", i)
		}
	}
	if c.memoryCount() > 1 && !required.IsEnabled(CoreFeatureMultiMemory) {
		return fmt.Errorf("compiled metadata invalid: multiple memories require multi-memory feature")
	}
	for name, index := range c.memoryExportMap() {
		if index < 0 || index >= c.memoryCount() {
			return fmt.Errorf("compiled metadata invalid: memory export %q index %d out of range", name, index)
		}
	}
	return nil
}

func (c *Compiled) declaredMemoryMaxBytes() (uint64, error) {
	const pageBytes = uint64(65536)
	var total uint64
	for i := 0; i < c.memoryCount(); i++ {
		def := c.memoryDef(i)
		pages := def.Max
		if !def.HasMax {
			// The exact Wasm type remains unbounded in metadata. Policy and managed-
			// instance accounting charge the finite implementation reservation used
			// by instantiation, matching the existing memory32 resource model.
			pages = 65535
		}
		if pages > ^uint64(0)/pageBytes {
			return 0, fmt.Errorf("memory %d maximum %d pages overflows bytes", i, pages)
		}
		bytes := pages * pageBytes
		if total > ^uint64(0)-bytes {
			return 0, fmt.Errorf("memory maximum total overflows uint64")
		}
		total += bytes
	}
	return total, nil
}

func (c *Compiled) tableCount() int {
	if !c.HasTable {
		return 0
	}
	return 1 + len(c.extraTables)
}

func (c *Compiled) tableImportCount() int {
	if c == nil || c.tableImport == "" {
		return 0
	}
	count := 1
	for i := range c.extraTables {
		if c.extraTables[i].ImportKey == "" {
			break
		}
		count++
	}
	return count
}

func (c *Compiled) tableImportAt(index int) (tableImportDef, bool) {
	if c == nil || index < 0 {
		return tableImportDef{}, false
	}
	if index == 0 && c.tableImport != "" {
		return tableImportDef{Key: c.tableImport, Min: c.tableImportMin, Max: c.tableImportMax, Type: c.tableElementType(0), ValueTypeIndex: c.TableValueTypeIndex, HasValueType: c.TableHasValueType, HasMax: c.tableImportHasMax, Addr64: c.TableAddr64}, true
	}
	if index > 0 && index-1 < len(c.extraTables) {
		table := c.extraTables[index-1]
		if table.ImportKey != "" {
			return tableImportDef{Key: table.ImportKey, Min: table.Size, Max: int(table.Max), Type: c.tableElementType(index), ValueTypeIndex: table.ValueTypeIndex, HasValueType: table.HasValueType, HasMax: table.ImportHasMax, Addr64: table.Addr64}, true
		}
	}
	return tableImportDef{}, false
}

func (c *Compiled) tableDef(index int) tableDef {
	if index == 0 {
		return tableDef{Size: c.TableSize, Max: c.TableMax, Type: c.TableType, ValueTypeIndex: c.TableValueTypeIndex, HasValueType: c.TableHasValueType, HasInitFunc: c.HasTableInitFunc, HasMax: c.TableHasMax, Addr64: c.TableAddr64, InitFunc: c.TableInitFunc}
	}
	return c.extraTables[index-1]
}

func (c *Compiled) inertUnobservableTableDeclaration(index int) bool {
	return c != nil && index == 0 && c.tableCount() == 1 && c.tableImport == "" && len(c.Funcs) == 0 && c.NumImports == 0 && len(c.Elems) == 0 && len(c.passiveElems) == 0 && len(c.tableExports) == 0
}

func (c *Compiled) stagedInertOversizedTable64(index int) bool {
	if !c.inertUnobservableTableDeclaration(index) {
		return false
	}
	def := c.tableDef(0)
	return def.Addr64 && def.HasMax && def.Size >= 0 && uint64(def.Size) <= frontend.StagedTable64Max() && def.Max > frontend.StagedTable64Max()
}

func (c *Compiled) tableRuntimeCapacity(index int) int {
	def := c.tableDef(index)
	if def.Max == 0 {
		return def.Size
	}
	entryBytes := c.tableEntryBytes(index)
	if def.HasMax && c.inertUnobservableTableDeclaration(index) && def.Max > uint64((wruntime.InstantiateArenaSize-8)/entryBytes) {
		return def.Size
	}
	return int(def.Max)
}

func (c *Compiled) tableMinimum(index int) int {
	if def, ok := c.tableImportAt(index); ok {
		return def.Min
	}
	return c.tableDef(index).Size
}

func (c *Compiled) validateArenaFootprint() error {
	maxParams, maxResults, err := c.maxCallSlots()
	if err != nil {
		return fmt.Errorf("compiled metadata invalid: %w", err)
	}
	funcRefCount := 0
	if c.needsFuncRefDescs() {
		funcRefCount = len(c.FuncTypeID) + 1
	}
	tagCount := 0
	if c.memoryDir != nil {
		tagCount = len(c.memoryDir.ehTags)
	}
	tableSize, tableCapacity := c.TableSize, c.tableRuntimeCapacity(0)
	var tableCaps []int
	var tableEntryBytes []int
	if c.HasTable {
		tableEntryBytes = make([]int, c.tableCount())
		for i := range tableEntryBytes {
			tableEntryBytes[i] = c.tableEntryBytes(i)
		}
	}
	if len(c.extraTables) != 0 {
		tableSize, tableCapacity = 0, 0
		tableCaps = make([]int, c.tableCount())
		for i := range tableCaps {
			tableCaps[i] = c.tableRuntimeCapacity(i)
		}
	}
	passiveElemBytes := 0
	for i, elem := range c.passiveElems {
		stride := wruntime.TableEntryBytes
		if normalizedElemRefType(elem.RefType) == ValExternRef {
			stride = 8
		}
		if len(elem.Values) > (maxInt()-passiveElemBytes)/stride {
			return fmt.Errorf("compiled metadata invalid: passive element %d payload overflows arena allocation", i)
		}
		passiveElemBytes += len(elem.Values) * stride
	}
	hostCallBytes := 0
	if c.needsPublicFuncrefHostReentry() {
		hostCallBytes = wruntime.HostCtrlFrameBytes
	}
	need, err := wruntime.InstantiateArenaNeed(wruntime.InstantiateFootprint{
		FuncImportCount:    len(c.Imports),
		HostCallBytes:      hostCallBytes,
		FuncRefCount:       funcRefCount,
		TagCount:           tagCount,
		GlobalCount:        len(c.Globals),
		MemoryCount:        c.memoryCount(),
		HasTable:           c.HasTable,
		TableSize:          tableSize,
		TableCapacity:      tableCapacity,
		TableCapacities:    tableCaps,
		TableEntryBytes:    tableEntryBytes,
		ImportedTableCount: c.tableImportCount(),
		ElemCount:          len(c.Elems),
		PassiveElemCount:   len(c.passiveElems),
		PassiveElemBytes:   passiveElemBytes,
		PassiveDataCount:   len(c.PassiveData),
		MaxParamSlots:      maxParams,
		MaxResultSlots:     maxResults,
	})
	if err != nil {
		return fmt.Errorf("compiled metadata invalid: %w", err)
	}
	if need > maxInt()-wruntime.InstanceContextBytes {
		return fmt.Errorf("compiled metadata invalid: instantiate arena need overflows instance context")
	}
	need += wruntime.InstanceContextBytes
	if need > wruntime.InstantiateArenaSize {
		return fmt.Errorf("compiled metadata invalid: instantiate arena need %d > limit %d", need, wruntime.InstantiateArenaSize)
	}
	c.maxParamSlots = maxParams
	c.maxResultSlots = maxResults
	c.instantiateArenaNeed = need
	return nil
}

func (c *Compiled) maxCallSlots() (params, results int, err error) {
	for i, fn := range c.Funcs {
		paramSlots, err := valTypesSlots(fn.Params)
		if err != nil || paramSlots > maxInt()/8 {
			return 0, 0, fmt.Errorf("function %d parameter slots overflow call buffer", i)
		}
		resultSlots, err := valTypesSlots(fn.Results)
		if err != nil || resultSlots > maxInt()/8 {
			return 0, 0, fmt.Errorf("function %d result slots overflow call buffer", i)
		}
		if paramSlots > params {
			params = paramSlots
		}
		if resultSlots > results {
			results = resultSlots
		}
	}
	return params, results, nil
}

func (c *Compiled) validateDeferredOffsetGlobal(kind string, seg, idx int) error {
	if idx < 0 || idx >= len(c.Globals) {
		return fmt.Errorf("compiled metadata invalid: %s %d offset global %d out of range", kind, seg, idx)
	}
	g := c.Globals[idx]
	if idx >= len(c.GlobalImports) || g.Mutable || g.Type != ValI32 {
		return fmt.Errorf("compiled metadata invalid: %s %d offset global %d must be imported immutable i32", kind, seg, idx)
	}
	return nil
}

const wagoMagic = "WAGO"

// Version 27 adds bounded exception-tag declarations, imports, and exports to
// the version-26 indexed-memory, binding-independent import dispatch,
// structural type, table, element, extended constant expression, feature, and
// exact table32/table64 address-form metadata. Version 26 blobs are rejected
// because they have no tag directory or export map. The codec never serializes
// live owners, mappings, tokens, targets, active handlers, thunk addresses, or
// store identity.
const wagoVersion = 27

// MarshalBinary serializes the precompiled module to a ".wago" blob.
//
// Signals-based (guard-page) modules cannot be serialized: their code has the
// inline bounds checks elided and is only safe against a guard-page memory,
// which a loaded blob has no way to record. Recompile from wasm with the desired
// config at load time instead.
func (c *Compiled) MarshalBinary() ([]byte, error) {
	if c.boundsMode == BoundsChecksSignalsBased {
		return nil, errors.New("wago: signals-based compiled modules cannot be serialized; recompile from wasm at load time")
	}
	if len(c.Entry) == 0 && len(c.Funcs) > 0 {
		return nil, errors.New("wago: compiled module has functions but no native entries")
	}
	if c.NumImports > 0 && !c.dynamicImports {
		return nil, errors.New("wago: imported-function code lacks dynamic dispatch metadata")
	}
	if err := c.validateCodecMetadata(); err != nil {
		return nil, err
	}
	return marshalCompiled(c)
}

// UnmarshalBinary loads a ".wago" blob produced by MarshalBinary.
func (c *Compiled) UnmarshalBinary(data []byte) error {
	if !IsCompiled(data) {
		return fmt.Errorf("not a wago module")
	}
	if data[4] != wagoVersion {
		return fmt.Errorf("wago module version %d unsupported (want %d)", data[4], wagoVersion)
	}
	var decoded Compiled
	if err := unmarshalCompiled(&decoded, data[5:]); err != nil {
		return err
	}
	if len(decoded.tableExports) == 0 {
		decoded.tableExports = nil
	}
	decoded.hasTableExportMetadata = true
	if decoded.memoryDir == nil {
		decoded.memoryDir = &compiledMemoryDirectory{}
	}
	if len(decoded.memoryDir.exports) == 0 {
		decoded.memoryDir.exports = nil
	}
	decoded.memoryDir.exactExports = true
	if inferred := compiledStructuralRequiredFeatures(&decoded); inferred&^decoded.requiredFeatures != 0 {
		return fmt.Errorf("compiled metadata invalid: structural metadata requires unrecorded features %s", inferred&^decoded.requiredFeatures)
	}
	if err := decoded.validate(); err != nil {
		return err
	}
	if decoded.requiredFeatures.IsEnabled(CoreFeatureSIMD) && !hostSupportsSIMD() {
		return fmt.Errorf("wago: compiled module requires SIMD CPU features unavailable on this host")
	}
	*c = decoded
	installCompiledFinalizer(c)
	return nil
}

// IsCompiled reports whether b is a precompiled wago module (vs raw wasm).
func IsCompiled(b []byte) bool { return len(b) >= 5 && string(b[:4]) == wagoMagic }

// Load returns a *Compiled from either a precompiled ".wago" blob or raw wasm
// (which it compiles).
func Load(b []byte) (*Compiled, error) {
	if IsCompiled(b) {
		c := &Compiled{}
		return c, c.UnmarshalBinary(b)
	}
	return Compile(nil, b)
}

// Invoke marshals slot-based arguments/results around one native WasmWrapper
// call. The returned slice is backed by an instance-owned buffer and stays valid
// only until the next call on this Instance; copy it if you need to retain it.
// Invoke calls an exported function. Arguments and results are raw uint64 slots
// interpreted per the function's signature (encode/decode scalar slots with
// I32/I64/F32/F64 and AsI32/AsI64/AsF32/AsF64). A v128 occupies two adjacent
// little-endian uint64 slots in the argument and result slices. Public funcref
// slots use opaque store-owned tokens: zero is null, and nonzero tokens are valid
// only in the Runtime store (or standalone private store) that issued them.
func (in *Instance) Invoke(export string, args ...uint64) ([]uint64, error) {
	return in.invoke(export, args, nil)
}

// InvokeContext is like Invoke but honors context cancellation. If ctx is
// cancelled or its deadline expires while the guest is executing, the call is
// interrupted at the next native safepoint and returns ctx.Err() (e.g.
// context.DeadlineExceeded) instead of blocking on a runaway guest.
//
// Native cancellation is available on amd64/arm64; on other architectures ctx
// is only checked before the call begins. A nil or already-cancelled ctx is
// handled up front; a Background context (Done() == nil) keeps Invoke's
// zero-goroutine fast path.
//
// This is the raw-uint64 sibling of Call, for callers that invoke by slot
// values rather than typed Values (e.g. AssemblyScript rule dispatch).
func (in *Instance) InvokeContext(ctx context.Context, export string, args ...uint64) ([]uint64, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}

	var cancel <-chan struct{}
	if nativeCancellationSupported() && ctx != nil {
		cancel = ctx.Done()
	}

	out, err := in.invoke(export, args, cancel)

	return out, contextInterruptError(ctx, err)
}

func (in *Instance) invoke(export string, args []uint64, cancel <-chan struct{}) ([]uint64, error) {
	ic := in.findInvokeCache(export)
	if ic == nil {
		var err error
		ic, err = in.fillInvokeCache(export)
		if err != nil {
			return nil, err
		}
	}
	li := ic.li
	if li < 0 {
		importIdx := -li - 1
		if importIdx < 0 || importIdx >= len(in.c.Imports) {
			return nil, fmt.Errorf("export %q imported function index %d has no binding", export, importIdx)
		}
		ex, ok := in.imports[in.c.Imports[importIdx]].(*InstanceExport)
		if !ok || ex == nil || ex.inst == nil {
			return nil, fmt.Errorf("export %q is an imported function without an InstanceExport owner", export)
		}
		return ex.inst.invokeLocalContext(ex.localIdx, args, cancel)
	}
	if len(args) != ic.paramSlots {
		return nil, fmt.Errorf("%s expects %d arg slot(s), got %d", export, ic.paramSlots, len(args))
	}
	if len(args) > len(in.serArgs)/8 {
		return nil, fmt.Errorf("%s requires %d arg slot(s), instance buffer has %d", export, len(args), len(in.serArgs)/8)
	}
	if ic.resultSlots > len(in.results)/8 {
		return nil, fmt.Errorf("%s requires %d result slot(s), instance buffer has %d", export, ic.resultSlots, len(in.results)/8)
	}
	if ic.hasFuncRefParams {
		params, _, err := exactFuncSignatureView(in.c.Funcs[li], in.c.Types)
		if err != nil {
			return nil, fmt.Errorf("%s exact parameters: %w", export, err)
		}
		if err := in.marshalPublicReferenceArgs(export, args, in.c.Funcs[li].Params, params); err != nil {
			return nil, err
		}
	} else {
		marshalPublicScalarArgs(in.serArgs, args, in.c.Funcs[li].Params)
	}
	if len(in.hostLog) > 0 {
		binary.LittleEndian.PutUint32(in.hostLog, 0) // reset host-call log
	}
	entry := in.base + uintptr(in.c.Entry[li])
	if in.importsFuncrefStorage() || in.table != nil {
		defer in.reconcileFuncrefRoots()
	}
	stopCancel := in.startCancellationWatch(cancel)
	if in.syncMode {
		if err := in.callNativeSync(entry); err != nil {
			stopCancel()
			return nil, err
		}
	} else {
		if err := in.callNativeAsync(entry, false); err != nil {
			stopCancel()
			return nil, err
		}
		if err := in.replayHostLog(); err != nil {
			stopCancel()
			return nil, err
		}
	}
	stopCancel()
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	out := in.resultVals[:ic.resultSlots]
	for i, wide := range ic.resultWide {
		off := i * 8
		if off+8 > len(in.results) {
			return nil, fmt.Errorf("%s result slot %d exceeds instance result buffer", export, i)
		}
		if wide { // i64/f64 or one half of a v128
			out[i] = binary.LittleEndian.Uint64(in.results[off:])
		} else { // i32 / f32 (4-byte)
			out[i] = uint64(binary.LittleEndian.Uint32(in.results[off:]))
		}
	}
	if ic.hasFuncRefResults {
		_, results, err := exactFuncSignatureView(in.c.Funcs[li], in.c.Types)
		if err != nil {
			return nil, fmt.Errorf("%s exact results: %w", export, err)
		}
		if err := in.translatePublicReferenceResults(export, out, in.c.Funcs[li].Results, results); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// invokeLocal calls this instance's local function `li` directly (bypassing the
// export-name cache). Used to call through a re-exported import into the instance
// that satisfies it. It shares the instance's call buffers, so the returned slice
// is valid only until the next call on this instance.
func (in *Instance) invokeLocal(li int, args []uint64) ([]uint64, error) {
	return in.invokeLocalContext(li, args, nil)
}

func (in *Instance) invokeLocalContext(li int, args []uint64, cancel <-chan struct{}) ([]uint64, error) {
	if li < 0 || li >= len(in.c.Funcs) || li >= len(in.c.Entry) {
		return nil, fmt.Errorf("invalid function index %d", li)
	}
	sig := in.c.Funcs[li]
	paramSlots, err := valTypesSlots(sig.Params)
	if err != nil {
		return nil, fmt.Errorf("function parameter slots: %w", err)
	}
	resultSlots, err := valTypesSlots(sig.Results)
	if err != nil {
		return nil, fmt.Errorf("function result slots: %w", err)
	}
	if len(args) != paramSlots {
		return nil, fmt.Errorf("function expects %d arg slot(s), got %d", paramSlots, len(args))
	}
	if len(args) > len(in.serArgs)/8 {
		return nil, fmt.Errorf("requires %d arg slot(s), instance buffer has %d", len(args), len(in.serArgs)/8)
	}
	if resultSlots > len(in.results)/8 {
		return nil, fmt.Errorf("requires %d result slot(s), instance buffer has %d", resultSlots, len(in.results)/8)
	}
	if hasReferenceValType(sig.Params) {
		params, _, err := exactFuncSignatureView(sig, in.c.Types)
		if err != nil {
			return nil, fmt.Errorf("function exact parameters: %w", err)
		}
		if err := in.marshalPublicReferenceArgs("function", args, sig.Params, params); err != nil {
			return nil, err
		}
	} else {
		marshalPublicScalarArgs(in.serArgs, args, sig.Params)
	}
	if len(in.hostLog) > 0 {
		binary.LittleEndian.PutUint32(in.hostLog, 0)
	}
	entry := in.base + uintptr(in.c.Entry[li])
	if in.importsFuncrefStorage() || in.table != nil {
		defer in.reconcileFuncrefRoots()
	}
	stopCancel := in.startCancellationWatch(cancel)
	if in.syncMode {
		if err := in.callNativeSync(entry); err != nil {
			stopCancel()
			return nil, err
		}
	} else {
		if err := in.callNativeAsync(entry, false); err != nil {
			stopCancel()
			return nil, err
		}
		if err := in.replayHostLog(); err != nil {
			stopCancel()
			return nil, err
		}
	}
	stopCancel()
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	out := in.resultVals[:resultSlots]
	resSlot := 0
	for _, rt := range sig.Results {
		if rt == ValV128 {
			for half := 0; half < 2; half++ {
				off := resSlot * 8
				if off+8 > len(in.results) {
					return nil, fmt.Errorf("result slot %d exceeds instance result buffer", resSlot)
				}
				out[resSlot] = binary.LittleEndian.Uint64(in.results[off:])
				resSlot++
			}
			continue
		}
		off := resSlot * 8
		if off+8 > len(in.results) {
			return nil, fmt.Errorf("result slot %d exceeds instance result buffer", resSlot)
		}
		if isWideValType(rt) {
			out[resSlot] = binary.LittleEndian.Uint64(in.results[off:])
		} else {
			out[resSlot] = uint64(binary.LittleEndian.Uint32(in.results[off:]))
		}
		resSlot++
	}
	if hasReferenceValType(sig.Results) {
		_, results, err := exactFuncSignatureView(sig, in.c.Types)
		if err != nil {
			return nil, fmt.Errorf("function exact results: %w", err)
		}
		if err := in.translatePublicReferenceResults("function", out, sig.Results, results); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// nativeCancellationSupported reports whether the railshot backend for this
// GOARCH emits cooperative cancellation polls (function-entry and loop-header
// trap-cell checks). Both the amd64 and arm64 backends do, so a context-aware
// Call can arm the watcher on either.
func nativeCancellationSupported() bool {
	return goruntime.GOARCH == "amd64" || goruntime.GOARCH == "arm64"
}

// startCancellationWatch arms the native safepoints for a high-level
// context-aware Call. Background contexts keep the zero-goroutine fast path.
func (in *Instance) startCancellationWatch(cancel <-chan struct{}) func() {
	if !nativeCancellationSupported() || cancel == nil || len(in.trap) < 4 {
		return func() {}
	}
	done := make(chan struct{})
	stopped := make(chan struct{})
	trap := (*uint32)(unsafe.Pointer(&in.trap[0]))
	go func() {
		defer close(stopped)
		select {
		case <-done:
			return
		case <-cancel:
		}
		// Keep publishing until native code observes the request. This closes the
		// small arm-before-entry race where Engine.Call establishes a zero trap cell.
		for {
			atomic.StoreUint32(trap, uint32(wruntime.TrapInterrupted))
			select {
			case <-done:
				return
			default:
				goruntime.Gosched()
			}
		}
	}()
	return func() {
		close(done)
		<-stopped
		atomic.CompareAndSwapUint32(trap, uint32(wruntime.TrapInterrupted), 0)
	}
}

// replayHostLog runs the void host imports the last native call logged. Each
// logged entry carries the single i32 argument the codegen captured; it is passed
// to the stack-form HostFunc as params[0], with no results.
func (in *Instance) replayHostLog() (err error) {
	if len(in.hostLog) == 0 {
		return nil
	}
	// A replayed host call may panic(HostExit{...}) to end
	// execution — recover it as an *ExitError, exactly like the synchronous path.
	defer func() {
		if r := recover(); r != nil {
			if ex, ok := r.(HostExit); ok {
				err = &ExitError{Code: ex.Code}
				return
			}
			if missing, ok := r.(missingHostFunc); ok {
				err = fmt.Errorf("missing host function for import index %d", missing.importIdx)
				return
			}
			panic(r)
		}
	}()
	n := binary.LittleEndian.Uint32(in.hostLog)
	var params [1]uint64
	for i := uint32(0); i < n; i++ {
		off := 8 + i*8
		imp := binary.LittleEndian.Uint32(in.hostLog[off:])
		arg := int32(binary.LittleEndian.Uint32(in.hostLog[off+4:]))
		if int(imp) < len(in.c.Imports) {
			if fn := in.hosts[in.c.Imports[imp]]; fn != nil {
				params[0] = uint64(uint32(arg))
				if in.rt == nil || !in.rt.scopedHostCalls() {
					fn(staticHostModule{in: in}, params[:], nil)
					continue
				}
				caller := in.beginHostCallScope()
				func() {
					defer caller.scope.end(caller.generation)
					fn(caller, params[:], nil)
				}()
			}
		}
	}
	return nil
}

// fillInvokeCache resolves export and memoizes it so subsequent Invokes skip the
// exports map probe. Local functions store their local index; an imported
// InstanceExport stores -1-importIndex and forwards through its original owner.
func (in *Instance) fillInvokeCache(export string) (*invokeCache, error) {
	gfi, ok := in.c.Exports[export]
	if !ok {
		return nil, fmt.Errorf("no exported function %q", export)
	}
	if gfi < 0 {
		return nil, fmt.Errorf("export %q function index %d out of range", export, gfi)
	}
	if gfi < in.c.NumImports {
		if gfi >= len(in.c.Imports) {
			return nil, fmt.Errorf("export %q imported function index %d has no binding", export, gfi)
		}
		ex, ok := in.imports[in.c.Imports[gfi]].(*InstanceExport)
		if !ok || ex == nil || ex.inst == nil {
			return nil, fmt.Errorf("export %q is an imported function without an InstanceExport owner", export)
		}
		slot := &in.ic[int(in.icNext)%len(in.ic)]
		in.icNext++
		*slot = invokeCache{export: export, valid: true, li: -1 - gfi, resultWide: slot.resultWide[:0]}
		return slot, nil
	}
	li := gfi - in.c.NumImports
	if li < 0 || li >= len(in.c.Funcs) {
		return nil, fmt.Errorf("export %q function index %d out of range", export, gfi)
	}
	sig := in.c.Funcs[li]
	paramSlots, err := valTypesSlots(sig.Params)
	if err != nil {
		return nil, fmt.Errorf("%s parameter slots: %w", export, err)
	}
	resultSlots, err := valTypesSlots(sig.Results)
	if err != nil {
		return nil, fmt.Errorf("%s result slots: %w", export, err)
	}
	slot := &in.ic[int(in.icNext)%len(in.ic)]
	in.icNext++
	rw := slot.resultWide[:0]
	if cap(rw) < resultSlots {
		rw = make([]bool, 0, resultSlots)
	}
	for _, r := range sig.Results {
		if r == ValV128 {
			rw = append(rw, true, true)
		} else {
			rw = append(rw, isWideValType(r))
		}
	}
	*slot = invokeCache{
		export:            export,
		valid:             true,
		li:                li,
		paramSlots:        paramSlots,
		resultSlots:       resultSlots,
		hasFuncRefParams:  hasReferenceValType(sig.Params),
		hasFuncRefResults: hasReferenceValType(sig.Results),
		resultWide:        rw,
	}
	return slot, nil
}

func marshalPublicScalarArgs(dst []byte, values []uint64, types []ValType) {
	slot := 0
	for _, typ := range types {
		if typ == ValV128 {
			binary.LittleEndian.PutUint64(dst[slot*8:], values[slot])
			binary.LittleEndian.PutUint64(dst[(slot+1)*8:], values[slot+1])
			slot += 2
			continue
		}
		bits := values[slot]
		if !isWideValType(typ) {
			bits = uint64(uint32(bits))
		}
		binary.LittleEndian.PutUint64(dst[slot*8:], bits)
		slot++
	}
}

func hasValType(types []ValType, want ValType) bool {
	for _, typ := range types {
		if typ == want {
			return true
		}
	}
	return false
}

func hasReferenceValType(types []ValType) bool {
	for _, typ := range types {
		if isReferenceValType(typ) {
			return true
		}
	}
	return false
}

func exactReferenceType(types []ValueTypeDescriptor, index int, legacy ValType) (ValueTypeDescriptor, bool) {
	if index >= 0 && index < len(types) {
		if types[index].Kind != ValueTypeReference {
			return ValueTypeDescriptor{}, false
		}
		return types[index], true
	}
	return valueTypeDescriptorFromValType(legacy)
}

func (in *Instance) marshalPublicReferenceArgs(subject string, values []uint64, types []ValType, exact []ValueTypeDescriptor) error {
	slot := 0
	for i, typ := range types {
		if typ == ValV128 {
			binary.LittleEndian.PutUint64(in.serArgs[slot*8:], values[slot])
			binary.LittleEndian.PutUint64(in.serArgs[(slot+1)*8:], values[slot+1])
			slot += 2
			continue
		}
		bits := values[slot]
		switch typ {
		case ValFuncRef:
			required, ok := exactReferenceType(exact, i, typ)
			if !ok {
				return fmt.Errorf("%s: missing exact funcref type for argument %d", subject, i)
			}
			if bits == 0 {
				if !required.Ref.Nullable {
					return fmt.Errorf("%s: null funcref for non-null argument %d", subject, i)
				}
			} else {
				if in.refStore == nil {
					return fmt.Errorf("%s: invalid funcref token for argument %d", subject, i)
				}
				descriptor, ok := in.refStore.resolve(bits)
				if !ok {
					return fmt.Errorf("%s: invalid funcref token for argument %d", subject, i)
				}
				actual, actualTypes, valid := in.refStore.tokenFuncrefExactType(bits)
				if !valid {
					return fmt.Errorf("%s: invalid funcref token for argument %d", subject, i)
				}
				if !valueTypeSubtype(actual, actualTypes, required, in.c.Types) {
					return fmt.Errorf("%s: funcref argument %d does not match its exact structural type", subject, i)
				}
				bits = descriptor
			}
		case ValExternRef:
			if bits != 0 && (in.refStore == nil || !in.validExternrefToken(bits)) {
				return fmt.Errorf("%s: invalid externref token for argument %d", subject, i)
			}
		}
		if !isWideValType(typ) {
			bits = uint64(uint32(bits))
		}
		binary.LittleEndian.PutUint64(in.serArgs[slot*8:], bits)
		slot++
	}
	return nil
}

func (in *Instance) translatePublicReferenceResults(subject string, values []uint64, types []ValType, exact []ValueTypeDescriptor) error {
	slot := 0
	for i, typ := range types {
		if typ == ValFuncRef {
			required, ok := exactReferenceType(exact, i, typ)
			if !ok {
				clear(values)
				return fmt.Errorf("%s: missing exact funcref type for result %d", subject, i)
			}
			if values[slot] == 0 {
				if !required.Ref.Nullable {
					clear(values)
					return fmt.Errorf("%s: null funcref for non-null result %d", subject, i)
				}
			} else {
				store, err := in.funcrefStoreForEgress()
				if err != nil {
					clear(values)
					return fmt.Errorf("%s: invalid funcref result %d: %w", subject, i, err)
				}
				actual, actualTypes, valid := store.descriptorFuncrefExactType(in, values[slot])
				if !valid {
					clear(values)
					return fmt.Errorf("%s: invalid funcref result %d", subject, i)
				}
				if !valueTypeSubtype(actual, actualTypes, required, in.c.Types) {
					clear(values)
					return fmt.Errorf("%s: funcref result %d does not match its exact structural type", subject, i)
				}
				token, err := store.issue(in, values[slot])
				if err != nil {
					clear(values)
					return fmt.Errorf("%s: invalid funcref result %d: %w", subject, i, err)
				}
				values[slot] = token
			}
		}
		if typ == ValExternRef && values[slot] != 0 && !in.validExternrefToken(values[slot]) {
			clear(values)
			return fmt.Errorf("%s: invalid externref result %d", subject, i)
		}
		if typ == ValV128 {
			slot += 2
		} else {
			slot++
		}
	}
	return nil
}

func (in *Instance) findInvokeCache(export string) *invokeCache {
	for i := range in.ic {
		if in.ic[i].valid && sameExportName(in.ic[i].export, export) {
			return &in.ic[i]
		}
	}
	return nil
}

func sameExportName(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return len(a) == 0 || unsafe.StringData(a) == unsafe.StringData(b) || a == b
}

// CodeBase returns the base address of the instance's mapped native code and the
// per-local-function entry offsets, for external profilers (e.g. writing a
// /tmp/perf-<pid>.map JIT symbol map). Debug/introspection use only.
func (in *Instance) CodeBase() (base uintptr, entries []int) {
	// Copy the entry table so callers cannot mutate the compiled module's
	// (potentially shared) state through the returned slice.
	return in.base, append([]int(nil), in.c.Entry...)
}

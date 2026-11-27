// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	goruntime "runtime"
	"sort"
	"strings"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/wago-org/wago/internal/buildfeatures"
	"github.com/wago-org/wago/internal/functionworkers"
	"github.com/wago-org/wago/internal/runtimebridge"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	wruntime "github.com/wago-org/wago/src/core/runtime"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

type GCConfig = gc.Config
type GCProfile = gc.Profile
type GCAllocatorKind = gc.AllocatorKind
type GCRuntimeKind = gc.RuntimeKind
type GCNativeCodeTelemetry = gc.NativeCodeTelemetry

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

func moduleDynamicFuncrefEscape(m *wasm.Module) bool {
	typeMayCarryFuncref := func(typeIdx uint32) bool {
		ft, ok := m.TypeFunc(typeIdx)
		if !ok {
			return false
		}
		for _, param := range ft.Params {
			if param.Kind() == wasm.ValRef {
				return true
			}
		}
		for _, result := range ft.Results {
			if result.Kind() == wasm.ValRef {
				return true
			}
		}
		return false
	}
	isDynamicCall := func(kind wasm.InstrKind) bool {
		switch kind {
		case wasm.InstrCallIndirect, wasm.InstrReturnCallIndirect, wasm.InstrCallRef, wasm.InstrReturnCallRef:
			return true
		default:
			return false
		}
	}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	for i := range m.Code {
		if body := m.Code[i].BodyBytes; len(body) != 0 {
			r := wasm.NewReader(body)
			var imm wasm.InstructionImmediate
			for r.HasNext() {
				op, err := r.Byte()
				if err != nil || classifier.ClassifyInto(r, op, &imm) != nil {
					break
				}
				if isDynamicCall(imm.Kind) && typeMayCarryFuncref(uint32(imm.Index)) {
					return true
				}
			}
			continue
		}
		for _, in := range m.Code[i].Body.Instrs {
			if isDynamicCall(in.Kind) && typeMayCarryFuncref(in.Index) {
				return true
			}
		}
	}
	return false
}

func moduleDynamicFuncrefEscapeWithValidation(m *wasm.Module, analysis *wasm.ValidatedModuleAnalysis) bool {
	if analysis.ValidFor(m) {
		return analysis.Flags()&wasm.ValidatedFuncDynamicReferenceCall != 0
	}
	return moduleDynamicFuncrefEscape(m)
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
	return compileWithConfigAndInstructions(cfg, wasmBytes, emptyPluginInstructions())
}

func compileWithConfigAndInstructions(cfg *RuntimeConfig, wasmBytes []byte, instructions pluginInstructions) (*Compiled, error) {
	if cfg == nil {
		cfg = NewRuntimeConfig()
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	compiled, err := compileWithFrontendFeaturesAndInstructions(cfg, wasmBytes, cfg.frontendFeatures(), instructions)
	// TinyGo's conservative collector must not reclaim compiler inputs whose
	// derived slices and pointers remain in use below this public boundary.
	// Keep the complete owners live until decoding and code generation finish.
	goruntime.KeepAlive(wasmBytes)
	goruntime.KeepAlive(cfg)
	goruntime.KeepAlive(instructions)
	return compiled, err
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
				return false, fmt.Errorf("instruction %s is outside the exact table operation slice", in.Kind.String())
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

func stagedTableBodyAllowed(body wasm.Expr, allowed func(wasm.InstrKind) bool, classifier *wasm.ModuleInstructionClassifier) (bool, error) {
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
		var imm wasm.InstructionImmediate
		if err := classifier.ClassifyInto(r, op, &imm); err != nil {
			return false, err
		}
		if _, tableOperation := stagedTwoLocalTableOperation(imm.Kind); tableOperation {
			if !allowed(imm.Kind) {
				return false, fmt.Errorf("instruction %s is outside the exact table operation slice", imm.Kind.String())
			}
			found = true
		}
	}
	return found, nil
}

func stagedTwoLocalExternrefReadWriteShape(m *wasm.Module) bool {
	return m.ImportedTableCount() == 0 && len(m.Tables) == 2 &&
		m.Tables[0].Type.Limits.Addr64 && m.Tables[1].Type.Limits.Addr64 &&
		!m.Tables[0].Type.Limits.HasMax && !m.Tables[1].Type.Limits.HasMax &&
		wasm.EqualValType(wasm.RefVal(m.Tables[0].Type.Ref), wasm.ExternRef) &&
		wasm.EqualValType(wasm.RefVal(m.Tables[1].Type.Ref), wasm.FuncRef)
}

func stagedTwoLocalExternrefFillShape(m *wasm.Module) bool {
	return m.ImportedTableCount() == 0 && len(m.Tables) == 2 &&
		!m.Tables[0].Type.Limits.Addr64 && m.Tables[1].Type.Limits.Addr64 &&
		!m.Tables[0].Type.Limits.HasMax && !m.Tables[1].Type.Limits.HasMax &&
		wasm.EqualValType(wasm.RefVal(m.Tables[0].Type.Ref), wasm.ExternRef) &&
		wasm.EqualValType(wasm.RefVal(m.Tables[1].Type.Ref), wasm.ExternRef)
}

func stagedExactTableOperationShape(m *wasm.Module, label string, allowed func(wasm.InstrKind) bool) error {
	found := false
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	for i := range m.Code {
		bodyFound, err := stagedTableBodyAllowed(wasm.Expr{Instrs: m.Code[i].Body.Instrs, BodyBytes: m.Code[i].BodyBytes}, allowed, &classifier)
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
		m.Tables[0].Type.Limits.Addr64 && !m.Tables[0].Type.Limits.HasMax &&
		wasm.EqualValType(wasm.RefVal(m.Tables[0].Type.Ref), wasm.ExternRef)
}

func stagedTwoLocalNoMaxTable64DeclarationShape(m *wasm.Module) bool {
	if m.ImportedTableCount() != 0 || len(m.Tables) != 2 || len(m.Code) != 0 || len(m.Elements) != 0 || len(m.Exports) != 0 {
		return false
	}
	for i := range m.Tables {
		t := &m.Tables[i]
		if t.Init != nil || !t.Type.Limits.Addr64 || t.Type.Limits.Min != 0 || t.Type.Limits.HasMax ||
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
	for _, tt := range []wasm.TableType{imported.Type.TableType(), m.Tables[0].Type} {
		if !tt.Limits.Addr64 || tt.Limits.Min != 0 || tt.Limits.HasMax ||
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
	if t.Init != nil || !t.Type.Limits.Addr64 || !t.Type.Limits.HasMax ||
		t.Type.Limits.Min > frontend.StagedTable64Max() || t.Type.Limits.Max <= frontend.StagedTable64Max() ||
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
			return false, fmt.Errorf("instruction %s is outside the exact three-local table.init64 slice", in.Kind.String())
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

func stagedThreeLocalTableInit64Body(body wasm.Expr, classifier *wasm.ModuleInstructionClassifier) (foundInit, foundIndirect bool, err error) {
	if len(body.BodyBytes) == 0 {
		return stagedThreeLocalTableInit64Instrs(body.Instrs)
	}
	r := wasm.NewReader(body.BodyBytes)
	for r.HasNext() {
		op, readErr := r.Byte()
		if readErr != nil {
			return false, false, readErr
		}
		var imm wasm.InstructionImmediate
		if classifyErr := classifier.ClassifyInto(r, op, &imm); classifyErr != nil {
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
		return errors.New("the exact three-local table.init64 slice requires three local tables and no table imports")
	}
	if m.ImportedFuncCount() == 0 {
		return errors.New("the exact three-local table.init64 slice requires retained function imports")
	}
	for i := range m.Tables {
		t := &m.Tables[i]
		if !wasm.EqualValType(wasm.RefVal(t.Type.Ref), wasm.FuncRef) {
			return fmt.Errorf("table %d is not funcref in the exact three-local table.init64 slice", i)
		}
		if t.Init != nil {
			return fmt.Errorf("table %d initializer expression is outside the exact three-local table.init64 slice", i)
		}
		if !t.Type.Limits.HasMax {
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
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	for i := range m.Code {
		bodyInit, bodyIndirect, err := stagedThreeLocalTableInit64Body(wasm.Expr{Instrs: m.Code[i].Body.Instrs, BodyBytes: m.Code[i].BodyBytes}, &classifier)
		if err != nil {
			return fmt.Errorf("function %d: %w", i, err)
		}
		foundInit = foundInit || bodyInit
		foundIndirect = foundIndirect || bodyIndirect
	}
	if !foundInit || !foundIndirect {
		return errors.New("the exact three-local table.init64 slice requires table.init and call_indirect on table 2")
	}
	return nil
}

func stagedTwoLocalTableShape(m *wasm.Module) error {
	if m.ImportedTableCount() != 0 || len(m.Tables) != 2 {
		return errors.New("the exact two-local-table slice requires two local tables and no table imports")
	}
	for i := range m.Tables {
		if m.Tables[i].Init != nil {
			return fmt.Errorf("table %d initializer expression is outside the exact two-local-table slice", i)
		}
	}
	exactExternrefReadWrite := stagedTwoLocalExternrefReadWriteShape(m)
	exactExternrefFill := stagedTwoLocalExternrefFillShape(m)
	exactIndexedIndirect := goruntime.GOOS == "linux" && goruntime.GOARCH == "riscv64" && (stagedOwnedTwoTable32IndirectShape(m) || stagedOwnedTwoTable64IndirectCallShape(m))
	exactIndexedTail := goruntime.GOOS == "linux" && goruntime.GOARCH == "riscv64" && stagedOwnedTwoTable64IndirectTailShape(m)
	if !exactExternrefReadWrite && !exactExternrefFill {
		for i := range m.Tables {
			if !m.Tables[i].Type.Limits.HasMax {
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
		if exactIndexedIndirect && k == wasm.InstrCallIndirect {
			return true
		}
		if exactIndexedTail && k == wasm.InstrReturnCallIndirect {
			return true
		}
		ok, _ := stagedTwoLocalTableOperation(k)
		return ok
	}
	return stagedExactTableOperationShape(m, "exact two-local-table slice", allowed)
}

func stagedTagFuncType(m *wasm.Module, index uint32, dst *wasm.CompType) bool {
	for i := range m.Imports {
		im := &m.Imports[i]
		if im.Type.Kind != wasm.ExternTag {
			continue
		}
		if index == 0 {
			return m.ResolveTypeFunc(im.Type.TagType().Type.Index, dst)
		}
		index--
	}
	if int(index) >= len(m.Tags) {
		return false
	}
	return m.ResolveTypeFunc(m.Tags[index].Type.Index, dst)
}

func stagedLocalFuncrefExceptionPayload(m *wasm.Module) (funcIndex uint32, typeIndex uint32, ok bool, err error) {
	for tag := uint32(0); tag < uint32(m.TagCount()); tag++ {
		var ft wasm.CompType
		if !stagedTagFuncType(m, tag, &ft) {
			return 0, 0, false, fmt.Errorf("bounded exception handling tag %d signature is unavailable", tag)
		}
		for _, typ := range ft.Params {
			if wasm.EqualValType(typ, wasm.I32) || wasm.EqualValType(typ, wasm.I64) || wasm.EqualValType(typ, wasm.F32) || wasm.EqualValType(typ, wasm.F64) {
				continue
			}
			if ok {
				return 0, 0, false, errors.New("bounded exception handling admits only one reference tag payload")
			}
			rt := typ.Ref()
			heap := rt.Heap()
			if m.TagCount() != 1 || len(ft.Params) != 1 || typ.Kind() != wasm.ValRef || rt.Nullable() || rt.Exact() || heap.Kind() != wasm.HeapTypeIndex {
				return 0, 0, false, errors.New("bounded exception handling admits only one local non-null indexed-function tag payload")
			}
			var payloadFunc wasm.CompType
			if !m.ResolveTypeFunc(heap.Type().Index, &payloadFunc) || len(payloadFunc.Params) != 0 || len(payloadFunc.Results) != 0 {
				return 0, 0, false, errors.New("bounded exception handling indexed-function payload must have type () -> ()")
			}
			typeIndex, ok = heap.Type().Index, true
		}
	}
	if !ok {
		return 0, 0, false, nil
	}
	if m.ImportedFuncCount() != 0 || m.ImportedTagCount() != 0 || m.Start != nil {
		return 0, 0, false, errors.New("bounded exception handling reference payload requires local functions, a local tag, and no start")
	}
	if len(m.Elements) != 1 || m.Elements[0].Mode.Kind != wasm.ElemDeclarative || m.Elements[0].Kind.Kind != wasm.ElemFuncs || len(m.Elements[0].Kind.Funcs) != 1 {
		return 0, 0, false, errors.New("bounded exception handling reference payload requires one declarative local function element")
	}
	funcIndex = uint32(m.Elements[0].Kind.Funcs[0])
	if int(funcIndex) >= m.FuncCount() || int(funcIndex) < m.ImportedFuncCount() {
		return 0, 0, false, errors.New("bounded exception handling reference payload element must name one local function")
	}
	declaredType, found := m.FuncTypeIndex(funcIndex)
	if !found || declaredType.Index != typeIndex {
		return 0, 0, false, errors.New("bounded exception handling reference payload function must have the exact indexed type")
	}
	return funcIndex, typeIndex, true, nil
}

func stagedExceptionHandlingShape(m *wasm.Module, exceptionReferences, tailCalls bool) error {
	if railshotExceptionHandlingSource(m) {
		return nil
	}
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
		return errors.New("bounded exception handling requires functions without memory/global/data state and only the exact immutable local tail table")
	}
	for _, ex := range m.Exports {
		if ex.Index.Kind == wasm.ExternTable {
			return errors.New("bounded exception handling rejects exported tables")
		}
		if ex.Index.Kind == wasm.ExternFunc {
			ft, ok := m.FuncSignature(ex.Index.Index)
			if !ok {
				return fmt.Errorf("bounded exception handling export %q has no function signature", ex.Name)
			}
			if hasFuncrefPayload {
				if len(ft.Params) != 0 || len(ft.Results) > 1 {
					return errors.New("bounded exception handling reference-payload exports must be () -> () or return the sole nullable indexed function")
				}
				if len(ft.Results) == 1 {
					result := ft.Results[0]
					rt := result.Ref()
					heap := rt.Heap()
					if result.Kind() != wasm.ValRef || !rt.Nullable() || rt.Exact() || heap.Kind() != wasm.HeapTypeIndex || heap.Type().Index != payloadType {
						return fmt.Errorf("bounded exception handling reference-payload export %q has an unsupported result", ex.Name)
					}
				}
			}
			for _, typ := range append(append([]wasm.ValType(nil), ft.Params...), ft.Results...) {
				if typ.Kind() == wasm.ValRef && typ.Ref().Heap().Kind() == wasm.HeapAbs && (typ.Ref().Heap().Abs() == wasm.HeapExn || typ.Ref().Heap().Abs() == wasm.HeapNoExn) {
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
		var ft wasm.CompType
		if !stagedTagFuncType(m, i, &ft) || len(ft.Results) != 0 || len(ft.Params) > maxStagedEHTagParams {
			return fmt.Errorf("bounded exception handling tag %d requires at most %d scalar payloads and no results", i, maxStagedEHTagParams)
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
					return errors.New("bounded exception handling throw must target a declared tag")
				}
				throwCount++
			case 0xd2:
				function, err := r.U32()
				if err != nil {
					return err
				}
				if !hasFuncrefPayload || function != payloadFunc {
					return errors.New("bounded exception handling ref.func must name the exact local payload function")
				}
				refFuncCount++
			case 0x0a:
				if !exceptionReferences {
					return errors.New("bounded exception handling rejects throw_ref")
				}
			case 0x12:
				if hasFuncrefPayload {
					return errors.New("bounded exception handling reference payload rejects tail calls")
				}
				if !tailCalls {
					return errors.New("bounded exception handling rejects tail calls before handler transfer is proven")
				}
				if _, err := r.U32(); err != nil {
					return err
				}
			case 0x13:
				if hasFuncrefPayload {
					return errors.New("bounded exception handling reference payload rejects tail calls")
				}
				if !tailCalls {
					return errors.New("bounded exception handling rejects tail calls before handler transfer is proven")
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
					return errors.New("bounded exception handling reference payload rejects call_ref")
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
							return errors.New("bounded exception handling catch must target a declared tag")
						}
						if wasm.CatchKind(kind) == wasm.CatchRef {
							if !exceptionReferences {
								return errors.New("bounded exception handling rejects exception-reference catches")
							}
							rootCount++
						}
					case wasm.CatchAll:
					case wasm.CatchAllRef:
						if !exceptionReferences {
							return errors.New("bounded exception handling rejects exception-reference catches")
						}
						rootCount++
					default:
						return fmt.Errorf("bounded exception handling rejects unknown catch kind %d", kind)
					}
					if rootCount > 4 {
						return errors.New("bounded exception handling admits at most 4 rooted exception values per function")
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
				return errors.New("bounded exception handling reference catches must expose one rooted exn value and drop it immediately")
			}
		}
	}
	if hasFuncrefPayload && (refFuncCount != 1 || throwCount != 1) {
		return errors.New("bounded exception handling reference payload requires one ref.func and one throw")
	}
	_ = throwCount // retained for bounded support-scan accounting and diagnostics
	return nil
}

// compileWithFrontendFeatures is the internal staged path used to prove an
// implementation family before SupportedFeatures admits its public bit. Public
// entry points always validate RuntimeConfig first and pass its exact mapping.
type stagedNullReferenceProduct uint8

const (
	stagedNullReferenceProductFirst stagedNullReferenceProduct = iota + 1
	stagedNullReferenceProductBottomGlobals
)

func stagedNullReferenceProductShape(m *wasm.Module) (stagedNullReferenceProduct, error) {
	if m == nil || len(m.Imports) != 0 || m.Start != nil || m.TagCount() != 0 || m.MemCount() != 0 || m.TableCount() != 0 || len(m.Elements) != 0 || len(m.Data) != 0 {
		return 0, errors.New("null-reference product requires only local functions and immutable local globals")
	}
	if len(m.Types) == 10 && m.FuncCount() == 9 && len(m.Code) == 9 && len(m.Globals) == 18 && len(m.Exports) == 9 {
		if err := stagedBottomNullReferenceProductShape(m); err != nil {
			return 0, err
		}
		return stagedNullReferenceProductBottomGlobals, nil
	}
	if len(m.Types) != 6 || m.FuncCount() != 5 || len(m.Code) != 5 || len(m.Globals) != 5 || len(m.Exports) != 5 {
		return 0, errors.New("null-reference product is not one of the two exact pinned module shapes")
	}
	var base wasm.CompType
	if !m.ResolveTypeFunc(0, &base) || len(base.Params) != 0 || len(base.Results) != 0 {
		return 0, errors.New("first null-reference product type 0 must be () -> ()")
	}
	indexed := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))
	results := []wasm.ValType{
		wasm.RefVal(wasm.AbsRef(wasm.HeapAny)),
		wasm.FuncRef,
		wasm.RefVal(wasm.AbsRef(wasm.HeapExn)),
		wasm.ExternRef,
		indexed,
	}
	heaps := []int64{-18, -16, -23, -17, 0}
	exports := []string{"anyref", "funcref", "exnref", "externref", "ref"}
	for i := range results {
		ft, found := m.FuncSignature(uint32(i))
		if !found || len(ft.Params) != 0 || len(ft.Results) != 1 || !wasm.EqualValType(ft.Results[0], results[i]) {
			return 0, fmt.Errorf("first null-reference product function %d has an unexpected signature", i)
		}
		if len(m.Code[i].Locals.Runs) != 0 || !isExactRefNullBody(m.Code[i].BodyBytes, heaps[i]) {
			return 0, fmt.Errorf("first null-reference product function %d must return exactly one null", i)
		}
		g := &m.Globals[i]
		if g.Type.Mutable || !wasm.EqualValType(g.Type.Type, results[i]) || !isExactRefNullBody(g.Init.BodyBytes, heaps[i]) {
			return 0, fmt.Errorf("first null-reference product global %d must be an immutable exact null", i)
		}
	}
	for i, ex := range m.Exports {
		if ex.Name != exports[i] || ex.Index.Kind != wasm.ExternFunc || int(ex.Index.Index) != i {
			return 0, fmt.Errorf("first null-reference product export %d is outside the exact function export set", i)
		}
	}
	return stagedNullReferenceProductFirst, nil
}

func stagedBottomNullReferenceProductShape(m *wasm.Module) error {
	var base wasm.CompType
	if !m.ResolveTypeFunc(0, &base) || len(base.Params) != 0 || len(base.Results) != 0 {
		return errors.New("bottom-global null-reference product type 0 must be () -> ()")
	}
	anyref := wasm.RefVal(wasm.AbsRef(wasm.HeapAny))
	nullref := wasm.RefVal(wasm.AbsRef(wasm.HeapNone))
	exnref := wasm.RefVal(wasm.AbsRef(wasm.HeapExn))
	nullexnref := wasm.RefVal(wasm.AbsRef(wasm.HeapNoExn))
	nullfuncref := wasm.RefVal(wasm.AbsRef(wasm.HeapNoFunc))
	nullexternref := wasm.RefVal(wasm.AbsRef(wasm.HeapNoExtern))
	indexed := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))
	results := []wasm.ValType{anyref, nullref, wasm.FuncRef, nullfuncref, exnref, nullexnref, wasm.ExternRef, nullexternref, indexed}
	globalGets := []uint32{0, 0, 1, 1, 2, 2, 3, 3, 1}
	exports := []string{"anyref", "nullref", "funcref", "nullfuncref", "exnref", "nullexnref", "externref", "nullexternref", "ref"}
	for i := range results {
		ft, found := m.FuncSignature(uint32(i))
		if !found || len(ft.Params) != 0 || len(ft.Results) != 1 || !wasm.EqualValType(ft.Results[0], results[i]) {
			return fmt.Errorf("bottom-global null-reference product function %d has an unexpected signature", i)
		}
		if len(m.Code[i].Locals.Runs) != 0 || !isExactGlobalGetBody(m.Code[i].BodyBytes, globalGets[i]) {
			return fmt.Errorf("bottom-global null-reference product function %d must return one exact immutable global", i)
		}
	}
	globalTypes := []wasm.ValType{
		nullref, nullfuncref, nullexnref, nullexternref,
		anyref, anyref, wasm.FuncRef, wasm.FuncRef, exnref, exnref, wasm.ExternRef, wasm.ExternRef,
		nullref, nullfuncref, nullexnref, nullexternref, indexed, indexed,
	}
	globalHeaps := []int64{-15, -13, -12, -14, -18, -15, -16, -13, -23, -12, -17, -14, -15, -13, -12, -14, 0, -13}
	for i := range globalTypes {
		g := &m.Globals[i]
		if g.Type.Mutable || !wasm.EqualValType(g.Type.Type, globalTypes[i]) || !isExactRefNullBody(g.Init.BodyBytes, globalHeaps[i]) {
			return fmt.Errorf("bottom-global null-reference product global %d must be an immutable exact null", i)
		}
	}
	for i, ex := range m.Exports {
		if ex.Name != exports[i] || ex.Index.Kind != wasm.ExternFunc || int(ex.Index.Index) != i {
			return fmt.Errorf("bottom-global null-reference product export %d is outside the exact function export set", i)
		}
	}
	return nil
}

func isExactRefNullBody(body []byte, heap int64) bool {
	r := wasm.NewReader(body)
	op, err := r.Byte()
	if err != nil || op != 0xd0 {
		return false
	}
	got, err := r.S33()
	if err != nil || got != heap {
		return false
	}
	end, err := r.Byte()
	return err == nil && end == 0x0b && r.BytesLeft() == 0
}

func isExactGlobalGetBody(body []byte, index uint32) bool {
	r := wasm.NewReader(body)
	op, err := r.Byte()
	if err != nil || op != 0x23 {
		return false
	}
	got, err := r.U32()
	if err != nil || got != index {
		return false
	}
	end, err := r.Byte()
	return err == nil && end == 0x0b && r.BytesLeft() == 0
}

func directCrossTailRegABI(ft *wasm.CompType) bool {
	if ft == nil || len(ft.Results) > 2 {
		return false
	}
	if len(ft.Results) == 2 && (!isIntegerWasmType(ft.Results[0]) || !isIntegerWasmType(ft.Results[1])) {
		return false
	}
	gp, fp := 0, 0
	for _, typ := range ft.Params {
		switch {
		case isIntegerWasmType(typ):
			gp++
		case wasm.EqualValType(typ, wasm.F32), wasm.EqualValType(typ, wasm.F64):
			fp++
		default:
			return false
		}
	}
	if gp > 7 || fp > 8 {
		return false
	}
	for _, typ := range ft.Results {
		if !isIntegerWasmType(typ) && !wasm.EqualValType(typ, wasm.F32) && !wasm.EqualValType(typ, wasm.F64) {
			return false
		}
	}
	return true
}

func isIntegerWasmType(typ wasm.ValType) bool {
	return wasm.EqualValType(typ, wasm.I32) || wasm.EqualValType(typ, wasm.I64)
}

func directCrossTailTargetABI(ft *wasm.CompType) bool {
	integerOnly := true
	for _, typ := range ft.Params {
		integerOnly = integerOnly && isIntegerWasmType(typ)
	}
	for _, typ := range ft.Results {
		integerOnly = integerOnly && isIntegerWasmType(typ)
	}
	if integerOnly {
		return true
	}
	if len(ft.Params) == 2 && wasm.EqualValType(ft.Params[0], wasm.I32) && wasm.EqualValType(ft.Params[1], wasm.F64) &&
		len(ft.Results) == 1 && wasm.EqualValType(ft.Results[0], wasm.F64) {
		return true
	}
	return len(ft.Params) == 1 && wasm.EqualValType(ft.Params[0], wasm.F64) &&
		len(ft.Results) == 1 && wasm.EqualValType(ft.Results[0], wasm.I32)
}

func unsafeDirectTailImportBitset(m *wasm.Module) ([]uint64, error) {
	imported := m.ImportedFuncCount()
	if imported == 0 {
		return nil, nil
	}
	var bits []uint64
	for localIdx := range m.Code {
		caller, ok := m.FuncSignature(uint32(imported + localIdx))
		if !ok {
			return nil, fmt.Errorf("function %d signature is unavailable", localIdx)
		}
		body := m.Code[localIdx].BodyBytes
		if len(body) == 0 {
			encoded, err := wasm.EncodeExpr(m.Code[localIdx].Body)
			if err != nil {
				return nil, fmt.Errorf("function %d body: %w", localIdx, err)
			}
			body = encoded
		}
		r := wasm.NewReader(body)
		for r.HasNext() {
			op, err := r.Byte()
			if err != nil {
				return nil, fmt.Errorf("function %d tail scan: %w", localIdx, err)
			}
			imm, err := wasm.ClassifyInstructionImmediate(r, op)
			if err != nil {
				return nil, fmt.Errorf("function %d tail scan: %w", localIdx, err)
			}
			if imm.Kind != wasm.InstrReturnCall || int(imm.Index) >= imported {
				continue
			}
			target, ok := m.FuncSignature(imm.Index)
			if !ok {
				return nil, fmt.Errorf("function %d return_call target %d signature is unavailable", localIdx, imm.Index)
			}
			if directCrossTailRegABI(caller) && directCrossTailRegABI(target) && directCrossTailTargetABI(target) {
				continue
			}
			if bits == nil {
				bits = make([]uint64, (imported+63)/64)
			}
			bits[imm.Index>>6] |= uint64(1) << (imm.Index & 63)
		}
	}
	return bits, nil
}

func validateThreadedExecutionBoundary(m *wasm.Module, bounds BoundsCheckMode, analyses ...*wasm.ValidatedModuleAnalysis) error {
	if m == nil {
		return errors.New("threads module is nil")
	}
	// Validation already requires a memory for every atomic access and wait.
	// atomic.fence is independent of linear memory and its ownership boundary.
	memories := m.MemCount()
	if memories == 0 {
		return nil
	}
	for i := 0; i < memories; i++ {
		mt, ok := m.MemoryType(uint32(i))
		if !ok || mt.Shared && !mt.Limits.HasMax {
			return errors.New("threads currently require shared memory with an exact maximum")
		}
	}
	if bounds != BoundsChecksExplicit {
		return errors.New("threads currently require explicit bounds checks")
	}
	if len(analyses) != 0 && analyses[0].ValidFor(m) {
		if analyses[0].Flags()&wasm.ValidatedFuncUsesNonAtomicMemory == 0 {
			return nil
		}
		// Preserve the exact instruction and function diagnostic on rejection.
	}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	for function, fn := range m.Code {
		r := wasm.NewReader(fn.BodyBytes)
		for r.HasNext() {
			op, err := r.Byte()
			if err != nil {
				return err
			}
			var imm wasm.InstructionImmediate
			if err := classifier.ClassifyInto(r, op, &imm); err != nil {
				return err
			}
			// Memory accesses use the shared backing directory. Passive data
			// descriptors remain private to the instance control mapping.
			admittedMemory := op == 0xfe || op >= 0x28 && op <= 0x3e || op == 0xfd || imm.Kind == wasm.InstrMemorySize || imm.Kind == wasm.InstrMemoryCopy || imm.Kind == wasm.InstrMemoryFill || imm.Kind == wasm.InstrMemoryGrow || imm.Kind == wasm.InstrMemoryInit
			if (imm.TouchesMemory || imm.Kind == wasm.InstrMemoryGrow) && !admittedMemory {
				return fmt.Errorf("threads function %d uses %s outside the shared memory access boundary", function, imm.Kind.String())
			}
		}
	}
	return nil
}

func compileWithFrontendFeatures(cfg *RuntimeConfig, wasmBytes []byte, features frontend.Features) (*Compiled, error) {
	return compileWithFrontendFeaturesAndInstructions(cfg, wasmBytes, features, emptyPluginInstructions())
}

func narrowFrontendFeatures(features *frontend.Features, requiredByModule CoreFeatures) {
	if !requiredByModule.IsEnabled(CoreFeatureTypedFunctionReferences) && !requiredByModule.IsEnabled(CoreFeatureGC) {
		features.TypedFunctionReferences = false
		features.TypedTailCalls = false
	}
	if !requiredByModule.IsEnabled(CoreFeatureTailCall) {
		features.TailCalls = false
		features.TypedTailCalls = false
	}
	if !requiredByModule.IsEnabled(CoreFeatureExceptionHandling) {
		features.ExceptionHandling = false
		features.ExceptionReferences = false
	}
}

func guardedMemoryGrowFunctions(m *wasm.Module, analysis *wasm.ValidatedModuleAnalysis) []bool {
	if !analysis.ValidFor(m) || analysis.Flags()&wasm.ValidatedFuncUsesMemoryGrow == 0 {
		return nil
	}
	grows := make([]bool, len(m.Code))
	callHeads := make([]uint32, len(m.Code))
	type callEdge struct {
		caller uint32
		next   uint32
	}
	var callEdges []callEdge
	queue := make([]uint32, 0, len(m.Code))
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	imported := uint32(m.ImportedFuncCount())
	for caller, fn := range m.Code {
		facts := analysis.Func(caller)
		if facts.Flags&wasm.ValidatedFuncUsesMemoryGrow != 0 {
			grows[caller] = true
			queue = append(queue, uint32(caller))
		}
		if facts.Flags&wasm.ValidatedFuncHasDirectCall == 0 {
			continue
		}
		r := wasm.NewReader(fn.BodyBytes)
		for r.HasNext() {
			op, err := r.Byte()
			if err != nil {
				return nil
			}
			if op == 0x10 || op == 0x12 { // call, return_call
				target, err := r.U32()
				if err != nil {
					return nil
				}
				if target >= imported && target-imported < uint32(len(m.Code)) {
					local := target - imported
					callEdges = append(callEdges, callEdge{caller: uint32(caller), next: callHeads[local]})
					callHeads[local] = uint32(len(callEdges))
				}
				continue
			}
			if _, immediateFree := wasm.ImmediateFreeInstructionKind(op); !immediateFree {
				if err := classifier.SkipImmediate(r, op); err != nil {
					return nil
				}
			}
		}
	}
	for len(queue) != 0 {
		callee := queue[0]
		queue = queue[1:]
		for edgeIndex := callHeads[callee]; edgeIndex != 0; edgeIndex = callEdges[edgeIndex-1].next {
			caller := callEdges[edgeIndex-1].caller
			if !grows[caller] {
				grows[caller] = true
				queue = append(queue, caller)
			}
		}
	}
	return grows
}

func normalizeAdapterReturnOffsets(offsets []uint32) []uint32 {
	sort.Slice(offsets, func(i, j int) bool { return offsets[i] < offsets[j] })
	if len(offsets) < 2 {
		return offsets
	}
	out := offsets[:1]
	for _, off := range offsets[1:] {
		if off != out[len(out)-1] {
			out = append(out, off)
		}
	}
	return out
}

func isUnsupportedProposalError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"tag section", "exception", "throw", "try_table",
		"return_call", "tail call",
		"reference instruction", "reference type", "ref func", "ref type ", "call_ref", "br_on_null", "br_on_non_null", "ref.as_non_null",
		"shared memory", "atomic instruction", "atomic.",
		"extended-constant-expressions", "extended-const-expressions",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
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
			return m.Imports[i].Type.MemType().Limits.Min, true
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

func elementPayloads(m *wasm.Module, types []DefinedTypeDescriptor, constExprCtx *constExprCompileContext, e *wasm.Elem) (ValType, ValueTypeDescriptor, []RefInit, error) {
	switch e.Kind.Kind {
	case wasm.ElemFuncs:
		out := make([]RefInit, len(e.Kind.Funcs))
		for i, fidx := range e.Kind.Funcs {
			out[i] = RefInit{FuncIndex: uint32(fidx)}
		}
		exact, err := valueTypeDescriptorInModule(m, wasm.RefVal(wasm.Ref(false, wasm.AbsHeap(wasm.HeapFunc), false)))
		if err != nil {
			return 0, ValueTypeDescriptor{}, nil, err
		}
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
			body := ex.BodyBytes
			if len(body) == 0 {
				var err error
				body, err = wasm.EncodeExpr(ex)
				if err != nil {
					return 0, ValueTypeDescriptor{}, nil, fmt.Errorf("expression %d encode: %w", i, err)
				}
			}
			r := wasm.NewReader(body)
			if op, err := r.Byte(); err == nil && op == 0x23 {
				globalIndex, readErr := r.U32()
				end, endErr := r.Byte()
				if readErr == nil && endErr == nil && end == 0x0b && r.BytesLeft() == 0 {
					gt, ok := m.GlobalTypeByIndex(globalIndex)
					if !ok || gt.Mutable {
						return 0, ValueTypeDescriptor{}, nil, fmt.Errorf("expression %d has invalid global.get initializer", i)
					}
					globalType, err := valueTypeDescriptorInModule(m, gt.Type)
					if err != nil || !valueTypeSubtype(globalType, types, exact, types) {
						return 0, ValueTypeDescriptor{}, nil, fmt.Errorf("expression %d global type does not match segment type", i)
					}
					out[i] = RefInit{GlobalIndex: globalIndex, HasGlobal: true}
					continue
				}
			}
			if wasmGCBuilt && refType == ValI31Ref {
				if err := lowerI31ElementInitializer(body, wasm.RefVal(e.Kind.Ref), constExprCtx, i, &out[i]); err != nil {
					return 0, ValueTypeDescriptor{}, nil, err
				}
				continue
			}
			result, exprErr := evalConstExprBytesWithContext(body, wasm.RefVal(e.Kind.Ref), constExprCtx)
			if exprErr == nil {
				if len(result.Expr) != 0 {
					out[i] = RefInit{Expr: append([]byte(nil), result.Expr...)}
					continue
				}
				// Core 3 element expressions may use a null from any reference
				// hierarchy. The Release 2 fallback parser only covers func/extern.
				if isNullReferenceConstExprResult(result) {
					out[i] = RefInit{Null: true}
					continue
				}
				// A direct ref.i31 evaluates to tagged bits. Preserve its semantic
				// expression when the declared segment type is a supertype such as
				// anyref; FuncIndex is reserved for function-reference payloads here.
				if wasmGCBuilt && isI31ReferenceConstExprResult(result) {
					out[i] = RefInit{Expr: append([]byte(nil), body...)}
					continue
				}
			}
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

func isNullReferenceConstExprResult(result constExprResult) bool {
	return result.vtype.Kind() == wasm.ValRef && result.bits == 0 && result.GlobalIndex < 0 && result.FuncIndex < 0
}

func isI31ReferenceConstExprResult(result constExprResult) bool {
	return isI31RefType(result.vtype) && result.bits != 0 && result.GlobalIndex < 0 && result.FuncIndex < 0
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

func (c *Compiled) importsRequireSync(imports resolvedImports, force bool) bool {
	return c.importsRequireSyncWithAtomicHelpers(imports, force, c.usesAtomicWaitHelpers())
}

func (c *Compiled) importsRequireSyncWithAtomicHelpers(imports resolvedImports, force, atomicHelpers bool) bool {
	if force || atomicHelpers || c.opaqueExternHelperSlots() != 0 || railshotRequiresNativeHostControl(c) || c.needsPublicFuncrefHostReentry() || c.usesGCStructHelpers() || c.usesGCArrayHelpers() || c.needsOwnedExceptionReferences() || c.usesDynamicFuncRefTest() || c.usesGuestGlobalStoreHelper() {
		return true
	}
	for i := 0; hostFunctionImportsBuilt && i < len(c.Imports); i++ {
		bindingKey := c.functionImportBindingKey(i)
		if isI32HostEvent(imports[bindingKey]) {
			continue
		}
		if export, cross := imports[bindingKey].(*InstanceExport); cross {
			// A cross-instance-only consumer needs the parked-host loop only when
			// its target can itself park. syncMode is immutable after the producer
			// is instantiated and already includes its transitive direct imports.
			if export == nil || export.inst == nil || export.inst.syncMode {
				return true
			}
			continue
		}
		// Only the explicit I32HostEvent contract may use deferred replay. Every
		// ordinary host binding remains synchronous; otherwise a callback could be
		// delayed despite expecting immediate side effects or a Caller capability.
		return true
	}
	// An imported funcref table can be mutated to contain a host or
	// cross-instance descriptor after instantiation, so it remains conservative.
	for i := 0; tableImportsForImage && i < c.tableImportCount(); i++ {
		if c.tableElementType(i) == ValFuncRef {
			return true
		}
	}
	return false
}

func (c *Compiled) exceptionExternPayloads() bool {
	if compiledArchitecture != 4 || c == nil || !c.requiredFeatures.IsEnabled(CoreFeatureExceptionHandling) || c.memoryDir == nil {
		return false
	}
	for _, tag := range c.memoryDir.exceptionTags() {
		if uint64(tag.TypeIndex) >= uint64(len(c.Types)) {
			continue
		}
		for _, value := range c.Types[tag.TypeIndex].Params {
			if value.Kind == ValueTypeReference && !value.Ref.Heap.Defined && (value.Ref.Heap.Abstract == AbstractHeapExtern || value.Ref.Heap.Abstract == AbstractHeapNoExtern) {
				return true
			}
		}
	}
	return false
}

// validateImportBindings checks cross-instance signatures and reference-store
// compatibility. Imported calls are already compiled; instantiation only writes
// concrete targets into the per-instance dispatch table.
func (c *Compiled) validateImportBindings(imports resolvedImports, store *referenceStore) error {
	return c.validateImportBindingsWithPluginGC(imports, store, nil)
}

func (c *Compiled) validateImportBindingsWithPluginGC(imports resolvedImports, store *referenceStore, pluginGCImports map[uint32]struct{}) error {
	privateWaitGC := nativeCollectorFrameRootsBuilt && buildfeatures.Threads != 0 && c.usesAtomicWaitHelpers() && store != nil && !store.private && c.needsRuntimeGCCollectorDomain()
	hasFunctionImports := hostFunctionImportsBuilt && len(c.Imports) != 0
	if !hasFunctionImports && !privateWaitGC {
		return nil
	}
	dynamicFuncrefReachability := nativeCollectorFrameRootsBuilt && compiledHasDynamicFuncrefReachability(c)
	if privateWaitGC && dynamicFuncrefReachability {
		return errors.New("dynamic funcref reachability is unsupported for modules with atomic wait helpers")
	}
	if !hasFunctionImports {
		return nil
	}
	ehNativeCalls := buildfeatures.ExceptionHandling != 0 && c.stagedFeatures().IsEnabled(CoreFeatureExceptionHandling)
	ehExternPayloads := buildfeatures.ExceptionHandling != 0 && c.exceptionExternPayloads()
	gcSubtypeLinkProduct := c.stagedGCTypeSubtypingProduct()
	gcSubtypeLinkConsumer := wasmGCBuilt && gcSubtypeLinkProduct.isLinkConsumer()
	for i, key := range c.Imports {
		bindingKey := c.functionImportBindingKey(i)
		sigHasGCRefs := wasmGCBuilt && i < len(c.importFuncSigs) && funcSigHasGCRefs(c.importFuncSigs[i])
		if privateWaitGC && sigHasGCRefs {
			return fmt.Errorf("collector-reference import %q is unsupported for modules with atomic wait helpers", key)
		}
		ex, ok := imports[bindingKey].(*InstanceExport)
		if !ok {
			if sigHasGCRefs {
				_, pluginImport := pluginGCImports[uint32(i)]
				if err := c.validateCollectorHostImport(i, key, imports[bindingKey], store, pluginImport); err != nil {
					return err
				}
				continue
			}
			if gcSubtypeLinkConsumer {
				return fmt.Errorf("cross-instance import %q requires the exact gc/type-subtyping link provider", key)
			}
			// Synchronous host imports already park and restore the active EH/GC
			// control state. Only direct cross-instance native transfers need the
			// retained InstanceExport checks below.
			continue
		}
		if ex == nil || ex.inst == nil {
			return fmt.Errorf("cross-instance import %q is nil", key)
		}
		// A void call can throw an extern payload without an explicit
		// reference signature. Such tokens still belong to one store.
		if buildfeatures.ExceptionHandling != 0 && compiledArchitecture == 4 && c.requiredFeatures.IsEnabled(CoreFeatureExceptionHandling) && (ehExternPayloads || ex.inst.c.exceptionExternPayloads()) && (store == nil || ex.inst.refStore != store) {
			return fmt.Errorf("exception-handler transfer import %q requires the same reference store", key)
		}
		if ex.inst.executionFlags.Load()&executionFlagDynamicGCDomain != 0 && ex.inst.refStore != store {
			return fmt.Errorf("cross-instance import %q from a dynamic funcref producer requires the same Runtime", key)
		}
		if nativeCollectorFrameRootsBuilt && (dynamicFuncrefReachability || privateWaitGC) {
			if err := ex.inst.validateGCInvocationImport(key, store, dynamicFuncrefReachability, privateWaitGC); err != nil {
				return err
			}
		}
		if ex.localIdx < 0 || ex.localIdx >= len(ex.inst.c.Entry) {
			return fmt.Errorf("cross-instance import %q references an unavailable function", key)
		}
		if i >= len(c.importFuncSigs) {
			return fmt.Errorf("cross-instance import %q is missing its signature", key)
		}
		if c.importFuncSigs[i].unsafeCrossTail && !railshotNativeTailImport(c, i) {
			return fmt.Errorf("cross-instance import %q requires unsupported cross-instance tail ABI", key)
		}
		if gcSubtypeLinkConsumer && ex.inst.c.stagedGCTypeSubtypingProduct() != gcSubtypeLinkProduct.linkProviderProduct() {
			return fmt.Errorf("cross-instance import %q signature mismatch: producer is outside the exact gc/type-subtyping link product", key)
		}
		sig := c.importFuncSigs[i]
		if !sigMatches(sig, c.Types, ex, gcSubtypeLinkConsumer || (c.stagedFeatures()|c.requiredFeatures)&(CoreFeatureTypedFunctionReferences|CoreFeatureGC) != 0) {
			return fmt.Errorf("cross-instance import %q signature mismatch", key)
		}
		if referenceSignaturesBuilt && (hasValType(sig.Params, ValExternRef) || hasValType(sig.Results, ValExternRef)) {
			if store == nil || ex.inst.refStore != store {
				return fmt.Errorf("cross-instance externref import %q requires the same reference store", key)
			}
		}
		if sigHasGCRefs {
			if store == nil || ex.inst.refStore != store || ex.inst.gc == nil || !store.ownsGCCollector(ex.inst.gc) {
				return fmt.Errorf("cross-instance GC import %q requires live instances in the same Runtime GC domain", key)
			}
			// Module-local GC type indexes are canonicalized when the consumer joins
			// the producer's Runtime collector domain. Signature validation above
			// proves structural compatibility; raw descriptor-table equality is not
			// cross-module type identity.
			if c.genericGCFrameRoots() == nil || ex.inst.c.genericGCFrameRoots() == nil {
				return fmt.Errorf("cross-instance GC import %q requires exact native root maps", key)
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

func sigMatches(required FuncSig, requiredTypes []DefinedTypeDescriptor, ex *InstanceExport, allowSubtype bool) bool {
	if ex == nil || ex.inst == nil || ex.localIdx < 0 || ex.localIdx >= len(ex.inst.c.Funcs) {
		return false
	}
	actual := ex.inst.c.Funcs[ex.localIdx]
	if required.HasTypeIndex && actual.HasTypeIndex {
		if allowSubtype {
			actualRef := ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true, TypeIndex: actual.TypeIndex}}
			requiredRef := ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true, TypeIndex: required.TypeIndex}}
			if !referenceTypeSubtype(actualRef, ex.inst.c.Types, requiredRef, requiredTypes) {
				return false
			}
		} else if !tagTypeEquivalent(actual.TypeIndex, ex.inst.c.Types, required.TypeIndex, requiredTypes) {
			return false
		}
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
		if allowSubtype {
			if !valueTypeSubtype(requiredParams[i], requiredTypes, actualParams[i], ex.inst.c.Types) {
				return false
			}
		} else if !valueTypeEquivalent(actualParams[i], ex.inst.c.Types, requiredParams[i], requiredTypes) {
			return false
		}
	}
	for i := range requiredResults {
		if allowSubtype {
			if !valueTypeSubtype(actualResults[i], ex.inst.c.Types, requiredResults[i], requiredTypes) {
				return false
			}
		} else if !valueTypeEquivalent(actualResults[i], ex.inst.c.Types, requiredResults[i], requiredTypes) {
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
		segmentStateCount(imm.Kind, imm.Index, imm.Index2, elemCount, dataCount)
	}
	return true
}

func instrsSegmentStateCounts(instrs []wasm.Instruction, elemCount, dataCount *int) {
	for i := range instrs {
		in := &instrs[i]
		segmentStateCount(in.Kind, in.Index, in.Index2, elemCount, dataCount)
		instrsSegmentStateCounts(in.Body().Instrs, elemCount, dataCount)
		instrsSegmentStateCounts(in.Then(), elemCount, dataCount)
		instrsSegmentStateCounts(in.Else(), elemCount, dataCount)
	}
}

func segmentStateCount(kind wasm.InstrKind, index, index2 uint32, elemCount, dataCount *int) {
	count := int(index) + 1
	switch kind {
	case wasm.InstrTableInit, wasm.InstrElemDrop:
		if count > *elemCount {
			*elemCount = count
		}
	case wasm.InstrArrayNewElem, wasm.InstrArrayInitElem:
		count = int(index2) + 1
		if count > *elemCount {
			*elemCount = count
		}
	case wasm.InstrMemoryInit, wasm.InstrDataDrop:
		if count > *dataCount {
			*dataCount = count
		}
	case wasm.InstrArrayNewData, wasm.InstrArrayInitData:
		count = int(index2) + 1
		if count > *dataCount {
			*dataCount = count
		}
	}
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
func (c *Compiled) ExportedFunctions() []string { return sortedKeys(c.executionView().Exports) }

// ExportedGlobals returns the names of the module's exported globals, sorted.
func (c *Compiled) ExportedGlobals() []string { return sortedKeys(c.executionView().GlobalExports) }

// MemoryImport returns the "module.name" key when the module imports exactly one
// memory. Modules with zero or multiple memory imports return false; use
// MemoryImports for the complete ordered list.
func (c *Compiled) MemoryImport() (string, bool) {
	c = c.executionView()
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
	c = c.executionView()
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
	c = c.executionView()
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
	c = c.executionView()
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
	if mt.Limits.HasMax {
		def.Max = mt.Limits.Max
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
	params, results, err = c.signatureView(export)
	if err != nil {
		return nil, nil, err
	}
	return append([]ValType(nil), params...), append([]ValType(nil), results...), nil
}

func (c *Compiled) signatureView(export string) (params, results []ValType, err error) {
	c = c.executionView()
	if c == nil {
		return nil, nil, errors.New("compiled module is nil")
	}
	gfi, ok := c.Exports[export]
	if !ok {
		return nil, nil, fmt.Errorf("no exported function %q", export)
	}
	if gfi < 0 {
		return nil, nil, fmt.Errorf("export %q function index %d out of range", export, gfi)
	}
	if functionReexportsForImage && gfi < c.NumImports {
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
	c = c.executionView()
	if c == nil {
		return nil, nil, errors.New("compiled module is nil")
	}
	gfi, ok := c.Exports[export]
	if !ok {
		return nil, nil, fmt.Errorf("no exported function %q", export)
	}
	var sig FuncSig
	if gfi < 0 {
		return nil, nil, fmt.Errorf("export %q function index %d out of range", export, gfi)
	} else if functionReexportsForImage && gfi < c.NumImports {
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
	c = c.executionView()
	if c == nil {
		return nil
	}
	return cloneDefinedTypeDescriptors(c.Types)
}

// FuncName returns the name-section name for a global function index.
func (c *Compiled) FuncName(funcIdx uint32) (string, bool) {
	c = c.executionView()
	if !nameSectionForImage || c == nil || c.Names == nil {
		return "", false
	}
	return c.Names.FuncName(funcIdx)
}

// LocalFuncName returns the name-section name for a locally-defined function
// index (that is, an index into Compiled.Funcs rather than wasm's global
// function-index space).
func (c *Compiled) LocalFuncName(localIdx int) (string, bool) {
	c = c.executionView()
	if c == nil || localIdx < 0 {
		return "", false
	}
	return c.FuncName(uint32(localIdx + c.NumImports))
}

// FuncDebugName returns a stable display name for a global function index,
// preferring the wasm name section and falling back to exports or funcN.
func (c *Compiled) FuncDebugName(funcIdx uint32) string {
	c = c.executionView()
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
		return errors.New("compiled module is nil")
	}
	if err := railshotValidateCompiled(c); err != nil {
		return err
	}
	return c.validateMetadata()
}

// validateMetadata checks stored types, entries, resources, and initializers.
// Native execution admission remains a separate requirement in validate.
func (c *Compiled) validateMetadata() error {
	if standaloneBareImage {
		if buildfeatures.StandaloneStructuralDiagnostic != "" {
			return errors.New(buildfeatures.StandaloneStructuralDiagnostic)
		}
		if !aggregateTypesForImage && hasAggregateTypeDeclarations(c.Types) {
			return errors.New("compiled metadata invalid: aggregate types exceed the standalone runtime profile")
		}
		if c.invocationTypes()&^invocationTypesForImage != 0 {
			return errors.New("compiled metadata invalid: invocation types exceed the standalone runtime profile")
		}
	} else if err := c.validateStructuralMetadata(); err != nil {
		return err
	}
	if !multiTableForImage && len(c.extraTables) != 0 {
		return errors.New("compiled metadata invalid: multiple tables exceed the standalone runtime profile")
	}
	if !tableInitializersForImage && c.hasTableFunctionInitializers() {
		return errors.New("compiled metadata invalid: table initializers exceed the standalone runtime profile")
	}
	if !structuralCallKeysForImage && c.needsFuncRefDescs() {
		return errors.New("compiled metadata invalid: function reference descriptors exceed the standalone runtime profile")
	}
	if !funcRefContextHeaderForImage && c.needsFuncRefContextHeader {
		return errors.New("compiled metadata invalid: function reference context header exceeds the standalone runtime profile")
	}
	needsNativeGCABI := c.needsNativeGCABI()
	if needsNativeGCABI {
		if !collectorRuntimeForImage {
			return errors.New("compiled metadata invalid: native GC ABI exceeds the standalone runtime profile")
		}
		if c.nativeGCABIRequirement() != gc.NativeABIVersion {
			return fmt.Errorf("compiled metadata invalid: native GC ABI version %d unsupported (want %d)", c.nativeGCABIRequirement(), gc.NativeABIVersion)
		}
		if err := gc.ValidateNativeABI(); err != nil {
			return wrapContextError("compiled metadata invalid: native GC ABI", err)
		}
	} else if c.nativeGCABIRequirement() != 0 {
		return fmt.Errorf("compiled metadata invalid: native GC ABI version %d recorded without native GC execution", c.nativeGCABIRequirement())
	}
	if standaloneBareImage {
		if buildfeatures.StandaloneArenaDiagnostic != "" {
			return errors.New(buildfeatures.StandaloneArenaDiagnostic)
		}
		c.instantiateArenaNeed = buildfeatures.StandaloneArenaBytes
		c.maxParamSlots = buildfeatures.StandaloneMaxParamSlots
		c.maxResultSlots = buildfeatures.StandaloneMaxResultSlots
		c.syncHostSlots = buildfeatures.StandaloneSyncHostSlots
		return nil
	}
	return c.validateArenaFootprint()
}

// validateStructuralMetadata checks the linked declarations and code offsets
// independently of the destination runtime's ABI and allocation layout.
func (c *Compiled) validateStructuralMetadata() error {
	if !tablesForImage && c.hasTableDeclarationMetadata() {
		return errors.New("compiled metadata invalid: table declarations exceed the standalone runtime profile")
	}
	if !tableImportsForImage && c.tableImport != "" {
		return errors.New("compiled metadata invalid: table imports exceed the standalone runtime profile")
	}
	if !globalImportsForImage && len(c.GlobalImports) != 0 {
		return errors.New("compiled metadata invalid: global imports exceed the standalone runtime profile")
	}
	if !globalsForImage && len(c.Globals) != 0 ||
		!passiveDataForImage && len(c.PassiveData) != 0 {
		return errors.New("compiled metadata invalid: declarations exceed the standalone runtime profile")
	}
	if !guardedMemoryForImage && c.HasMemory && (c.boundsMode == BoundsChecksSignalsBased || c.prefersGuardMemory()) {
		return errors.New("compiled metadata invalid: guarded memory exceeds the standalone runtime profile")
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
	if !hostFunctionImportsBuilt && c.NumImports != 0 {
		return errors.New("compiled metadata invalid: function imports exceed the standalone runtime profile")
	}
	if err := c.validateImportModuleEnds(); err != nil {
		return err
	}
	if c.dynamicImports != (c.NumImports > 0) {
		return fmt.Errorf("compiled metadata invalid: dynamic import dispatch=%v with %d function import(s)", c.dynamicImports, c.NumImports)
	}
	if c.HasStart && (c.StartIsImport && !importedStartForImage || !c.StartIsImport && !localStartForImage) {
		return errStartOutsideImageProfile
	}
	if !c.HasStart {
		if c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 {
			return errors.New("compiled metadata invalid: inactive start function fields")
		}
	} else {
		var start FuncSig
		if c.StartIsImport {
			if c.StartLocalFunc != 0 || c.StartImportIdx < 0 || c.StartImportIdx >= c.NumImports {
				return errors.New("compiled metadata invalid: imported start function index")
			}
			start = c.importFuncSigs[c.StartImportIdx]
		} else {
			if c.StartImportIdx != 0 || c.StartLocalFunc < 0 || c.StartLocalFunc >= len(c.Funcs) {
				return errors.New("compiled metadata invalid: defined start function index")
			}
			start = c.Funcs[c.StartLocalFunc]
		}
		if len(start.Params) != 0 || len(start.Results) != 0 {
			return errors.New("compiled metadata invalid: start function must have no parameters or results")
		}
	}
	if c.NumImports > maxInt()-len(c.Funcs) {
		return errors.New("compiled metadata invalid: function count overflows int")
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
			if err := validateFuncSignature(sig, c.Types); err != nil {
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
	if buildfeatures.Custom {
		// Validate once through validateCached for a published module.
		if err := checkCompiledFeatures(required | compiledStructuralRequiredFeatures(c)); err != nil {
			return err
		}
	}
	unsupported := required &^ coreFeaturesWithoutSidecar
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
	staged |= required & platformCoreFeatures() & (CoreFeatureMultiMemory | CoreFeatureMemory64 | CoreFeatureTable64 | CoreFeatureThreads)
	// RV64 execution contracts independently validate helpers and exact roots.
	// Inert declarations need no compile-only product on any supported target.
	// Other executable legacy artifacts must retain their own feature sidecars.
	if compiledArchitecture == 4 || c.hasNoFunctionCode() && (!gc.HasHeapObjectTypes(c.GCTypeDescs) || c.genericGCFrameRoots() != nil) {
		staged |= required & platformCoreFeatures() & (CoreFeatureTypedFunctionReferences | CoreFeatureGC)
	}
	if c.memoryDir != nil && len(c.memoryDir.exceptionTags()) != 0 {
		if !exceptionTagsForImage {
			return errors.New("compiled metadata invalid: tags exceed the standalone runtime profile")
		}
		staged |= CoreFeatureExceptionHandling
		importCount := 0
		for i, tag := range c.memoryDir.exceptionTags() {
			if int(tag.TypeIndex) >= len(c.Types) || c.Types[tag.TypeIndex].Kind != CompositeTypeFunction || len(c.Types[tag.TypeIndex].Results) != 0 || len(c.Types[tag.TypeIndex].Params) > maxStagedEHTagParams {
				return errors.New("compiled metadata invalid: staged exception tag directory")
			}
			if tag.ImportKey != "" {
				if !tagImportsForImage {
					return errors.New("compiled metadata invalid: tag imports exceed the standalone runtime profile")
				}
				if i != importCount {
					return errors.New("compiled metadata invalid: staged exception tag imports must precede locals")
				}
				importCount++
			}
		}
		for name, index := range c.memoryDir.exceptionTagExports() {
			if name == "" || index < 0 || index >= len(c.memoryDir.exceptionTags()) {
				return errors.New("compiled metadata invalid: staged exception tag export")
			}
		}
	}
	// RV64 tag-free EH images carry the requirement in the canonical image
	// contract. Final native admission authenticates that contract before entry.
	if compiledArchitecture == 4 && c.nativeContextTrapABI && c.needsFuncRefContextHeader {
		staged |= required & CoreFeatureExceptionHandling
	}
	staged |= c.stagedFeatures()
	if referenceSignaturesBuilt && required&(CoreFeatureGC|CoreFeatureTypedFunctionReferences) != 0 && (compiledFunctionOnlyTypeGraph(c) || compiledArchitecture == 4 && railshotPrimitiveReferenceDeclarations(c)) {
		staged |= required & (CoreFeatureGC | CoreFeatureTypedFunctionReferences)
	}
	// Owned EH carries exact collector types without requiring a guest struct
	// allocation product. This restores only metadata grammar; native admission
	// still checks the canonical image, type directory and complete frame proof.
	if nativeCollectorFrameRootsBuilt && compiledArchitecture == 4 && c.needsOwnedExceptionReferences() && required.IsEnabled(CoreFeatureExceptionHandling) &&
		c.nativeContextTrapABI && c.needsFuncRefContextHeader && c.nativeGCABIRequirement() == gc.NativeABIVersion &&
		c.genericGCFrameRoots() != nil && validateCompiledGCFrameRoots(c, c.genericGCFrameRoots()) == nil {
		staged |= required & (CoreFeatureGC | CoreFeatureTypedFunctionReferences)
	}
	// Non-collecting internal collector carriers retain an exact native ABI and
	// frame contract even when no allocation helper product is selected.
	if nativeCollectorFrameRootsBuilt && c.hasCollectorDeclarationRoots() && rv64SourceContractValid(c) && validateCompiledGCFrameRoots(c, c.genericGCFrameRoots()) == nil {
		staged |= required & (CoreFeatureGC | CoreFeatureTypedFunctionReferences)
	}
	// Type declarations alone have no executable GC obligations. Preserve their
	// validated graph on reload without selecting an allocation product.
	if c.hasNoFunctionCode() && c.NumImports == 0 && len(c.Imports) == 0 && len(c.Globals) == 0 && len(c.GlobalImports) == 0 && c.tableCount() == 0 && c.memoryCount() == 0 && (c.memoryDir == nil || len(c.memoryDir.exceptionTags()) == 0) && c.activeDataCount() == 0 && len(c.PassiveData) == 0 && len(c.Elems) == 0 && len(c.passiveElems) == 0 && !c.HasStart {
		staged |= required & CoreFeatureGC
	}
	if c.hasNoFunctionCode() || c.tableCount() != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 {
		staged |= compiledStructuralRequiredFeatures(c) & CoreFeatureTypedFunctionReferences
	}
	// Aggregate storage declarations are fully represented by exact persisted
	// type metadata. Tag-free EH instructions still require their independent
	// execution marker and cannot be inferred through this path.
	aggregateSidecarFeatures := unsupported & CoreFeatures(buildfeatures.TypedFunctionReferences|buildfeatures.ExceptionHandling)
	if aggregateTypesForImage && aggregateSidecarFeatures != 0 {
		staged |= compiledAggregateStorageRequiredFeatures(c) & aggregateSidecarFeatures
	}
	if unsupported&^staged != 0 {
		return fmt.Errorf("compiled metadata invalid: unknown required feature bits 0x%x", uint64(unsupported&^staged))
	}
	if err := c.validateMemoryMetadata(required); err != nil {
		return err
	}
	if tablesForImage {
		if err := c.validateTableDeclarations(required); err != nil {
			return err
		}
	}
	if len(c.Entry) != len(c.Funcs) {
		return fmt.Errorf("compiled metadata invalid: Entry length %d != Funcs length %d", len(c.Entry), len(c.Funcs))
	}
	for i, off := range c.Entry {
		if off < 0 || off >= len(c.code) {
			return fmt.Errorf("compiled metadata invalid: Entry[%d] offset %d out of code range %d", i, off, len(c.code))
		}
	}
	if err := c.validateInternalEntries(); err != nil {
		return err
	}
	totalFuncs := c.NumImports + len(c.Funcs)
	if len(c.FuncTypeID) != totalFuncs {
		return fmt.Errorf("compiled metadata invalid: FuncTypeID length %d != function count %d", len(c.FuncTypeID), totalFuncs)
	}
	if tablesForImage {
		if err := c.validateTableBindings(totalFuncs); err != nil {
			return err
		}
	}
	for name, gfi := range c.Exports {
		if gfi < 0 || gfi >= totalFuncs {
			return fmt.Errorf("compiled metadata invalid: function export %q index %d out of range", name, gfi)
		}
		if !functionReexportsForImage && gfi < c.NumImports {
			return fmt.Errorf("compiled metadata invalid: function export %q exceeds the standalone runtime profile", name)
		}
	}
	if globalsForImage && referenceSignaturesBuilt {
		if err := c.validateRuntimeReferenceGlobalMetadata(); err != nil {
			return err
		}
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
		if globalInitExpressionsForImage {
			if err := c.validateGlobalInitExpr(i, g); err != nil {
				return err
			}
		} else if len(g.InitExpr) != 0 {
			return errors.New("compiled metadata invalid: standalone image has no deferred constant expressions")
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
	validateOffset := func(kind string, seg int, offset OffsetInit, want ValType, context constExprContext) error {
		if offset.HasGlobal && len(offset.Expr) != 0 {
			return fmt.Errorf("compiled metadata invalid: %s %d has multiple offset initializer forms", kind, seg)
		}
		if offset.HasGlobal {
			if !segmentGlobalOffsetsForImage {
				return errors.New("compiled metadata invalid: standalone image has no global segment offsets")
			}
			if want != ValI32 {
				return fmt.Errorf("compiled metadata invalid: %s %d uses compact i32 global offset for %s address", kind, seg, want)
			}
			return c.validateDeferredOffsetGlobal(kind, seg, offset.Global)
		}
		if len(offset.Expr) != 0 {
			if !segmentOffsetExpressionsForImage {
				return errors.New("compiled metadata invalid: standalone image has no segment offset expressions")
			}
			if err := validateCompiledScalarConstExpr(offset.Expr, want, c.Globals, constExprGlobalScope{context: context, limit: len(c.Globals)}); err != nil {
				return fmt.Errorf("compiled metadata invalid: %s %d extended %s offset: %w", kind, seg, want, err)
			}
		}
		return nil
	}
	for seg, el := range c.Elems {
		if !activeElementsForImage {
			return errors.New("compiled metadata invalid: active elements exceed the standalone runtime profile")
		}
		refType := normalizedElemRefType(el.RefType)
		if el.Mode != ElemModeActive {
			return fmt.Errorf("compiled metadata invalid: active element %d has mode %d", seg, el.Mode)
		}
		if uint64(el.TableIndex) >= uint64(c.tableCount()) {
			return fmt.Errorf("compiled metadata invalid: active element %d table index %d out of range", seg, el.TableIndex)
		}
		if c.tableEntryBytes(int(el.TableIndex)) != elemEntryBytes(refType) {
			return fmt.Errorf("compiled metadata invalid: active element %d type %s has incompatible table %d storage %s", seg, refType, el.TableIndex, c.tableElementType(int(el.TableIndex)))
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
		if err := validateOffset("element", seg, el.Offset, offsetType, constExprElementOffset); err != nil {
			return err
		}
		if err := c.validateElementValues("active", seg, el); err != nil {
			return err
		}
	}
	for seg, el := range c.passiveElems {
		if !passiveElementsForImage {
			return errors.New("compiled metadata invalid: passive elements exceed the standalone runtime profile")
		}
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
		if err := c.validateElementValues("element-state", seg, el); err != nil {
			return err
		}
	}
	for seg := 0; seg < c.activeDataCount(); seg++ {
		d, _ := c.activeDataInfoAt(seg)
		if count := c.memoryCount(); d.MemoryIndex != 0 || count != 0 {
			if uint64(d.MemoryIndex) >= uint64(count) {
				return fmt.Errorf("compiled metadata invalid: active data %d memory index %d out of range", seg, d.MemoryIndex)
			}
		}
		want := ValI32
		if count := c.memoryCount(); count != 0 && c.memoryDef(int(d.MemoryIndex)).Addr64 {
			want = ValI64
		}
		if err := validateOffset("data", seg, d.Offset, want, constExprDataOffset); err != nil {
			return err
		}
	}
	for seg, d := range c.PassiveData {
		if uint64(len(d.Bytes)) > uint64(^uint32(0)) {
			return fmt.Errorf("compiled metadata invalid: passive data %d length %d overflows descriptor", seg, len(d.Bytes))
		}
	}
	if err := c.validateGCTypeDescriptors(); err != nil {
		return wrapContextError("compiled metadata invalid: GCTypeDescs", err)
	}
	return nil
}

func (c *Compiled) validateGCTypeDescriptors() error {
	if !collectorRuntimeForImage {
		return gc.ValidatePlainFunctionTypeDescs(c.GCTypeDescs)
	}
	return frontend.ValidateCompileTypeDescs(c.GCTypeDescs)
}

func (c *Compiled) validateElementValues(kind string, seg int, elem ElemInit) error {
	refType := normalizedElemRefType(elem.RefType)
	if refType != ValFuncRef && refType != ValExternRef && refType != ValExnRef && refType != ValI31Ref && refType != ValAnyRef {
		return fmt.Errorf("compiled metadata invalid: %s element %d has unsupported reference type %s", kind, seg, refType)
	}
	required, err := c.elemExactType(elem)
	if err != nil {
		return fmt.Errorf("compiled metadata invalid: %s element %d exact type: %w", kind, seg, err)
	}
	for k, value := range elem.Values {
		if value.RepeatPrevious {
			if k == 0 || value.HasGlobal || value.Null || value.I31Wrap || value.FuncIndex != 0 || len(value.Expr) != 0 {
				return fmt.Errorf("compiled metadata invalid: %s element %d value %d has an invalid repeat", kind, seg, k)
			}
			continue
		}
		if len(value.Expr) != 0 {
			if buildfeatures.GC == 0 || !elementExpressionsForImage {
				return fmt.Errorf("compiled metadata invalid: %s element %d value %d uses disabled GC expressions", kind, seg, k)
			}
			if value.HasGlobal || value.Null || value.I31Wrap || value.FuncIndex != 0 || value.RepeatPrevious {
				return fmt.Errorf("compiled metadata invalid: %s element %d value %d has multiple initializer forms", kind, seg, k)
			}
			gcConstExpr := c.requiredFeatures.IsEnabled(CoreFeatureGC) || c.usesGenericGCExecution() || c.stagedGCStructProduct().requiresExternConversion() || c.stagedGCI31Product() != 0
			if !gcConstExpr || (refType != ValAnyRef && refType != ValI31Ref && !(refType == ValExternRef && (c.stagedGCStructProduct().requiresExternConversion() || c.compactExternFrameRoots()))) {
				return fmt.Errorf("compiled metadata invalid: %s element %d value %d has invalid GC expression", kind, seg, k)
			}
			if err := c.validateGCConstExpr(value.Expr, required, len(c.Globals)); err != nil {
				return fmt.Errorf("compiled metadata invalid: %s element %d value %d GC expression: %w", kind, seg, k, err)
			}
			continue
		}
		if value.HasGlobal {
			if int(value.GlobalIndex) >= len(c.Globals) || c.Globals[value.GlobalIndex].Mutable {
				return fmt.Errorf("compiled metadata invalid: %s element %d value %d global %d is unavailable or mutable", kind, seg, k, value.GlobalIndex)
			}
			if value.I31Wrap {
				if refType != ValI31Ref || c.Globals[value.GlobalIndex].Type != ValI32 {
					return fmt.Errorf("compiled metadata invalid: %s element %d value %d i31 global type mismatch", kind, seg, k)
				}
				continue
			}
			actual, actualErr := c.globalExactType(int(value.GlobalIndex))
			if actualErr != nil || !valueTypeSubtype(actual, c.Types, required, c.Types) {
				return fmt.Errorf("compiled metadata invalid: %s element %d value %d global type mismatch", kind, seg, k)
			}
			continue
		}
		if value.I31Wrap {
			return fmt.Errorf("compiled metadata invalid: %s element %d value %d has i31 wrapper without global", kind, seg, k)
		}
		if value.Null {
			if required.Kind != ValueTypeReference || !required.Ref.Nullable {
				return fmt.Errorf("compiled metadata invalid: %s element %d value %d is null for a non-null type", kind, seg, k)
			}
			continue
		}
		if refType == ValI31Ref {
			if value.FuncIndex&1 == 0 {
				return fmt.Errorf("compiled metadata invalid: %s element %d value %d has invalid i31 immediate", kind, seg, k)
			}
			continue
		}
		if refType != ValFuncRef {
			return fmt.Errorf("compiled metadata invalid: %s element %d value %d is non-null %s", kind, seg, k, refType)
		}
		if int(value.FuncIndex) >= len(c.importFuncSigs)+len(c.Funcs) {
			return fmt.Errorf("compiled metadata invalid: %s element %d function %d index %d out of range", kind, seg, k, value.FuncIndex)
		}
		actual, actualErr := c.functionRefExactType(value.FuncIndex)
		if actualErr != nil || !valueTypeSubtype(actual, c.Types, required, c.Types) {
			return fmt.Errorf("compiled metadata invalid: %s element %d value %d function type mismatch", kind, seg, k)
		}
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
		if g.Type == ValExnRef && g.Bits != 0 {
			return fmt.Errorf("compiled metadata invalid: non-null %s global initializer at global %d is unsupported", g.Type, i)
		}
		if g.Type == ValAnyRef && g.Bits != 0 && (g.Bits>>32 != 0 || uint32(g.Bits)&1 == 0) {
			return fmt.Errorf("compiled metadata invalid: non-i31 anyref global initializer at global %d is unsupported", i)
		}
		if g.Type == ValI31Ref && g.Bits != 0 && (g.Bits>>32 != 0 || uint32(g.Bits)&1 == 0) {
			return fmt.Errorf("compiled metadata invalid: invalid i31 global initializer at global %d", i)
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
	if tablesForImage && c.HasTable {
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
			return 0, errors.New("value slot count overflows int")
		}
		n += s
	}
	return n, nil
}

func (c *Compiled) needsFuncRefDescs() bool {
	return c.NeedsFuncRefDescs || tablesForImage && c.hasFuncrefTable()
}

func (c *Compiled) needsFuncRefContext() bool {
	return c.needsFuncRefContextHeader || c.needsFuncRefDescs()
}

func normalizedElemRefType(t ValType) ValType {
	if t == ValExternRef || t == ValExnRef || t == ValAnyRef || t == ValI31Ref {
		return t
	}
	return ValFuncRef
}

func normalizedTableElementType(t ValType) ValType {
	switch t {
	case ValExternRef, ValExnRef, ValAnyRef, ValI31Ref:
		return t
	default:
		return ValFuncRef
	}
}

func (c *Compiled) tableElementType(index int) ValType {
	if index == 0 {
		return normalizedTableElementType(c.TableType)
	}
	return normalizedTableElementType(c.extraTables[index-1].Type)
}

func elemEntryBytes(t ValType) int {
	switch normalizedElemRefType(t) {
	case ValExternRef, ValExnRef, ValAnyRef, ValI31Ref:
		return 8
	default:
		return wruntime.TableEntryBytes
	}
}

func (c *Compiled) tableEntryBytes(index int) int {
	return elemEntryBytes(c.tableElementType(index))
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

func (c *Compiled) threadedMemory0() bool {
	if !sharedMemoryRuntimeBuilt || c == nil || !c.requiredFeatures.IsEnabled(CoreFeatureThreads) || c.memoryCount() == 0 {
		return false
	}
	return c.memoryDef(0).Shared
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
	def := memoryDef{ImportKey: c.memoryImport, Min: uint64(c.MemMinPages), Max: uint64(c.MemMaxPages), HasMax: c.MemHasMax}
	return def
}

func (c *Compiled) validateMemoryMetadata(required CoreFeatures) error {
	if !memoriesForImage {
		if c.memoryCount() != 0 || len(c.memoryExportMap()) != 0 {
			return errors.New("compiled metadata invalid: memories exceed the standalone runtime profile")
		}
		return nil
	}
	seenLocal := false
	for i := 0; i < c.memoryCount(); i++ {
		memory := c.memoryDef(i)
		if memory.HasMax && memory.Max < memory.Min {
			return fmt.Errorf("compiled metadata invalid: memory %d maximum %d < minimum %d", i, memory.Max, memory.Min)
		}
		if memory.Shared && !memory.HasMax {
			return fmt.Errorf("compiled metadata invalid: shared memory %d has no maximum", i)
		}
		if buildfeatures.Threads != 0 && !sharedMemoryRuntimeBuilt && memory.Shared {
			return fmt.Errorf("compiled metadata invalid: memory %d exceeds the standalone shared memory profile", i)
		}
		if memory.Shared && !required.IsEnabled(CoreFeatureThreads) {
			return fmt.Errorf("compiled metadata invalid: shared memory %d requires threads feature", i)
		}
		if !memoryImportsForImage && memory.ImportKey != "" {
			return fmt.Errorf("compiled metadata invalid: memory %d exceeds the standalone memory import profile", i)
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
		return errors.New("compiled metadata invalid: multiple memories require multi-memory feature")
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
			// by instantiation. Imported unbounded memories retain the conservative
			// full memory32 charge; their physical reservation belongs to the provider.
			pages = 1 << 16
			if def.Addr64 && def.ImportKey == "" {
				pages = 65535
			}
		}
		if pages > ^uint64(0)/pageBytes {
			return 0, fmt.Errorf("memory %d maximum %d pages overflows bytes", i, pages)
		}
		bytes := pages * pageBytes
		if total > ^uint64(0)-bytes {
			return 0, errors.New("memory maximum total overflows uint64")
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
		return tableImportDef{Key: c.tableImport, Min: uint64(c.tableImportMin), Max: uint64(c.tableImportMax), Type: c.tableElementType(0), ValueTypeIndex: c.TableValueTypeIndex, HasValueType: c.TableHasValueType, HasMax: c.tableImportHasMax, Addr64: c.TableAddr64}, true
	}
	if index > 0 && index-1 < len(c.extraTables) {
		table := c.extraTables[index-1]
		if table.ImportKey != "" {
			return tableImportDef{Key: table.ImportKey, Min: uint64(table.Size), Max: table.Max, Type: c.tableElementType(index), ValueTypeIndex: table.ValueTypeIndex, HasValueType: table.HasValueType, HasMax: table.ImportHasMax, Addr64: table.Addr64}, true
		}
	}
	return tableImportDef{}, false
}

func (c *Compiled) hasTableFunctionInitializers() bool {
	if c.HasTableInitFunc {
		return true
	}
	for _, table := range c.extraTables {
		if table.HasInitFunc {
			return true
		}
	}
	return false
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
	if !table64ForImage || !c.inertUnobservableTableDeclaration(index) {
		return false
	}
	def := c.tableDef(0)
	return def.Addr64 && def.HasMax && def.Size >= 0 && uint64(def.Size) <= frontend.StagedTable64Max() && def.Max > frontend.StagedTable64Max()
}

func (c *Compiled) tableRuntimeCapacity(index int) int {
	if capacity, ok := railshotTableReservedCapacity(c, index); ok {
		return capacity
	}
	def := c.tableDef(index)
	if def.Max == 0 {
		return def.Size
	}
	entryBytes := c.tableEntryBytes(index)
	if def.HasMax && c.inertUnobservableTableDeclaration(index) && def.Max > uint64((wruntime.InstantiateArenaCacheBytes-8)/entryBytes) {
		return def.Size
	}
	return int(def.Max)
}

func (c *Compiled) tableMinimum(index int) int {
	if def, ok := c.tableImportAt(index); ok {
		// Import admission and codec validation prove the executable minimum fits int;
		// tableImportDef retains the exact uint64 declaration for metadata/linking.
		return int(def.Min)
	}
	return c.tableDef(index).Size
}

func (c *Compiled) validateArenaFootprint() error {
	return c.validateArenaFootprintForImage(functionCodeForImage, retainedNativeContextForImage)
}

func (c *Compiled) validateArenaFootprintForImage(callBuffers, retainedNativeContext bool) error {
	maxParams, maxResults, err := c.maxCallSlots()
	if err != nil {
		return wrapContextError("compiled metadata invalid", err)
	}
	funcRefCount, funcRefTypeIDCount := 0, 0
	if c.needsFuncRefDescs() {
		funcRefCount = len(c.FuncTypeID) + 1
		if c.usesDynamicFuncRefTest() {
			funcRefTypeIDCount = len(c.FuncTypeID)
		}
	} else if c.needsFuncRefContextHeader {
		funcRefCount = 1
	}
	tagCount := 0
	if exceptionTagsForImage && c.memoryDir != nil {
		tagCount = len(c.memoryDir.exceptionTags())
	}
	v128GlobalCount := 0
	for _, global := range c.Globals {
		if global.Type == ValV128 {
			v128GlobalCount++
		}
	}
	var tableSize, tableCapacity, importedTableCount int
	var tableCaps, tableEntryBytes []int
	if tableImportsForImage {
		importedTableCount = c.tableImportCount()
	}
	if tablesForImage && c.HasTable {
		tableSize, tableCapacity = c.TableSize, c.tableRuntimeCapacity(0)
		tableEntryBytes = make([]int, c.tableCount())
		for i := range tableEntryBytes {
			tableEntryBytes[i] = c.tableEntryBytes(i)
		}
		if len(c.extraTables) != 0 {
			tableSize, tableCapacity = 0, 0
			tableCaps = make([]int, c.tableCount())
			for i := range tableCaps {
				tableCaps[i] = c.tableRuntimeCapacity(i)
			}
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
	syncHostSlots := wruntime.MaxHostArity
	internalHelperSlots := 0
	for i, sig := range c.importFuncSigs {
		paramSlots, slotErr := valTypesSlots(sig.Params)
		if slotErr != nil {
			return fmt.Errorf("compiled metadata invalid: import %d parameters: %w", i, slotErr)
		}
		resultSlots, slotErr := valTypesSlots(sig.Results)
		if slotErr != nil {
			return fmt.Errorf("compiled metadata invalid: import %d results: %w", i, slotErr)
		}
		if paramSlots > syncHostSlots {
			syncHostSlots = paramSlots
		}
		if resultSlots > syncHostSlots {
			syncHostSlots = resultSlots
		}
	}
	ownedExceptions := c.needsOwnedExceptionReferences()
	if wasmGCBuilt && (c.usesGCStructHelpers() || c.usesGCArrayHelpers() || ownedExceptions) {
		gcSlots, slotErr := gcSyncHostSlotCapacity(c.GCTypeDescs)
		if slotErr != nil {
			return wrapContextError("compiled metadata invalid", slotErr)
		}
		if gcSlots > syncHostSlots {
			syncHostSlots = gcSlots
		}
		if compiledArchitecture == 4 && c.stagedGCArrayProduct() == stagedGCArrayProductGeneric && !(rv64LegacyGCProductsForImage && rv64FixedRootlessGCProduct(c)) {
			syncHostSlots = max(syncHostSlots, rv64GenericArrayFixedSlots)
		}
		internalHelperSlots, slotErr = gcInternalHelperSlotCapacity(c.GCTypeDescs, c.usesGCStructHelpers(), c.usesGCArrayHelpers(), compiledArchitecture == 4 && c.stagedGCArrayProduct() == stagedGCArrayProductGeneric)
		if slotErr != nil {
			return wrapContextError("compiled metadata invalid", slotErr)
		}
	}
	if ownedExceptions {
		internalHelperSlots = max(internalHelperSlots, 1)
	}
	maxParams = max(maxParams, internalHelperSlots, c.opaqueExternHelperSlots())
	if c.syncHostSlots != 0 && int(c.syncHostSlots) != syncHostSlots {
		return fmt.Errorf("compiled metadata invalid: synchronous host slot capacity %d, want %d", c.syncHostSlots, syncHostSlots)
	}
	if syncHostControlForImage && (c.needsPublicFuncrefHostReentry() || c.usesGCStructHelpers() || c.usesGCArrayHelpers() ||
		ownedExceptions || c.usesDynamicFuncRefTest() || c.usesAtomicWaitHelpers() || c.usesGuestGlobalStoreHelper()) {
		hostCallBytes, err = wruntime.HostCtrlFrameBytesForSlots(syncHostSlots)
		if err != nil {
			return wrapContextError("compiled metadata invalid", err)
		}
		c.syncHostSlots = uint16(syncHostSlots)
	}
	need, err := wruntime.InstantiateArenaNeed(wruntime.InstantiateFootprint{
		FuncImportCount:    len(c.Imports),
		HostCallBytes:      hostCallBytes,
		FuncRefCount:       funcRefCount,
		FuncRefTypeIDCount: funcRefTypeIDCount,
		TagCount:           tagCount,
		GlobalCount:        len(c.Globals),
		V128GlobalCount:    v128GlobalCount,
		MemoryCount:        c.memoryCount(),
		HasTable:           c.HasTable,
		TableSize:          tableSize,
		TableCapacity:      tableCapacity,
		TableCapacities:    tableCaps,
		TableEntryBytes:    tableEntryBytes,
		ImportedTableCount: importedTableCount,
		ElemCount:          len(c.Elems),
		PassiveElemCount:   len(c.passiveElems),
		PassiveElemBytes:   passiveElemBytes,
		PassiveDataCount:   len(c.PassiveData),
		MaxParamSlots:      maxParams,
		MaxResultSlots:     maxResults,
		OmitCallBuffers:    !callBuffers,
	})
	if err != nil {
		return wrapContextError("compiled metadata invalid", err)
	}
	if retainedNativeContext {
		if need > maxInt()-railshotNativeInvocationContextBytes {
			return errors.New("compiled metadata invalid: instantiate arena need overflows instance context")
		}
		need += railshotNativeInvocationContextBytes
	}
	c.maxParamSlots = maxParams
	c.maxResultSlots = maxResults
	if c.syncHostSlots == 0 {
		c.syncHostSlots = uint16(syncHostSlots)
	}
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
	return railshotImportedCallSlots(c, params, results)
}

func (c *Compiled) validateGlobalInitExpr(index int, g GlobalDef) error {
	if len(g.InitExpr) == 0 {
		return nil
	}
	gcConstExpr := c.usesGenericGCExecution() || c.stagedGCStructProduct().requiresExternConversion()
	if buildfeatures.GC != 0 && gcConstExpr && (g.Type == ValAnyRef || g.Type == ValI31Ref || (g.Type == ValExternRef && (c.stagedGCStructProduct().requiresExternConversion() || c.compactExternFrameRoots()))) {
		want, err := c.globalExactType(index)
		if err != nil {
			return fmt.Errorf("compiled metadata invalid: global %d GC initializer type: %w", index, err)
		}
		if err := c.validateGCConstExpr(g.InitExpr, want, index); err != nil {
			return fmt.Errorf("compiled metadata invalid: global %d GC initializer: %w", index, err)
		}
		return nil
	}
	want := g.Type
	if want == ValAnyRef {
		// Without collector-backed GC execution, the only non-null value an
		// any/eq global can hold is an i31.
		want = ValI31Ref
	}
	if err := validateCompiledScalarConstExpr(g.InitExpr, want, c.Globals, constExprGlobalScope{context: constExprGlobalInitializer, limit: index}); err != nil {
		return fmt.Errorf("compiled metadata invalid: global %d extended initializer: %w", index, err)
	}
	return nil
}

func (c *Compiled) validateDeferredOffsetGlobal(kind string, seg, idx int) error {
	if idx < 0 || idx >= len(c.Globals) {
		return fmt.Errorf("compiled metadata invalid: %s %d offset global %d out of range", kind, seg, idx)
	}
	g := c.Globals[idx]
	if g.Mutable || g.Type != ValI32 {
		return fmt.Errorf("compiled metadata invalid: %s %d offset global %d must be immutable i32", kind, seg, idx)
	}
	return nil
}

// CodeSize reports the exact native-code byte length. It returns zero after
// the compiled module is closed.
func (c *Compiled) CodeSize() int {
	if c == nil {
		return 0
	}
	c.ensureCodeCache()
	c.codeCache.mu.Lock()
	defer c.codeCache.mu.Unlock()
	if c.codeCache.closed {
		return 0
	}
	return len(c.code)
}

// GCNativeCodeTelemetry returns opt-in per-family WasmGC code attribution for a
// freshly compiled module. The bool is false unless RuntimeConfig enabled
// WithGCCodeTelemetry.
func (c *Compiled) GCNativeCodeTelemetry() (GCNativeCodeTelemetry, bool) {
	if c == nil || !c.hasGCCodeTelemetry {
		return GCNativeCodeTelemetry{}, false
	}
	c.ensureCodeCache()
	c.codeCache.mu.Lock()
	defer c.codeCache.mu.Unlock()
	if c.codeCache.closed {
		return GCNativeCodeTelemetry{}, false
	}
	return c.gcCodeTelemetry, true
}

// WriteCodeTo streams native-code bytes for diagnostics without exposing
// mutable storage. It does not change the image's W^X
// state.
func (c *Compiled) WriteCodeTo(w io.Writer) (int64, error) {
	if c == nil {
		return 0, errors.New("wago: compiled module is nil")
	}
	if w == nil {
		return 0, errors.New("wago: native code writer is nil")
	}
	c.ensureCodeCache()
	c.codeCache.mu.Lock()
	defer c.codeCache.mu.Unlock()
	if c.codeCache.closed {
		return 0, errors.New("wago: compiled module is closed")
	}
	n, err := w.Write(c.code)
	if err != nil {
		return int64(n), err
	}
	if n != len(c.code) {
		return int64(n), io.ErrShortWrite
	}
	return int64(n), nil
}

// Load compiles raw Wasm.
func Load(b []byte) (*Compiled, error) { return Compile(nil, b) }

// Invoke marshals slot-based arguments/results around one native WasmWrapper
// call. The returned slice is backed by an instance-owned buffer and stays valid
// only until the next call on this Instance; copy it if you need to retain it.
// Invoke calls an exported function. Arguments and results are raw uint64 slots
// interpreted per the function's signature (encode/decode scalar slots with
// I32/I64/F32/F64 and AsI32/AsI64/AsF32/AsF64). A v128 occupies two adjacent
// little-endian uint64 slots in the argument and result slices. Public funcref
// slots use opaque store-owned tokens: zero is null, and nonzero tokens are valid
// only in the Runtime store (or standalone private store) that issued them.
// Calls on one Instance are serialized, including while another call is parked
// in a host callback. A host callback that must synchronously re-enter Wasm must
// use InvokeFromHost with the HostModule value it received. Direct invocation
// fails with ErrPermissionDenied while callback-scoped guest storage is borrowed.
func (in *Instance) Invoke(export string, args ...uint64) ([]uint64, error) {
	if codeProfileEnabled && in.boundaryProfile() != nil {
		return in.invokeEntry(export, args, invocationContextSet{})
	}
	if in == nil || in.rt != nil {
		return in.invokeEntry(export, args, invocationContextSet{})
	}
	if !hostFunctionImportsBuilt && in.syncMode {
		return in.invokeEntry(export, args, invocationContextSet{})
	}
	if hostFunctionImportsBuilt && in.syncMode {
		if goruntime.GOARCH == "amd64" || goruntime.GOARCH == "arm64" {
			if out, err, ok := in.tryInvokeCachedHostScalar1(export, args); ok {
				return out, err
			}
		}
		// Preserve AMD64's single-import entry before trying multi-imports.
		// ARM64 benefits from admitting multi-imports before taking that gate.
		if goruntime.GOARCH == "arm64" && len(in.syncHosts) > 1 {
			if out, err, ok := in.tryInvokeCachedMultiNumeric(export, args); ok {
				return out, err
			}
		}
		// Resolve immutable binding eligibility here so the admitted live
		// driver does not need an additional routing frame. AMD64 uses the fresh
		// private integer certificate; mutable ownership is checked in the driver.
		if (goruntime.GOARCH == "arm64" || goruntime.GOARCH == "amd64" && privateNumericLiveRouteEnabled && detachedNumericHostEnabled && integerNumericHostEnabled && in.c.integerHostContextAllowed()) && !codeProfileEnabled && in.boundedHostSegments() && len(in.syncHosts) == 1 &&
			(in.syncHosts[0].scalarKind == syncHostScalar || in.syncHosts[0].privateBoundedTypedHost()) {
			if (goruntime.GOARCH == "amd64" || goruntime.GOARCH == "arm64") && in.syncHosts[0].scalarKind == syncHostTypedI32 && smallTypedEntryEnabled {
				if out, err, ok := in.tryInvokePrivateTypedI32(export, args); ok {
					return out, err
				}
			} else {
				if out, err, ok := in.tryInvokeCachedBoundedNumericLive(export, args); ok {
					return out, err
				}
			}
		} else {
			if out, err, ok := in.tryInvokeCachedBoundedNumeric(export, args); ok {
				return out, err
			}
		}
		if goruntime.GOARCH != "arm64" && len(in.syncHosts) > 1 {
			if out, err, ok := in.tryInvokeCachedMultiNumeric(export, args); ok {
				return out, err
			}
		}
		if in.executionFlags.Load()&executionFlagNativeScalarLeaf == 0 || !in.c.boundedNativeScalarLeaf() {
			return in.invokeEntry(export, args, invocationContextSet{})
		}
	}
	state := in.pluginState.Load()
	if state != nil && state.invokeMu.state.shared == &in.invocationState.word && in.invocationState.word.CompareAndSwap(0, instanceFastAdmission) {
		// The common single-export hit avoids the cache-search call.
		ic := &in.ic[0]
		if !ic.valid || !sameExportName(ic.export, export) {
			ic = in.findInvokeCache(export)
		}
		privateRefStore := in.refStore == nil || in.refStore.private
		isolatedWrapper := ic != nil && invokePrivateEntryEnabled && preparedIsolatedEntryEnabled && ic.entryMode == preparedEntryIsolated
		if ic != nil && (directIntegerForImage && ic.directIntFast || directFloatOrMixedForImage && ic.directFloatFast || isolatedWrapper) && len(args) == int(ic.paramSlots) && privateRefStore && state.guestStorageBorrow.Load() == 0 && in.preparedFastStateValid() {
			var out []uint64
			var err error
			if cachedBoundedI32OneArgForImage && ic.directI32Fast && len(args) == 1 {
				out, err = in.invokeCachedDirectI32ToI32(ic, args[0])
			} else if directIntegerForImage && ic.directIntFast || directFloatOrMixedForImage && ic.directFloatFast {
				entry := ic.directEntry
				if cachedIntOneArgForImage && ic.directIntFast && len(args) == 1 && ic.resultSlots <= 1 {
					out, err = in.invokeCachedDirectInt1(ic, entry, args[0])
				} else {
					out, err = in.invokeCachedDirectNumeric(ic, entry, args)
				}
			} else {
				out, err = in.invokeCachedNumericEntry(export, ic, args, true, false)
			}
			in.endFastInvocation(state)
			return out, err
		}
		in.endFastInvocation(state)
	}
	return in.invokeEntry(export, args, invocationContextSet{})
}

// tryInvokeCachedHostScalar1 reuses the prepared typed-host portal for a warm
// name-based call. Callbacks can publish resources while parked, so this holds
// the ordinary invocation gate rather than a revocation-blocking fast gate.
func (in *Instance) tryInvokeCachedHostScalar1(export string, args []uint64) ([]uint64, error, bool) {
	if len(args) != 1 || len(in.syncHosts) != 1 {
		return nil, nil, false
	}
	// Binding shape is fixed at instantiation. Check it before reserving
	// the invocation so generic callbacks reach their dispatch path directly.
	binding := &in.syncHosts[0]
	kind := binding.scalarKind
	if binding.callbackGate() != nil || kind != syncHostTypedI32 && kind != syncHostTypedI32x2 && kind < syncHostTypedNone {
		return nil, nil, false
	}
	state := in.pluginState.Load()
	if !in.tryBeginOrdinaryInvocation(state) {
		return nil, nil, false
	}
	ic := in.findInvokeCache(export)
	hostCache := state.hostInvokeCache
	privateRefStore := in.refStore == nil || in.refStore.private
	if ic == nil || hostCache == nil || hostCache[ic.slotIndex] == nil || ic.paramSlots != 1 || ic.resultSlots != 1 || ic.scalarWideMask != 0 || ic.scalarResultWide ||
		!privateRefStore || in.guestStorageBorrowed() || !in.preparedFastStateValid() || !in.usesIndependentExecution() ||
		in.gc != nil || in.threadedMemoryZero || in.table != nil || in.importsFuncrefStorage() || len(in.hostLog) != 0 ||
		!in.singleTypedScalarHostEligible() {
		in.endOrdinaryInvocation(state)
		return nil, nil, false
	}
	state.setInvocationIdentity(newInvocationID())
	defer func() {
		state.setInvocationIdentity(0)
		in.endOrdinaryInvocation(state)
	}()
	binary.LittleEndian.PutUint64(in.serArgs, uint64(uint32(args[0])))
	var err error
	if prepared := in.eng.PreparedScalarHost(); (goruntime.GOARCH == "amd64" || goruntime.GOARCH == "arm64") && prepared != nil && prepared.DetachedNumericContext() && ic.boundedNumericHost {
		entry := in.base + uintptr(in.c.Entry[ic.li])
		if in.syncHosts[0].scalarKind == syncHostScalar {
			err = in.callCachedBoundedHostView(entry)
		} else {
			err = in.callCachedBoundedTypedHost(entry)
		}
	} else {
		err = hostCache[ic.slotIndex].callScalarHostPrepared()
	}
	var out []uint64
	if err == nil {
		out = in.resultVals[:1]
		out[0] = uint64(binary.LittleEndian.Uint32(in.results))
	}
	return out, err, true
}

// tryInvokeCachedBoundedNumeric uses a warm export's numeric marshalling plan.
// The module and its sole ordinary Go binding exclude collector references and
// foreign native callees. Keep the ordinary gate: a Caller can publish resources
// while parked, revoking private execution before the next entry.
func (in *Instance) tryInvokeCachedBoundedNumeric(export string, args []uint64) ([]uint64, error, bool) {
	if goruntime.GOARCH == "arm64" {

		if !in.boundedHostSegments() {
			return nil, nil, false
		}
		// Binding metadata is immutable for the lifetime of the instance. Mutable
		// ownership is still checked under the gates in either selected driver.
		if !codeProfileEnabled && (goruntime.GOARCH == "amd64" || goruntime.GOARCH == "arm64") && len(in.syncHosts) == 1 {
			binding := &in.syncHosts[0]
			if binding.scalarKind == syncHostScalar || binding.privateBoundedTypedHost() {
				return in.tryInvokeCachedBoundedNumericLive(export, args)
			}
		}
		return in.tryInvokeCachedBoundedNumericGeneral(export, args)

	}

	if !in.boundedHostSegments() {
		return nil, nil, false
	}
	state := in.pluginState.Load()
	if !in.tryBeginOrdinaryInvocation(state) {
		return nil, nil, false
	}
	ic := in.findInvokeCache(export)
	privateRefStore := in.refStore == nil || in.refStore.private
	if ic == nil || !ic.boundedNumericHost || len(args) != int(ic.paramSlots) ||
		!privateRefStore || in.guestStorageBorrowed() || !in.preparedFastStateValid() || !in.usesIndependentExecution() ||
		in.gc != nil || in.threadedMemoryZero || in.table != nil || in.importsFuncrefStorage() || len(in.hostLog) != 0 {
		in.endOrdinaryInvocation(state)
		return nil, nil, false
	}
	state.invocationID = newInvocationID()
	defer func() {
		state.invocationID = 0
		in.endOrdinaryInvocation(state)
	}()
	// Cached eligibility has already proved the immutable binding's signature
	// and absence of a plugin gate; the checks above revalidate mutable ownership.
	if (goruntime.GOARCH == "amd64" || goruntime.GOARCH == "arm64") && !codeProfileEnabled &&
		in.syncHosts[0].scalarKind == syncHostScalar {
		copyPublicScalarSlotsByClass(nativeUint64Slots(in.serArgs), args, ic.slotWide[:ic.paramSlots], ic.paramWidthClass)
		entry := in.base + uintptr(in.c.Entry[ic.li])
		if err := in.callCachedBoundedHostView(entry); err != nil {
			return nil, err, true
		}
		out := in.resultVals[:ic.resultSlots]
		copyPublicScalarSlotsByClass(out, nativeUint64Slots(in.results), ic.slotWide[ic.paramSlots:], ic.resultWidthClass)
		return out, nil, true
	}
	if (goruntime.GOARCH == "amd64" || goruntime.GOARCH == "arm64") && !codeProfileEnabled &&
		in.syncHosts[0].privateBoundedTypedHost() {
		copyPublicScalarSlotsByClass(nativeUint64Slots(in.serArgs), args, ic.slotWide[:ic.paramSlots], ic.paramWidthClass)
		entry := in.base + uintptr(in.c.Entry[ic.li])
		if err := in.callCachedBoundedTypedHost(entry); err != nil {
			return nil, err, true
		}
		out := in.resultVals[:ic.resultSlots]
		copyPublicScalarSlotsByClass(out, nativeUint64Slots(in.results), ic.slotWide[ic.paramSlots:], ic.resultWidthClass)
		return out, nil, true
	}
	hostScalar := goruntime.GOARCH == "arm64" && in.syncHosts[0].privateBoundedTypedHost()
	out, err := in.invokeCachedNumericEntry(export, ic, args, false, hostScalar)
	return out, err, true

}

func (in *Instance) tryInvokeCachedBoundedNumericLive(export string, args []uint64) (out []uint64, err error, admitted bool) {
	state := in.pluginState.Load()
	if !in.tryBeginOrdinaryInvocation(state) {
		return nil, nil, false
	}
	ic := in.findInvokeCache(export)
	privateRefStore := in.refStore == nil || in.refStore.private
	if ic == nil || !ic.boundedNumericHost || len(args) != int(ic.paramSlots) ||
		!privateRefStore || in.guestStorageBorrowed() || !in.preparedFastStateValid() || !in.usesIndependentExecution() ||
		in.gc != nil || in.threadedMemoryZero || in.table != nil || in.importsFuncrefStorage() || len(in.hostLog) != 0 {
		in.endOrdinaryInvocation(state)
		return nil, nil, false
	}
	state.invocationID = newInvocationID()
	admitted = true
	var locked executionLease
	nativeOwned := false
	defer func() {
		defer func() { state.invocationID = 0; in.endOrdinaryInvocation(state) }()
		if nativeOwned && !locked.privateContext() {
			defer in.unlockNativeEntry(locked)
		}
		if r := recover(); r != nil {
			if !nativeOwned {
				panic(r)
			}
			setNativeSyncPanicError(r, &err)
		}
		if nativeOwned && err != nil {
			err = in.decorateTrap(err)
		}
	}()
	// Cached eligibility has already proved the immutable binding's signature
	// and absence of a plugin gate; the checks above revalidate mutable ownership.
	copyPublicScalarSlotsByClass(nativeUint64Slots(in.serArgs), args, ic.slotWide[:ic.paramSlots], ic.paramWidthClass)
	entry := in.base + uintptr(in.c.Entry[ic.li])
	var reuse bool
	locked, reuse, err = in.beginCachedBoundedViewEntry()
	if err != nil {
		return nil, err, true
	}
	nativeOwned = true
	if hasHostInvocationBindings() {
		restore := bindHostInvocationParent(in, nil)
		defer restore()
	}
	if !reuse {
		in.jm.SetStackFence(in.eng.StackLimit())
		in.jm.SetCustomCtx(offHeapSlicePtr(in.ctrl))
		state.boundedViewVersion = state.nativeContextVersion.Load()
		state.boundedViewMemBase = in.jm.LinMemBase()
	}
	activation := boundedViewHostActivation{
		boundedTypedHostActivation: boundedTypedHostActivation{
			root: in, ctrl: offHeapSlicePtr(in.ctrl), state: state, entryNativeMu: locked.local,
		},
	}
	binding := &in.syncHosts[0]
	rawSlots := uint32(len(binding.sig.Params)) | uint32(len(binding.sig.Results))<<16
	if binding.privateBoundedTypedHost() {
		fixed := wruntime.FixedScalarHostContextCall(boundedTypedHostDispatchI32)
		fallback := wruntime.FixedScalarHostCall(activation.dispatchI32)
		switch binding.scalarKind {
		case syncHostTypedI32x2:
			fixed = boundedTypedHostDispatchI32x2
			fallback = activation.dispatchI32x2
		case syncHostTypedI64:
			fixed = boundedTypedHostDispatchI64
			fallback = activation.dispatchI64
		}
		if prepared := in.eng.PreparedScalarHost(); prepared != nil {
			if locked.privateContext() {
				switch binding.scalarKind {
				case syncHostTypedI32:
					fixed = detachedNumericDispatchI32
				case syncHostTypedI32x2:
					fixed = detachedNumericDispatchI32x2
				case syncHostTypedI64:
					fixed = detachedNumericDispatchI64
				}
			}
			if prepared.IntegerGuestContext() {
				err = prepared.CallInteger(entry, locked.memoryBase(in), unsafe.Pointer(&activation.boundedTypedHostActivation), fixed)
			} else {
				err = prepared.Call(entry, locked.memoryBase(in), unsafe.Pointer(&activation.boundedTypedHostActivation), fixed)
			}
		} else {
			err = in.eng.CallWithHostBaseScalarBoundedContextLive(runtimebridge.GrantHostScalarCall(), entry, in.serArgs, in.jm.LinMemBase(), in.trap, in.results, in.ctrl, rawSlots, unsafe.Pointer(&activation.boundedTypedHostActivation), fixed, fallback)
		}
	} else {
		fixed := wruntime.FixedHostContextCallView(boundedHostDispatchHostCallView)
		fallback := wruntime.FixedHostCallView(activation.dispatchHostCallView)
		_, caller := binding.fn.(CallerHostCallFunc)
		if caller {
			fixed = boundedHostDispatchCallerView
			fallback = activation.dispatchCallerView
		}
		// Both native architectures retain the method adapter for uncached Caller views.
		if prepared := in.eng.PreparedScalarHost(); prepared != nil {
			if locked.privateContext() {
				fixed = detachedNumericDispatchHostCall
				if caller {
					fixed = detachedNumericDispatchCaller
				}
			}
			if prepared.IntegerGuestContext() {
				err = prepared.CallIntegerView(entry, locked.memoryBase(in), unsafe.Pointer(&activation), fixed)
			} else {
				err = prepared.CallView(entry, locked.memoryBase(in), unsafe.Pointer(&activation), fixed)
			}
		} else if caller && (goruntime.GOARCH == "amd64" || goruntime.GOARCH == "arm64") {
			err = in.eng.CallWithHostBaseFixedViewBoundedLive(runtimebridge.GrantHostScalarCall(), entry, in.serArgs, in.jm.LinMemBase(), in.trap, in.results, in.ctrl, rawSlots, fallback)
		} else {
			err = in.eng.CallWithHostBaseFixedViewBoundedContextLive(runtimebridge.GrantHostScalarCall(), entry, in.serArgs, in.jm.LinMemBase(), in.trap, in.results, in.ctrl, rawSlots, unsafe.Pointer(&activation), fixed, fallback)
		}
	}
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	if err != nil {
		return nil, err, true
	}
	out = in.resultVals[:ic.resultSlots]
	copyPublicScalarSlotsByClass(out, nativeUint64Slots(in.results), ic.slotWide[ic.paramSlots:], ic.resultWidthClass)
	return out, nil, true
}

func (in *Instance) tryInvokeCachedBoundedNumericGeneral(export string, args []uint64) ([]uint64, error, bool) {
	if !in.boundedHostSegments() {
		return nil, nil, false
	}
	state := in.pluginState.Load()
	if !in.tryBeginOrdinaryInvocation(state) {
		return nil, nil, false
	}
	ic := in.findInvokeCache(export)
	privateRefStore := in.refStore == nil || in.refStore.private
	if ic == nil || !ic.boundedNumericHost || len(args) != int(ic.paramSlots) ||
		!privateRefStore || in.guestStorageBorrowed() || !in.preparedFastStateValid() || !in.usesIndependentExecution() ||
		in.gc != nil || in.threadedMemoryZero || in.table != nil || in.importsFuncrefStorage() || len(in.hostLog) != 0 {
		in.endOrdinaryInvocation(state)
		return nil, nil, false
	}
	state.invocationID = newInvocationID()
	defer func() {
		state.invocationID = 0
		in.endOrdinaryInvocation(state)
	}()
	// Cached eligibility has already proved the immutable binding's signature
	// and absence of a plugin gate; the checks above revalidate mutable ownership.
	if (goruntime.GOARCH == "amd64" || goruntime.GOARCH == "arm64") && !codeProfileEnabled &&
		in.syncHosts[0].scalarKind == syncHostScalar {
		copyPublicScalarSlotsByClass(nativeUint64Slots(in.serArgs), args, ic.slotWide[:ic.paramSlots], ic.paramWidthClass)
		entry := in.base + uintptr(in.c.Entry[ic.li])
		if err := in.callCachedBoundedHostView(entry); err != nil {
			return nil, err, true
		}
		out := in.resultVals[:ic.resultSlots]
		copyPublicScalarSlotsByClass(out, nativeUint64Slots(in.results), ic.slotWide[ic.paramSlots:], ic.resultWidthClass)
		return out, nil, true
	}
	if (goruntime.GOARCH == "amd64" || goruntime.GOARCH == "arm64") && !codeProfileEnabled &&
		in.syncHosts[0].privateBoundedTypedHost() {
		copyPublicScalarSlotsByClass(nativeUint64Slots(in.serArgs), args, ic.slotWide[:ic.paramSlots], ic.paramWidthClass)
		entry := in.base + uintptr(in.c.Entry[ic.li])
		if err := in.callCachedBoundedTypedHost(entry); err != nil {
			return nil, err, true
		}
		out := in.resultVals[:ic.resultSlots]
		copyPublicScalarSlotsByClass(out, nativeUint64Slots(in.results), ic.slotWide[ic.paramSlots:], ic.resultWidthClass)
		return out, nil, true
	}
	hostScalar := goruntime.GOARCH == "arm64" && in.syncHosts[0].privateBoundedTypedHost()
	out, err := in.invokeCachedNumericEntry(export, ic, args, false, hostScalar)
	return out, err, true
}

// invocationContextSet keeps callback-visible cancellation independent from
// native interruption. Targets without native cancellation still propagate the
// public invocation's cancellation and deadline to synchronous host callbacks.
type invocationContextSet struct {
	interrupt context.Context
	callback  context.Context
}

func (contexts invocationContextSet) admissionContext() context.Context {
	if contexts.interrupt != nil {
		return contexts.interrupt
	}
	return contexts.callback
}

func invocationContextSetFor(ctx context.Context) (contexts invocationContextSet) {
	if ctx == nil || ctx.Done() == nil {
		return contexts
	}
	contexts.callback = ctx
	contexts.interrupt = ctx
	return contexts
}

// InvokeContext is like Invoke but honors context cancellation. If ctx is
// cancelled or its deadline expires while the guest is executing, the call is
// asynchronously interrupted on Linux/amd64 or stopped at the next native
// safepoint on other supported targets, and returns ctx.Err() (e.g.
// context.DeadlineExceeded) instead of blocking on a runaway guest.
//
// Native cancellation is available on amd64/arm64 with standard Go or TinyGo
// using -scheduler=threads. Other TinyGo schedulers reject cancelable native
// calls before entry. On Linux/amd64 with standard Go it uses a
// thread-directed signal and CPU-context rewrite with no generated-code polls.
// Other supported targets use function-entry and loop-header safepoints. A nil
// or already-cancelled ctx is handled up front; a Background context (Done() ==
// nil) keeps Invoke's zero-goroutine fast path.
//
// This is the raw-uint64 sibling of Call, for callers that invoke by slot
// values rather than typed Values (e.g. AssemblyScript rule dispatch).
func (in *Instance) InvokeContext(ctx context.Context, export string, args ...uint64) ([]uint64, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// A context without Done cannot interrupt native execution.
		if ctx.Done() == nil {
			return in.Invoke(export, args...)
		}
	} else {
		return in.Invoke(export, args...)
	}

	contexts := invocationContextSetFor(ctx)
	out, err := in.invokeEntry(export, args, contexts)

	return out, contextInterruptError(ctx, err)
}

func (in *Instance) invokeAdmitted(export string, args []uint64, contexts invocationContextSet, reservation pluginReservation) (result []uint64, resultErr error) {
	if codeProfileEnabled && in.boundaryProfile() != nil {
		span, restore := in.beginProfileInvocation(export)
		defer restore()
		completed := false
		defer finishProfileBoundary(span, &resultErr, &completed)
		state := in.ensurePluginState()
		result, resultErr = in.invokeWithToken(export, args, contexts, state.invocationID, true, true, reservation)
		completed = true
		return result, resultErr
	}
	state := in.ensurePluginState()
	return in.invokeWithToken(export, args, contexts, state.invocationID, true, true, reservation)
}

func (in *Instance) invokeEntry(export string, args []uint64, contexts invocationContextSet) (result []uint64, resultErr error) {
	if in == nil {
		return nil, nilInstanceInvokeError()
	}
	// Close hooks run after the invocation gate is published and may probe that
	// later calls fail closed. Check the gate before waiting for the per-instance
	// serialization lock so such a probe cannot deadlock behind the activation
	// that Close is interrupting. invokeWithToken checks again after locking to
	// close the race with a concurrent Close.
	if in.invocationState.Load()&instanceInvocationClosed != 0 {
		return nil, fmt.Errorf("invoke %q: instance is closed", export)
	}
	if state := in.pluginState.Load(); state != nil && state.guestStorageBorrow.Load() != 0 {
		return nil, fmt.Errorf("invoke %q: guest storage is borrowed: %w", export, ErrPermissionDenied)
	}
	state, err := in.lockInvocationContext(contexts.admissionContext(), 0)
	if err != nil {
		return nil, err
	}
	admittedHere := false
	var cleanup resourceCleanup
	cleanup.run(func() {
		result, resultErr = in.invokeEntryLocked(export, args, contexts, state, &admittedHere)
	})
	if admittedHere {
		cleanup.run(in.endInvocation)
	}
	cleanup.run(state.unlockInvocation)
	cleanup.finish()
	return result, resultErr
}

// invokeEntryLocked runs with the ordinary invocation gate and identity held.
// The caller journals this body with lifetime and gate release so a body or
// finalization panic cannot replace an earlier panic or strand the gate.
func (in *Instance) invokeEntryLocked(export string, args []uint64, contexts invocationContextSet, state *instancePluginState, admittedHere *bool) (result []uint64, resultErr error) {
	if codeProfileEnabled && in.boundaryProfile() != nil {
		if err := in.beginInvocation(); err != nil {
			return nil, fmt.Errorf("invoke %q: %w", export, err)
		}
		*admittedHere = true
		span, restore := in.beginProfileInvocation(export)
		defer restore()
		completed := false
		defer finishProfileBoundary(span, &resultErr, &completed)
		result, resultErr = in.invokeEntryLockedBody(export, args, contexts, true, state, admittedHere)
		completed = true
		return result, resultErr
	}
	return in.invokeEntryLockedBody(export, args, contexts, false, state, admittedHere)
}

func (in *Instance) invokeEntryLockedBody(export string, args []uint64, contexts invocationContextSet, alreadyAdmitted bool, state *instancePluginState, admittedHere *bool) (result []uint64, resultErr error) {
	privateRefStore := in.refStore == nil || in.refStore.private
	if !alreadyAdmitted && contexts.interrupt == nil && contexts.callback == nil && privateRefStore {
		// Admit before reading cached or compiled entry metadata. Close publishes
		// its gate without taking invokeMu, so the invocation count is what keeps
		// those resources live through this fast-path probe and native entry.
		if err := in.beginInvocation(); err != nil {
			return nil, fmt.Errorf("invoke %q: %w", export, err)
		}
		*admittedHere = true
		alreadyAdmitted = true
		ic := in.findInvokeCache(export)
		if ic == nil {
			var err error
			ic, err = in.fillInvokeCache(export)
			if err != nil {
				return nil, err
			}
		}
		// Cross-instance attachment can make native control shared or publish an
		// imported GC invocation domain after immutable export metadata was cached.
		// Keep both dynamic security bits on the entry boundary and fall back to
		// the audited rebinding and topology-lease route.
		executionFlags := in.executionFlags.Load()
		const directBlocked = preparedFastBlocked
		if (directIntegerForImage && ic.directIntFast || directFloatOrMixedForImage && ic.directFloatFast) && executionFlags&directBlocked == 0 && in.lockPreparedFastState() {
			defer in.unlockPreparedFastState()
			if len(args) != int(ic.paramSlots) {
				return nil, fmt.Errorf("%s expects %d arg slot(s), got %d", export, ic.paramSlots, len(args))
			}
			directEntry := in.base + uintptr(internalEntryOffset(in.c.InternalEntry[ic.li]))
			return in.invokeCachedDirectNumeric(ic, directEntry, args)
		}
		privateScalar := invokePrivateEntryEnabled && ic.entryMode != preparedEntryGeneral && executionFlags&directBlocked == 0
		hostScalar := hostFunctionImportsBuilt && !privateScalar && ic.li >= 0 && in.syncMode && !in.threadedMemoryZero && in.usesIndependentExecution() &&
			(in.hasSingleDirectTypedScalarHost() || in.hasSingleExpandedTypedScalarHost()) && !ic.hasFuncRefParams && !ic.hasFuncRefResults &&
			in.table == nil && !in.importsFuncrefStorage() && len(in.hostLog) == 0
		if privateScalar || hostScalar {
			var out []uint64
			var err error
			var cleanup resourceCleanup
			cleanup.run(func() {
				out, err = in.invokeCachedNumericEntry(export, ic, args, false, hostScalar)
			})
			if hostScalar && in.table != nil {
				cleanup.run(in.reconcileFuncrefRoots)
			}
			cleanup.finish()
			return out, err
		}
	}
	return in.invokeWithToken(export, args, contexts, state.invocationID, true, alreadyAdmitted, noPluginReservation())
}

// invokeCachedDirectNumeric requires an invocation lifetime lease, the
// serialized invocation gate, and a valid isolated-entry proof.
func (in *Instance) invokeCachedDirectNumeric(ic *invokeCache, entry uintptr, args []uint64) ([]uint64, error) {
	if directFloatOrMixedForImage && ic.directFloatFast {
		if directMixedForImage && (!directFloatForImage || ic.scalarWideMask&directMixedEnabled != 0) {
			return in.invokeDirectMixedEntry(entry, ic.scalarWideMask, ic.slotWide[:ic.paramSlots], ic.slotWide[ic.paramSlots:], args)
		}
		if directFloatForImage {
			return in.invokeDirectFloatEntry(entry, ic.slotWide[:ic.paramSlots], ic.slotWide[ic.paramSlots:], args)
		}
	}
	if directWideArityForImage && (len(args) > 4 || ic.resultSlots > 2) {
		return in.invokeDirectIntWideEntry(entry, ic.slotWide[:ic.paramSlots], ic.slotWide[ic.paramSlots:], args)
	}
	if directPairResultsForImage && ic.resultSlots == 2 {
		return in.invokeDirectIntPairEntry(entry, ic.scalarWideMask, ic.slotWide[ic.paramSlots:], args)
	}
	if directIntFixedForImage {
		if len(args) > maxFixedDirectArgsForImage {
			return nil, fmt.Errorf("wago: direct integer entry has %d argument slots", len(args))
		}
		a0, a1, a2, a3 := fixedDirectArguments(args)
		return in.invokeDirectIntEntry(entry, int(ic.paramSlots), int(ic.resultSlots), ic.scalarWideMask, ic.scalarResultWide, true, ic.directIntLight, ic.directIntBounded, a0, a1, a2, a3)
	}
	return nil, errors.New("wago: direct integer entry exceeds the standalone runtime profile")
}

// reserved means the isolated gate and invocation lease are both held.
func (in *Instance) invokeCachedNumericEntry(export string, ic *invokeCache, args []uint64, reserved, hostScalar bool) ([]uint64, error) {
	if guardedGrowthForImage && guardedMemoryGrowEntry(in.c.InternalEntry[ic.li]) {
		defer in.commitGuardedGrowth()
	}
	if len(args) != int(ic.paramSlots) {
		return nil, fmt.Errorf("%s expects %d arg slot(s), got %d", export, ic.paramSlots, len(args))
	}
	if len(args) > len(in.serArgs)/8 {
		return nil, fmt.Errorf("%s requires %d arg slot(s), instance buffer has %d", export, len(args), len(in.serArgs)/8)
	}
	if int(ic.resultSlots) > len(in.results)/8 {
		return nil, fmt.Errorf("%s requires %d result slot(s), instance buffer has %d", export, ic.resultSlots, len(in.results)/8)
	}
	copyPublicScalarSlotsByClass(nativeUint64Slots(in.serArgs), args, ic.slotWide[:ic.paramSlots], ic.paramWidthClass)
	if len(in.hostLog) > 0 {
		binary.LittleEndian.PutUint32(in.hostLog, 0)
	}
	entry := in.base + uintptr(in.c.Entry[ic.li])
	var err error
	if hostFunctionImportsBuilt && hostScalar && (goruntime.GOARCH == "amd64" || goruntime.GOARCH == "arm64") {
		state := in.ensurePluginState()
		if state.hostInvokeCache == nil {
			state.hostInvokeCache = make([]*WasmFunc, in.invokeCacheSlotCount())
		}
		fn := state.hostInvokeCache[ic.slotIndex]
		if fn == nil {
			fn = &WasmFunc{in: in, export: export, entry: entry}
			state.hostInvokeCache[ic.slotIndex] = fn
		}
		err = fn.callScalarHostPrepared()
	} else if syncHostControlForImage && in.syncMode && ic.entryMode == preparedEntryIsolated && ic.boundedWrapper {
		err = in.callPreparedIsolated(entry, in.trap, reserved, true)
	} else if syncHostControlForImage && in.syncMode {
		err = in.callNativeSyncWithTrapContext(entry, in.trap, nil)
	} else if reserved || ic.entryMode == preparedEntryIsolated && preparedIsolatedEntryEnabled {
		err = in.callPreparedIsolated(entry, in.trap, reserved, ic.boundedWrapper)
	} else {
		err = in.callPreparedPrivate(entry, in.trap)
	}
	if err != nil {
		return nil, err
	}
	if hostFunctionImportsBuilt && len(in.hostLog) != 0 {
		if err := in.replayHostLog(); err != nil {
			return nil, err
		}
	}
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	out := in.resultVals[:ic.resultSlots]
	copyPublicScalarSlotsByClass(out, nativeUint64Slots(in.results), ic.slotWide[ic.paramSlots:], ic.resultWidthClass)
	return out, nil
}

//go:noinline
func nilInstanceInvokeError() error {
	return errors.New("instance is nil")
}

func (in *Instance) invokeWithToken(export string, args []uint64, contexts invocationContextSet, id invocationID, gateHeld, alreadyAdmitted bool, reservation pluginReservation) (result []uint64, resultErr error) {
	var state *instancePluginState
	gateOwned := false
	admittedHere := false
	var cleanup resourceCleanup
	cleanup.run(func() {
		ctx := contexts.admissionContext()
		reentry := hostFunctionImportsBuilt && !gateHeld && isNativeActive(in, id) && railshotCanReenter(in, id)
		if !reentry && !gateHeld {
			// Acquire the target instance gate before the shared collector lease. A
			// parked same-domain callback must be able to reacquire the collector and
			// finish releasing this gate while a second callback waits to enter.
			var err error
			state, err = in.lockInvocationContext(ctx, id)
			if err != nil {
				resultErr = err
				return
			}
			gateOwned = true
		}
		// Cancellation may arrive while this call waits for the instance gate.
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				resultErr = err
				return
			}
		}
		if !alreadyAdmitted {
			if err := in.beginInvocation(); err != nil {
				resultErr = fmt.Errorf("invoke %q: %w", export, err)
				return
			}
			admittedHere = true
		}
		var gcLease gcInvocationLease
		if ctx == nil {
			gcLease = in.lockGCInvocation(id)
		} else {
			var err error
			gcLease, err = in.lockGCInvocationContext(ctx, id)
			if err != nil {
				resultErr = err
				return
			}
		}
		result, resultErr = in.invokeWithTokenLeased(export, args, contexts, id, gateHeld, reservation, reentry, gcLease)
	})
	if admittedHere {
		cleanup.run(in.endInvocation)
	}
	if gateOwned {
		cleanup.run(state.unlockInvocation)
	}
	cleanup.finish()
	return result, resultErr
}

// invokeWithTokenLeased owns the collector lease through every body defer and
// reference reconciliation. Each consumed unit remains independently releasable
// after an earlier body or cleanup panic.
func (in *Instance) invokeWithTokenLeased(export string, args []uint64, contexts invocationContextSet, id invocationID, gateHeld bool, reservation pluginReservation, reentry bool, gcLease gcInvocationLease) (result []uint64, resultErr error) {
	var reconcileAttached *Instance
	var restoreNativeImports func()
	var cleanup resourceCleanup
	cleanup.run(func() {
		result, resultErr = in.invokeWithTokenAdmitted(export, args, contexts, id, gateHeld, reservation, reentry, &restoreNativeImports, &reconcileAttached)
	})
	if restoreNativeImports != nil {
		cleanup.run(restoreNativeImports)
	}
	gcLease.release(&cleanup)
	if in.importsFuncrefStorage() || in.table != nil {
		cleanup.run(in.reconcileFuncrefRoots)
	}
	if functionReexportsForImage && reconcileAttached != nil && (reconcileAttached.importsFuncrefStorage() || reconcileAttached.table != nil) {
		cleanup.run(reconcileAttached.reconcileFuncrefRoots)
	}
	cleanup.finish()
	return result, resultErr
}

func (in *Instance) invokeWithTokenAdmitted(export string, args []uint64, contexts invocationContextSet, id invocationID, gateHeld bool, reservation pluginReservation, reentry bool, restoreNativeImports *func(), reconcileAttached **Instance) (result []uint64, resultErr error) {
	if !exportReferenceInvocationForImage {
		return in.invokeWithTokenAdmittedBody(export, args, contexts, id, gateHeld, reservation, reentry, restoreNativeImports, reconcileAttached, nil)
	}
	var argumentOwners railshotPublicFuncrefArguments
	var cleanup resourceCleanup
	cleanup.run(func() {
		result, resultErr = in.invokeWithTokenAdmittedBody(export, args, contexts, id, gateHeld, reservation, reentry, restoreNativeImports, reconcileAttached, &argumentOwners)
	})
	cleanup.run(func() { railshotReleasePublicFuncrefArguments(argumentOwners) })
	cleanup.finish()
	return result, resultErr
}

func (in *Instance) invokeWithTokenAdmittedBody(export string, args []uint64, contexts invocationContextSet, id invocationID, gateHeld bool, reservation pluginReservation, reentry bool, restoreNativeImports *func(), reconcileAttached **Instance, argumentOwners *railshotPublicFuncrefArguments) ([]uint64, error) {
	if hostFunctionImportsBuilt && syncHostControlForImage && reentry {
		restore, err := in.prepareHostReentryState()
		if err != nil {
			return nil, err
		}
		defer restore()
	}
	if hostFunctionImportsBuilt && !gateHeld {
		// Only a host-authorized entry shares an already active invocation ID.
		// Ordinary entries hold their own gate and cannot form a parked cycle.
		if restore, err := in.prepareNativeImportReentries(id); err != nil {
			return nil, err
		} else {
			*restoreNativeImports = restore
		}
	}
	// Ordinary Invoke and hook-free Call entries carry no reservation and start
	// with a fresh activation, so there is no map state to clear or restore.
	if hasPluginReservation(reservation) {
		previousReservation := in.swapInvocationReservation(reservation)
		defer in.swapInvocationReservation(previousReservation)
	}
	if sharedMemoryRuntimeBuilt {
		if threadedMu := in.lockThreadedInstanceState(); threadedMu != nil {
			defer threadedMu.Unlock()
		}
	}
	ic := in.findInvokeCache(export)
	if ic == nil {
		var err error
		ic, err = in.fillInvokeCache(export)
		if err != nil {
			return nil, err
		}
	}
	if standaloneBareImage && referenceSignaturesBuilt && referenceSignatureTypesForImage && (ic.hasFuncRefParams || ic.hasFuncRefResults) {
		return nil, fmt.Errorf("invoke %q: standalone export requires a scalar signature", export)
	}
	li := ic.li
	if li < 0 {
		importIdx := -li - 1
		if !functionReexportsForImage || importIdx < 0 || importIdx >= len(in.c.Imports) {
			return nil, fmt.Errorf("export %q imported function index %d has no binding", export, importIdx)
		}
		if ex, ok := railshotNativeFunctionAttachment(in, importIdx); ok && ex != nil && ex.inst != nil {
			*reconcileAttached = ex.inst
			if out, handled, err := railshotInvokeNativeReexport(in, export, importIdx, args, contexts); handled {
				return out, err
			}
			// Native cross-instance calls carry only the caller's invocation lease and
			// trap cell; the import attachment retains the producer's physical resources.
			// Keep this Go-level re-export path identical so producer Close neither owns
			// nor strands an invocation initiated through the relay.
			return ex.inst.invokeAttachedLocalContext(ex.localIdx, args, contexts, in.trap, true)
		}
		return in.invokeReexportedHost(export, importIdx, args, id, contexts.callback)
	}
	if guardedGrowthForImage && guardedMemoryGrowEntry(in.c.InternalEntry[li]) {
		defer in.commitGuardedGrowth()
	}
	if len(args) != int(ic.paramSlots) {
		return nil, fmt.Errorf("%s expects %d arg slot(s), got %d", export, ic.paramSlots, len(args))
	}
	if len(args) > len(in.serArgs)/8 {
		return nil, fmt.Errorf("%s requires %d arg slot(s), instance buffer has %d", export, len(args), len(in.serArgs)/8)
	}
	if int(ic.resultSlots) > len(in.results)/8 {
		return nil, fmt.Errorf("%s requires %d result slot(s), instance buffer has %d", export, ic.resultSlots, len(in.results)/8)
	}
	if err := in.collectGenericGCAtBoundary(); err != nil {
		return nil, fmt.Errorf("%s GC boundary collection: %w", export, err)
	}
	if exportReferenceInvocationForImage && ic.hasFuncRefParams {
		if collectorObjectTokensBuilt {
			defer in.clearGCRefArgumentRoots()
		}
		var err error
		if *argumentOwners, err = railshotBeginPublicFuncrefArguments(in); err != nil {
			return nil, fmt.Errorf("%s reference argument owners: %w", export, err)
		}
		params, _, err := exactFuncSignatureView(in.c.Funcs[li], in.c.Types)
		if err != nil {
			return nil, fmt.Errorf("%s exact parameters: %w", export, err)
		}
		if err := in.marshalPublicReferenceArgs(export, args, in.c.Funcs[li].Params, params, argumentOwners); err != nil {
			return nil, err
		}
	} else {
		marshalPublicScalarArgs(in.serArgs, args, in.c.Funcs[li].Params)
	}
	if len(in.hostLog) > 0 {
		binary.LittleEndian.PutUint32(in.hostLog, 0) // reset host-call log
	}
	entry := in.base + uintptr(in.c.Entry[li])
	if !standaloneBareImage && contexts.interrupt != nil {
		stopCancel, err := in.startCancellationWatch(contexts.interrupt, in.trap)
		if err != nil {
			return nil, err
		}
		defer stopCancel()
	}
	var executionErr error
	if syncHostControlForImage && in.syncMode {
		if hostFunctionImportsBuilt && contexts.interrupt != nil && in.contextBoundedTypedI64Eligible(ic) {
			// Admission, ownership, and the cancellation watcher above are shared
			// with the general path. Only the final native driver changes.
			executionErr = in.callCachedBoundedTypedHostContext(entry, contexts.callback)
		} else {
			executionErr = in.callNativeSyncWithTrapContext(entry, in.trap, contexts.callback)
		}
	} else {
		privateScalar := invokePrivateEntryEnabled && contexts.interrupt == nil && !in.nativeControlIsShared() &&
			ic.entryMode != preparedEntryGeneral
		entryMode := preparedEntryGeneral
		if privateScalar {
			entryMode = ic.entryMode
		}
		if entryMode == preparedEntryIsolated && preparedIsolatedEntryEnabled {
			executionErr = in.callPreparedIsolated(entry, in.trap, false, ic.boundedWrapper)
		} else if entryMode != preparedEntryGeneral {
			executionErr = in.callPreparedPrivate(entry, in.trap)
		} else {
			executionErr = in.callNativeAsync(entry, false)
		}
		if hostFunctionImportsBuilt && executionErr == nil {
			executionErr = in.replayHostLog()
		}
	}
	if err := in.reconcileGCGlobalRootsAfter(executionErr); err != nil {
		return nil, err
	}
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	out := in.resultVals[:ic.resultSlots]
	for i, wide := range ic.slotWide[ic.paramSlots:] {
		off := i * 8
		if off+8 > len(in.results) {
			return nil, fmt.Errorf("%s result slot %d exceeds instance result buffer", export, i)
		}
		if wideInvocationSlotsForImage && wide { // i64/f64 or one half of a v128
			out[i] = binary.LittleEndian.Uint64(in.results[off:])
		} else { // i32 / f32 (4-byte)
			out[i] = uint64(binary.LittleEndian.Uint32(in.results[off:]))
		}
	}
	if exportReferenceInvocationForImage && ic.hasFuncRefResults {
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

// contextBoundedTypedI64Eligible keeps cancelable entry on the ordinary gate
// and watcher while using the proven private integer host bridge. Atomic wait
// helpers retain the general driver's separate parent-context publication.
func (in *Instance) contextBoundedTypedI64Eligible(ic *invokeCache) bool {
	if ic == nil || ic.li < 0 || !ic.boundedNumericHost || in.c.usesAtomicWaitHelpers() || !in.preparedFastStateValid() ||
		!in.preparedBoundedNumericEligible() || !in.syncMode || len(in.syncHosts) != 1 ||
		in.syncHosts[0].scalarKind != syncHostTypedI64 || !in.syncHosts[0].privateBoundedTypedHost() {
		return false
	}
	prepared := in.eng.PreparedScalarHost()
	return prepared != nil && prepared.DetachedNumericContext() && prepared.IntegerGuestContext()
}

// invokeLocal calls this instance's local function `li` directly (bypassing the
// export-name cache). Used to call through a re-exported import into the instance
// that satisfies it. activeTrap is the outer caller's invocation cell, matching
// the native dynamic-call ABI so closing a re-exporting caller interrupts code
// executing in the producer. It shares the instance's call buffers, so the
// returned slice is valid only until the next call on this Instance.
func (in *Instance) invokeLocal(li int, args []uint64) ([]uint64, error) {
	return in.invokeLocalContext(li, args, invocationContextSet{}, in.trap)
}

func (in *Instance) invokeLocalContext(li int, args []uint64, contexts invocationContextSet, activeTrap []byte) (result []uint64, resultErr error) {
	if err := in.beginInvocation(); err != nil {
		return nil, fmt.Errorf("invoke function %d: %w", li, err)
	}
	var gcLease gcInvocationLease
	leaseOwned := false
	var cleanup resourceCleanup
	cleanup.run(func() {
		var err error
		gcLease, err = in.lockGCInvocationContext(contexts.admissionContext(), in.currentInvocationID())
		if err != nil {
			resultErr = err
			return
		}
		leaseOwned = true
		result, resultErr = in.invokeAttachedLocalContext(li, args, contexts, activeTrap, false)
	})
	if leaseOwned {
		gcLease.release(&cleanup)
		if in.importsFuncrefStorage() || in.table != nil {
			cleanup.run(in.reconcileFuncrefRoots)
		}
	}
	cleanup.run(in.endInvocation)
	cleanup.finish()
	return result, resultErr
}

// invokeAttachedLocalContext enters a producer retained by the caller's function
// import attachment. The caller owns the invocation lease and active trap cell,
// exactly as for a native dynamic import call. attachedResult permits first-time
// funcref tokenization to transfer the attachment's finalization lifetime.
func (in *Instance) invokeAttachedLocalContext(li int, args []uint64, contexts invocationContextSet, activeTrap []byte, attachedResult bool) (result []uint64, resultErr error) {
	if !referenceSignaturesBuilt {
		return in.invokeAttachedLocalContextBody(li, args, contexts, activeTrap, attachedResult, nil)
	}
	var argumentOwners railshotPublicFuncrefArguments
	var cleanup resourceCleanup
	cleanup.run(func() {
		result, resultErr = in.invokeAttachedLocalContextBody(li, args, contexts, activeTrap, attachedResult, &argumentOwners)
	})
	cleanup.run(func() { railshotReleasePublicFuncrefArguments(argumentOwners) })
	cleanup.finish()
	return result, resultErr
}

func (in *Instance) invokeAttachedLocalContextBody(li int, args []uint64, contexts invocationContextSet, activeTrap []byte, attachedResult bool, argumentOwners *railshotPublicFuncrefArguments) ([]uint64, error) {
	if li < 0 || li >= len(in.c.Funcs) || li >= len(in.c.Entry) {
		return nil, fmt.Errorf("invalid function index %d", li)
	}
	if guardedGrowthForImage && guardedMemoryGrowEntry(in.c.InternalEntry[li]) {
		defer in.commitGuardedGrowth()
	}
	sig := in.c.Funcs[li]
	paramSlots, err := valTypesSlots(sig.Params)
	if err != nil {
		return nil, wrapContextError("function parameter slots", err)
	}
	resultSlots, err := valTypesSlots(sig.Results)
	if err != nil {
		return nil, wrapContextError("function result slots", err)
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
	if err := in.collectGenericGCAtBoundary(); err != nil {
		return nil, wrapContextError("function GC boundary collection", err)
	}
	if referenceSignaturesBuilt && hasReferenceValType(sig.Params) {
		if collectorObjectTokensBuilt {
			defer in.clearGCRefArgumentRoots()
		}
		var err error
		if *argumentOwners, err = railshotBeginPublicFuncrefArguments(in); err != nil {
			return nil, wrapContextError("function reference argument owners", err)
		}
		params, _, err := exactFuncSignatureView(sig, in.c.Types)
		if err != nil {
			return nil, wrapContextError("function exact parameters", err)
		}
		if err := in.marshalPublicReferenceArgs("function", args, sig.Params, params, argumentOwners); err != nil {
			return nil, err
		}
	} else {
		marshalPublicScalarArgs(in.serArgs, args, sig.Params)
	}
	if len(in.hostLog) > 0 {
		binary.LittleEndian.PutUint32(in.hostLog, 0)
	}
	entry := in.base + uintptr(in.c.Entry[li])
	if len(activeTrap) < 4 {
		activeTrap = in.trap
	}
	if !standaloneBareImage && contexts.interrupt != nil {
		stopCancel, err := in.startCancellationWatch(contexts.interrupt, activeTrap)
		if err != nil {
			return nil, err
		}
		defer stopCancel()
	}
	var executionErr error
	if syncHostControlForImage && in.syncMode {
		executionErr = in.callNativeSyncWithTrapContext(entry, activeTrap, contexts.callback)
	} else {
		executionErr = in.callNativeAsyncWithTrap(entry, false, activeTrap)
		if hostFunctionImportsBuilt && executionErr == nil {
			executionErr = in.replayHostLog()
		}
	}
	if err := in.reconcileGCGlobalRootsAfter(executionErr); err != nil {
		return nil, err
	}
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	out := in.resultVals[:resultSlots]
	resSlot := 0
	for _, rt := range sig.Results {
		if vectorInvocationForImage && rt == ValV128 {
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
		if wideInvocationSlotsForImage && isWideValType(rt) {
			out[resSlot] = binary.LittleEndian.Uint64(in.results[off:])
		} else {
			out[resSlot] = uint64(binary.LittleEndian.Uint32(in.results[off:]))
		}
		resSlot++
	}
	if referenceSignaturesBuilt && hasReferenceValType(sig.Results) {
		_, results, err := exactFuncSignatureView(sig, in.c.Types)
		if err != nil {
			return nil, wrapContextError("function exact results", err)
		}
		if err := in.translatePublicReferenceResultsMode("function", out, sig.Results, results, attachedResult); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// nativeCancellationSupported reports whether this target has either
// signal/context interruption or compiler-emitted cancellation polls.
func nativeCancellationSupported() bool {
	return (goruntime.GOARCH == "amd64" || goruntime.GOARCH == "arm64" || (goruntime.GOOS == "linux" && goruntime.GOARCH == "riscv64")) && wruntime.NativeCancellationSchedulerAvailable()
}

// startCancellationWatch arms the native safepoints for a high-level
// context-aware Call. Background contexts keep the zero-goroutine fast path.
func noOpCancellationWatch() {}

func (in *Instance) startCancellationWatch(cancel context.Context, activeTrap []byte) (func(), error) {
	if cancel == nil || cancel.Done() == nil || len(activeTrap) < 4 {
		return noOpCancellationWatch, nil
	}
	if !nativeCancellationSupported() {
		return nil, errors.New("wago: native context cancellation requires a concurrent scheduler (TinyGo: -scheduler=threads)")
	}
	const (
		watchStopped  = uint32(1)
		watchFinished = uint32(2)
	)
	var watchState atomic.Uint32
	trap := (*uint32)(unsafe.Pointer(&activeTrap[0]))
	clearDeadline := noOpCancellationWatch
	if deadline, ok := cancel.Deadline(); ok {
		var err error
		clearDeadline, err = wruntime.SetInterruptDeadline(activeTrap, deadline)
		if err != nil {
			return nil, err
		}
	}
	stopCallback := context.AfterFunc(cancel, func() {
		defer watchState.Add(watchFinished)
		// One asynchronous owner keeps the authenticated request token valid
		// across queued deliveries. Stop it before the trap buffer is released.
		stopInterrupt := wruntime.RequestInterruptAsync(activeTrap)
		defer stopInterrupt()
		retry := time.NewTicker(50 * time.Microsecond)
		defer retry.Stop()
		for watchState.Load()&watchStopped == 0 {
			// Cooperative hosts can overwrite the one-shot trap while publishing
			// a host call. Restore cancellation until this invocation stops.
			if !wruntime.HostInterruptSupported() && atomic.LoadUint32(trap) != uint32(wruntime.TrapInterrupted) {
				wruntime.RequestInterrupt(activeTrap)
			}
			<-retry.C
		}

	})
	return func() {
		for {
			state := watchState.Load()
			if state&watchStopped != 0 {
				return
			}
			if watchState.CompareAndSwap(state, state|watchStopped) {
				break
			}
		}
		clearDeadline()
		if !stopCallback() {
			// Cancellation already started its callback. It observes the stop bit
			// no later than the next retry tick; wait for it before clearing the trap.
			for watchState.Load()&watchFinished == 0 {
				goruntime.Gosched()
			}
		}
		atomic.CompareAndSwapUint32(trap, uint32(wruntime.TrapInterrupted), 0)
	}, nil
}

// replayHostLog delivers the deferred events the last native call logged. Each
// entry carries its import index and one i32 payload.
func (in *Instance) replayHostLog() (err error) {
	if !hostFunctionImportsBuilt || len(in.hostLog) == 0 {
		return nil
	}
	// A replayed host call may panic HostExit or HostTrap to end execution;
	// recover both exactly like the synchronous path.
	completed := false
	defer func() {
		r := recover()
		if !completed || r != nil {
			switch trap := r.(type) {
			case HostTrap:
				if trap.Err == nil {
					err = errors.New("wago: host trapped without an error")
				} else {
					err = trap.Err
				}
				return
			case *HostTrap:
				if trap == nil || trap.Err == nil {
					err = errors.New("wago: host trapped without an error")
				} else {
					err = trap.Err
				}
				return
			}
			if ex, ok := r.(HostExit); ok {
				err = &ExitError{Code: ex.Code}
				return
			}
			if ex, ok := r.(*HostExit); ok && ex != nil {
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
	for i := uint32(0); i < n; i++ {
		off := 8 + i*8
		imp := binary.LittleEndian.Uint32(in.hostLog[off:])
		arg := int32(binary.LittleEndian.Uint32(in.hostLog[off+4:]))
		if in.hostEvents != nil && int(imp) < len(in.hostEvents.byImport) {
			binding := &in.hostEvents.byImport[imp]
			if binding.eventI32 != nil {
				if gate := binding.callbackGate(); pluginRuntimeBuilt && gate != nil {
					if err := gate.enter(); err != nil {
						panic(HostTrap{Err: err})
					}
					func() {
						defer gate.release()
						binding.eventI32(arg)
					}()
				} else {
					binding.eventI32(arg)
				}
				continue
			}
		}
	}
	completed = true
	return nil
}

func (in *Instance) invokeReexportedHost(export string, importIdx int, args []uint64, id invocationID, parent context.Context) (results []uint64, err error) {
	if importIdx < 0 || importIdx >= len(in.syncHosts) || !in.syncHosts[importIdx].callable() || importIdx >= len(in.c.importFuncSigs) {
		return nil, fmt.Errorf("export %q is an imported function without a callable host owner", export)
	}
	sig := in.c.importFuncSigs[importIdx]
	for _, typ := range sig.Params {
		if isReferenceValType(typ) {
			return nil, fmt.Errorf("export %q re-exported host reference parameters require an owned host funcref", export)
		}
	}
	for _, typ := range sig.Results {
		if isReferenceValType(typ) {
			return nil, fmt.Errorf("export %q re-exported host reference results require an owned host funcref", export)
		}
	}
	paramSlots, slotErr := valTypesSlots(sig.Params)
	if slotErr != nil {
		return nil, fmt.Errorf("%s parameter slots: %w", export, slotErr)
	}
	resultSlots, slotErr := valTypesSlots(sig.Results)
	if slotErr != nil {
		return nil, fmt.Errorf("%s result slots: %w", export, slotErr)
	}
	if len(args) != paramSlots {
		return nil, fmt.Errorf("%s expects %d arg slot(s), got %d", export, paramSlots, len(args))
	}
	params := append([]uint64(nil), args...)
	results = make([]uint64, resultSlots)
	returned := false
	defer func() {
		recovered := recover()
		if !returned || recovered != nil {
			switch value := recovered.(type) {
			case HostTrap:
				if value.Err == nil {
					err = errors.New("wago: host trapped without an error")
				} else {
					err = value.Err
				}
				results = nil
			case *HostTrap:
				if value == nil || value.Err == nil {
					err = errors.New("wago: host trapped without an error")
				} else {
					err = value.Err
				}
				results = nil
			case HostExit:
				err = &ExitError{Code: value.Code}
				results = nil
			case *HostExit:
				if value == nil {
					panic(recovered)
				}
				err = &ExitError{Code: value.Code}
				results = nil
			default:
				panic(recovered)
			}
		}
	}()
	// This direct Go-level path does not pass through dispatchSynchronousHostCall,
	// but the imported function is still arbitrary host code. Release the shared
	// collector lease after scalar-only validation so same-domain InvokeFromHost
	// and CollectGC calls can proceed, then restore it before returning results to
	// the public invocation boundary.
	if nativeCollectorFrameRootsBuilt {
		gcSuspension := in.suspendGCInvocation(id)
		defer gcSuspension.resume()
	}
	restoreInvocationContext := bindHostInvocationParent(in, parent)
	defer restoreInvocationContext()
	if codeProfileEnabled && activeHostInvocationContext(in).profileSession() != nil {
		span, restore := in.beginProfileHostCallback(importIdx)
		defer restore()
		completed := false
		defer finishProfileBoundary(span, &err, &completed)
		fn := &in.syncHosts[importIdx]
		caller := in.ensurePluginState().hostScope.beginReservedWithID(in, id, currentInvocationReservation(in), parent)
		defer caller.scope.end(caller.generation, caller.parentGeneration)
		fn.call(caller, params, results)
		completed = true
		returned = true
		return results, nil
	}
	fn := &in.syncHosts[importIdx]
	caller := in.ensurePluginState().hostScope.beginReservedWithID(in, id, currentInvocationReservation(in), parent)
	defer caller.scope.end(caller.generation, caller.parentGeneration)
	fn.call(caller, params, results)
	returned = true
	return results, nil
}

// fillInvokeCache resolves export and memoizes it so subsequent Invokes skip the
// exports map probe. Local functions store their local index; an imported
// InstanceExport stores -1-importIndex and forwards through its original owner.
func (in *Instance) fillInvokeCache(export string) (*invokeCache, error) {
	var gfi int
	if standaloneOneShot {
		if export != standaloneInvokeExport {
			return nil, fmt.Errorf("no exported function %q", export)
		}
		gfi = standaloneInvokeFunctionIndex
	} else {
		var ok bool
		gfi, ok = in.c.Exports[export]
		if !ok {
			return nil, fmt.Errorf("no exported function %q", export)
		}
	}
	if gfi < 0 {
		return nil, fmt.Errorf("export %q function index %d out of range", export, gfi)
	}
	if functionReexportsForImage && gfi < in.c.NumImports {
		if gfi >= len(in.c.Imports) {
			return nil, fmt.Errorf("export %q imported function index %d has no binding", export, gfi)
		}
		if ex, ok := railshotNativeFunctionAttachment(in, gfi); (!ok || ex == nil || ex.inst == nil) && (gfi >= len(in.syncHosts) || !in.syncHosts[gfi].callable()) {
			return nil, fmt.Errorf("export %q is an imported function without a callable owner", export)
		}
		slot, index := in.nextInvokeCacheSlot()
		*slot = invokeCache{export: export, valid: true, li: -1 - gfi, slotWide: slot.slotWide[:0], slotIndex: index}
		return slot, nil
	}
	li := gfi - in.c.NumImports
	if li < 0 || li >= len(in.c.Funcs) {
		return nil, fmt.Errorf("export %q function index %d out of range", export, gfi)
	}
	sig := in.c.Funcs[li]
	paramSlots, resultSlots := len(sig.Params), len(sig.Results)
	if vectorInvocationForImage {
		var err error
		paramSlots, err = valTypesSlots(sig.Params)
		if err != nil {
			return nil, fmt.Errorf("%s parameter slots: %w", export, err)
		}
		resultSlots, err = valTypesSlots(sig.Results)
		if err != nil {
			return nil, fmt.Errorf("%s result slots: %w", export, err)
		}
	}
	hasReferenceParams := referenceSignatureTypesForImage && hasReferenceValType(sig.Params)
	hasReferenceResults := referenceSignatureTypesForImage && hasReferenceValType(sig.Results)
	const maxCachedSlots = int(^uint32(0) >> 1)
	if paramSlots > maxCachedSlots || resultSlots > maxCachedSlots {
		return nil, fmt.Errorf("%s signature has too many value slots to cache", export)
	}
	if standaloneBareImage && (paramSlots > buildfeatures.StandaloneMaxParamSlots || resultSlots > buildfeatures.StandaloneMaxResultSlots) {
		return nil, fmt.Errorf("%s signature exceeds the standalone invocation slot profile", export)
	}
	slot, index := in.nextInvokeCacheSlot()
	slotCount := paramSlots + resultSlots
	var widths []bool
	if wideInvocationSlotsForImage {
		widths = slot.slotWide[:0]
		if cap(widths) < slotCount {
			widths = make([]bool, 0, slotCount)
		}
		widths = appendValTypeSlotWidths(widths, sig.Params)
		widths = appendValTypeSlotWidths(widths, sig.Results)
	} else {
		widths = standaloneNarrowSlotWidths[:slotCount:slotCount]
	}
	entryMode := preparedEntryGeneral
	directEntryMode := preparedEntryGeneral
	var scalarWideMask uint8
	if !hasReferenceParams && !hasReferenceResults {
		entryMode = in.preparedEntryMode()
		directEntryMode = entryMode
		if directEntryMode == preparedEntryGeneral && in.c.boundsMode == BoundsChecksSignalsBased && in.c.directPreparedAt(li) {
			directEntryMode = in.preparedMemoryFreeEntryMode()
		}
		if wideInvocationSlotsForImage && paramSlots <= 4 {
			paramSlot := 0
			for _, typ := range sig.Params {
				if isWideValType(typ) {
					scalarWideMask |= 1 << paramSlot
				}
				paramSlot++
			}
		}
	}
	directIntFast := directIntegerForImage && preparedCallEnabled && invokePrivateEntryEnabled && preparedIsolatedEntryEnabled &&
		preparedDirectIntSupported && preparedDirectIntEnabled && directEntryMode == preparedEntryIsolated &&
		preparedDirectIntSignature(sig) && in.c.directPreparedAt(li) &&
		preparedDirectIntegerArityForImage(paramSlots, resultSlots) &&
		(!preparedDirectWideSupported || paramSlots <= 4 && resultSlots <= 2 || in.c.directPreparedBoundedAt(li)) &&
		(resultSlots <= 1 || preparedDirectPairSupported && in.c.directPreparedBoundedAt(li))
	directFloatFast := directFloatForImage && preparedCallEnabled && invokePrivateEntryEnabled && preparedIsolatedEntryEnabled &&
		preparedDirectIntEnabled && directEntryMode == preparedEntryIsolated &&
		preparedDirectFloatSignatureForImage(sig) && in.c.directPreparedAt(li) && in.c.directPreparedBoundedAt(li)
	directMixedFast := directMixedForImage && preparedCallEnabled && invokePrivateEntryEnabled && preparedIsolatedEntryEnabled &&
		preparedDirectIntEnabled && directEntryMode == preparedEntryIsolated &&
		preparedDirectMixedSignature(sig) && in.c.directPreparedAt(li) && in.c.directPreparedBoundedAt(li)
	var directMixedInfo uint8
	if directMixedFast {
		directMixedInfo = encodeDirectMixedInfo(sig)
		scalarWideMask = directMixedInfo
	}
	*slot = invokeCache{
		export:            export,
		valid:             true,
		directIntFast:     directIntFast,
		directI32Fast:     cachedBoundedI32OneArgForImage && directIntFast && in.c.directPreparedBoundedAt(li) && paramSlots == 1 && resultSlots == 1 && scalarWideMask == 0 && !widths[paramSlots],
		directFloatFast:   directFloatFast || directMixedFast,
		directIntLight:    directIntFast && in.c.directPreparedLightAt(li),
		directIntBounded:  directIntFast && in.c.directPreparedBoundedAt(li),
		boundedWrapper:    in.c.directPreparedBoundedAt(li) || in.isolatedNativeScalarLeaf(),
		scalarWideMask:    scalarWideMask,
		scalarResultWide:  resultSlots == 1 && widths[paramSlots],
		paramWidthClass:   classifyScalarSlotWidths(widths[:paramSlots]),
		resultWidthClass:  classifyScalarSlotWidths(widths[paramSlots:]),
		slotIndex:         index,
		li:                li,
		paramSlots:        int32(paramSlots),
		resultSlots:       int32(resultSlots),
		hasFuncRefParams:  hasReferenceParams,
		hasFuncRefResults: hasReferenceResults,
		boundedNumericHost: hostFunctionImportsBuilt && in.c.boundedHostSegments() && !in.c.needsFuncRefContext() &&
			!hasReferenceParams && !hasReferenceResults &&
			(in.hasSingleDirectTypedScalarHost() || in.hasSingleExpandedTypedScalarHost() || in.hasSingleHostCallFixedViewPortal() || in.hasBoundedCallerHostView()),
		slotWide:  widths,
		entryMode: entryMode,
	}
	if slot.directIntFast || slot.directFloatFast {
		slot.directEntry = in.base + uintptr(internalEntryOffset(in.c.InternalEntry[slot.li]))
	}
	return slot, nil
}

// nextInvokeCacheSlot evicts the matching prepared host handle with its export.
func (in *Instance) nextInvokeCacheSlot() (*invokeCache, uint8) {
	if standaloneOneShot {
		return &in.ic[0], 0
	}
	index := in.icNext
	in.icNext++
	if in.icNext == in.invokeCacheSlotCount() {
		in.icNext = 0
	}
	if state := in.pluginState.Load(); state != nil && state.hostInvokeCache != nil {
		state.hostInvokeCache[index] = nil
	}
	if index < uint8(len(in.ic)) {
		return &in.ic[index], index
	}
	return &in.pluginState.Load().invokeCacheExtra.entries[index-uint8(len(in.ic))], index
}

func (in *Instance) invokeCacheSlotCount() uint8 {
	if state := in.pluginState.Load(); state != nil && state.invokeCacheSlots != 0 {
		return state.invokeCacheSlots
	}
	return uint8(len(in.ic))
}

func marshalPublicScalarArgs(dst []byte, values []uint64, types []ValType) {
	slot := 0
	for _, typ := range types {
		if vectorInvocationForImage && typ == ValV128 {
			binary.LittleEndian.PutUint64(dst[slot*8:], values[slot])
			binary.LittleEndian.PutUint64(dst[(slot+1)*8:], values[slot+1])
			slot += 2
			continue
		}
		bits := values[slot]
		if !wideInvocationSlotsForImage || !isWideValType(typ) {
			bits = uint64(uint32(bits))
		}
		binary.LittleEndian.PutUint64(dst[slot*8:], bits)
		slot++
	}
}

type scalarSlotWidthClass uint8

// Every entry is false. Narrow launchers share this read-only view across
// cache entries and prepared handles instead of allocating per signature.
var standaloneNarrowSlotWidths [buildfeatures.StandaloneMaxParamSlots + buildfeatures.StandaloneMaxResultSlots]bool

const (
	scalarSlotMixed scalarSlotWidthClass = iota
	scalarSlotNarrow
	scalarSlotWide
)

func classifyScalarSlotWidths(wide []bool) scalarSlotWidthClass {
	if len(wide) == 0 {
		return scalarSlotWide
	}
	if !wideInvocationSlotsForImage {
		return scalarSlotNarrow
	}
	first := wide[0]
	for _, w := range wide[1:] {
		if w != first {
			return scalarSlotMixed
		}
	}
	if first {
		return scalarSlotWide
	}
	return scalarSlotNarrow
}

func copyPublicScalarSlotsByClass(dst, values []uint64, wide []bool, class scalarSlotWidthClass) {
	switch {
	case wideInvocationSlotsForImage && class == scalarSlotWide:
		copy(dst[:len(wide)], values[:len(wide)])
	case !wideInvocationSlotsForImage || class == scalarSlotNarrow:
		// Small numeric signatures avoid a second helper and its copy loop.
		switch len(wide) {
		case 1:
			dst, values = dst[:1], values[:1]
			dst[0] = uint64(uint32(values[0]))
		case 2:
			dst, values = dst[:2], values[:2]
			dst[0] = uint64(uint32(values[0]))
			dst[1] = uint64(uint32(values[1]))
		default:
			copyNarrowScalarSlots(dst, values, len(wide))
		}
	default:
		for i, w := range wide {
			bits := values[i]
			if !w {
				bits = uint64(uint32(bits))
			}
			dst[i] = bits
		}
	}
}

func appendValTypeSlotWidths(dst []bool, types []ValType) []bool {
	for _, typ := range types {
		if vectorInvocationForImage && typ == ValV128 {
			dst = append(dst, true, true)
		} else {
			dst = append(dst, isWideValType(typ))
		}
	}
	return dst
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

func (in *Instance) marshalPublicReferenceArgs(subject string, values []uint64, types []ValType, exact []ValueTypeDescriptor, argumentOwners *railshotPublicFuncrefArguments) error {
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
				token := bits
				descriptor, ok := in.refStore.resolve(token)
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
				if err := railshotStagePublicFuncrefArgument(in, argumentOwners, token, descriptor); err != nil {
					return fmt.Errorf("%s: funcref argument %d owner: %w", subject, i, err)
				}
				bits = descriptor
			}
		case ValExternRef:
			required, ok := exactReferenceType(exact, i, typ)
			if !ok || required.Kind != ValueTypeReference || required.Ref.Heap.Defined || required.Ref.Heap.Abstract != AbstractHeapExtern && required.Ref.Heap.Abstract != AbstractHeapNoExtern {
				return fmt.Errorf("%s: missing exact externref type for argument %d", subject, i)
			}
			if bits == 0 && !required.Ref.Nullable || bits != 0 && required.Ref.Heap.Abstract == AbstractHeapNoExtern {
				return fmt.Errorf("%s: externref argument %d does not match its exact structural type", subject, i)
			}
			if in.usesStagedExternConversion() {
				conversion := in.existingGCExternConversionState()
				if conversion == nil {
					return fmt.Errorf("%s: extern conversion state is unavailable for argument %d", subject, i)
				}
				internal, err := conversion.internalExternFromPublic(bits)
				if err != nil {
					return fmt.Errorf("%s: invalid externref token for argument %d: %w", subject, i, err)
				}
				bits = internal
			} else if bits != 0 {
				if in.refStore == nil || !in.validExternrefToken(bits) {
					return fmt.Errorf("%s: invalid externref token for argument %d", subject, i)
				}
				if err := railshotStagePublicExternrefArgument(in, argumentOwners, bits); err != nil {
					return fmt.Errorf("%s: externref argument %d lifetime: %w", subject, i, err)
				}
			}
		case ValAnyRef, ValExnRef:
			required, ok := exactReferenceType(exact, i, typ)
			if !ok {
				return fmt.Errorf("%s: missing exact %s type for argument %d", subject, typ, i)
			}
			if typ == ValAnyRef && in.usesStagedExternConversion() {
				conversion := in.existingGCExternConversionState()
				if conversion == nil {
					return fmt.Errorf("%s: anyref conversion state is unavailable for argument %d", subject, i)
				}
				internal, err := conversion.internalAnyFromPublic(bits)
				if err != nil {
					return fmt.Errorf("%s: invalid anyref token for argument %d: %w", subject, i, err)
				}
				bits = internal
			} else if collectorObjectTokensBuilt && (wasmGCBuilt && typ == ValAnyRef || typ == ValExnRef && in.c.needsOwnedExceptionReferences()) && bits != 0 {
				compact := gc.Ref(uint32(bits))
				if bits == uint64(compact) && compact.IsI31() {
					if !in.gcRefMatchesValueType(compact, required) {
						return fmt.Errorf("%s: i31 anyref argument %d does not match its exact type", subject, i)
					}
				} else {
					if in.refStore == nil {
						return fmt.Errorf("%s: invalid anyref token for argument %d", subject, i)
					}
					var internal gc.Ref
					var err error
					if wasmGCBuilt && in.c.compactExternFrameRoots() && in.validExternrefToken(bits) {
						internal, err = in.stageCompactExternAnyArgument(bits, required)
					} else {
						internal, err = in.refStore.stageGCRefArgument(in, bits, required)
					}
					if err != nil {
						return fmt.Errorf("%s: invalid anyref token for argument %d: %w", subject, i, err)
					}
					bits = uint64(internal)
				}
			} else if bits != 0 {
				return fmt.Errorf("%s: non-null %s argument %d is outside the null-only product", subject, typ, i)
			}
			if bits == 0 && !required.Ref.Nullable {
				return fmt.Errorf("%s: null %s for non-null argument %d", subject, typ, i)
			}
		case ValI31Ref:
			required, ok := exactReferenceType(exact, i, typ)
			if !ok || required.Ref.Heap.Defined || required.Ref.Heap.Abstract != AbstractHeapI31 {
				return fmt.Errorf("%s: missing exact i31 type for argument %d", subject, i)
			}
			if bits == 0 {
				if !required.Ref.Nullable {
					return fmt.Errorf("%s: null i31ref for non-null argument %d", subject, i)
				}
			} else if bits>>32 != 0 || uint32(bits)&1 == 0 {
				return fmt.Errorf("%s: invalid i31ref argument %d", subject, i)
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
	return in.translatePublicReferenceResultsMode(subject, values, types, exact, false)
}

// Result ownership remains provisional until every result has passed its exact
// type and token checks. Small result journals stay on the invocation stack.
type publicResultToken struct {
	store   *referenceStore // nil marks a new extern token in in.refStore
	token   uint64
	funcref *funcrefTokenEntry
}

func (in *Instance) translatePublicReferenceResultsMode(subject string, values []uint64, types []ValType, exact []ValueTypeDescriptor, attachedResult bool) (err error) {
	var inline [gcPublicSlotLimit]publicResultToken
	issued := inline[:]
	if len(types) > len(issued) {
		issued = make([]publicResultToken, len(types))
	}
	issuedCount := 0
	completed := false
	defer func() {
		var cleanup resourceCleanup
		if !completed && err == nil {
			cleanup.panicked, cleanup.value = true, recover()
		}
		for i := issuedCount - 1; i >= 0; i-- {
			token := issued[i]
			if token.store == nil {
				if !completed || err != nil {
					cleanup.run(func() {
						if !in.refStore.releaseExternref(token.token) {
							err = fmt.Errorf("%w; rollback extern result token failed", err)
						}
					})
				}
			} else if token.funcref != nil {
				cleanup.run(func() { token.store.finishFuncrefResult(token.funcref, completed && err == nil) })
			} else if collectorObjectTokensBuilt && (!completed || err != nil) {
				cleanup.run(func() {
					if releaseErr := token.store.releaseGCRef(in, token.token); releaseErr != nil {
						err = fmt.Errorf("%w; rollback GC result token: %v", err, releaseErr)
					}
				})
			}
		}
		cleanup.finish()
	}()
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
				var store *referenceStore
				var err error
				if attachedResult {
					store, err = in.funcrefStoreForAttachedEgress()
				} else {
					store, err = in.funcrefStoreForEgress()
				}
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
				token, fresh, err := store.issueResultMode(in, values[slot], attachedResult, issued[:issuedCount], true)
				if err != nil {
					clear(values)
					return fmt.Errorf("%s: invalid funcref result %d: %w", subject, i, err)
				}
				if fresh != nil {
					issued[issuedCount] = publicResultToken{store: store, funcref: fresh}
					issuedCount++
				}
				values[slot] = token
			}
		}
		if typ == ValExternRef {
			required, ok := exactReferenceType(exact, i, typ)
			if !ok || required.Kind != ValueTypeReference || required.Ref.Heap.Defined || required.Ref.Heap.Abstract != AbstractHeapExtern && required.Ref.Heap.Abstract != AbstractHeapNoExtern {
				clear(values)
				return fmt.Errorf("%s: missing exact externref type for result %d", subject, i)
			}
			if values[slot] == 0 && !required.Ref.Nullable || values[slot] != 0 && required.Ref.Heap.Abstract == AbstractHeapNoExtern {
				clear(values)
				return fmt.Errorf("%s: externref result %d does not match its exact structural type", subject, i)
			}
			if in.c.compactExternFrameRoots() && values[slot] != 0 {
				public, issueErr := in.issueCompactExternPublicToken(values[slot])
				if issueErr != nil {
					clear(values)
					return fmt.Errorf("%s: own externref result %d: %w", subject, i, issueErr)
				}
				if public != values[slot] {
					issued[issuedCount] = publicResultToken{token: public}
					issuedCount++
				}
				values[slot] = public
			} else if in.usesStagedExternConversion() {
				conversion := in.existingGCExternConversionState()
				if conversion == nil {
					clear(values)
					return fmt.Errorf("%s: extern conversion state is unavailable for result %d", subject, i)
				}
				public, err := conversion.publicExternFromInternal(values[slot])
				if err != nil {
					clear(values)
					return fmt.Errorf("%s: invalid externref result %d: %w", subject, i, err)
				}
				values[slot] = public
			} else if values[slot] != 0 && !in.validExternrefToken(values[slot]) {
				clear(values)
				return fmt.Errorf("%s: invalid externref result %d", subject, i)
			}
		}
		if collectorObjectTokensBuilt && (wasmGCBuilt && typ == ValAnyRef || typ == ValExnRef && in.c.needsOwnedExceptionReferences()) {
			required, ok := exactReferenceType(exact, i, typ)
			if !ok {
				clear(values)
				return fmt.Errorf("%s: missing exact anyref type for result %d", subject, i)
			}
			if values[slot] == 0 {
				if !required.Ref.Nullable {
					clear(values)
					return fmt.Errorf("%s: null anyref for non-null result %d", subject, i)
				}
			} else if in.usesStagedExternConversion() {
				conversion := in.existingGCExternConversionState()
				if conversion == nil {
					clear(values)
					return fmt.Errorf("%s: anyref conversion state is unavailable for result %d", subject, i)
				}
				public, err := conversion.publicAnyFromInternal(values[slot])
				if err != nil {
					clear(values)
					return fmt.Errorf("%s: own non-null anyref result %d: %w", subject, i, err)
				}
				values[slot] = public
			} else {
				if values[slot] != uint64(uint32(values[slot])) {
					clear(values)
					return fmt.Errorf("%s: invalid non-null anyref result %d", subject, i)
				}
				ref := gc.Ref(uint32(values[slot]))
				if wasmGCBuilt {
					if token, foreign := compactForeignAnyToken(in.gc, ref); foreign {
						if matched, _ := compactForeignAnyMatches(in.gc, ref, required); !matched {
							clear(values)
							return fmt.Errorf("%s: foreign anyref result %d does not match its exact type", subject, i)
						}
						values[slot] = token
						slot++
						continue
					}
				}
				if ref.IsI31() {
					if !in.gcRefMatchesValueType(ref, required) {
						clear(values)
						return fmt.Errorf("%s: i31 anyref result %d does not match its exact type", subject, i)
					}
					slot++
					continue
				}
				store, err := in.referenceStoreForBoundary()
				if err != nil {
					clear(values)
					return fmt.Errorf("%s: own non-null anyref result %d: %w", subject, i, err)
				}
				token, err := store.issueGCRef(in, ref, required)
				if err != nil {
					clear(values)
					return fmt.Errorf("%s: own non-null anyref result %d: %w", subject, i, err)
				}
				issued[issuedCount] = publicResultToken{store: store, token: token}
				issuedCount++
				values[slot] = token
			}
		}
		if typ == ValI31Ref {
			required, ok := exactReferenceType(exact, i, typ)
			if !ok || required.Ref.Heap.Defined || required.Ref.Heap.Abstract != AbstractHeapI31 {
				clear(values)
				return fmt.Errorf("%s: missing exact i31 type for result %d", subject, i)
			}
			if values[slot] == 0 {
				if !required.Ref.Nullable {
					clear(values)
					return fmt.Errorf("%s: null i31ref for non-null result %d", subject, i)
				}
			} else if values[slot]>>32 != 0 || uint32(values[slot])&1 == 0 {
				clear(values)
				return fmt.Errorf("%s: invalid i31ref result %d", subject, i)
			}
		}
		if typ == ValExnRef && !in.c.needsOwnedExceptionReferences() {
			required, ok := exactReferenceType(exact, i, typ)
			if !ok {
				clear(values)
				return fmt.Errorf("%s: missing exact exnref type for result %d", subject, i)
			}
			if values[slot] != 0 {
				clear(values)
				return fmt.Errorf("%s: non-null exnref result %d is outside the null-only product", subject, i)
			}
			if !required.Ref.Nullable {
				clear(values)
				return fmt.Errorf("%s: null exnref for non-null result %d", subject, i)
			}
		}
		if typ == ValV128 {
			slot += 2
		} else {
			slot++
		}
	}
	completed = true
	return nil
}

func (in *Instance) findInvokeCache(export string) *invokeCache {
	for i := range in.ic {
		if in.ic[i].valid && sameExportName(in.ic[i].export, export) {
			return &in.ic[i]
		}
	}
	if standaloneOneShot {
		return nil
	}
	if state := in.pluginState.Load(); state != nil && state.invokeCacheExtra != nil {
		extra := state.invokeCacheExtra
		for i := range extra.entries {
			entry := &extra.entries[i]
			if entry.valid && sameExportName(entry.export, export) {
				return entry
			}
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

func moduleHasOpaqueExternSignatures(m *wasm.Module) bool {
	for _, group := range m.Types {
		for _, sub := range group.SubTypes {
			if sub.Comp.Kind == wasm.CompFunc {
				for _, values := range [][]wasm.ValType{sub.Comp.Params, sub.Comp.Results} {
					for _, value := range values {
						if wasm.ExternReferenceType(value) {
							return true
						}
					}
				}
			}
		}
	}
	return false
}

func moduleHasOpaqueExternTable(m *wasm.Module) bool {
	for i := uint32(0); i < uint32(m.TableCount()); i++ {
		table, ok := m.TableType(i)
		if ok && wasm.ExternReferenceType(wasm.RefVal(table.Ref)) {
			return true
		}
	}
	return false
}

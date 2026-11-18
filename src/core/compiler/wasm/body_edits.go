// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wasm

import (
	"bytes"
	"errors"
	"sort"
	"sync"
)

var ErrBodySourceConsumed = errors.New("body validation source consumed")
var ErrInvalidBodyEdit = errors.New("invalid indexed function body edit")
var ErrBodyEditHints = errors.New("body edits require branch hint remapping")

// BodyEdit replaces only one local function's body and locals. Function types
// and all module declarations remain those of the validated source.
type BodyEdit struct {
	LocalFunction uint32
	BodyBytes     []byte
	Locals        Locals
}

// OwnedBodySource retains a private validated snapshot. Neither the constructor
// input nor a returned Snapshot aliases its storage. Apply consumes the owner
// on success, before mutable output becomes visible. Methods serialize access.
type OwnedBodySource struct {
	mu       sync.Mutex
	source   *Module
	analysis ValidatedModuleAnalysis
	features ValidationFeatures
	limits   ValidationLimits
	consumed bool
}

func normalizedBodyLimits(l ValidationLimits) ValidationLimits {
	if l.MaxFunctionLocals == 0 {
		l.MaxFunctionLocals = DefaultMaxFunctionLocals
	}
	if l.MaxMemoriesPerModule == 0 {
		l.MaxMemoriesPerModule = DefaultMaxMemoriesPerModule
	}
	return l
}

func NewOwnedBodySource(m *Module, features ValidationFeatures, workers int, limits ValidationLimits) (*OwnedBodySource, error) {
	if m == nil {
		return nil, &ValidationError{Code: ErrTypeMismatch, Func: -1, Detail: "nil module"}
	}
	s := &OwnedBodySource{source: cloneBodyEditModule(m), features: features, limits: normalizedBodyLimits(limits)}
	if err := ValidateModuleWithAnalysis(s.source, features, workers, limits, &s.analysis); err != nil {
		return nil, err
	}
	return s, nil
}

// NewOwnedBodySourceWithAnalysis also returns fresh facts for the constructor
// input. Keep m and its nested storage immutable from this call through the
// last fact consumer, exactly as with ValidateModuleWithAnalysis. The source
// retained for Apply is a separate deep copy, independent of later m changes.
func NewOwnedBodySourceWithAnalysis(m *Module, features ValidationFeatures, workers int, limits ValidationLimits) (*OwnedBodySource, ValidatedModuleAnalysis, error) {
	s, err := NewOwnedBodySource(m, features, workers, limits)
	if err != nil {
		return nil, ValidatedModuleAnalysis{}, err
	}
	// Validation used a detached private copy. Drop possibly stale cache pointers
	// on the original without clearing cache objects that another module owns.
	m.structuralTypeCache = nil
	m.typeIndexDirectory = nil
	m.importIndexDirectory = nil
	a := s.analysis
	a.module = m
	a.funcs = cloneBodySlice(a.funcs)
	return s, a, nil
}

// Snapshot returns independent mutable module storage with facts for that exact
// copy. As with ordinary validation, keep it immutable while consuming facts.
// Mutating it cannot change the owned source used by Apply.
func (s *OwnedBodySource) Snapshot() (*Module, ValidatedModuleAnalysis, error) {
	if s == nil {
		return nil, ValidatedModuleAnalysis{}, ErrInvalidBodyEdit
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.consumed {
		return nil, ValidatedModuleAnalysis{}, ErrBodySourceConsumed
	}
	m := cloneBodyEditModule(s.source)
	a := s.analysis
	a.module = m
	a.funcs = cloneBodySlice(a.funcs)
	return m, a, nil
}

func (s *OwnedBodySource) Apply(edits []BodyEdit, features ValidationFeatures, workers int, limits ValidationLimits) (*Module, ValidatedModuleAnalysis, error) {
	if s == nil {
		return nil, ValidatedModuleAnalysis{}, ErrInvalidBodyEdit
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.consumed {
		return nil, ValidatedModuleAnalysis{}, ErrBodySourceConsumed
	}
	m, a, err := applyValidatedBodyEdits(s.source, s.analysis, s.features, s.limits, edits, features, workers, limits, true)
	if err == nil {
		s.consumed = true
	}
	return m, a, err
}

// ApplyValidatedBodyEdits applies private body replacements to an already
// validated module. The result shares unchanged declaration and body storage
// with source and takes ownership of edited body and local storage. Callers
// must stop using source and edits after success. This narrower ownership is
// intended for the compiler's private module pipeline; OwnedBodySource keeps
// the detached-copy contract for general callers.
func ApplyValidatedBodyEdits(source *Module, analysis *ValidatedModuleAnalysis, edits []BodyEdit, features ValidationFeatures, workers int, limits ValidationLimits) (*Module, ValidatedModuleAnalysis, error) {
	if source == nil || analysis == nil || !analysis.ValidFor(source) {
		return nil, ValidatedModuleAnalysis{}, ErrInvalidBodyEdit
	}
	return applyValidatedBodyEdits(source, *analysis, features, normalizedBodyLimits(limits), edits, features, workers, limits, false)
}

func applyValidatedBodyEdits(source *Module, sourceAnalysis ValidatedModuleAnalysis, sourceFeatures ValidationFeatures, sourceLimits ValidationLimits, edits []BodyEdit, features ValidationFeatures, workers int, limits ValidationLimits, cloneEditedStorage bool) (*Module, ValidatedModuleAnalysis, error) {
	order := cloneBodySlice(edits)
	sort.Slice(order, func(i, j int) bool { return order[i].LocalFunction < order[j].LocalFunction })
	for i, e := range order {
		if uint64(e.LocalFunction) >= uint64(len(source.Code)) || len(e.BodyBytes) == 0 || i > 0 && order[i-1].LocalFunction == e.LocalFunction {
			return nil, ValidatedModuleAnalysis{}, ErrInvalidBodyEdit
		}
	}
	if len(order) != 0 {
		if len(source.BranchHints) != 0 {
			return nil, ValidatedModuleAnalysis{}, ErrBodyEditHints
		}
		for _, c := range source.Customs {
			if c.Name == "metadata.code.branch_hint" {
				return nil, ValidatedModuleAnalysis{}, ErrBodyEditHints
			}
		}
	}
	// Only internal, unexposed immutable declarations are shared. All mutable
	// cache owners are detached before either validation path can run.
	m := *source
	m.Code = cloneBodySlice(source.Code)
	m.structuralTypeCache = nil
	m.typeIndexDirectory = nil
	m.importIndexDirectory = nil
	for _, e := range order {
		if cloneEditedStorage {
			m.Code[e.LocalFunction] = Func{Locals: Locals{Runs: cloneBodySlice(e.Locals.Runs)}, BodyBytes: bytes.Clone(e.BodyBytes)}
		} else {
			m.Code[e.LocalFunction] = Func{Locals: Locals{Runs: e.Locals.Runs}, BodyBytes: e.BodyBytes}
		}
	}
	encodeDecls := func() error {
		for _, e := range order {
			decl := []byte{}
			appendU32(&decl, uint32(len(e.Locals.Runs)))
			for _, run := range m.Code[e.LocalFunction].Locals.Runs {
				appendU32(&decl, run.Count)
				typ := run.Type
				if typ.Kind() == ValRef && typ.Ref().Heap().Kind() == HeapDefType {
					rt := typ.Ref()
					group, member, _, present := rt.Heap().Def()
					directory := m.typeIndex()
					if !present || uint64(group) >= uint64(len(m.Types)) || uint64(member) >= uint64(len(m.Types[group].SubTypes)) {
						return ErrInvalidBodyEdit
					}
					flat := uint32(directory.bases[group]) + member
					typ = RefVal(Ref(rt.Nullable(), IndexedHeap(TypeIdx{Index: flat}), rt.Exact()))
				}
				if err := appendValType(&decl, typ); err != nil {
					return err
				}
			}
			m.Code[e.LocalFunction].LocalDeclBytes = uint32(len(decl))
		}
		return nil
	}

	full := func() (*Module, ValidatedModuleAnalysis, error) {
		var a ValidatedModuleAnalysis
		if err := ValidateModuleWithAnalysis(&m, features, workers, limits, &a); err != nil {
			return nil, ValidatedModuleAnalysis{}, err
		}
		if err := encodeDecls(); err != nil {
			return nil, ValidatedModuleAnalysis{}, err
		}
		return &m, a, nil
	}
	if len(order) == 0 || !sourceAnalysis.ValidFor(source) || features != sourceFeatures || normalizedBodyLimits(limits) != sourceLimits {
		return full()
	}
	a := ValidatedModuleAnalysis{module: &m, funcs: cloneBodySlice(sourceAnalysis.funcs)}
	v := moduleValidator{m: &m, funcIndex: -1, features: features, limits: sourceLimits, analysis: &a}
	v.ensureImportIndexes()
	v.analysisFuncBase = len(v.importIndexes[ExternFunc])
	if err := v.validateModule(); err != nil {
		return nil, ValidatedModuleAnalysis{}, err
	}
	fv := funcValidator{moduleValidator: &v}
	widths := moduleMemargWidths(&m)
	for _, e := range order {
		old, err := bodyEditSegmentCounts(source, source.Code[e.LocalFunction].BodyBytes)
		if err != nil {
			return nil, ValidatedModuleAnalysis{}, err
		}
		next, err := v.validateFunction(&fv, int(e.LocalFunction), v.analysisFuncBase, widths)
		if err != nil {
			return nil, ValidatedModuleAnalysis{}, err
		}
		if old != next {
			return full()
		}
	}
	a.elemStateCount = sourceAnalysis.elemStateCount
	a.dataStateCount = sourceAnalysis.dataStateCount
	a.finish()
	if err := encodeDecls(); err != nil {
		return nil, ValidatedModuleAnalysis{}, err
	}
	return &m, a, nil
}

func bodyEditSegmentCounts(m *Module, body []byte) (validationSegmentCounts, error) {
	var counts validationSegmentCounts
	c := NewModuleInstructionClassifier(m, true)
	r := NewReader(body)
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return counts, err
		}
		var in InstructionImmediate
		if err = c.ClassifyInto(r, op, &in); err != nil {
			return counts, err
		}
		switch in.Kind {
		case InstrMemoryInit, InstrDataDrop:
			counts.data = max(counts.data, segmentStateCount(in.Index))
		case InstrTableInit, InstrElemDrop:
			counts.elem = max(counts.elem, segmentStateCount(in.Index))
		}
	}
	return counts, nil
}

// cloneBodyEditModule copies all mutable declaration and expression storage.
// String values are immutable and can share their backing bytes.
// Cache pointers are deliberately not copied; no private validation can reset
// an original cache through a shallow pointer alias.
func cloneBodyEditModule(m *Module) *Module {
	out := *m
	out.structuralTypeCache = nil
	out.typeIndexDirectory = nil
	out.importIndexDirectory = nil
	out.Customs = cloneBodySlice(m.Customs)
	for i := range out.Customs {

		out.Customs[i].Data = bytes.Clone(out.Customs[i].Data)
	}
	out.RawNameSecPayload = bytes.Clone(m.RawNameSecPayload)
	out.Types = cloneBodySlice(m.Types)
	for i := range out.Types {
		out.Types[i].SubTypes = cloneBodySlice(m.Types[i].SubTypes)
		for j := range out.Types[i].SubTypes {
			st := &out.Types[i].SubTypes[j]
			st.Supers = cloneBodySlice(st.Supers)
			st.Comp.Fields = cloneBodySlice(st.Comp.Fields)
			st.Comp.Params = cloneBodySlice(st.Comp.Params)
			st.Comp.Results = cloneBodySlice(st.Comp.Results)
		}
	}
	out.Imports = cloneBodySlice(m.Imports)
	out.FuncTypes = cloneBodySlice(m.FuncTypes)
	out.Memories = cloneBodySlice(m.Memories)
	out.Tags = cloneBodySlice(m.Tags)
	out.StringRefs = cloneBodySlice(m.StringRefs)
	for i := range out.StringRefs {
		out.StringRefs[i] = bytes.Clone(m.StringRefs[i])
	}
	out.Tables = cloneBodySlice(m.Tables)
	for i := range out.Tables {
		if m.Tables[i].Init != nil {
			e := cloneBodyEditExpr(*m.Tables[i].Init)
			out.Tables[i].Init = &e
		}
	}
	out.Globals = cloneBodySlice(m.Globals)
	for i := range out.Globals {
		out.Globals[i].Init = cloneBodyEditExpr(m.Globals[i].Init)
	}
	out.Exports = cloneBodySlice(m.Exports)
	if m.Start != nil {
		x := *m.Start
		out.Start = &x
	}
	if m.DataCount != nil {
		x := *m.DataCount
		out.DataCount = &x
	}
	out.Elements = cloneBodySlice(m.Elements)
	for i := range out.Elements {
		e := &out.Elements[i]
		e.Mode.Offset = cloneBodyEditExpr(e.Mode.Offset)
		e.Kind.Funcs = cloneBodySlice(e.Kind.Funcs)
		e.Kind.Exprs = cloneBodySlice(e.Kind.Exprs)
		for j := range e.Kind.Exprs {
			e.Kind.Exprs[j] = cloneBodyEditExpr(e.Kind.Exprs[j])
		}
	}
	out.Data = cloneBodySlice(m.Data)
	for i := range out.Data {
		out.Data[i].Mode.Offset = cloneBodyEditExpr(out.Data[i].Mode.Offset)
		out.Data[i].Init = bytes.Clone(out.Data[i].Init)
	}
	out.Code = cloneBodySlice(m.Code)
	for i := range out.Code {
		out.Code[i].Locals.Runs = cloneBodySlice(m.Code[i].Locals.Runs)
		out.Code[i].BodyBytes = bytes.Clone(m.Code[i].BodyBytes)
		out.Code[i].Body = cloneBodyEditExpr(m.Code[i].Body)
	}
	out.BranchHints = cloneBodySlice(m.BranchHints)
	for i := range out.BranchHints {
		out.BranchHints[i].Hints = cloneBodySlice(m.BranchHints[i].Hints)
	}
	if m.NameSec != nil {
		n := *m.NameSec
		if n.ModuleName != nil {
			x := *n.ModuleName
			n.ModuleName = &x
		}
		n.FunctionNames = cloneBodyEditNames(n.FunctionNames)
		n.LocalNames = cloneBodyEditIndirectNames(n.LocalNames)
		n.LabelNames = cloneBodyEditIndirectNames(n.LabelNames)
		n.TypeNames = cloneBodyEditNames(n.TypeNames)
		n.TableNames = cloneBodyEditNames(n.TableNames)
		n.MemoryNames = cloneBodyEditNames(n.MemoryNames)
		n.GlobalNames = cloneBodyEditNames(n.GlobalNames)
		n.ElementNames = cloneBodyEditNames(n.ElementNames)
		n.DataNames = cloneBodyEditNames(n.DataNames)
		n.FieldNames = cloneBodyEditIndirectNames(n.FieldNames)
		n.TagNames = cloneBodyEditNames(n.TagNames)
		out.NameSec = &n
	}
	return &out
}
func cloneBodyEditNames(m NameMap) NameMap {
	return cloneBodySlice(m)
}
func cloneBodyEditIndirectNames(m IndirectNameMap) IndirectNameMap {
	out := cloneBodySlice(m)
	for i := range out {
		out[i].Names = cloneBodyEditNames(out[i].Names)
	}
	return out
}
func cloneBodyEditExpr(e Expr) Expr {
	return Expr{BodyBytes: bytes.Clone(e.BodyBytes), Instrs: cloneBodyEditInstructions(e.Instrs)}
}
func cloneBodyEditInstructions(in []Instruction) []Instruction {
	out := cloneBodySlice(in)
	for i := range out {
		if in[i].ext != nil {
			x := *in[i].ext
			x.Body = cloneBodyEditExpr(x.Body)
			x.Then = cloneBodyEditInstructions(x.Then)
			x.Else = cloneBodyEditInstructions(x.Else)
			x.Indices = cloneBodySlice(x.Indices)
			x.ValTypes = cloneBodySlice(x.ValTypes)
			x.Bytes = bytes.Clone(x.Bytes)
			x.Catches = cloneBodySlice(x.Catches)
			if x.MemArg.Mem != nil {
				idx := *x.MemArg.Mem
				x.MemArg.Mem = &idx
			}
			out[i].ext = &x
		}
	}
	return out
}

func cloneBodySlice[T any](in []T) []T {
	if in == nil {
		return nil
	}
	out := make([]T, len(in))
	copy(out, in)
	return out
}

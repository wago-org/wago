package wago

import (
	"fmt"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// freezeExecution captures metadata before compiler/loader publication, or on
// the first use of a hand-built Compiled. Instances never read the public view.
// Code ownership and validation are shared; public containers are deeply copied.
func (c *Compiled) freezeExecution() *Compiled {
	if c == nil {
		return nil
	}
	c.ensureCodeCache()
	c.codeCache.mu.Lock()
	defer c.codeCache.mu.Unlock()
	if c.validateMemo == nil {
		c.validateMemo = &validateMemo{}
	}
	if c.validateMemo.execution == nil {
		snapshot := cloneCompiledMetadata(c)
		c.validateMemo.execution = snapshot
	}
	return c.validateMemo.execution
}

func (c *Compiled) executionView() *Compiled {
	if c != nil && c.validateMemo != nil && c.validateMemo.execution != nil {
		return c.validateMemo.execution
	}
	return c
}

func cloneCompiledMetadata(c *Compiled) *Compiled {
	var valTypeCount, refInitCount, byteCount int
	for i := range c.Funcs {
		valTypeCount += len(c.Funcs[i].Params) + len(c.Funcs[i].Results)
	}
	for i := range c.importFuncSigs {
		valTypeCount += len(c.importFuncSigs[i].Params) + len(c.importFuncSigs[i].Results)
	}
	countElems := func(elems []ElemInit) {
		for i := range elems {
			byteCount += len(elems[i].Offset.Expr)
			refInitCount += len(elems[i].Values)
			for j := range elems[i].Values {
				byteCount += len(elems[i].Values[j].Expr)
			}
		}
	}
	for i := range c.Globals {
		byteCount += len(c.Globals[i].InitExpr)
	}
	countElems(c.Elems)
	countElems(c.passiveElems)
	for i := range c.Data {
		byteCount += len(c.Data[i].Bytes) + len(c.Data[i].Offset.Expr)
	}
	for i := range c.PassiveData {
		byteCount += len(c.PassiveData[i].Bytes)
	}
	entries := packedCloneStorage[int]{values: make([]int, len(c.Entry)+len(c.InternalEntry))}
	funcSigs := packedCloneStorage[FuncSig]{values: make([]FuncSig, len(c.Funcs)+len(c.importFuncSigs))}
	valTypes := packedCloneStorage[ValType]{values: make([]ValType, valTypeCount)}
	elems := packedCloneStorage[ElemInit]{values: make([]ElemInit, len(c.Elems)+len(c.passiveElems))}
	refInits := packedCloneStorage[RefInit]{values: make([]RefInit, refInitCount)}
	bytes := packedCloneStorage[byte]{values: make([]byte, byteCount)}

	out := *c
	out.Entry = entries.clone(c.Entry)
	out.InternalEntry = entries.clone(c.InternalEntry)
	out.Funcs = cloneFuncSigs(c.Funcs, &funcSigs, &valTypes)
	out.importFuncSigs = cloneFuncSigs(c.importFuncSigs, &funcSigs, &valTypes)
	out.Types, out.ValueTypes = cloneDefinedTypeDescriptorsAndValues(c.Types, c.ValueTypes)
	out.Imports = slices.Clone(c.Imports)
	out.Exports = cloneExportMap(c.Exports)
	out.Names = cloneCompiledNames(c.Names)
	out.GlobalImports = slices.Clone(c.GlobalImports)
	out.Globals = slices.Clone(c.Globals)
	for i := range out.Globals {
		out.Globals[i].InitExpr = bytes.clone(c.Globals[i].InitExpr)
	}
	out.GlobalExports = cloneExportMap(c.GlobalExports)
	out.FuncTypeID = slices.Clone(c.FuncTypeID)
	out.Elems = cloneCompiledElems(c.Elems, &elems, &refInits, &bytes)
	out.passiveElems = cloneCompiledElems(c.passiveElems, &elems, &refInits, &bytes)
	out.Data = slices.Clone(c.Data)
	for i := range out.Data {
		out.Data[i].Bytes = bytes.clone(c.Data[i].Bytes)
		out.Data[i].Offset.Expr = bytes.clone(c.Data[i].Offset.Expr)
	}
	out.PassiveData = slices.Clone(c.PassiveData)
	for i := range out.PassiveData {
		out.PassiveData[i].Bytes = bytes.clone(c.PassiveData[i].Bytes)
	}
	out.GCTypeDescs = slices.Clone(c.GCTypeDescs)
	gcFieldCount := 0
	for i := range c.GCTypeDescs {
		gcFieldCount += len(c.GCTypeDescs[i].Fields)
	}
	gcFields := packedCloneStorage[gc.FieldDesc]{values: make([]gc.FieldDesc, gcFieldCount)}
	for i := range out.GCTypeDescs {
		out.GCTypeDescs[i].Fields = gcFields.clone(c.GCTypeDescs[i].Fields)
	}
	return &out
}

type packedCloneStorage[T any] struct {
	values []T
	next   int
}

func (p *packedCloneStorage[T]) clone(in []T) []T {
	if in == nil {
		return nil
	}
	if len(in) == 0 {
		return make([]T, 0)
	}
	end := p.next + len(in)
	out := p.values[p.next:end:end]
	copy(out, in)
	p.next = end
	return out
}

func cloneFuncSigs(in []FuncSig, funcs *packedCloneStorage[FuncSig], valTypes *packedCloneStorage[ValType]) []FuncSig {
	out := funcs.clone(in)
	for i := range out {
		out[i].Params = valTypes.clone(in[i].Params)
		out[i].Results = valTypes.clone(in[i].Results)
	}
	return out
}

func cloneExportMap(in map[string]int) map[string]int {
	if in == nil {
		return nil
	}
	out := make(map[string]int, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneCompiledElems(in []ElemInit, elems *packedCloneStorage[ElemInit], refInits *packedCloneStorage[RefInit], bytes *packedCloneStorage[byte]) []ElemInit {
	out := elems.clone(in)
	for i := range out {
		out[i].Offset.Expr = bytes.clone(in[i].Offset.Expr)
		out[i].Values = refInits.clone(in[i].Values)
		for j := range out[i].Values {
			out[i].Values[j].Expr = bytes.clone(in[i].Values[j].Expr)
		}
	}
	return out
}

func cloneCompiledNames(in *wasm.NameSec) *wasm.NameSec {
	if in == nil {
		return nil
	}
	out := *in
	nameCount := len(in.FunctionNames) + len(in.TypeNames) + len(in.TableNames) + len(in.MemoryNames) +
		len(in.GlobalNames) + len(in.ElementNames) + len(in.DataNames) + len(in.TagNames)
	indirectCount := len(in.LocalNames) + len(in.LabelNames) + len(in.FieldNames)
	for _, names := range []wasm.IndirectNameMap{in.LocalNames, in.LabelNames, in.FieldNames} {
		for i := range names {
			nameCount += len(names[i].Names)
		}
	}
	names := packedCloneStorage[wasm.NameAssoc]{values: make([]wasm.NameAssoc, nameCount)}
	indirects := packedCloneStorage[wasm.IndirectNameAssoc]{values: make([]wasm.IndirectNameAssoc, indirectCount)}
	if in.ModuleName != nil {
		name := *in.ModuleName
		out.ModuleName = &name
	}
	out.FunctionNames = names.clone(in.FunctionNames)
	out.TypeNames = names.clone(in.TypeNames)
	out.TableNames = names.clone(in.TableNames)
	out.MemoryNames = names.clone(in.MemoryNames)
	out.GlobalNames = names.clone(in.GlobalNames)
	out.ElementNames = names.clone(in.ElementNames)
	out.DataNames = names.clone(in.DataNames)
	out.TagNames = names.clone(in.TagNames)
	cloneIndirect := func(in wasm.IndirectNameMap) wasm.IndirectNameMap {
		out := indirects.clone(in)
		for i := range out {
			out[i].Names = names.clone(in[i].Names)
		}
		return out
	}
	out.LocalNames = cloneIndirect(in.LocalNames)
	out.LabelNames = cloneIndirect(in.LabelNames)
	out.FieldNames = cloneIndirect(in.FieldNames)
	return &out
}

// Empty internal entries retain the legacy wrapper-only representation. Every
// present directory must cover all local functions. Only fresh compiler output
// may carry the direct-prepared marker; wire offsets are always non-negative.
func (c *Compiled) validateInternalEntries(artifact bool) error {
	if len(c.InternalEntry) != 0 && len(c.InternalEntry) != len(c.Entry) {
		return fmt.Errorf("compiled metadata invalid: InternalEntry length %d != Entry length %d", len(c.InternalEntry), len(c.Entry))
	}
	for i, entry := range c.InternalEntry {
		if artifact && entry < 0 {
			return fmt.Errorf("compiled metadata invalid: InternalEntry[%d] has compile-only marker", i)
		}
		off := internalEntryOffset(entry)
		if off < 0 || off >= len(c.code) {
			return fmt.Errorf("compiled metadata invalid: InternalEntry[%d] offset %d out of code range %d", i, off, len(c.code))
		}
	}
	return nil
}

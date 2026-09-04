package wago

import (
	"fmt"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/wasm"
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
	out := *c
	out.Entry = slices.Clone(c.Entry)
	out.InternalEntry = slices.Clone(c.InternalEntry)
	out.Funcs = cloneFuncSigs(c.Funcs)
	out.importFuncSigs = cloneFuncSigs(c.importFuncSigs)
	out.Types = cloneDefinedTypeDescriptors(c.Types)
	out.ValueTypes = slices.Clone(c.ValueTypes)
	out.Imports = slices.Clone(c.Imports)
	out.Exports = cloneExportMap(c.Exports)
	out.Names = cloneCompiledNames(c.Names)
	out.GlobalImports = slices.Clone(c.GlobalImports)
	out.Globals = slices.Clone(c.Globals)
	for i := range out.Globals {
		out.Globals[i].InitExpr = slices.Clone(c.Globals[i].InitExpr)
	}
	out.GlobalExports = cloneExportMap(c.GlobalExports)
	out.FuncTypeID = slices.Clone(c.FuncTypeID)
	out.Elems = cloneCompiledElems(c.Elems)
	out.passiveElems = cloneCompiledElems(c.passiveElems)
	out.Data = slices.Clone(c.Data)
	for i := range out.Data {
		out.Data[i].Bytes = slices.Clone(c.Data[i].Bytes)
		out.Data[i].Offset.Expr = slices.Clone(c.Data[i].Offset.Expr)
	}
	out.PassiveData = slices.Clone(c.PassiveData)
	for i := range out.PassiveData {
		out.PassiveData[i].Bytes = slices.Clone(c.PassiveData[i].Bytes)
	}
	out.GCTypeDescs = slices.Clone(c.GCTypeDescs)
	for i := range out.GCTypeDescs {
		out.GCTypeDescs[i].Fields = slices.Clone(c.GCTypeDescs[i].Fields)
	}
	return &out
}

func cloneFuncSigs(in []FuncSig) []FuncSig {
	out := slices.Clone(in)
	for i := range out {
		out[i].Params = slices.Clone(in[i].Params)
		out[i].Results = slices.Clone(in[i].Results)
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

func cloneCompiledElems(in []ElemInit) []ElemInit {
	out := slices.Clone(in)
	for i := range out {
		out[i].Offset.Expr = slices.Clone(in[i].Offset.Expr)
		out[i].Values = slices.Clone(in[i].Values)
		for j := range out[i].Values {
			out[i].Values[j].Expr = slices.Clone(in[i].Values[j].Expr)
		}
	}
	return out
}

func cloneCompiledNames(in *wasm.NameSec) *wasm.NameSec {
	if in == nil {
		return nil
	}
	out := *in
	if in.ModuleName != nil {
		name := *in.ModuleName
		out.ModuleName = &name
	}
	out.FunctionNames = slices.Clone(in.FunctionNames)
	out.TypeNames = slices.Clone(in.TypeNames)
	out.TableNames = slices.Clone(in.TableNames)
	out.MemoryNames = slices.Clone(in.MemoryNames)
	out.GlobalNames = slices.Clone(in.GlobalNames)
	out.ElementNames = slices.Clone(in.ElementNames)
	out.DataNames = slices.Clone(in.DataNames)
	out.TagNames = slices.Clone(in.TagNames)
	cloneIndirect := func(in wasm.IndirectNameMap) wasm.IndirectNameMap {
		out := slices.Clone(in)
		for i := range out {
			out[i].Names = slices.Clone(in[i].Names)
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

//go:build linux && !tinygo

package riscv64

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

type swarModuleImage struct {
	code      []byte
	functions []FunctionInfo
}

// This private module pilot owns a bounded source snapshot and admits every
// function before allocating an image. It has no production compiler entry or
// public feature gate. Only defined functions, their exports, inert metadata
// and one optional nonshared memory32 are supported; no initializers run here.
// The execution owner must provide matching call arenas and JobMemory layout.
func compileSWARModule(data []byte, mode emissionMode) (*swarModuleImage, error) {
	if !mode.production || len(data) > MaxModuleBytes {
		return nil, unsupported("SWAR module production/source bound")
	}
	owned := append([]byte(nil), data...)
	if err := admitSWARWire(owned); err != nil {
		return nil, err
	}
	decoded, err := wasm.DecodeModuleByteBackedWithLimits(owned, wasm.ValidationFeatures{}, wasm.DecodeLimits{MaxTypes: MaxFunctions, MaxMetadataBytes: 16 << 20})
	if err != nil {
		return nil, err
	}
	m := decoded.Module
	if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: maxSWARLocals}); err != nil {
		return nil, err
	}
	if len(m.Code) == 0 || len(m.Code) > MaxFunctions || len(m.FuncTypes) != len(m.Code) || len(m.Types) > MaxFunctions || len(m.Imports) != 0 || m.UsesCompactImports || len(m.Tables) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || m.Start != nil || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.BranchHints) != 0 || len(m.Memories) > 1 {
		return nil, unsupported("SWAR module state/features/function bound")
	}
	for _, custom := range m.Customs {
		if custom.Name != "name" && custom.Name != "producers" {
			return nil, unsupported("SWAR custom section " + custom.Name)
		}
	}
	types := make([]*wasm.CompType, len(m.Types))
	for i := range m.Types {
		rec := &m.Types[i]
		if len(rec.SubTypes) != 1 {
			return nil, unsupported("SWAR recursive type group")
		}
		sub := &rec.SubTypes[0]
		if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() {
			return nil, unsupported("SWAR type metadata")
		}
		if err := swarCallSignature(&sub.Comp); err != nil {
			return nil, err
		}
		types[i] = &sub.Comp
	}
	for _, ex := range m.Exports {
		if ex.Index.Kind != wasm.ExternFunc || uint64(ex.Index.Index) >= uint64(len(m.Code)) {
			return nil, unsupported("SWAR module export shape")
		}
	}
	var memory *wasm.MemType
	if len(m.Memories) != 0 {
		memory = &m.Memories[0]
		if memory.Shared || memory.Limits.Addr64 || memory.Limits.Min > 65536 || (memory.Limits.HasMax && (memory.Limits.Max < memory.Limits.Min || memory.Limits.Max > 65536)) {
			return nil, unsupported("SWAR module memory32 resource")
		}
	}
	functions := make([]*wasm.CompType, len(m.Code))
	for i := range functions {
		ft, ok := m.LocalFuncType(i)
		if !ok {
			return nil, unsupported("SWAR module function type")
		}
		functions[i] = ft
	}
	summaries := make([]swarSummary, len(functions))
	bound := 0
	for i := range functions {
		summaries[i], err = walkSWARPairsWithCalls(&m.Code[i], functions[i], memory != nil, types, functions, nil)
		if err != nil {
			return nil, fmt.Errorf("rv64: SWAR function %d: %w", i, err)
		}
		if summaries[i].usesLoops && !mode.interruptible {
			return nil, unsupported("SWAR module loops require interruption bridge")
		}
		if summaries[i].codeBound > MaxCodeBytes-bound-15 {
			return nil, unsupported("SWAR module aggregate code bound")
		}
		bound += summaries[i].codeBound + 15
	}
	image := &swarModuleImage{functions: make([]FunctionInfo, len(functions))}
	var calls []directCall
	for i := range functions {
		functionMode := mode
		functionMode.functionIndex = uint32(i)
		functionMode.entryPC = uint32(m.Code[i].LocalDeclBytes)
		code, info, relocations, err := compileSWARPairsWithCalls(&m.Code[i], functions[i], memory, types, functions, functionMode)
		if err != nil {
			return nil, fmt.Errorf("rv64: SWAR function %d: %w", i, err)
		}
		padding := (-len(image.code)) & 15
		if len(code) > MaxCodeBytes-len(image.code)-padding {
			return nil, unsupported("SWAR module image bound")
		}
		for ; padding > 0; padding -= 4 {
			image.code = append(image.code, 0x13, 0, 0, 0)
		}
		info.Entry, info.CodeBytes = len(image.code), len(code)
		image.functions[i] = info
		for j := range relocations {
			relocations[j].site += info.Entry
		}
		calls = append(calls, relocations...)
		image.code = append(image.code, code...)
	}
	a := enc.Asm{B: image.code}
	for _, call := range calls {
		if err := a.PatchLong(call.site, int64(image.functions[call.callee].Entry)); err != nil {
			return nil, fmt.Errorf("rv64: SWAR direct relocation: %w", err)
		}
	}
	return image, nil
}

// Reject unsupported sections even when empty. A decoded module alone cannot
// distinguish absent initializers from present empty feature sections.
func admitSWARWire(data []byte) error {
	if len(data) < 8 || string(data[:8]) != "\x00asm\x01\x00\x00\x00" {
		return fmt.Errorf("rv64: invalid SWAR module header")
	}
	r := wasm.ReaderFrom(data[8:])
	for r.HasNext() {
		id, err := r.Byte()
		if err != nil {
			return err
		}
		size, err := r.U32()
		if err != nil || uint64(size) > uint64(r.BytesLeft()) {
			return fmt.Errorf("rv64: truncated SWAR section")
		}
		switch id {
		case 0, 1, 3, 5, 7, 10:
		default:
			return unsupported(fmt.Sprintf("SWAR section %d", id))
		}
		if err := r.JumpTo(r.Offset() + int(size)); err != nil {
			return err
		}
	}
	return nil
}

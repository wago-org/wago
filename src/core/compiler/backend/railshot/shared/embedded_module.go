package shared

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
)

type EmbeddedFunctionMetadata struct {
	FuncIndex   uint32
	Offset      uint32
	Size        uint32
	ParamSlots  uint16
	ResultSlots uint16
}

type EmbeddedModule struct {
	Code              []byte
	Entry             []int
	Functions         []EmbeddedFunctionMetadata
	RequiredCodeBytes uint32
}

type PublishedEmbeddedModule struct {
	Block     embedded32.CodeBlock
	Entry     []uint32
	Functions []EmbeddedFunctionMetadata
}

func PublishEmbeddedModule(arena *embedded32.CodeArena, module *EmbeddedModule, publish embedded32.CodePublisher) (*PublishedEmbeddedModule, error) {
	if arena == nil || module == nil {
		return nil, embedded32.ErrInvalidArena
	}
	tx, err := arena.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	block, err := tx.Allocate(uint32(len(module.Code)), 16)
	if err != nil {
		return nil, err
	}
	copy(block.Bytes, module.Code)
	if err := tx.Commit(publish); err != nil {
		return nil, err
	}
	out := &PublishedEmbeddedModule{Block: block, Entry: make([]uint32, len(module.Entry)), Functions: make([]EmbeddedFunctionMetadata, len(module.Functions))}
	for i, entry := range module.Entry {
		out.Entry[i] = block.Offset + uint32(entry)
	}
	for i, meta := range module.Functions {
		meta.Offset += block.Offset
		out.Functions[i] = meta
	}
	return out, nil
}

type EmbeddedModuleOptions struct {
	CodeCapacity    uint32
	EnforceCapacity bool
}

type EmbeddedFunctionCompiler func(ft *wasm.CompType, locals []wasm.LocalRun, body []byte) ([]byte, error)

// CompileEmbeddedModule validates and lays out a module while delegating exact
// homogeneous or mixed-width function admission to the target compiler.
func CompileEmbeddedModule(m *wasm.Module, opts EmbeddedModuleOptions, target string, expansion int, alignmentPad []byte, compile EmbeddedFunctionCompiler) (*EmbeddedModule, error) {
	if m == nil {
		return nil, fmt.Errorf("%s: nil module", target)
	}
	if compile == nil || len(alignmentPad) == 0 || 16%len(alignmentPad) != 0 {
		return nil, fmt.Errorf("%s: invalid module compiler configuration", target)
	}
	if err := wasm.ValidateModule(m); err != nil {
		return nil, fmt.Errorf("%s: module validation: %w", target, err)
	}
	if len(m.Imports) != 0 {
		return nil, fmt.Errorf("%s: module imports are not supported", target)
	}
	if len(m.Tables) != 0 || len(m.Memories) > 1 || len(m.Globals) != 0 || len(m.Elements) != 0 || len(m.Data) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || m.Start != nil {
		return nil, fmt.Errorf("%s: module contains unsupported runtime state", target)
	}
	if len(m.Memories) == 1 && (m.Memories[0].Limits.Addr64 || m.Memories[0].Shared) {
		return nil, fmt.Errorf("%s: target requires unshared memory32", target)
	}
	if len(m.FuncTypes) != len(m.Code) {
		return nil, fmt.Errorf("%s: function/code count mismatch", target)
	}

	totalBody := 0
	bodies := make([][]byte, len(m.Code))
	types := make([]*wasm.CompType, len(m.Code))
	for i := range m.Code {
		ft, ok := m.LocalFuncType(i)
		if !ok || ft.Kind != wasm.CompFunc {
			return nil, fmt.Errorf("%s: function %d has no function type", target, i)
		}
		body := appendULEB32(nil, uint32(len(m.Code[i].Locals.Runs)))
		for _, run := range m.Code[i].Locals.Runs {
			encoded, ok := wasm.EncodeValType(run.Type)
			if !ok {
				return nil, fmt.Errorf("%s: function %d local type %s has no embedded encoding", target, i, run.Type)
			}
			body = appendULEB32(body, run.Count)
			body = append(body, encoded)
		}
		if len(m.Code[i].BodyBytes) == 0 {
			return nil, fmt.Errorf("%s: function %d has no byte-backed body", target, i)
		}
		body = append(body, m.Code[i].BodyBytes...)
		bodies[i], types[i] = body, ft
		totalBody += len(body)
	}

	required := ModuleCodeCapacity(totalBody, len(bodies), expansion)
	if required == 0 || uint64(required) > uint64(^uint32(0)) {
		return nil, fmt.Errorf("%s: module code capacity overflow", target)
	}
	bounded := opts.EnforceCapacity || opts.CodeCapacity != 0
	if bounded && uint32(required) > opts.CodeCapacity {
		return nil, fmt.Errorf("%s: code arena capacity %d is below preflight requirement %d", target, opts.CodeCapacity, required)
	}
	out := &EmbeddedModule{Code: make([]byte, 0, required), Entry: make([]int, len(bodies)), Functions: make([]EmbeddedFunctionMetadata, len(bodies)), RequiredCodeBytes: uint32(required)}
	for i, body := range bodies {
		pad := (16 - len(out.Code)%16) % 16
		if pad%len(alignmentPad) != 0 {
			return nil, fmt.Errorf("%s: function %d has incompatible code alignment", target, i)
		}
		for j := 0; j < pad; j += len(alignmentPad) {
			out.Code = append(out.Code, alignmentPad...)
		}
		entry := len(out.Code)
		fnCode, err := compile(types[i], m.Code[i].Locals.Runs, body)
		if err != nil {
			return nil, fmt.Errorf("%s: function %d: %w", target, i, err)
		}
		if len(fnCode)%len(alignmentPad) != 0 {
			return nil, fmt.Errorf("%s: function %d emitted misaligned code size %d", target, i, len(fnCode))
		}
		params, err := serializedSlots(types[i].Params)
		if err != nil {
			return nil, fmt.Errorf("%s: function %d parameters: %w", target, i, err)
		}
		results, err := serializedSlots(types[i].Results)
		if err != nil {
			return nil, fmt.Errorf("%s: function %d results: %w", target, i, err)
		}
		out.Entry[i] = entry
		out.Code = append(out.Code, fnCode...)
		out.Functions[i] = EmbeddedFunctionMetadata{FuncIndex: uint32(i), Offset: uint32(entry), Size: uint32(len(fnCode)), ParamSlots: uint16(params), ResultSlots: uint16(results)}
	}
	if bounded && uint32(len(out.Code)) > opts.CodeCapacity {
		return nil, fmt.Errorf("%s: compiled code size %d exceeds arena capacity %d", target, len(out.Code), opts.CodeCapacity)
	}
	return out, nil
}

// CompileEmbeddedI32Module preserves the original strict i32 entry point for
// tests and callers which intentionally admit only the initial scalar subset.
func CompileEmbeddedI32Module(m *wasm.Module, opts EmbeddedModuleOptions, target string, maxParams, expansion int, alignmentPad []byte, compile func(int, []byte) ([]byte, error)) (*EmbeddedModule, error) {
	return CompileEmbeddedModule(m, opts, target, expansion, alignmentPad, func(ft *wasm.CompType, locals []wasm.LocalRun, body []byte) ([]byte, error) {
		if len(ft.Params) > maxParams {
			return nil, fmt.Errorf("has %d parameters, maximum is %d", len(ft.Params), maxParams)
		}
		for _, typ := range ft.Params {
			if typ != wasm.I32 {
				return nil, fmt.Errorf("parameter type %s is not yet supported", typ)
			}
		}
		if len(ft.Results) > 1 || len(ft.Results) == 1 && ft.Results[0] != wasm.I32 {
			return nil, fmt.Errorf("result signature is not yet supported")
		}
		for _, run := range locals {
			if run.Type != wasm.I32 {
				return nil, fmt.Errorf("local type %s is not yet supported", run.Type)
			}
		}
		return compile(len(ft.Params), body)
	})
}

func serializedSlots(types []wasm.ValType) (int, error) {
	n := 0
	for _, typ := range types {
		switch typ {
		case wasm.I32, wasm.F32:
			n++
		case wasm.I64, wasm.F64:
			n += 2
		case wasm.V128:
			n += 4
		default:
			if typ.Kind == wasm.ValRef {
				n++
			} else {
				return 0, fmt.Errorf("unsupported value type %s", typ)
			}
		}
		if n > int(^uint16(0)) {
			return 0, fmt.Errorf("serialized slot count overflow")
		}
	}
	return n, nil
}

func appendULEB32(dst []byte, v uint32) []byte {
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		dst = append(dst, b)
		if v == 0 {
			return dst
		}
	}
}

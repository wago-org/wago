package shared

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
)

// EmbeddedFunctionMetadata describes one local function in a fixed embedded
// module image. Offset and Size are byte offsets within Code.
type EmbeddedFunctionMetadata struct {
	FuncIndex uint32
	Offset    uint32
	Size      uint32
}

// EmbeddedModule is the architecture-neutral layout produced by the first
// module-wide 32-bit compiler stage.
type EmbeddedModule struct {
	Code              []byte
	Entry             []int
	Functions         []EmbeddedFunctionMetadata
	RequiredCodeBytes uint32
}

// PublishedEmbeddedModule identifies one transactionally published module in a
// firmware code arena. Entry contains absolute arena offsets.
type PublishedEmbeddedModule struct {
	Block     embedded32.CodeBlock
	Entry     []uint32
	Functions []EmbeddedFunctionMetadata
}

// PublishEmbeddedModule copies a complete compiled image into one code-arena
// transaction. A capacity or publication failure rolls back every byte.
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
	out := &PublishedEmbeddedModule{
		Block:     block,
		Entry:     make([]uint32, len(module.Entry)),
		Functions: make([]EmbeddedFunctionMetadata, len(module.Functions)),
	}
	for i, entry := range module.Entry {
		out.Entry[i] = block.Offset + uint32(entry)
	}
	for i, meta := range module.Functions {
		meta.Offset += block.Offset
		out.Functions[i] = meta
	}
	return out, nil
}

// EmbeddedModuleOptions bounds compilation for a firmware code arena. A zero
// CodeCapacity performs an unbounded cross-host compile unless EnforceCapacity
// is set; bounded capacity is checked conservatively before any function is
// compiled and exactly afterward.
type EmbeddedModuleOptions struct {
	CodeCapacity    uint32
	EnforceCapacity bool
}

// CompileEmbeddedI32Module lays out a strict i32/control subset as one module
// image. It is shared by Thumb-2 and RV32 so compatibility rejection, local-run
// reconstruction, capacity preflight, alignment, and metadata stay identical.
func CompileEmbeddedI32Module(m *wasm.Module, opts EmbeddedModuleOptions, target string, maxParams, expansion int, alignmentPad []byte, compile func(int, []byte) ([]byte, error)) (*EmbeddedModule, error) {
	if m == nil {
		return nil, fmt.Errorf("%s: nil module", target)
	}
	if len(alignmentPad) == 0 || 16%len(alignmentPad) != 0 {
		return nil, fmt.Errorf("%s: invalid module alignment encoding", target)
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
	paramCounts := make([]int, len(m.Code))
	for i := range m.Code {
		ft, ok := m.LocalFuncType(i)
		if !ok || ft.Kind != wasm.CompFunc {
			return nil, fmt.Errorf("%s: function %d has no function type", target, i)
		}
		if len(ft.Params) > maxParams {
			return nil, fmt.Errorf("%s: function %d has %d parameters, maximum is %d", target, i, len(ft.Params), maxParams)
		}
		for _, typ := range ft.Params {
			if typ != wasm.I32 {
				return nil, fmt.Errorf("%s: function %d parameter type %s is not yet supported", target, i, typ)
			}
		}
		if len(ft.Results) > 1 || len(ft.Results) == 1 && ft.Results[0] != wasm.I32 {
			return nil, fmt.Errorf("%s: function %d result signature is not yet supported", target, i)
		}
		body := appendULEB32(nil, uint32(len(m.Code[i].Locals.Runs)))
		for _, run := range m.Code[i].Locals.Runs {
			if run.Type != wasm.I32 {
				return nil, fmt.Errorf("%s: function %d local type %s is not yet supported", target, i, run.Type)
			}
			body = appendULEB32(body, run.Count)
			body = append(body, byte(wasm.NumI32))
		}
		if len(m.Code[i].BodyBytes) == 0 {
			return nil, fmt.Errorf("%s: function %d has no byte-backed body", target, i)
		}
		body = append(body, m.Code[i].BodyBytes...)
		bodies[i] = body
		paramCounts[i] = len(ft.Params)
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

	out := &EmbeddedModule{
		Code:              make([]byte, 0, required),
		Entry:             make([]int, len(bodies)),
		Functions:         make([]EmbeddedFunctionMetadata, len(bodies)),
		RequiredCodeBytes: uint32(required),
	}
	for i, body := range bodies {
		pad := (16 - len(out.Code)%16) % 16
		if pad%len(alignmentPad) != 0 {
			return nil, fmt.Errorf("%s: function %d has incompatible code alignment", target, i)
		}
		for j := 0; j < pad; j += len(alignmentPad) {
			out.Code = append(out.Code, alignmentPad...)
		}
		entry := len(out.Code)
		fnCode, err := compile(paramCounts[i], body)
		if err != nil {
			return nil, fmt.Errorf("%s: function %d: %w", target, i, err)
		}
		if len(fnCode)%len(alignmentPad) != 0 {
			return nil, fmt.Errorf("%s: function %d emitted misaligned code size %d", target, i, len(fnCode))
		}
		out.Entry[i] = entry
		out.Code = append(out.Code, fnCode...)
		out.Functions[i] = EmbeddedFunctionMetadata{FuncIndex: uint32(i), Offset: uint32(entry), Size: uint32(len(fnCode))}
	}
	if bounded && uint32(len(out.Code)) > opts.CodeCapacity {
		return nil, fmt.Errorf("%s: compiled code size %d exceeds arena capacity %d", target, len(out.Code), opts.CodeCapacity)
	}
	return out, nil
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

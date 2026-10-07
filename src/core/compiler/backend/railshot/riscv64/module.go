//go:build linux && !tinygo

package riscv64

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	"github.com/wago-org/wago/src/core/runtime/abi"
	native "github.com/wago-org/wago/src/core/runtime/riscv64"
)

const (
	MaxModuleBytes = 1 << 20
	MaxFunctions   = 1024
	MaxCodeBytes   = 16 << 20
	MaxFrameBytes  = 128 << 10
)

var ErrUnsupported = errors.New("rv64: unsupported experimental integer shape")
var ErrCancelable = errors.New("rv64: cancelable invocation is not implemented")

// FunctionInfo describes the emitted whole-function path and frame.
type FunctionInfo struct {
	Entry, CodeBytes, Params, Results, FrameBytes, SpillSlots, Spills, Reloads int
	Result64                                                                   bool
	Fallback                                                                   bool
	FrameHeaderBytes, DirectCalls                                              int
	mode                                                                       emissionMode
}

// Compiled owns immutable heap code and metadata, never executable mappings.
// Every function/type/export was admitted before any instruction was emitted.
type Compiled struct {
	code      []byte
	functions []FunctionInfo
	exports   map[string]int
	state     moduleState
}

func (c *Compiled) Functions() []FunctionInfo {
	if c == nil {
		return nil
	}
	return append([]FunctionInfo(nil), c.functions...)
}

// Compile snapshots a bounded Wasm binary, normally decodes and validates it,
// admits the complete module, then selects shared scalar or integer fallback
// lowering for each whole function before emitting any code.
// Caller input must not be mutated concurrently with the initial snapshot.
func Compile(data []byte) (*Compiled, error) { return CompileWithOptions(data, CompileOptions{}) }

// CompileWithOptions retains the isolated experimental guest-context ABI.
// CompileModuleWith is the production shared compiler entry point.
func CompileWithOptions(data []byte, opts CompileOptions) (*Compiled, error) {
	return compileOptions(data, scalarRegisters[:], opts)
}

func compile(data []byte, regs []uint8) (*Compiled, error) {
	return compileOptions(data, regs, CompileOptions{})
}
func compileOptions(data []byte, regs []uint8, opts CompileOptions) (*Compiled, error) {
	if opts.ForeignTableCalls && (!opts.production || !opts.Interruptible) {
		return nil, unsupported("foreign table calls require the production cooperative runtime ABI")
	}
	if opts.Interruptible && !opts.production {
		return nil, unsupported("cooperative polls require the production trap ABI")
	}
	if len(data) > MaxModuleBytes {
		return nil, fmt.Errorf("rv64: module exceeds %d bytes", MaxModuleBytes)
	}
	owned := append([]byte(nil), data...)
	decoded, err := wasm.DecodeModuleByteBackedWithLimits(owned, wasm.ValidationFeatures{}, wasm.DecodeLimits{MaxTypes: MaxFunctions, MaxMetadataBytes: 16 << 20})
	if err != nil {
		return nil, err
	}
	m := decoded.Module
	if err = wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256}); err != nil {
		return nil, err
	}
	if err = admitWireMode(owned, opts.production); err != nil {
		return nil, err
	}
	if opts.production {
		if err = admitProductionWire(owned); err != nil {
			return nil, err
		}
	}
	return compileDecodedModule(m, regs, opts)
}

// The public frontend has already validated its configured limits and retains
// the decoded source. Lowering still enforces the bounded RV64 module policy.
func compileDecodedModule(m *wasm.Module, regs []uint8, opts CompileOptions) (*Compiled, error) {
	if m == nil {
		return nil, fmt.Errorf("rv64: nil module")
	}
	var err error
	if err = admitShapeProfile(m, opts.production, opts.publicStarts); err != nil {
		return nil, err
	}
	state, err := admitStatePolicy(m, opts.production, opts.hostImports, opts.publicStarts, opts.production && opts.publicStarts && opts.Interruptible && opts.ForeignTableCalls)
	if err != nil {
		return nil, err
	}
	summaries := make([]shared.ScalarSummary, len(m.Code))
	integerSummaries := make([]integerSummary, len(m.Code))
	fallback := make([]bool, len(m.Code))
	for i := range m.Code {
		ft, _ := m.LocalFuncType(i)
		summaries[i] = shared.AdmitScalarFunction(&m.Code[i], ft)
		fallback[i] = opts.ForceFallback || !summaries[i].Eligible
		if fallback[i] {
			integerSummaries[i], err = walkInteger(&m.Code[i], ft, &state, nil)
			if err != nil {
				return nil, fmt.Errorf("rv64: function %d: %w", i, err)
			}
		}
	}
	c := &Compiled{state: state, functions: make([]FunctionInfo, len(m.Code)), exports: make(map[string]int, len(m.Exports))}
	var worker shared.ScalarState
	var relocations []directCall
	defer worker.FinishWorker()
	for i := range m.Code {
		ft, _ := m.LocalFuncType(i)
		var code []byte
		var info FunctionInfo
		var calls []directCall
		mode := emissionMode{production: opts.production, interruptible: opts.Interruptible, foreignTableCalls: opts.ForeignTableCalls}
		if opts.production {
			mode.functionIndex = uint32(i + state.importedFunctions)
			mode.entryPC = uint32(m.Code[i].LocalDeclBytes)
		}
		if fallback[i] {
			code, info, calls, err = compileIntegerMode(&m.Code[i], ft, &state, integerSummaries[i], mode)
		} else {
			code, info, err = compileFunctionMode(&worker, &m.Code[i], ft, summaries[i], regs, mode)
		}
		if err != nil {
			return nil, fmt.Errorf("rv64: function %d: %w", i, err)
		}
		padding := 0
		if opts.production {
			padding = (-len(c.code)) & 15
		}
		if len(code) > MaxCodeBytes-len(c.code)-padding {
			return nil, fmt.Errorf("rv64: emitted code exceeds %d bytes", MaxCodeBytes)
		}
		for ; padding > 0; padding -= 4 {
			c.code = append(c.code, 0x13, 0, 0, 0) // fixed-width RV64 NOP
		}
		info.Entry = len(c.code)
		info.CodeBytes = len(code)
		c.code = append(c.code, code...)
		c.functions[i] = info
		for j := range calls {
			calls[j].site += info.Entry
		}
		relocations = append(relocations, calls...)
	}
	// All framed entries are now known. These remain image-relative transfers;
	// no executable addresses or alternate-backend code are retained.
	image := enc.Asm{B: c.code}
	for _, call := range relocations {
		if err := image.PatchLong(call.site, int64(c.functions[call.callee].Entry)); err != nil {
			return nil, fmt.Errorf("rv64: direct call relocation: %w", err)
		}
	}
	for _, ex := range m.Exports {
		if ex.Index.Kind == wasm.ExternFunc && ex.Index.Index >= uint32(state.importedFunctions) {
			c.exports[ex.Name] = int(ex.Index.Index) - state.importedFunctions
		}
	}
	return c, nil
}

func unsupported(detail string) error { return fmt.Errorf("%w: %s", ErrUnsupported, detail) }

// The wire allowlist also rejects present-but-empty unsupported sections and
// explicit singleton rec groups whose provenance the decoded Module loses.
// Normal decoding/validation always runs first; this is an execution policy.
func admitWire(data []byte) error { return admitWireMode(data, false) }

func admitWireMode(data []byte, floating bool) error {
	return admitWireProfile(data, floating, false)
}

func admitWireProfile(data []byte, floating, vectors bool) error {
	return admitWireStartProfile(data, floating, vectors, false)
}

func admitWireStartProfile(data []byte, floating, vectors, publicStarts bool) error {
	r := wasm.ReaderFrom(data[8:])
	for r.HasNext() {
		id, err := r.Byte()
		if err != nil {
			return err
		}
		n, err := r.U32()
		if err != nil {
			return err
		}
		if uint64(n) > uint64(r.BytesLeft()) {
			return fmt.Errorf("rv64: truncated section")
		}
		start := r.Offset()
		end := start + int(n)
		switch id {
		case 0, 2, 3, 5, 6, 7, 10, 11, 12:
		case 8:
			if !publicStarts {
				return unsupported("defined start execution is not enabled")
			}
		case 4, 9:
			if !floating {
				return unsupported("isolated table/element sections")
			}
		case 1:
			s := wasm.ReaderFrom(data[8+start : 8+end])
			count, err := s.U32()
			if err != nil {
				return err
			}
			for i := uint32(0); i < count; i++ {
				kind, err := s.Byte()
				if err != nil {
					return err
				}
				if kind != 0x60 {
					return unsupported("type grammar is not a plain function")
				}
				for v := 0; v < 2; v++ {
					n, err := s.U32()
					if err != nil {
						return err
					}
					if n > 256 {
						return unsupported("signature arity")
					}
					for j := uint32(0); j < n; j++ {
						t, err := s.Byte()
						if err != nil {
							return err
						}
						if t != 0x7f && t != 0x7e && (!floating || t != 0x7d && t != 0x7c) && (!vectors || t != 0x7b) {
							return unsupported("noninteger type")
						}
					}
				}
			}
			if s.HasNext() {
				return unsupported("extra type bytes")
			}
		default:
			return unsupported(fmt.Sprintf("section %d", id))
		}
		if err = r.JumpTo(end); err != nil {
			return err
		}
	}
	return nil
}

func admitShape(m *wasm.Module) error { return admitShapeMode(m, false) }

func admitShapeMode(m *wasm.Module, floating bool) error {
	return admitShapeProfile(m, floating, false)
}

func admitShapeProfile(m *wasm.Module, floating, publicStarts bool) error {
	// Public modules may contain only state. Import-only modules execute through
	// instance-owned bridges; neither shape needs placeholder instructions.
	if len(m.Code) > MaxFunctions || len(m.Code) == 0 && (!floating || !publicStarts) {
		return unsupported("function count outside 1..1024")
	}
	if publicStarts {
		if err := admitProductionStart(m); err != nil {
			return err
		}
	}
	if m.UsesCompactImports || (!floating && (len(m.Tables) != 0 || len(m.Elements) != 0)) || len(m.Tags) != 0 || len(m.StringRefs) != 0 || m.Start != nil && !publicStarts || len(m.BranchHints) != 0 {
		return unsupported("imports, state, initializers or feature metadata")
	}
	for _, custom := range m.Customs {
		// Public compilation ignores opaque custom payloads. Branch hints have
		// a decoded semantic representation and retain their existing gate,
		// including a present empty section. Private pilots keep their allowlist.
		if custom.Name == "metadata.code.branch_hint" || !publicStarts && custom.Name != "name" && custom.Name != "producers" {
			return unsupported("custom section " + custom.Name)
		}
	}
	for _, rec := range m.Types {
		if len(rec.SubTypes) != 1 {
			return unsupported("recursive type group")
		}
		s := &rec.SubTypes[0]
		if !s.Final || s.HasPrefix || len(s.Supers) != 0 || s.Metadata.Describes.Present() || s.Metadata.Descriptor.Present() || s.Comp.Kind != wasm.CompFunc {
			return unsupported("unsupported type metadata")
		}
		if len(s.Comp.Params) > 256 || len(s.Comp.Results) > 256 {
			return unsupported("signature arity")
		}
		for _, v := range append(append([]wasm.ValType(nil), s.Comp.Params...), s.Comp.Results...) {
			if !scalarType(v, floating) {
				return unsupported("noninteger signature")
			}
		}
	}
	return nil
}

func compileFunction(worker *shared.ScalarState, fn *wasm.Func, ft *wasm.CompType, summary shared.ScalarSummary, regs []uint8) ([]byte, FunctionInfo, error) {
	return compileFunctionMode(worker, fn, ft, summary, regs, emissionMode{})
}

func compileFunctionMode(worker *shared.ScalarState, fn *wasm.Func, ft *wasm.CompType, summary shared.ScalarSummary, regs []uint8, mode emissionMode) ([]byte, FunctionInfo, error) {
	var widths [256]bool
	nlocals := len(ft.Params)
	for i, t := range ft.Params {
		widths[i] = wasm.EqualValType(t, wasm.I64)
	}
	for _, run := range fn.Locals.Runs {
		for i := uint32(0); i < run.Count; i++ {
			widths[nlocals] = wasm.EqualValType(run.Type, wasm.I64)
			nlocals++
		}
	}
	f := target{regs: regs, nlocals: nlocals, mode: mode}
	if regallocCheckEnabled {
		defer f.checkEndLifetimes()
	}
	slots, err := worker.CompileScalar(fn.BodyBytes, summary, widths[:nlocals], len(ft.Params), &f)
	if err != nil {
		return nil, FunctionInfo{}, err
	}
	if f.err != nil {
		return nil, FunctionInfo{}, f.err
	}
	if f.a.Err() != nil {
		return nil, FunctionInfo{}, f.a.Err()
	}
	info := FunctionInfo{Params: len(ft.Params), Results: 1, SpillSlots: slots, Spills: worker.Spills, Reloads: worker.Reloads, Result64: wasm.EqualValType(ft.Results[0], wasm.I64), mode: mode}
	return frameFunction(f.a.B, widths[:nlocals], len(ft.Params), slots, false, -1, info)
}

func frameFunction(body []byte, widths []bool, nparams, slots int, initialize bool, contextOffset int, info FunctionInfo) ([]byte, FunctionInfo, error) {
	return frameFunctionCalls(body, widths, nparams, slots, initialize, contextOffset, info, nil)
}
func frameFunctionCalls(body []byte, widths []bool, nparams, slots int, initialize bool, contextOffset int, info FunctionInfo, calls []directCall) ([]byte, FunctionInfo, error) {
	nlocals := len(widths)
	if info.FrameHeaderBytes == 0 {
		info.FrameHeaderBytes = 16
	}
	frame := (info.FrameHeaderBytes + 8*nlocals + 8*slots + 15) &^ 15
	if frame > MaxFrameBytes {
		return nil, FunctionInfo{}, fmt.Errorf("frame exceeds %d bytes", MaxFrameBytes)
	}
	entry := target{header: info.FrameHeaderBytes, mode: info.mode}
	fence := enc.X13
	if info.mode.production {
		entry.a.Ld(enc.X29, enc.X25, -abi.StackFenceOffset)
		fence = enc.X29
	}
	reversed := entry.a.BranchLong(enc.LTU, enc.SP, fence)
	entry.a.Constant64(enc.Scratch0, int64(frame))
	entry.a.Sub(enc.Scratch1, enc.SP, fence)
	bad := entry.a.BranchLong(enc.LTU, enc.Scratch1, enc.Scratch0)
	entry.emitProductionEntryPoll()
	entry.a.Sub(enc.SP, enc.SP, enc.Scratch0)
	entry.a.Sd(enc.RA, enc.SP, 0)
	result := enc.X11
	if info.mode.production {
		result = enc.X13
	}
	entry.a.Sd(result, enc.SP, 8)
	if info.FrameHeaderBytes == callFrameHeader {
		entry.a.Sd(enc.X10, enc.SP, 16)
		entry.a.Sd(enc.X12, enc.SP, 24)
		if info.mode.production {
			entry.a.Sd(enc.X11, enc.SP, 32)
		} else {
			entry.a.Sd(enc.X13, enc.SP, 32)
		}
	}
	if contextOffset != -1 && !info.mode.production {
		if contextOffset <= 2047 {
			entry.a.Ld(enc.X24, enc.X10, int32(contextOffset))
		} else {
			entry.a.Constant64(enc.Scratch1, int64(contextOffset))
			entry.a.Add(enc.Scratch1, enc.X10, enc.Scratch1)
			entry.a.Ld(enc.X24, enc.Scratch1, 0)
		}
	}
	for i := 0; i < nparams; i++ {
		entry.a.Ld(enc.Scratch0, enc.X10, int32(i*8))
		entry.Store(entry.LocalOffset(i), uint8(enc.Scratch0), widths[i])
	}
	if initialize {
		for i := nparams; i < nlocals; i++ {
			entry.Store(entry.LocalOffset(i), uint8(enc.Zero), widths[i])
		}
	}
	for j := range calls {
		calls[j].site += entry.Position()
	}
	entry.a.B = append(entry.a.B, body...)
	entry.a.Ld(enc.RA, enc.SP, 0)
	entry.a.Constant64(enc.Scratch0, int64(frame))
	entry.a.Add(enc.SP, enc.SP, enc.Scratch0)
	entry.a.Ret()
	if err := entry.a.PatchLong(bad, int64(entry.a.Len())); err != nil {
		return nil, FunctionInfo{}, err
	}
	if err := entry.a.PatchLong(reversed, int64(entry.a.Len())); err != nil {
		return nil, FunctionInfo{}, err
	}
	// The failure edge occurs before body allocation; original a2 and RA/SP
	// are still valid. No stack writes or SP changes precede this edge.
	if info.mode.production {
		entry.emitProductionTrap(abi.TrapStackFenceBreached, info.mode.entryPC)
	} else {
		entry.a.Addi(enc.Scratch0, enc.Zero, 1)
		entry.a.Sw(enc.Scratch0, enc.X12, 0)
		entry.a.Addi(enc.X10, enc.Zero, 0)
		entry.a.Ret()
	}
	if entry.err != nil {
		return nil, FunctionInfo{}, entry.err
	}
	if entry.a.Err() != nil {
		return nil, FunctionInfo{}, entry.a.Err()
	}
	info.FrameBytes = frame
	return entry.a.B, info, nil
}

// Instance owns all executable and call resources. It accepts noncancelable
// contexts; deadlines and host reentry are unsupported.
type Instance struct {
	mu        sync.Mutex
	closed    bool
	program   *native.Program
	engine    *native.Engine
	call      *native.CallFrame
	compiled  *Compiled
	memory    *native.LinearMemory
	lease     *native.MemoryLease
	context   *native.GuestContext
	ownMemory bool
}

// Instantiate publishes only previously admitted RV64 code. On error a nonnil
// Instance is closed and retains failed cleanup ownership; call Close to retry.
func (c *Compiled) Instantiate(stackBytes int) (*Instance, error) {
	return c.InstantiateWithOptions(stackBytes, InstantiateOptions{})
}
func (c *Compiled) InstantiateWithOptions(stackBytes int, opts InstantiateOptions) (*Instance, error) {
	if c == nil || len(c.code) == 0 {
		return nil, fmt.Errorf("rv64: empty compiled module")
	}
	i := &Instance{compiled: c}
	var err error
	i.memory, i.lease, i.ownMemory, err = c.state.bindMemory(opts)
	if err == nil {
		i.engine, err = native.NewEngine(stackBytes)
	}
	if err == nil {
		i.call, err = native.NewCallFrameLayout(c.state.capacity, c.state.resultCapacity)
	}
	if err == nil && (i.memory != nil || len(c.state.globals) != 0 || len(c.state.passiveData) != 0) {
		var descriptor uint64
		if i.lease != nil {
			descriptor, err = i.lease.Descriptor()
		}
		if err == nil {
			values := make([]uint64, len(c.state.globals))
			for j, g := range c.state.globals {
				values[j] = g.value
			}
			i.context, err = native.NewGuestContextWithData(descriptor, values, c.state.passiveData)
		}
	}
	if err == nil && i.lease != nil {
		err = i.lease.Initialize(c.state.data)
	}
	if err == nil {
		i.program, err = native.MapCode(c.code)
	}
	if err != nil {
		if cleanup := i.Close(); cleanup != nil {
			return i, errors.Join(err, cleanup)
		}
		return nil, err
	}
	return i, nil
}

// Invoke is the single-result convenience entry. It rejects other result arities
// before executing guest code; use InvokeInto for void/multivalue functions.
func (i *Instance) Invoke(ctx context.Context, name string, args ...uint64) (uint64, error) {
	var result [1]uint64
	err := i.InvokeInto(ctx, name, args, result[:])
	return result[0], err
}

// InvokeInto uses caller-owned results, with exact arity checked before entry.
// On errors, including guest traps, results remain unchanged.
func (i *Instance) InvokeInto(ctx context.Context, name string, args, results []uint64) error {
	if i == nil {
		return native.ErrClosed
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed || i.compiled == nil || i.engine == nil || i.program == nil || i.call == nil {
		return native.ErrClosed
	}
	if ctx == nil {
		return fmt.Errorf("rv64: nil invocation context")
	}
	_, deadline := ctx.Deadline()
	if ctx.Done() != nil || deadline || ctx.Err() != nil {
		return ErrCancelable
	}
	index, ok := i.compiled.exports[name]
	if !ok {
		return fmt.Errorf("rv64: unknown integer export %q", name)
	}
	f := i.compiled.functions[index]
	if len(args) != f.Params {
		return fmt.Errorf("rv64: export %q expects %d arguments, got %d", name, f.Params, len(args))
	}
	if len(results) != f.Results {
		return fmt.Errorf("rv64: export %q expects %d results, got %d", name, f.Results, len(results))
	}
	if i.context != nil {
		return i.context.CallInto(i.lease, i.call, i.engine, i.program, f.Entry, args, results, f.FrameBytes)
	}
	return i.call.CallIntoWithContext(i.engine, i.program, f.Entry, args, results, f.FrameBytes, 0)
}

// Close waits for invocation completion and retries any retained failed unmaps.
func (i *Instance) Close() error {
	if i == nil {
		return nil
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	i.closed = true
	var memoryErr error
	if i.ownMemory {
		memoryErr = i.memory.Close()
	}
	return errors.Join(i.program.Close(), i.engine.Close(), i.call.Close(), i.context.Close(), i.lease.Close(), memoryErr)
}

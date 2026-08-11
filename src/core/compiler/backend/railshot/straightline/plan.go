// Package straightline builds the small value graph used by Railshot's
// straight-line scheduler directly from validated Wasm bytecode.
package straightline

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type ValueID uint32
type InstID uint32

const InvalidValue ValueID = ^ValueID(0)

type Range struct {
	Start uint32
	Len   uint32
}

type ValueDefKind uint8

const (
	ValueDefInvalid ValueDefKind = iota
	ValueDefInst
)

type Value struct {
	Type    wasm.ValType
	DefKind ValueDefKind
	Def     uint32
}

type Op uint8

const (
	OpInvalid Op = iota
	OpConst
	OpIBinary
	OpConvert
	OpLoad
	OpStore
	OpLocalGet
	OpLocalSet
	OpLocalTee
)

type IBinaryOp uint8

const (
	IBinAdd IBinaryOp = iota + 1
	IBinSub
	IBinMul
	IBinDivS
	IBinDivU
	IBinRemS
	IBinRemU
	IBinAnd
	IBinOr
	IBinXor
	IBinShl
	IBinShrS
	IBinShrU
	IBinRotl
	IBinRotr
)

type ConvertOp uint8

const ConvWrapI64ToI32 ConvertOp = 1

type MemOp uint8

const (
	MemI32 MemOp = iota + 1
	MemI64
	MemF32
	MemF64
	MemI32Load8S
	MemI32Load8U
	MemI32Load16S
	MemI32Load16U
	MemI64Load8S
	MemI64Load8U
	MemI64Load16S
	MemI64Load16U
	MemI64Load32S
	MemI64Load32U
	MemI32Store8
	MemI32Store16
	MemI64Store8
	MemI64Store16
	MemI64Store32
)

type Inst struct {
	Op      Op
	Args    Range
	Results Range
	Aux     uint64
}

// Func is the scheduler's bounded, single-region value graph. It deliberately
// has no blocks, terminators, effects, module metadata, or general IR contract.
type Func struct {
	Insts    []Inst
	Values   []Value
	ValueIDs []ValueID
}

type StraightLinePlan struct {
	Aliases       []ValueID
	InitialLocals []ValueID
}

func (p *StraightLinePlan) Resolve(v ValueID) ValueID {
	for v != InvalidValue && int(v) < len(p.Aliases) && p.Aliases[v] != v {
		v = p.Aliases[v]
	}
	return v
}

type builder struct {
	m       *wasm.Module
	funcIdx int
	code    *wasm.Func
	r       wasm.Reader
	out     Func
	stack   []ValueID
	locals  []wasm.ValType
}

// BuildFunc scans one already-validated function body and builds only the
// integer/memory value graph understood by the straight-line scheduler.
func BuildFunc(m *wasm.Module, funcIdx int) (*Func, error) {
	if m == nil || funcIdx < 0 || funcIdx >= len(m.Code) {
		return nil, fmt.Errorf("straightline: function %d out of range", funcIdx)
	}
	ft, ok := m.LocalFuncType(funcIdx)
	if !ok || ft.Kind != wasm.CompFunc || len(ft.Results) != 0 {
		return nil, fmt.Errorf("straightline: unsupported function signature")
	}
	code := &m.Code[funcIdx]
	if len(code.BodyBytes) == 0 {
		return nil, fmt.Errorf("straightline: encoded body unavailable")
	}
	b := builder{m: m, funcIdx: funcIdx, code: code, r: wasm.ReaderFrom(code.BodyBytes)}
	b.locals = append(b.locals, ft.Params...)
	for _, run := range code.Locals.Runs {
		if run.Count > uint32(len(code.BodyBytes)) || uint64(len(b.locals))+uint64(run.Count) > uint64(^uint(0)>>1) {
			return nil, fmt.Errorf("straightline: local space too large")
		}
		for i := uint32(0); i < run.Count; i++ {
			b.locals = append(b.locals, run.Type)
		}
	}
	capHint := min(len(code.BodyBytes)/2, 8192)
	b.out.Insts = make([]Inst, 0, capHint)
	b.out.Values = make([]Value, 0, capHint)
	b.out.ValueIDs = make([]ValueID, 0, min(len(code.BodyBytes), 16384))
	if err := b.build(); err != nil {
		return nil, err
	}
	return &b.out, nil
}

func (b *builder) build() error {
	for b.r.HasNext() {
		op, err := b.r.Byte()
		if err != nil {
			return err
		}
		switch {
		case op == 0x0b:
			if b.r.HasNext() || len(b.stack) != 0 {
				return fmt.Errorf("straightline: invalid function end")
			}
			return nil
		case op == 0x01: // nop
		case op >= 0x20 && op <= 0x22:
			if err := b.local(op); err != nil {
				return err
			}
		case op == 0x41 || op == 0x42:
			if err := b.constant(op); err != nil {
				return err
			}
		case op >= 0x28 && op <= 0x3e:
			if err := b.memory(op); err != nil {
				return err
			}
		case integerBinary(op):
			if err := b.binary(op); err != nil {
				return err
			}
		case op == 0xa7:
			v, err := b.pop(wasm.I64)
			if err != nil {
				return err
			}
			b.pushResult(OpConvert, uint64(ConvWrapI64ToI32), []ValueID{v}, wasm.I32)
		default:
			return fmt.Errorf("straightline: unsupported opcode 0x%02x", op)
		}
	}
	return fmt.Errorf("straightline: missing function end")
}

func (b *builder) local(op byte) error {
	x, err := b.r.U32()
	if err != nil || int(x) >= len(b.locals) {
		return fmt.Errorf("straightline: invalid local %d", x)
	}
	t := b.locals[x]
	if t != wasm.I32 && t != wasm.I64 {
		return fmt.Errorf("straightline: unsupported local type %s", t)
	}
	switch op {
	case 0x20:
		b.pushResult(OpLocalGet, uint64(x), nil, t)
	case 0x21:
		v, err := b.pop(t)
		if err != nil {
			return err
		}
		b.addInst(OpLocalSet, uint64(x), []ValueID{v}, nil)
	case 0x22:
		v, err := b.pop(t)
		if err != nil {
			return err
		}
		b.pushResult(OpLocalTee, uint64(x), []ValueID{v}, t)
	}
	return nil
}

func (b *builder) constant(op byte) error {
	if op == 0x41 {
		v, err := b.r.I32()
		if err != nil {
			return err
		}
		b.pushResult(OpConst, uint64(uint32(v)), nil, wasm.I32)
		return nil
	}
	v, err := b.r.I64()
	if err != nil {
		return err
	}
	b.pushResult(OpConst, uint64(v), nil, wasm.I64)
	return nil
}

func integerBinary(op byte) bool {
	return op >= 0x6a && op <= 0x78 || op >= 0x7c && op <= 0x8a
}

func (b *builder) binary(op byte) error {
	t := wasm.I32
	base := byte(0x6a)
	if op >= 0x7c {
		t, base = wasm.I64, 0x7c
	}
	k := IBinaryOp(op-base) + IBinAdd
	if k == IBinDivS || k == IBinDivU || k == IBinRemS || k == IBinRemU {
		return fmt.Errorf("straightline: trapping integer binary")
	}
	right, err := b.pop(t)
	if err != nil {
		return err
	}
	left, err := b.pop(t)
	if err != nil {
		return err
	}
	b.pushResult(OpIBinary, uint64(k), []ValueID{left, right}, t)
	return nil
}

func (b *builder) memory(op byte) error {
	align, err := b.r.U32()
	if err != nil {
		return err
	}
	mt, ok := b.m.MemoryType(0)
	if !ok || mt.Limits.Addr64 {
		return fmt.Errorf("straightline: memory32 required")
	}
	offset, err := b.r.U32()
	if err != nil {
		return err
	}
	k, result, value, store := memoryShape(op)
	if k == 0 || align > naturalAlign(k) {
		return fmt.Errorf("straightline: unsupported memory opcode 0x%02x", op)
	}
	aux := uint64(k) | uint64(offset)<<32
	if store {
		v, err := b.pop(value)
		if err != nil {
			return err
		}
		addr, err := b.pop(wasm.I32)
		if err != nil {
			return err
		}
		b.addInst(OpStore, aux, []ValueID{addr, v}, nil)
		return nil
	}
	addr, err := b.pop(wasm.I32)
	if err != nil {
		return err
	}
	b.pushResult(OpLoad, aux, []ValueID{addr}, result)
	return nil
}

func memoryShape(op byte) (MemOp, wasm.ValType, wasm.ValType, bool) {
	switch op {
	case 0x28:
		return MemI32, wasm.I32, wasm.ValType{}, false
	case 0x29:
		return MemI64, wasm.I64, wasm.ValType{}, false
	case 0x2c:
		return MemI32Load8S, wasm.I32, wasm.ValType{}, false
	case 0x2d:
		return MemI32Load8U, wasm.I32, wasm.ValType{}, false
	case 0x2e:
		return MemI32Load16S, wasm.I32, wasm.ValType{}, false
	case 0x2f:
		return MemI32Load16U, wasm.I32, wasm.ValType{}, false
	case 0x30:
		return MemI64Load8S, wasm.I64, wasm.ValType{}, false
	case 0x31:
		return MemI64Load8U, wasm.I64, wasm.ValType{}, false
	case 0x32:
		return MemI64Load16S, wasm.I64, wasm.ValType{}, false
	case 0x33:
		return MemI64Load16U, wasm.I64, wasm.ValType{}, false
	case 0x34:
		return MemI64Load32S, wasm.I64, wasm.ValType{}, false
	case 0x35:
		return MemI64Load32U, wasm.I64, wasm.ValType{}, false
	case 0x36:
		return MemI32, wasm.ValType{}, wasm.I32, true
	case 0x37:
		return MemI64, wasm.ValType{}, wasm.I64, true
	case 0x3a:
		return MemI32Store8, wasm.ValType{}, wasm.I32, true
	case 0x3b:
		return MemI32Store16, wasm.ValType{}, wasm.I32, true
	case 0x3c:
		return MemI64Store8, wasm.ValType{}, wasm.I64, true
	case 0x3d:
		return MemI64Store16, wasm.ValType{}, wasm.I64, true
	case 0x3e:
		return MemI64Store32, wasm.ValType{}, wasm.I64, true
	default:
		return 0, wasm.ValType{}, wasm.ValType{}, false
	}
}

func naturalAlign(k MemOp) uint32 {
	switch k {
	case MemI32, MemI64Load32S, MemI64Load32U, MemI64Store32:
		return 2
	case MemI64:
		return 3
	case MemI32Load16S, MemI32Load16U, MemI64Load16S, MemI64Load16U, MemI32Store16, MemI64Store16:
		return 1
	default:
		return 0
	}
}

func (b *builder) pop(t wasm.ValType) (ValueID, error) {
	if len(b.stack) == 0 {
		return InvalidValue, fmt.Errorf("straightline: operand stack underflow")
	}
	v := b.stack[len(b.stack)-1]
	b.stack = b.stack[:len(b.stack)-1]
	if b.out.Values[v].Type != t {
		return InvalidValue, fmt.Errorf("straightline: operand type mismatch")
	}
	return v, nil
}

func (b *builder) addInst(op Op, aux uint64, args []ValueID, results []wasm.ValType) []ValueID {
	in := Inst{Op: op, Aux: aux, Args: Range{Start: uint32(len(b.out.ValueIDs)), Len: uint32(len(args))}}
	b.out.ValueIDs = append(b.out.ValueIDs, args...)
	in.Results.Start = uint32(len(b.out.ValueIDs))
	instID := uint32(len(b.out.Insts))
	for _, t := range results {
		v := ValueID(len(b.out.Values))
		b.out.Values = append(b.out.Values, Value{Type: t, DefKind: ValueDefInst, Def: instID})
		b.out.ValueIDs = append(b.out.ValueIDs, v)
		in.Results.Len++
	}
	b.out.Insts = append(b.out.Insts, in)
	return b.out.ValueIDs[in.Results.Start : in.Results.Start+in.Results.Len]
}

func (b *builder) pushResult(op Op, aux uint64, args []ValueID, t wasm.ValType) {
	results := b.addInst(op, aux, args, []wasm.ValType{t})
	b.stack = append(b.stack, results[0])
}

// BuildStraightLinePlan renames Wasm locals into value identities. This is a
// scheduler-local alias pass, not a module or control-flow IR transformation.
func BuildStraightLinePlan(f *Func) *StraightLinePlan {
	if f == nil {
		return nil
	}
	localCount := 0
	for i := range f.Insts {
		if f.Insts[i].Op >= OpLocalGet && int(uint32(f.Insts[i].Aux))+1 > localCount {
			localCount = int(uint32(f.Insts[i].Aux)) + 1
		}
	}
	p := &StraightLinePlan{Aliases: make([]ValueID, len(f.Values)), InitialLocals: make([]ValueID, localCount)}
	current := make([]ValueID, localCount)
	for i := range p.Aliases {
		p.Aliases[i] = ValueID(i)
	}
	for i := range p.InitialLocals {
		p.InitialLocals[i], current[i] = InvalidValue, InvalidValue
	}
	for i := range f.Insts {
		in := &f.Insts[i]
		x := int(uint32(in.Aux))
		switch in.Op {
		case OpLocalGet:
			out := f.ValueIDs[in.Results.Start]
			if current[x] == InvalidValue {
				current[x], p.InitialLocals[x] = out, out
			} else {
				p.Aliases[out] = p.Resolve(current[x])
			}
		case OpLocalSet, OpLocalTee:
			v := p.Resolve(f.ValueIDs[in.Args.Start])
			current[x] = v
			if in.Op == OpLocalTee {
				p.Aliases[f.ValueIDs[in.Results.Start]] = v
			}
		}
	}
	return p
}

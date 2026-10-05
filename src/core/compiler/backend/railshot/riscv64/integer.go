//go:build linux && !tinygo

package riscv64

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

const (
	TrapUnreachable     = 2
	TrapDivideByZero    = 3
	TrapIntegerOverflow = 4
	integerBudget       = 16384
	maxTableEntries     = 1024
)

type integerSummary struct {
	maxStack int
	context  bool
}
type integerControl struct {
	base                                         int
	kind                                         byte
	result, wide, entry, then, incoming, hasElse bool
	start, falseSite                             int
	sites                                        []int
}
type integerTrap struct{ site, code int }
type integerEmitter struct {
	target
	traps []integerTrap
}

// The separate fallback uses fixed temporary registers and canonical operand
// slots, not shared value identities or a second register allocator. One walker
// performs complete pre-emission admission and then lowering of the same bytes.
func walkInteger(fn *wasm.Func, ft *wasm.CompType, state *moduleState, emit *integerEmitter) (integerSummary, error) {
	var locals [256]bool
	nlocals := len(ft.Params)
	for i, t := range ft.Params {
		locals[i] = wasm.EqualValType(t, wasm.I64)
	}
	for _, run := range fn.Locals.Runs {
		if run.Count != 0 && !wasm.EqualValType(run.Type, wasm.I32) && !wasm.EqualValType(run.Type, wasm.I64) {
			return integerSummary{}, unsupported("noninteger local")
		}
		if int(run.Count) > 256-nlocals {
			return integerSummary{}, unsupported("local bound")
		}
		for i := uint32(0); i < run.Count; i++ {
			locals[nlocals] = wasm.EqualValType(run.Type, wasm.I64)
			nlocals++
		}
	}
	if len(fn.BodyBytes) == 0 || len(fn.BodyBytes) > 64<<10 {
		return integerSummary{}, unsupported("integer body bound")
	}
	var stack [512]bool
	var ctrl [33]integerControl
	ctrl[0] = integerControl{kind: 0xff, result: true, wide: wasm.EqualValType(ft.Results[0], wasm.I64), entry: true}
	depth, h, peak, budget := 1, 0, 0, nlocals
	live := true
	contextUsed := false
	pop := func() (bool, error) {
		if h <= ctrl[depth-1].base {
			if !live {
				return false, nil
			}
			return false, fmt.Errorf("rv64: validated operand stack underflow")
		}
		h--
		return stack[h], nil
	}
	push := func(w bool) error {
		if h >= len(stack) {
			return unsupported("integer operand bound")
		}
		stack[h] = w
		h++
		peak = max(peak, h)
		return nil
	}
	position := func() int {
		if emit == nil {
			return 0
		}
		return emit.Position()
	}
	patch := func(site, pos int) error {
		if emit == nil {
			return nil
		}
		return emit.Patch(site, pos)
	}
	branch := func(label uint32) error {
		if int(label) >= depth {
			return fmt.Errorf("rv64: invalid validated label")
		}
		fr := &ctrl[depth-1-int(label)]
		if !live {
			return nil
		}
		if fr.kind != 0x03 && fr.result {
			if h == 0 {
				return fmt.Errorf("rv64: missing branch result")
			}
			if emit != nil {
				emit.Load(7, emit.SpillOffset(h-1), fr.wide)
				emit.Store(emit.SpillOffset(fr.base), 7, fr.wide)
			}
		}
		if emit != nil {
			site := emit.Jump()
			if fr.kind == 0x03 {
				if err := patch(site, fr.start); err != nil {
					return err
				}
			} else {
				fr.sites = append(fr.sites, site)
			}
		}
		if fr.kind != 0x03 {
			fr.incoming = true
		}
		return nil
	}
	r := wasm.ReaderFrom(fn.BodyBytes)
	for depth > 0 {
		op, err := r.Byte()
		if err != nil {
			return integerSummary{}, err
		}
		budget++
		if budget > integerBudget {
			return integerSummary{}, unsupported("integer instruction/label budget")
		}
		switch op {
		case 0x00:
			if live && emit != nil {
				emit.trap(TrapUnreachable, emit.Jump())
			}
			live = false
			h = ctrl[depth-1].base
		case 0x01:
		case 0x02, 0x03, 0x04:
			bt, e := r.Byte()
			if e != nil {
				return integerSummary{}, e
			}
			if bt != 0x40 && bt != 0x7f && bt != 0x7e {
				return integerSummary{}, unsupported("indexed/noninteger block signature")
			}
			if depth == len(ctrl) {
				return integerSummary{}, unsupported("integer control bound")
			}
			if op == 0x04 {
				if _, e = pop(); e != nil {
					return integerSummary{}, e
				}
				if live && emit != nil {
					emit.Load(5, emit.SpillOffset(h), false)
				}
			}
			fr := integerControl{base: h, kind: op, result: bt != 0x40, wide: bt == 0x7e, entry: live, start: position(), falseSite: -1}
			if op == 0x04 && live && emit != nil {
				fr.falseSite = emit.BranchZero(5)
			}
			ctrl[depth] = fr
			depth++
		case 0x05:
			fr := &ctrl[depth-1]
			if fr.kind != 0x04 || fr.hasElse {
				return integerSummary{}, fmt.Errorf("rv64: invalid validated else")
			}
			fr.then = live
			fr.hasElse = true
			if live && emit != nil {
				fr.sites = append(fr.sites, emit.Jump())
			}
			if fr.entry && emit != nil {
				if err = patch(fr.falseSite, position()); err != nil {
					return integerSummary{}, err
				}
			}
			live = fr.entry
			h = fr.base
		case 0x0b:
			fr := ctrl[depth-1]
			if emit != nil {
				for _, site := range fr.sites {
					if err = patch(site, position()); err != nil {
						return integerSummary{}, err
					}
				}
				if fr.kind == 0x04 && !fr.hasElse && fr.entry {
					if err = patch(fr.falseSite, position()); err != nil {
						return integerSummary{}, err
					}
				}
			}
			live = live || fr.incoming || fr.then || fr.kind == 0x04 && !fr.hasElse && fr.entry
			h = fr.base
			if fr.result {
				if err = push(fr.wide); err != nil {
					return integerSummary{}, err
				}
			}
			depth--
			if depth == 0 && live && emit != nil {
				emit.Load(5, emit.SpillOffset(0), fr.wide)
				emit.Return(5, fr.wide, 0)
			}
		case 0x0c, 0x0d:
			label, e := r.U32()
			if e != nil {
				return integerSummary{}, e
			}
			if op == 0x0d {
				if _, e = pop(); e != nil {
					return integerSummary{}, e
				}
				skip := -1
				if live && emit != nil {
					emit.Load(5, emit.SpillOffset(h), false)
					skip = emit.BranchZero(5)
				}
				if e = branch(label); e != nil {
					return integerSummary{}, e
				}
				if live && emit != nil {
					if e = patch(skip, position()); e != nil {
						return integerSummary{}, e
					}
				}
			} else {
				if e = branch(label); e != nil {
					return integerSummary{}, e
				}
				live = false
				h = ctrl[depth-1].base
			}
		case 0x0e:
			n, e := r.U32()
			if e != nil {
				return integerSummary{}, e
			}
			if n > maxTableEntries || int(n) > integerBudget-budget {
				return integerSummary{}, unsupported("branch table bound")
			}
			budget += int(n)
			if _, e = pop(); e != nil {
				return integerSummary{}, e
			}
			if live && emit != nil {
				emit.Load(5, emit.SpillOffset(h), false)
			}
			for i := uint32(0); i <= n; i++ {
				label, e := r.U32()
				if e != nil {
					return integerSummary{}, e
				}
				skip := -1
				if i < n && live && emit != nil {
					emit.Constant(6, int64(i), false)
					skip = emit.a.BranchLong(enc.NE, enc.X5, enc.X6)
				}
				if e = branch(label); e != nil {
					return integerSummary{}, e
				}
				if i < n && live && emit != nil {
					if e = patch(skip, position()); e != nil {
						return integerSummary{}, e
					}
				}
			}
			live = false
			h = ctrl[depth-1].base
		case 0x0f:
			if err = branch(uint32(depth - 1)); err != nil {
				return integerSummary{}, err
			}
			live = false
			h = ctrl[depth-1].base
		case 0x1a:
			if _, err = pop(); err != nil {
				return integerSummary{}, err
			}
		case 0x1b, 0x1c:
			var typedWide bool
			if op == 0x1c {
				n, e := r.U32()
				if e != nil {
					return integerSummary{}, e
				}
				bt, e := r.Byte()
				if e != nil {
					return integerSummary{}, e
				}
				if n != 1 || bt != 0x7f && bt != 0x7e {
					return integerSummary{}, unsupported("noninteger typed select")
				}
				typedWide = bt == 0x7e
			}
			if _, err = pop(); err != nil {
				return integerSummary{}, err
			}
			cond := h
			w, e := pop()
			if e != nil {
				return integerSummary{}, e
			}
			if _, err = pop(); err != nil {
				return integerSummary{}, err
			}
			if op == 0x1c {
				w = typedWide
			}
			if live && emit != nil {
				emit.Load(5, emit.SpillOffset(cond), false)
				emit.Load(6, emit.SpillOffset(h+1), w)
				emit.Load(7, emit.SpillOffset(h), w)
				site := emit.BranchZero(5)
				emit.Move(5, 7, w)
				end := emit.Jump()
				if e = patch(site, position()); e != nil {
					return integerSummary{}, e
				}
				emit.Move(5, 6, w)
				if e = patch(end, position()); e != nil {
					return integerSummary{}, e
				}
				emit.Store(emit.SpillOffset(h), 5, w)
			}
			if err = push(w); err != nil {
				return integerSummary{}, err
			}
		case 0x20, 0x21, 0x22:
			idx, e := r.U32()
			if e != nil {
				return integerSummary{}, e
			}
			if int(idx) >= nlocals {
				return integerSummary{}, fmt.Errorf("rv64: invalid validated local")
			}
			w := locals[idx]
			if op == 0x20 {
				if live && emit != nil {
					emit.Load(5, emit.LocalOffset(int(idx)), w)
					emit.Store(emit.SpillOffset(h), 5, w)
				}
				if err = push(w); err != nil {
					return integerSummary{}, err
				}
			} else {
				if _, err = pop(); err != nil {
					return integerSummary{}, err
				}
				if live && emit != nil {
					emit.Load(5, emit.SpillOffset(h), w)
					emit.Store(emit.LocalOffset(int(idx)), 5, w)
				}
				if op == 0x22 {
					if err = push(w); err != nil {
						return integerSummary{}, err
					}
				}
			}
		case 0x23, 0x24:
			idx, e := r.U32()
			if e != nil {
				return integerSummary{}, e
			}
			if state == nil || int(idx) >= len(state.globals) {
				return integerSummary{}, unsupported("global index")
			}
			g := state.globals[idx]
			contextUsed = true
			if op == 0x24 {
				if !g.mutable {
					return integerSummary{}, unsupported("immutable global set")
				}
				if _, err = pop(); err != nil {
					return integerSummary{}, err
				}
				if live && emit != nil {
					emit.Load(5, emit.SpillOffset(h), g.wide)
					emit.global(idx, g.wide, true)
				}
			} else {
				if live && emit != nil {
					emit.global(idx, g.wide, false)
					emit.Store(emit.SpillOffset(h), 5, g.wide)
				}
				if err = push(g.wide); err != nil {
					return integerSummary{}, err
				}
			}
		case 0xfc:
			var imm wasm.InstructionImmediate
			if err = wasm.ClassifyInstructionImmediateIntoWithFeatures(&r, op, &imm, false, false); err != nil {
				return integerSummary{}, err
			}
			if state == nil {
				return integerSummary{}, unsupported("bulk instruction without state")
			}
			switch imm.Kind {
			case wasm.InstrMemoryInit, wasm.InstrDataDrop:
				if int(imm.Index) >= len(state.passiveData) || imm.Index2 != 0 {
					return integerSummary{}, unsupported("bulk data/memory index")
				}
			case wasm.InstrMemoryCopy, wasm.InstrMemoryFill:
				if imm.Index != 0 || imm.Index2 != 0 {
					return integerSummary{}, unsupported("bulk memory index")
				}
			default:
				return integerSummary{}, unsupported("unsupported prefixed instruction")
			}
			contextUsed = true
			dataOffset := int32((len(state.globals)+1)*8 + int(imm.Index)*16)
			if imm.Kind == wasm.InstrDataDrop {
				if live && emit != nil {
					emit.dataDrop(dataOffset)
				}
				break
			}
			if state.memory == nil {
				return integerSummary{}, unsupported("bulk instruction without memory")
			}
			for _, reg := range []uint8{7, 6, 5} {
				if _, err = pop(); err != nil {
					return integerSummary{}, err
				}
				if live && emit != nil {
					emit.Load(reg, emit.SpillOffset(h), false)
				}
			}
			if live && emit != nil {
				emit.memoryBulk(imm.Kind, dataOffset)
			}
		case 0x28, 0x29, 0x2c, 0x2d, 0x2e, 0x2f, 0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e, 0x3f, 0x40:
			if state == nil || state.memory == nil {
				return integerSummary{}, unsupported("memory instruction without memory")
			}
			contextUsed = true
			var imm wasm.InstructionImmediate
			if err = wasm.ClassifyInstructionImmediateInto(&r, op, &imm); err != nil {
				return integerSummary{}, err
			}
			if imm.MemIndex != 0 || imm.Index != 0 {
				return integerSummary{}, unsupported("nonzero memory index")
			}
			if op == 0x3f || op == 0x40 {
				if op == 0x40 {
					if _, err = pop(); err != nil {
						return integerSummary{}, err
					}
				}
				if live && emit != nil {
					if op == 0x40 {
						emit.Load(5, emit.SpillOffset(h), false)
					}
					emit.memorySizeGrow(op == 0x40)
					emit.Store(emit.SpillOffset(h), 5, false)
				}
				if err = push(false); err != nil {
					return integerSummary{}, err
				}
			} else {
				size, w, signed, store, ok := memoryOpcode(op)
				if !ok {
					return integerSummary{}, unsupported("noninteger memory access")
				}
				if store {
					if _, err = pop(); err != nil {
						return integerSummary{}, err
					}
					if live && emit != nil {
						emit.Load(6, emit.SpillOffset(h), w)
					}
				}
				if _, err = pop(); err != nil {
					return integerSummary{}, err
				}
				if live && emit != nil {
					emit.Load(5, emit.SpillOffset(h), false)
					emit.memoryAccess(imm.MemOffset, size, w, signed, store)
					if !store {
						emit.Store(emit.SpillOffset(h), 5, w)
					}
				}
				if !store {
					if err = push(w); err != nil {
						return integerSummary{}, err
					}
				}
			}

		case 0x41, 0x42:
			w := op == 0x42
			var v int64
			if w {
				v, err = r.I64()
			} else {
				var x int32
				x, err = r.I32()
				v = int64(x)
			}
			if err != nil {
				return integerSummary{}, err
			}
			if live && emit != nil {
				emit.Constant(5, v, w)
				emit.Store(emit.SpillOffset(h), 5, w)
			}
			if err = push(w); err != nil {
				return integerSummary{}, err
			}
		default:
			operation, w, unary, resultWide, ok := integerOpcode(op)
			if !ok {
				return integerSummary{}, unsupported(fmt.Sprintf("integer opcode 0x%x", op))
			}
			if !unary {
				if _, err = pop(); err != nil {
					return integerSummary{}, err
				}
				if live && emit != nil {
					emit.Load(6, emit.SpillOffset(h), w)
				}
			}
			if _, err = pop(); err != nil {
				return integerSummary{}, err
			}
			if live && emit != nil {
				emit.Load(5, emit.SpillOffset(h), w)
				emit.operation(operation, w, unary, resultWide)
				emit.Store(emit.SpillOffset(h), 5, resultWide)
			}
			if err = push(resultWide); err != nil {
				return integerSummary{}, err
			}
		}
	}
	if r.HasNext() {
		return integerSummary{}, fmt.Errorf("rv64: trailing validated bytecode")
	}
	return integerSummary{maxStack: peak, context: contextUsed}, nil
}

func integerOpcode(op byte) (shared.IntOp, bool, bool, bool, bool) {
	if o, w, ok := shared.ScalarOpcode(op); ok {
		return o, w, false, w && !(o >= shared.IntEq && o <= shared.IntGeU), true
	}
	switch op {
	case 0x45:
		return shared.IntEqz, false, true, false, true
	case 0x50:
		return shared.IntEqz, true, true, false, true
	case 0x67, 0x68, 0x69:
		return []shared.IntOp{shared.IntClz, shared.IntCtz, shared.IntPopcnt}[op-0x67], false, true, false, true
	case 0x79, 0x7a, 0x7b:
		return []shared.IntOp{shared.IntClz, shared.IntCtz, shared.IntPopcnt}[op-0x79], true, true, true, true
	case 0x6d, 0x6e, 0x6f, 0x70:
		return []shared.IntOp{shared.IntDivS, shared.IntDivU, shared.IntRemS, shared.IntRemU}[op-0x6d], false, false, false, true
	case 0x7f, 0x80, 0x81, 0x82:
		return []shared.IntOp{shared.IntDivS, shared.IntDivU, shared.IntRemS, shared.IntRemU}[op-0x7f], true, false, true, true
	case 0x77, 0x78:
		return []shared.IntOp{shared.IntRotl, shared.IntRotr}[op-0x77], false, false, false, true
	case 0x89, 0x8a:
		return []shared.IntOp{shared.IntRotl, shared.IntRotr}[op-0x89], true, false, true, true
	case 0xa7:
		return shared.IntWrap, true, true, false, true
	case 0xac:
		return shared.IntSExt32, false, true, true, true
	case 0xad:
		return shared.IntZExt32, false, true, true, true
	case 0xc0, 0xc1:
		return []shared.IntOp{shared.IntSExt8, shared.IntSExt16}[op-0xc0], false, true, false, true
	case 0xc2, 0xc3, 0xc4:
		return []shared.IntOp{shared.IntSExt8, shared.IntSExt16, shared.IntSExt32}[op-0xc2], true, true, true, true
	}
	return 0, false, false, false, false
}

func (f *integerEmitter) trap(code, site int) { f.traps = append(f.traps, integerTrap{site, code}) }
func (f *integerEmitter) operation(op shared.IntOp, w, unary, resultWide bool) {
	if !unary {
		switch op {
		case shared.IntDivS, shared.IntDivU, shared.IntRemS, shared.IntRemU:
			f.trap(TrapDivideByZero, f.a.BranchLong(enc.EQ, enc.X6, enc.Zero))
			if op == shared.IntDivS {
				min := int64(-1 << 63)
				if !w {
					min = -1 << 31
				}
				f.Constant(7, min, w)
				skip := f.a.BranchLong(enc.NE, enc.X5, enc.X7)
				f.Constant(7, -1, w)
				f.trap(TrapIntegerOverflow, f.a.BranchLong(enc.EQ, enc.X6, enc.X7))
				if e := f.Patch(skip, f.Position()); e != nil {
					f.err = e
				}
			}
			if w {
				switch op {
				case shared.IntDivS:
					f.a.Div(enc.X5, enc.X5, enc.X6)
				case shared.IntDivU:
					f.a.Divu(enc.X5, enc.X5, enc.X6)
				case shared.IntRemS:
					f.a.Rem(enc.X5, enc.X5, enc.X6)
				case shared.IntRemU:
					f.a.Remu(enc.X5, enc.X5, enc.X6)
				}
			} else {
				switch op {
				case shared.IntDivS:
					f.a.Divw(enc.X5, enc.X5, enc.X6)
				case shared.IntDivU:
					f.a.Divuw(enc.X5, enc.X5, enc.X6)
				case shared.IntRemS:
					f.a.Remw(enc.X5, enc.X5, enc.X6)
				case shared.IntRemU:
					f.a.Remuw(enc.X5, enc.X5, enc.X6)
				}
			}
		case shared.IntRotl, shared.IntRotr:
			f.a.Sub(enc.X28, enc.Zero, enc.X6)
			if w {
				if op == shared.IntRotl {
					f.a.Sll(enc.X7, enc.X5, enc.X6)
					f.a.Srl(enc.X28, enc.X5, enc.X28)
				} else {
					f.a.Srl(enc.X7, enc.X5, enc.X6)
					f.a.Sll(enc.X28, enc.X5, enc.X28)
				}
			} else {
				if op == shared.IntRotl {
					f.a.Sllw(enc.X7, enc.X5, enc.X6)
					f.a.Srlw(enc.X28, enc.X5, enc.X28)
				} else {
					f.a.Srlw(enc.X7, enc.X5, enc.X6)
					f.a.Sllw(enc.X28, enc.X5, enc.X28)
				}
			}
			f.a.Or(enc.X5, enc.X7, enc.X28)
		default:
			f.Binary(op, w, 5, 5, shared.ScalarOperand{Kind: shared.ScalarRegister, Reg: 6})
		}
	} else {
		switch op {
		case shared.IntEqz:
			f.a.Sltiu(enc.X5, enc.X5, 1)
		case shared.IntWrap, shared.IntZExt32:
			f.NormalizeI32(5)
		case shared.IntSExt8:
			f.a.Slli(enc.X5, enc.X5, 56)
			f.a.Srai(enc.X5, enc.X5, 56)
		case shared.IntSExt16:
			f.a.Slli(enc.X5, enc.X5, 48)
			f.a.Srai(enc.X5, enc.X5, 48)
		case shared.IntSExt32:
			f.a.Addiw(enc.X5, enc.X5, 0)
		case shared.IntClz, shared.IntCtz, shared.IntPopcnt:
			width := 32
			if w {
				width = 64
			}
			initial := width
			if op == shared.IntPopcnt {
				initial = 0
			}
			f.Constant(7, int64(initial), true)
			done := f.a.BranchLong(enc.EQ, enc.X5, enc.Zero)
			f.Constant(7, 0, true)
			start := f.Position()
			if op == shared.IntPopcnt {
				f.a.Addi(enc.X6, enc.X5, -1)
				f.a.And(enc.X5, enc.X5, enc.X6)
				f.a.Addi(enc.X7, enc.X7, 1)
				again := f.a.BranchLong(enc.NE, enc.X5, enc.Zero)
				if e := f.Patch(again, start); e != nil {
					f.err = e
				}
			} else {
				if op == shared.IntClz {
					f.a.Srli(enc.X6, enc.X5, uint8(width-1))
				} else {
					f.a.Addi(enc.X28, enc.Zero, 1)
					f.a.And(enc.X6, enc.X5, enc.X28)
				}
				stop := f.a.BranchLong(enc.NE, enc.X6, enc.Zero)
				if op == shared.IntClz {
					f.a.Slli(enc.X5, enc.X5, 1)
					if !w {
						f.NormalizeI32(5)
					}
				} else {
					f.a.Srli(enc.X5, enc.X5, 1)
				}
				f.a.Addi(enc.X7, enc.X7, 1)
				back := f.Jump()
				if e := f.Patch(back, start); e != nil {
					f.err = e
				}
				if e := f.Patch(stop, f.Position()); e != nil {
					f.err = e
				}
			}
			if e := f.Patch(done, f.Position()); e != nil {
				f.err = e
			}
			f.Move(5, 7, true)
		default:
			f.err = fmt.Errorf("rv64: unsupported integer unary %d", op)
		}
	}
	if !resultWide {
		f.NormalizeI32(5)
	}
}

func compileInteger(fn *wasm.Func, ft *wasm.CompType, state *moduleState, summary integerSummary) ([]byte, FunctionInfo, error) {
	var widths [256]bool
	n := len(ft.Params)
	for i, t := range ft.Params {
		widths[i] = wasm.EqualValType(t, wasm.I64)
	}
	for _, run := range fn.Locals.Runs {
		for i := uint32(0); i < run.Count; i++ {
			widths[n] = wasm.EqualValType(run.Type, wasm.I64)
			n++
		}
	}
	f := integerEmitter{target: target{nlocals: n}}
	actual, err := walkInteger(fn, ft, state, &f)
	if err != nil {
		return nil, FunctionInfo{}, err
	}
	if actual != summary {
		return nil, FunctionInfo{}, fmt.Errorf("rv64: integer admission changed")
	}
	exits := []int{f.Jump()}
	for code := TrapUnreachable; code <= TrapMemoryBounds; code++ {
		pos := f.Position()
		used := false
		for _, tr := range f.traps {
			if tr.code == code {
				if err = f.Patch(tr.site, pos); err != nil {
					return nil, FunctionInfo{}, err
				}
				used = true
			}
		}
		if used {
			f.Constant(5, int64(code), false)
			f.a.Sw(enc.X5, enc.X12, 0)
			f.Constant(10, 0, true)
			exits = append(exits, f.Jump())
		}
	}
	for _, site := range exits {
		if err = f.Patch(site, f.Position()); err != nil {
			return nil, FunctionInfo{}, err
		}
	}
	if f.err != nil {
		return nil, FunctionInfo{}, f.err
	}
	if f.a.Err() != nil {
		return nil, FunctionInfo{}, f.a.Err()
	}
	info := FunctionInfo{Params: len(ft.Params), SpillSlots: summary.maxStack, Result64: wasm.EqualValType(ft.Results[0], wasm.I64), Fallback: true}
	contextOffset := -1
	if summary.context {
		contextOffset = (state.capacity + 1) * 8
	}
	return frameFunction(f.a.B, widths[:n], len(ft.Params), summary.maxStack, true, contextOffset, info)
}

//go:build amd64 && !tinygo && !wago_profile

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Whole-function structural admission runs before entry-policy selection. The
// frontend has already checked types and indexes; unsupported syntax falls back
// before any native instruction from this engine is emitted.
func admitStreamBody(c *wasm.Func, ft *wasm.CompType) streamSummary {
	s := streamSummary{HasIf: true, ResultN: len(ft.Results)}
	if len(ft.Results) > 1 || len(c.BodyBytes) == 0 || len(c.BodyBytes) > 64<<10 {
		return streamSummary{}
	}
	if len(ft.Results) == 1 {
		if !streamInteger(ft.Results[0]) {
			return streamSummary{}
		}
		s.ResultWide = wasm.EqualValType(ft.Results[0], wasm.I64)
	}
	n := len(ft.Params)
	for _, t := range ft.Params {
		if !streamInteger(t) {
			return streamSummary{}
		}
	}
	for _, run := range c.Locals.Runs {
		if run.Count != 0 && (!streamInteger(run.Type) || run.Count > 256 || n > 256-int(run.Count)) {
			return streamSummary{}
		}
		n += int(run.Count)
	}
	if n > 256 {
		return streamSummary{}
	}
	type frame struct {
		base, result             int
		kind                     byte
		entryDead, ends, hasElse bool
	}
	var frames [129]frame
	frames[0] = frame{result: s.ResultN, kind: 0xff}
	straight := streamingLocalLifetimesEnabled
	var events []uint32
	depth, height, dead, budget := 1, 0, false, n+1
	r := wasm.ReaderFrom(c.BodyBytes)
	fail := func() streamSummary { return streamSummary{} }
	for depth > 0 {
		pc := r.Offset()
		op, err := r.Byte()
		if err != nil {
			return fail()
		}
		if op == 0x00 || (op >= 0x02 && op <= 0x05) || (op >= 0x0c && op <= 0x0f) {
			straight = false
			events = nil
		}
		budget++
		pop, push := 0, 0
		switch op {
		case 0x00:
			dead = true
			height = frames[depth-1].base
		case 0x01:
		case 0x41:
			if _, err = r.I32(); err != nil {
				return fail()
			}
			push = 1
		case 0x42:
			if _, err = r.I64(); err != nil {
				return fail()
			}
			push = 1
		case 0x20, 0x21, 0x22:
			x, e := r.U32()
			if e != nil || int(x) >= n {
				return fail()
			}
			if straight {
				event := uint32(pc) | x<<16
				if op != 0x20 {
					event |= streamEventWrite
				}
				events = append(events, event)
			}
			if op == 0x20 {
				push = 1
			} else if op == 0x21 {
				pop = 1
			}
		case 0x1a:
			pop = 1
		case 0x1b:
			pop, push = 3, 1
		case 0x02, 0x03, 0x04:
			t, e := r.Byte()
			if e != nil || depth == len(frames) {
				return fail()
			}
			result := 0
			if t == 0x7f || t == 0x7e {
				result = 1
			} else if t != 0x40 {
				return fail()
			}
			if op == 0x04 && !dead {
				height--
				if height < frames[depth-1].base {
					return fail()
				}
			}
			frames[depth] = frame{base: height, result: result, kind: op, entryDead: dead}
			depth++
			budget += n + 2*s.MaxStack
			if depth-1 > int(s.MaxControlDepth) {
				s.MaxControlDepth = uint8(depth - 1)
			}
		case 0x05:
			f := &frames[depth-1]
			if f.kind != 0x04 || f.hasElse {
				return fail()
			}
			f.ends = f.ends || !dead
			f.hasElse = true
			height = f.base
			dead = f.entryDead
			budget += 2 * (n + s.MaxStack)
		case 0x0b:
			f := frames[depth-1]
			if !dead && height != f.base+f.result {
				return fail()
			}
			live := !dead || f.ends || (f.kind == 0x04 && !f.hasElse && !f.entryDead)
			depth--
			height = f.base + f.result
			dead = !live
			budget += 2 * (n + s.MaxStack)
		case 0x0c, 0x0d, 0x0f:
			target := depth - 1
			if op != 0x0f {
				v, e := r.U32()
				if e != nil || int(v) >= depth {
					return fail()
				}
				target = int(v)
			}
			if !dead {
				if op == 0x0d {
					height--
				}
				f := &frames[depth-1-target]
				need := f.result
				if f.kind == 0x03 {
					need = 0
				}
				if height < frames[depth-1].base+need {
					return fail()
				}
				if f.kind != 0x03 {
					f.ends = true
				}
				if op != 0x0d {
					dead = true
					height = frames[depth-1].base
				}
			}
			budget += 2 * (n + s.MaxStack)
		case 0xfc:
			sub, e := r.U32()
			if e != nil {
				return fail()
			}
			switch sub {
			case 10:
				a, e := r.U32()
				if e != nil || a != 0 {
					return fail()
				}
				b, e := r.U32()
				if e != nil || b != 0 {
					return fail()
				}
			case 11:
				a, e := r.U32()
				if e != nil || a != 0 {
					return fail()
				}
			default:
				return fail()
			}
			pop = 3
			s.Memory = true
			s.Bulk = true
		default:
			if size, _, _, store := streamMemoryShape(op); size != 0 {
				if e := wasm.SkipInstructionImmediate(&r, op); e != nil {
					return fail()
				}
				s.Memory = true
				if store {
					pop = 2
				} else {
					pop, push = 1, 1
				}
			} else if _, _, ok := streamUnaryShape(op); ok {
				pop, push = 1, 1
			} else if _, ok := streamRotateShape(op); ok {
				pop, push = 2, 1
			} else if _, _, _, ok := streamDivShape(op); ok {
				pop, push = 2, 1
			} else if _, _, ok := streamOpcode(op); ok {
				pop, push = 2, 1
			} else {
				return fail()
			}
		}
		if !dead {
			height += push - pop
			if height < 0 || height > 512 {
				return fail()
			}
			if height > s.MaxStack {
				s.MaxStack = height
			}
		}
		if budget > streamMaxNodes {
			return fail()
		}
	}
	if r.Offset() != len(c.BodyBytes) {
		return fail()
	}
	if straight {
		var nextRead [256]bool
		for i := len(events) - 1; i >= 0; i-- {
			x := byte(events[i] >> 16)
			if !nextRead[x] {
				events[i] |= streamEventDead
			}
			nextRead[x] = events[i]&streamEventWrite == 0
		}
		s.LocalEvents = events
	}
	s.Nodes = budget
	s.Eligible = true
	return s
}

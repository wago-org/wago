package shared

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const (
	ExperimentMaxBodyBytes      = 256
	ExperimentMaxOperations     = 64
	ExperimentMaxLocals         = 16
	ExperimentMaxMemoryStreams  = 4
	ExperimentMaxFunctionGrowth = 1536
	ExperimentMaxModuleGrowth   = 32768
)

// ExperimentOperation retains original positions. Replay encodes Wasm
// operations, never native bytes. Native register selection remains in each
// target's existing emitter. Dependencies are preserved by source-order replay.
type ExperimentOperation struct {
	Start, End    uint16
	Local         uint32
	Offset        uint64
	Opcode, Width byte
	Read, Write   bool
}

type ReplicationPlan struct {
	Operations                     [ExperimentMaxOperations]ExperimentOperation
	OperationN                     int
	Start, BodyStart, BodyEnd, End int
	Counter                        uint32
	Factor                         int
	Guarded                        bool
	LocalReads, LocalWrites        uint16
	MemoryStreams                  int
	EstimatedGrowth                int
}

func experimentTail(body []byte, at int, counter uint32) (end int, ok bool) {
	r := wasm.ReaderFrom(body)
	if r.JumpTo(at) != nil {
		return 0, false
	}
	op, e := r.Byte()
	if e != nil || op != 0x20 {
		return 0, false
	}
	idx, e := r.U32()
	if e != nil || idx != counter {
		return 0, false
	}
	for _, want := range []byte{0x41, 1, 0x6b, 0x21} {
		got, e := r.Byte()
		if e != nil || got != want {
			return 0, false
		}
	}
	idx, e = r.U32()
	if e != nil || idx != counter {
		return 0, false
	}
	for _, want := range []byte{0x0c, 0, 0x0b, 0x0b} {
		got, e := r.Byte()
		if e != nil || got != want {
			return 0, false
		}
	}
	return r.Offset(), true
}

// InspectReplication recognizes one empty-stack, top-tested countdown. Each
// iteration can have dependencies and checked memory effects. Counter changes
// occur only at the fixed tail. Unsupported operations are rejected before any
// change to the caller's module. Function scanning is bounded as well as bodies.
func InspectReplication(body []byte, m *wasm.Module, factor int, guarded, simd bool, p *ReplicationPlan) string {
	*p = ReplicationPlan{Factor: factor, Guarded: guarded}
	if factor != 2 && factor != 4 {
		return "factor"
	}
	r := wasm.ReaderFrom(body)
	classify := wasm.NewModuleInstructionClassifier(m, true)
	for n := 0; n < 64 && r.Offset() < 512; n++ {
		at := r.Offset()
		op, err := r.Byte()
		if err != nil {
			return "no-loop"
		}
		if op == 0x02 {
			for _, want := range []byte{0x40, 0x03, 0x40, 0x20} {
				got, e := r.Byte()
				if e != nil || got != want {
					return "header"
				}
			}
			c, e := r.U32()
			if e != nil || c >= ExperimentMaxLocals {
				return "counter"
			}
			for _, want := range []byte{0x45, 0x0d, 1} {
				got, e := r.Byte()
				if e != nil || got != want {
					return "header"
				}
			}
			p.Start, p.BodyStart, p.Counter = at, r.Offset(), c
			break
		}
		if !experimentPlain(op, false, 0) {
			return "prefix-control-or-operation"
		}
		var imm wasm.InstructionImmediate
		if classify.ClassifyInto(&r, op, &imm) != nil {
			return "decode"
		}
	}
	if p.BodyStart == 0 {
		return "no-bounded-loop"
	}
	for p.OperationN < ExperimentMaxOperations && r.Offset()-p.BodyStart < ExperimentMaxBodyBytes {
		at := r.Offset()
		if end, ok := experimentTail(body, at, p.Counter); ok {
			p.BodyEnd, p.End = at, end
			p.EstimatedGrowth = (p.BodyEnd-p.BodyStart+12)*factor + 64
			if p.EstimatedGrowth > ExperimentMaxFunctionGrowth {
				return "growth"
			}
			return ""
		}
		op, err := r.Byte()
		if err != nil {
			return "decode"
		}
		var imm wasm.InstructionImmediate
		if classify.ClassifyInto(&r, op, &imm) != nil {
			return "decode"
		}
		if !experimentPlain(op, simd, imm.Subopcode) {
			return "body-control-or-operation"
		}
		x := ExperimentOperation{Start: uint16(at - p.BodyStart), End: uint16(r.Offset() - p.BodyStart), Opcode: op, Offset: imm.MemOffset, Local: imm.Index}
		switch op {
		case 0x20, 0x21, 0x22:
			if imm.Index >= ExperimentMaxLocals {
				return "locals"
			}
			x.Read = op != 0x21
			x.Write = op != 0x20
			if x.Write && imm.Index == p.Counter {
				return "counter-write"
			}
			if x.Read && imm.Index == p.Counter {
				return "counter-read"
			}
			if x.Read {
				p.LocalReads |= 1 << imm.Index
			}
			if x.Write {
				p.LocalWrites |= 1 << imm.Index
			}
		case 0x28, 0x2a, 0x36, 0x38:
			x.Width = 4
		case 0x29, 0x2b, 0x37, 0x39:
			x.Width = 8
		case 0xfd:
			if imm.Subopcode == 0 || imm.Subopcode == 11 {
				x.Width = 16
			}
		}
		if x.Width != 0 {
			if imm.MemIndex != 0 {
				return "memory-index"
			}
			p.MemoryStreams++
			if p.MemoryStreams > ExperimentMaxMemoryStreams {
				return "memory-streams"
			}
		}
		p.Operations[p.OperationN] = x
		p.OperationN++
	}
	return "body-budget"
}

func experimentPlain(op byte, simd bool, sub uint32) bool {
	switch op {
	case 0x20, 0x21, 0x22, 0x41, 0x42, 0x43, 0x44, 0x28, 0x29, 0x2a, 0x2b, 0x36, 0x37, 0x38, 0x39,
		0x6a, 0x6b, 0x6c, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x7c, 0x7d, 0x7e, 0x83, 0x84, 0x85, 0x86, 0x87, 0x88,
		0x92, 0x93, 0x94, 0xa0, 0xa1, 0xa2:
		return true
	case 0xfd:
		return simd && (sub == 0 || sub == 11 || sub == 174 || sub == 228 || sub == 229 || sub == 230)
	}
	return false
}
func experimentU32(dst []byte, v uint32) []byte {
	for v >= 128 {
		dst = append(dst, byte(v)|128)
		v >>= 7
	}
	return append(dst, byte(v))
}
func experimentGet(dst []byte, idx uint32) []byte { return experimentU32(append(dst, 0x20), idx) }
func experimentSet(dst []byte, idx uint32) []byte { return experimentU32(append(dst, 0x21), idx) }
func (p *ReplicationPlan) replay(out, body []byte) []byte {
	for _, x := range p.Operations[:p.OperationN] {
		out = append(out, body[p.BodyStart+int(x.Start):p.BodyStart+int(x.End)]...)
	}
	out = experimentGet(out, p.Counter)
	out = append(out, 0x41, 1, 0x6b)
	return experimentSet(out, p.Counter)
}
func (p *ReplicationPlan) zeroExit(out []byte) []byte {
	out = experimentGet(out, p.Counter)
	return append(out, 0x45, 0x0d, 1)
}
func (p *ReplicationPlan) Emit(body []byte) []byte {
	out := make([]byte, 0, len(body)+p.EstimatedGrowth)
	out = append(out, body[:p.Start]...)
	if p.Guarded {
		out = append(out, 0x02, 0x40, 0x03, 0x40)
		for i := 0; i < p.Factor; i++ {
			out = p.zeroExit(out)
			out = p.replay(out, body)
		}
		out = append(out, 0x0c, 0, 0x0b, 0x0b)
	} else {
		out = append(out, 0x02, 0x40, 0x02, 0x40, 0x03, 0x40)
		out = experimentGet(out, p.Counter)
		out = append(out, 0x41, byte(p.Factor), 0x49, 0x0d, 1) // unsigned count < factor
		for i := 0; i < p.Factor; i++ {
			out = p.replay(out, body)
		}
		out = append(out, 0x0c, 0, 0x0b, 0x0b, 0x03, 0x40)
		out = p.zeroExit(out)
		out = p.replay(out, body)
		out = append(out, 0x0c, 0, 0x0b, 0x0b)
	}
	return append(out, body[p.End:]...)
}

// RewriteReplication is an experimental compiler-stage lowering. Input Wasm
// and the decoded module remain immutable. Allocation occurs only on accepted
// loops. Validation and scratch-allocation costs are included in compile timing.
func RewriteReplication(m *wasm.Module, mode string) (*wasm.Module, string, error) {

	if mode == "" || mode == "count1" || mode == "simd1" {
		return m, "disabled", nil
	}
	factor, guarded, simd := 2, false, false
	switch mode {
	case "count2":
	case "count4":
		factor = 4
	case "guard2":
		guarded = true
	case "simd2":
		simd = true
	case "simd4":
		factor, simd = 4, true
	default:
		return m, "mode", nil
	}
	if len(m.Tags) != 0 || len(m.Customs) != 0 {
		return m, "tags-or-custom", nil
	}
	mt, ok := m.MemoryType(0)
	if ok && (mt.Shared || mt.Limits.Addr64) {
		return m, "memory-kind", nil
	}
	var out *wasm.Module
	growth := 0
	reason := "no-loop"
	for i, f := range m.Code {
		var p ReplicationPlan
		why := InspectReplication(f.BodyBytes, m, factor, guarded, simd, &p)
		if why != "" {
			reason = why
			continue
		}
		if growth+p.EstimatedGrowth > ExperimentMaxModuleGrowth {
			reason = "module-growth"
			continue
		}
		replacement := p.Emit(f.BodyBytes)
		if bytes.Equal(replacement, f.BodyBytes) {
			continue
		}
		if out == nil {
			copy := *m
			out = &copy
			out.Code = append([]wasm.Func(nil), m.Code...)
			out.BranchHints = nil
		}
		out.Code[i].BodyBytes = replacement
		out.Code[i].Body = wasm.Expr{}
		growth += len(replacement) - len(f.BodyBytes)
	}
	if out == nil {
		return m, reason, nil
	}
	if err := wasm.ValidateModule(out); err != nil {
		return m, "validation", err
	}
	return out, "accepted", nil
}

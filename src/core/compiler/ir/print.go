package ir

import (
	"fmt"
	"strings"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func FormatModule(m *Module) string {
	var b strings.Builder
	for i := range m.Funcs {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(FormatFunc(&m.Funcs[i]))
	}
	return b.String()
}

func FormatFunc(f *Func) string {
	var b strings.Builder
	fmt.Fprintf(&b, "func $%d", f.Index)
	writeTypes(&b, f.Sig.Params)
	b.WriteString(" -> ")
	writeResultTypes(&b, f.Sig.Results)
	b.WriteString(" {\n")
	for bi := range f.Blocks {
		blk := &f.Blocks[bi]
		fmt.Fprintf(&b, "b%d", bi)
		writeValueList(&b, f, blk.Params)
		b.WriteString(":\n")
		for ii := blk.Insts.Start; ii < blk.Insts.End(); ii++ {
			inst := &f.Insts[ii]
			b.WriteString("  ")
			if inst.Results.Len > 0 {
				writeInstResults(&b, f, inst.Results)
				b.WriteString(" = ")
			}
			writeInst(&b, f, inst)
			b.WriteByte('\n')
		}
		b.WriteString("  ")
		writeTerm(&b, f, &blk.Term)
		b.WriteByte('\n')
	}
	b.WriteString("}\n")
	return b.String()
}

func writeTypes(b *strings.Builder, ts []wasm.ValType) {
	b.WriteByte('(')
	for i, t := range ts {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(t.String())
	}
	b.WriteByte(')')
}
func writeResultTypes(b *strings.Builder, ts []wasm.ValType) {
	if len(ts) == 0 {
		b.WriteString("()")
		return
	}
	if len(ts) == 1 {
		b.WriteString(ts[0].String())
		return
	}
	writeTypes(b, ts)
}
func writeValueList(b *strings.Builder, f *Func, r Range) {
	b.WriteByte('(')
	for i := uint32(0); i < r.Len; i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		v := f.ValueIDs[r.Start+i]
		fmt.Fprintf(b, "%%%d:%s", v, f.Values[v].Type)
	}
	b.WriteByte(')')
}
func writeInstResults(b *strings.Builder, f *Func, r Range) {
	for i := uint32(0); i < r.Len; i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		v := f.ValueIDs[r.Start+i]
		fmt.Fprintf(b, "%%%d:%s", v, f.Values[v].Type)
	}
}
func writeArgs(b *strings.Builder, f *Func, r Range) {
	for i := uint32(0); i < r.Len; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		v := f.ValueIDs[r.Start+i]
		fmt.Fprintf(b, " %%%d", v)
	}
}

func writeInst(b *strings.Builder, f *Func, in *Inst) {
	b.WriteString(opName(in.Op))
	switch in.Op {
	case OpConst:
		fmt.Fprintf(b, " %s", constString(auxTypeFromResult(f, in), in.Aux))
	case OpIUnary, OpIBinary, OpICmp, OpITest, OpFUnary, OpFBinary, OpFCmp, OpConvert, OpReinterpret:
		fmt.Fprintf(b, ".%s", auxName(in.Op, auxKind(in.Aux)))
	case OpLoad, OpStore:
		fmt.Fprintf(b, ".%s offset=%d align=%d mem=%d", memName(memKind(in.Aux)), memOffset(in.Aux), memAlign(in.Aux), memIndex(in.Aux))
	case OpMemorySize, OpMemoryGrow, OpMemoryFill:
		fmt.Fprintf(b, " mem=%d", uint32(in.Aux))
	case OpMemoryCopy:
		fmt.Fprintf(b, " dstmem=%d srcmem=%d", uint32(in.Aux), uint32(in.Aux>>32))
	case OpGlobalGet, OpGlobalSet, OpLocalGet, OpLocalSet, OpLocalTee:
		fmt.Fprintf(b, " %d", uint32(in.Aux))
	case OpCall, OpCallImport:
		fmt.Fprintf(b, " $%d", uint32(in.Aux))
	case OpCallIndirect:
		fmt.Fprintf(b, " type=%d table=%d canon=%d", callIndirectType(in.Aux), callIndirectTable(in.Aux), uint32(in.Aux2))
	case OpSelect:
		fmt.Fprintf(b, " %s", wasm.ValType(byte(in.Aux)).String())
	}
	writeArgs(b, f, in.Args)
}

func auxTypeFromResult(f *Func, in *Inst) wasm.ValType {
	if in.Results.Len == 0 {
		return 0
	}
	return f.Values[f.ValueIDs[in.Results.Start]].Type
}
func constString(t wasm.ValType, aux uint64) string {
	switch t {
	case wasm.I32:
		return fmt.Sprintf("i32 %d", int32(aux))
	case wasm.I64:
		return fmt.Sprintf("i64 %d", int64(aux))
	case wasm.F32:
		return fmt.Sprintf("f32 0x%08x", uint32(aux))
	case wasm.F64:
		return fmt.Sprintf("f64 0x%016x", aux)
	default:
		return fmt.Sprintf("0x%x", aux)
	}
}

func writeTerm(b *strings.Builder, f *Func, t *Term) {
	switch t.Kind {
	case TermBr:
		e := f.Edges[t.Edges.Start]
		fmt.Fprintf(b, "br b%d", e.To)
		writeArgs(b, f, e.Args)
	case TermCondBr:
		fmt.Fprintf(b, "condbr %%%d", t.Cond)
		e0 := f.Edges[t.Edges.Start]
		e1 := f.Edges[t.Edges.Start+1]
		fmt.Fprintf(b, " b%d", e0.To)
		writeArgs(b, f, e0.Args)
		fmt.Fprintf(b, " else b%d", e1.To)
		writeArgs(b, f, e1.Args)
	case TermSwitch:
		fmt.Fprintf(b, "switch %%%d", t.Index)
		for i := uint32(0); i < t.Edges.Len; i++ {
			e := f.Edges[t.Edges.Start+i]
			if i == t.Edges.Len-1 {
				b.WriteString(" default")
			} else {
				fmt.Fprintf(b, " %d", i)
			}
			fmt.Fprintf(b, ":b%d", e.To)
			writeArgs(b, f, e.Args)
		}
	case TermReturn:
		b.WriteString("return")
		writeArgs(b, f, t.Args)
	case TermTrap:
		b.WriteString("trap")
	default:
		b.WriteString("<invalid>")
	}
}

func auxName(op Op, k uint8) string {
	// Formatting is used in diagnostics, so keep it total even for malformed IR
	// that has not passed VerifyFunc yet.
	name := func(names []string) string {
		if int(k) < len(names) && names[k] != "" {
			return names[k]
		}
		return fmt.Sprintf("kind%d", k)
	}
	switch op {
	case OpIBinary:
		return name([]string{"", "add", "sub", "mul", "div_s", "div_u", "rem_s", "rem_u", "and", "or", "xor", "shl", "shr_s", "shr_u", "rotl", "rotr"})
	case OpIUnary:
		return name([]string{"", "clz", "ctz", "popcnt", "extend8_s", "extend16_s", "extend32_s"})
	case OpICmp:
		return name([]string{"", "eq", "ne", "lt_s", "lt_u", "gt_s", "gt_u", "le_s", "le_u", "ge_s", "ge_u"})
	case OpITest:
		return name([]string{"", "eqz"})
	case OpFUnary:
		return name([]string{"", "abs", "neg", "ceil", "floor", "trunc", "nearest", "sqrt"})
	case OpFBinary:
		return name([]string{"", "add", "sub", "mul", "div", "min", "max", "copysign"})
	case OpFCmp:
		return name([]string{"", "eq", "ne", "lt", "gt", "le", "ge"})
	case OpConvert:
		return name([]string{"", "wrap_i64_i32", "trunc_f_i_s", "trunc_f_i_u", "extend_i32_s", "extend_i32_u", "convert_i_f_s", "convert_i_f_u", "demote_f64_f32", "promote_f32_f64", "trunc_sat_f_i_s", "trunc_sat_f_i_u"})
	case OpReinterpret:
		return name([]string{"", "f32_to_i32", "f64_to_i64", "i32_to_f32", "i64_to_f64"})
	}
	return fmt.Sprintf("kind%d", k)
}
func memName(k MemOp) string {
	if d, ok := lookupMemDesc(k); ok {
		return d.name
	}
	return fmt.Sprintf("mem%d", k)
}

package wago

import (
	"math"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// These are source-work limits, not wall-clock guarantees. Bulk bytes count both reads
// and writes for copies. Scalar memory operations are charged to the independent
// source-operation budget. Unknown helpers, control flow and loop-carried counts fail
// closed. The resulting capability is private to a fresh compilation.
const (
	boundedBulkBytes = uint64(4 << 20)
	boundedBulkSteps = uint64(4096)
	boundedBulkNodes = 4096
)

type bulkWork struct {
	steps, fixed, argument uint64
	argumentIndex          uint32
	depth                  uint8
	bulk                   bool
}

func (w *bulkWork) add(v bulkWork, factor uint64) bool {
	add := func(dst *uint64, x, limit uint64) bool {
		if *dst > limit || factor != 0 && x > (limit-*dst)/factor {
			return false
		}
		*dst += x * factor
		return true
	}
	if v.argument != 0 {
		if w.argument != 0 && w.argumentIndex != v.argumentIndex {
			return false
		}
		w.argumentIndex = v.argumentIndex
	}
	if !add(&w.steps, v.steps, boundedBulkSteps) || !add(&w.fixed, v.fixed, boundedBulkBytes) || !add(&w.argument, v.argument, boundedBulkBytes) {
		return false
	}
	w.depth = max(w.depth, v.depth)
	w.bulk = w.bulk || v.bulk
	return true
}

type bulkValue struct {
	kind  uint8
	value uint32
}

const (
	bulkUnknown uint8 = iota
	bulkConstant
	bulkArgument
)

type bulkNode struct {
	op   byte
	arg  uint32
	body []bulkNode
}
type bulkProof struct {
	state  uint8
	work   bulkWork
	result bool
	params int
}
type bulkAnalyzer struct {
	module *wasm.Module
	proofs []bulkProof
	nodes  int
}

// boundedBulkArgumentPlans proves complete invocations with up to four i32 parameters.
// Constant-count loops and acyclic local calls may multiply a parameter's bulk
// byte cost for one count parameter; mutable loop-carried values never retain an argument/constant fact.
// The low word selects the count parameter plus one (zero is input independent);
// the high word limits its unsigned value. A zero plan means no proof. No names, hashes or export identities are used.
func boundedBulkArgumentPlans(m *wasm.Module) []uint64 {
	if m == nil || len(m.Code) == 0 || len(m.Code) > 4096 || len(m.Imports) != 0 || len(m.Memories) != 1 || m.Memories[0].Shared || m.Memories[0].Limits.Addr64 || len(m.Tables) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 {
		return nil
	}
	a := bulkAnalyzer{module: m, proofs: make([]bulkProof, len(m.Code))}
	var limits []uint64
	for i := range m.Code {
		w, ok := a.function(i, 0)
		if !ok || !w.bulk {
			continue
		}
		limit := uint64(math.MaxUint32)
		if w.argument != 0 {
			limit = (boundedBulkBytes - w.fixed) / w.argument
		}
		if limit == 0 {
			continue
		}
		if limits == nil {
			limits = make([]uint64, len(m.Code))
		}
		argument := uint32(0)
		if w.argument != 0 {
			argument = w.argumentIndex + 1
		}
		limits[i] = min(limit, uint64(math.MaxUint32))<<32 | uint64(argument)
	}
	return limits
}

func (a *bulkAnalyzer) parse(r *wasm.Reader, depth int) ([]bulkNode, bool) {
	if depth > 8 {
		return nil, false
	}
	var out []bulkNode
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return nil, false
		}
		if op == 0x0b {
			return out, true
		}
		a.nodes++
		if a.nodes > boundedBulkNodes {
			return nil, false
		}
		n := bulkNode{op: op}
		switch op {
		case 0x03:
			bt, err := r.Byte()
			if err != nil || bt != 0x40 {
				return nil, false
			}
			var ok bool
			n.body, ok = a.parse(r, depth+1)
			if !ok {
				return nil, false
			}
		case 0x10, 0x20, 0x21, 0x22, 0x0d:
			n.arg, err = r.U32()
		case 0x41:
			var v int32
			v, err = r.I32()
			n.arg = uint32(v)
		case 0x28, 0x2c, 0x2d, 0x2e, 0x2f, 0x36, 0x3a, 0x3b:
			var align uint32
			align, err = r.U32()
			if err != nil || align >= 64 {
				return nil, false
			}
			_, err = r.U32()
		case 0xfc:
			n.arg, err = r.U32()
			if err != nil || (n.arg != 10 && n.arg != 11) {
				return nil, false
			}
			index, e := r.U32()
			if e != nil || index != 0 {
				return nil, false
			}
			if n.arg == 10 {
				index, err = r.U32()
				if err != nil || index != 0 {
					return nil, false
				}
			}
		case 0x01, 0x1a, 0x45, 0x46, 0x47, 0x48, 0x49, 0x4a, 0x4b, 0x4c, 0x4d, 0x4e, 0x4f, 0x6a, 0x6b, 0x6c, 0x71, 0x72, 0x73:
		default:
			return nil, false
		}
		if err != nil {
			return nil, false
		}
		out = append(out, n)
	}
	return nil, false
}

func (a *bulkAnalyzer) function(index, depth int) (bulkWork, bool) {
	if index < 0 || index >= len(a.proofs) || depth > 64 {
		return bulkWork{}, false
	}
	p := &a.proofs[index]
	if p.state == 2 {
		return p.work, true
	}
	if p.state != 0 {
		return bulkWork{}, false
	}
	p.state = 1
	ok := false
	defer func() {
		if !ok {
			p.state = 3
		}
	}()
	ft, valid := a.module.LocalFuncType(index)
	if !valid || len(ft.Params) > 4 || len(ft.Results) > 1 || len(ft.Results) == 1 && ft.Results[0] != wasm.I32 {
		return bulkWork{}, false
	}
	for _, typ := range ft.Params {
		if typ != wasm.I32 {
			return bulkWork{}, false
		}
	}
	fn := &a.module.Code[index]
	if len(fn.BodyBytes) == 0 || len(fn.BodyBytes) > 1024 {
		return bulkWork{}, false
	}
	locals := make([]bulkValue, len(ft.Params))
	for i := range locals {
		locals[i] = bulkValue{bulkArgument, uint32(i)}
	}
	for _, run := range fn.Locals.Runs {
		if run.Type != wasm.I32 || uint64(run.Count) > uint64(64-len(locals)) {
			return bulkWork{}, false
		}
		for range run.Count {
			locals = append(locals, bulkValue{kind: bulkConstant})
		}
	}
	r := wasm.ReaderFrom(fn.BodyBytes)
	nodes, valid := a.parse(&r, 0)
	if !valid || r.HasNext() {
		return bulkWork{}, false
	}
	stack := make([]bulkValue, 0, 16)
	w, valid := a.sequence(nodes, locals, &stack, depth)
	if !valid || len(stack) != len(ft.Results) {
		return bulkWork{}, false
	}
	w.depth = max(w.depth, 1)
	if w.depth > 16 {
		return bulkWork{}, false
	}
	p.work, p.result, p.params, p.state = w, len(ft.Results) == 1, len(ft.Params), 2
	ok = true
	return w, true
}

func (a *bulkAnalyzer) sequence(nodes []bulkNode, locals []bulkValue, stack *[]bulkValue, depth int) (bulkWork, bool) {
	var w bulkWork
	floor := len(*stack)
	pop := func() (bulkValue, bool) {
		if len(*stack) <= floor {
			return bulkValue{}, false
		}
		i := len(*stack) - 1
		v := (*stack)[i]
		*stack = (*stack)[:i]
		return v, true
	}
	push := func(v bulkValue) bool {
		if len(*stack) >= 64 {
			return false
		}
		*stack = append(*stack, v)
		return true
	}
	for _, n := range nodes {
		if !w.add(bulkWork{steps: 1}, 1) {
			return w, false
		}
		switch n.op {
		case 0x01:
		case 0x41:
			if !push(bulkValue{bulkConstant, n.arg}) {
				return w, false
			}
		case 0x20:
			if uint64(n.arg) >= uint64(len(locals)) || !push(locals[n.arg]) {
				return w, false
			}
		case 0x21, 0x22:
			v, ok := pop()
			if !ok || uint64(n.arg) >= uint64(len(locals)) {
				return w, false
			}
			locals[n.arg] = v
			if n.op == 0x22 && !push(v) {
				return w, false
			}
		case 0x1a:
			if _, ok := pop(); !ok {
				return w, false
			}
		case 0x45:
			v, ok := pop()
			if !ok {
				return w, false
			}
			if v.kind == bulkConstant {
				if v.value == 0 {
					v.value = 1
				} else {
					v.value = 0
				}
			} else {
				v = bulkValue{}
			}
			if !push(v) {
				return w, false
			}
		case 0x6a, 0x6b, 0x6c, 0x71, 0x72, 0x73, 0x46, 0x47, 0x48, 0x49, 0x4a, 0x4b, 0x4c, 0x4d, 0x4e, 0x4f:
			b, bok := pop()
			av, aok := pop()
			if !aok || !bok {
				return w, false
			}
			v := bulkValue{}
			if av.kind == bulkConstant && b.kind == bulkConstant {
				v.kind = bulkConstant
				switch n.op {
				case 0x6a:
					v.value = av.value + b.value
				case 0x6b:
					v.value = av.value - b.value
				case 0x6c:
					v.value = av.value * b.value
				case 0x71:
					v.value = av.value & b.value
				case 0x72:
					v.value = av.value | b.value
				case 0x73:
					v.value = av.value ^ b.value
				default:
					v.kind = bulkUnknown
				}
			}
			if !push(v) {
				return w, false
			}
		case 0x28, 0x2c, 0x2d, 0x2e, 0x2f:
			if _, ok := pop(); !ok || !push(bulkValue{}) {
				return w, false
			}
		case 0x36, 0x3a, 0x3b:
			if _, ok := pop(); !ok {
				return w, false
			}
			if _, ok := pop(); !ok {
				return w, false
			}
		case 0xfc:
			count, ok := pop()
			if !ok {
				return w, false
			}
			for range 2 {
				if _, ok = pop(); !ok {
					return w, false
				}
			}
			cost := bulkWork{bulk: true}
			bytes := uint64(1)
			if n.arg == 10 {
				bytes = 2
			}
			if count.kind == bulkConstant {
				cost.fixed = uint64(count.value) * bytes
			} else if count.kind == bulkArgument {
				cost.argument = bytes
				cost.argumentIndex = count.value
			} else {
				return w, false
			}
			if !w.add(cost, 1) {
				return w, false
			}
		case 0x10:
			cost, ok := a.function(int(n.arg), depth+1)
			if !ok {
				return w, false
			}
			callee := &a.proofs[n.arg]
			if len(*stack)-floor < callee.params {
				return w, false
			}
			base := len(*stack) - callee.params
			arg := bulkValue{}
			if cost.argument != 0 {
				if uint64(cost.argumentIndex) >= uint64(callee.params) {
					return w, false
				}
				arg = (*stack)[base+int(cost.argumentIndex)]
			}
			*stack = (*stack)[:base]
			cost.depth++
			coefficient := cost.argument
			cost.argument = 0
			if arg.kind == bulkConstant {
				if coefficient != 0 && uint64(arg.value) > (boundedBulkBytes-cost.fixed)/coefficient {
					return w, false
				}
				cost.fixed += coefficient * uint64(arg.value)
			} else if arg.kind == bulkArgument {
				cost.argument, cost.argumentIndex = coefficient, arg.value
			} else if coefficient != 0 {
				return w, false
			}
			if !w.add(cost, 1) || callee.result && !push(bulkValue{}) {
				return w, false
			}

		case 0x03:
			cost, ok := a.loop(n.body, locals, stack, depth)
			if !ok || !w.add(cost, 1) {
				return w, false
			}
		default:
			return w, false
		}
	}
	return w, true
}

func (a *bulkAnalyzer) loop(body []bulkNode, locals []bulkValue, stack *[]bulkValue, depth int) (bulkWork, bool) {
	if len(body) < 8 {
		return bulkWork{}, false
	}
	t := body[len(body)-8:]
	counter := t[0].arg
	if t[0].op != 0x20 || t[1].op != 0x41 || t[1].arg == 0 || t[2].op != 0x6a || t[3].op != 0x21 || t[3].arg != counter || t[4].op != 0x20 || t[4].arg != counter || t[5].op != 0x41 || t[6].op != 0x49 || t[7].op != 0x0d || t[7].arg != 0 || uint64(counter) >= uint64(len(locals)) || locals[counter].kind != bulkConstant {
		return bulkWork{}, false
	}
	start, step, bound := uint64(locals[counter].value), uint64(t[1].arg), uint64(t[5].arg)
	iterations := uint64(1)
	if start < bound {
		iterations = (bound - start + step - 1) / step
	}
	if iterations > boundedBulkSteps || start+iterations*step > math.MaxUint32 {
		return bulkWork{}, false
	}
	prefix := body[:len(body)-8]
	written := make([]bool, len(locals))
	var writes func([]bulkNode) bool
	writes = func(ns []bulkNode) bool {
		for _, n := range ns {
			if n.op == 0x21 || n.op == 0x22 {
				if uint64(n.arg) >= uint64(len(written)) || n.arg == counter {
					return false
				}
				written[n.arg] = true
			}
			if !writes(n.body) {
				return false
			}
		}
		return true
	}
	if !writes(prefix) {
		return bulkWork{}, false
	}
	written[counter] = true
	for i, on := range written {
		if on {
			locals[i] = bulkValue{}
		}
	}
	height := len(*stack)
	cost, ok := a.sequence(prefix, locals, stack, depth)
	if !ok || len(*stack) != height || !cost.add(bulkWork{steps: 8}, 1) {
		return bulkWork{}, false
	}
	for i, on := range written {
		if on {
			locals[i] = bulkValue{}
		}
	}
	locals[counter] = bulkValue{bulkConstant, uint32(start + iterations*step)}
	var total bulkWork
	if !total.add(cost, iterations) {
		return total, false
	}
	return total, true
}

func (c *Compiled) boundedBulkArgumentPlan(local int) uint64 {
	if c == nil || local < 0 || local >= len(c.boundedBulkPlans) {
		return 0
	}
	return c.boundedBulkPlans[local]
}

func (fn *WasmFunc) boundedBulkArguments(args []uint64) bool {
	index := int(fn.boundedBulkArgument)
	return fn.boundedBulkLimit != 0 && fn.in.gc == nil && (index == 0 || index <= len(args) && uint32(args[index-1]) <= fn.boundedBulkLimit)
}

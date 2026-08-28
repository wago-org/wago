// Package railssa implements Dragline's compact, function-local scalar SSA.
package railssa

import (
	"fmt"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// ValueID is a dense index into Func.Values.
type ValueID uint32

// Range indexes a contiguous run in Func.Args.
type Range struct {
	Start uint32
	Len   uint16
}

type Op uint8

const (
	OpInvalid Op = iota
	OpParam
	OpConst
	OpAdd
	OpSub
	OpAnd
	OpOr
	OpXor
)

func (op Op) String() string {
	switch op {
	case OpParam:
		return "param"
	case OpConst:
		return "const"
	case OpAdd:
		return "add"
	case OpSub:
		return "sub"
	case OpAnd:
		return "and"
	case OpOr:
		return "or"
	case OpXor:
		return "xor"
	default:
		return "invalid"
	}
}

// Value is one SSA definition. Variable operands live in Func.Args.
type Value struct {
	Type wasm.ValType
	Op   Op
	Args Range
	Aux  uint64
}

// Func is the first deliberately small Dragline IR: dense values, shared
// operands, direct local SSA, and at most one scalar result.
type Func struct {
	Index      uint32
	Params     []wasm.ValType
	Results    []wasm.ValType
	Values     []Value
	Args       []ValueID
	Result     ValueID
	Stack      *StackFunc
	Structured *StackFunc
	// HelperSafepointBase is the module-global identity assigned to this
	// function's first allocating parked helper.
	HelperSafepointBase uint32
}

// CapacityBytes reports compiler-owned backing storage retained by f. It is
// capacity-based so reusable arena headroom is included in peak-live metrics.
func (f *Func) CapacityBytes() uint64 {
	if f == nil {
		return 0
	}
	if f.Stack != nil {
		return f.Stack.CapacityBytes()
	}
	bytes := uint64(cap(f.Params))*uint64(unsafe.Sizeof(wasm.ValType{})) +
		uint64(cap(f.Results))*uint64(unsafe.Sizeof(wasm.ValType{})) +
		uint64(cap(f.Values))*uint64(unsafe.Sizeof(Value{})) +
		uint64(cap(f.Args))*uint64(unsafe.Sizeof(ValueID(0)))
	if f.Structured != nil {
		bytes += f.Structured.CapacityBytes()
	}
	return bytes
}

// PeakBuildBytes includes temporary prepass storage that was released before
// the function was returned to the emitter.
func (f *Func) PeakBuildBytes() uint64 {
	if f == nil {
		return 0
	}
	if f.Stack != nil && f.Stack.BuildPeakBytes > f.Stack.CapacityBytes() {
		return f.Stack.BuildPeakBytes
	}
	return f.CapacityBytes()
}

func (f *Func) operands(v *Value) []ValueID {
	start := int(v.Args.Start)
	return f.Args[start : start+int(v.Args.Len)]
}

// Operands returns the immutable operand view for value.
func (f *Func) Operands(value ValueID) []ValueID {
	return f.operands(&f.Values[value])
}

// BuildFunc lowers the straight-line integer MVP subset directly into SSA.
func BuildFunc(m *wasm.Module, localIndex int) (*Func, error) {
	if m == nil || localIndex < 0 || localIndex >= len(m.Code) || localIndex >= len(m.FuncTypes) {
		return nil, fmt.Errorf("railssa: local function %d is unavailable", localIndex)
	}
	ft, ok := m.LocalFuncType(localIndex)
	if !ok || ft == nil {
		return nil, fmt.Errorf("railssa: local function %d has no function type", localIndex)
	}
	if len(ft.Results) > 1 {
		return nil, fmt.Errorf("railssa: function %d has %d results; MVP supports at most one", localIndex, len(ft.Results))
	}
	for i, typ := range ft.Params {
		if !scalarInt(typ) {
			return nil, fmt.Errorf("railssa: function %d parameter %d has unsupported type %s", localIndex, i, typ)
		}
	}
	for i, typ := range ft.Results {
		if !scalarInt(typ) {
			return nil, fmt.Errorf("railssa: function %d result %d has unsupported type %s", localIndex, i, typ)
		}
	}
	fn := &Func{Index: uint32(m.ImportedFuncCount() + localIndex), Params: append([]wasm.ValType(nil), ft.Params...), Results: append([]wasm.ValType(nil), ft.Results...)}
	locals := make([]ValueID, 0, len(ft.Params)+8)
	localTypes := make([]wasm.ValType, 0, len(ft.Params)+8)
	for i, typ := range ft.Params {
		id := fn.addValue(Value{Type: typ, Op: OpParam, Aux: uint64(i)})
		locals = append(locals, id)
		localTypes = append(localTypes, typ)
	}
	for runIndex, run := range m.Code[localIndex].Locals.Runs {
		if !scalarInt(run.Type) {
			return nil, fmt.Errorf("railssa: function %d local run %d has unsupported type %s", localIndex, runIndex, run.Type)
		}
		if uint64(len(locals))+uint64(run.Count) > 4096 {
			return nil, &BudgetError{Resource: fmt.Sprintf("function %d direct SSA locals", localIndex), Required: uint64(len(locals)) + uint64(run.Count), Limit: 4096}
		}
		zero := fn.addValue(Value{Type: run.Type, Op: OpConst})
		for range run.Count {
			locals = append(locals, zero)
			localTypes = append(localTypes, run.Type)
		}
	}
	body := m.Code[localIndex].BodyBytes
	if len(body) == 0 {
		return nil, fmt.Errorf("railssa: function %d has no byte-backed body", localIndex)
	}
	r := wasm.NewReader(body)
	stack := make([]ValueID, 0, 16)
	ended := false
	for r.HasNext() {
		offset := r.Offset()
		opcode, err := r.Byte()
		if err != nil {
			return nil, fmt.Errorf("railssa: function %d byte %d: %w", localIndex, offset, err)
		}
		switch opcode {
		case 0x0b: // end
			if r.HasNext() {
				return nil, fmt.Errorf("railssa: function %d has nested control flow outside the MVP", localIndex)
			}
			ended = true
		case 0x0f: // return
			return nil, fmt.Errorf("railssa: function %d explicit return is outside the straight-line MVP", localIndex)
		case 0x1a: // drop
			if _, err := pop(&stack); err != nil {
				return nil, fn.wrap(offset, err)
			}
		case 0x20, 0x21, 0x22: // local.get/set/tee
			index, err := r.U32()
			if err != nil {
				return nil, fn.wrap(offset, err)
			}
			if int(index) >= len(locals) {
				return nil, fn.wrap(offset, fmt.Errorf("local %d is out of range", index))
			}
			if opcode == 0x20 {
				stack = append(stack, locals[index])
				continue
			}
			value, err := pop(&stack)
			if err != nil {
				return nil, fn.wrap(offset, err)
			}
			if fn.Values[value].Type != localTypes[index] {
				return nil, fn.wrap(offset, fmt.Errorf("local %d type mismatch", index))
			}
			locals[index] = value
			if opcode == 0x22 {
				stack = append(stack, value)
			}
		case 0x41:
			value, err := r.I32()
			if err != nil {
				return nil, fn.wrap(offset, err)
			}
			stack = append(stack, fn.addValue(Value{Type: wasm.I32, Op: OpConst, Aux: uint64(uint32(value))}))
		case 0x42:
			value, err := r.I64()
			if err != nil {
				return nil, fn.wrap(offset, err)
			}
			stack = append(stack, fn.addValue(Value{Type: wasm.I64, Op: OpConst, Aux: uint64(value)}))
		default:
			kind, ok := wasm.ImmediateFreeInstructionKind(opcode)
			if !ok {
				return nil, fn.wrap(offset, fmt.Errorf("opcode 0x%02x is outside the MVP", opcode))
			}
			op, typ := integerBinary(kind)
			if op == OpInvalid {
				return nil, fn.wrap(offset, fmt.Errorf("instruction %s is outside the MVP", kind))
			}
			rhs, err := pop(&stack)
			if err != nil {
				return nil, fn.wrap(offset, err)
			}
			lhs, err := pop(&stack)
			if err != nil {
				return nil, fn.wrap(offset, err)
			}
			if fn.Values[lhs].Type != typ || fn.Values[rhs].Type != typ {
				return nil, fn.wrap(offset, fmt.Errorf("%s operand type mismatch", kind))
			}
			stack = append(stack, fn.addBinary(typ, op, lhs, rhs))
		}
	}
	if !ended {
		return nil, fmt.Errorf("railssa: function %d is missing end", localIndex)
	}
	if len(fn.Results) == 0 {
		if len(stack) != 0 {
			return nil, fmt.Errorf("railssa: function %d leaves %d values on the stack", localIndex, len(stack))
		}
	} else {
		if len(stack) != 1 || fn.Values[stack[0]].Type != fn.Results[0] {
			return nil, fmt.Errorf("railssa: function %d result stack does not match %s", localIndex, fn.Results[0])
		}
		fn.Result = stack[0]
	}
	if err := Verify(fn); err != nil {
		return nil, err
	}
	return fn, nil
}

func scalarInt(typ wasm.ValType) bool { return typ == wasm.I32 || typ == wasm.I64 }

func integerBinary(kind wasm.InstrKind) (Op, wasm.ValType) {
	switch kind {
	case wasm.InstrI32Add:
		return OpAdd, wasm.I32
	case wasm.InstrI32Sub:
		return OpSub, wasm.I32
	case wasm.InstrI32And:
		return OpAnd, wasm.I32
	case wasm.InstrI32Or:
		return OpOr, wasm.I32
	case wasm.InstrI32Xor:
		return OpXor, wasm.I32
	case wasm.InstrI64Add:
		return OpAdd, wasm.I64
	case wasm.InstrI64Sub:
		return OpSub, wasm.I64
	case wasm.InstrI64And:
		return OpAnd, wasm.I64
	case wasm.InstrI64Or:
		return OpOr, wasm.I64
	case wasm.InstrI64Xor:
		return OpXor, wasm.I64
	default:
		return OpInvalid, wasm.ValType{}
	}
}

func pop(stack *[]ValueID) (ValueID, error) {
	values := *stack
	if len(values) == 0 {
		return 0, fmt.Errorf("operand stack underflow")
	}
	value := values[len(values)-1]
	*stack = values[:len(values)-1]
	return value, nil
}

func (f *Func) addValue(value Value) ValueID {
	id := ValueID(len(f.Values))
	f.Values = append(f.Values, value)
	return id
}

func (f *Func) addBinary(typ wasm.ValType, op Op, lhs, rhs ValueID) ValueID {
	r := Range{Start: uint32(len(f.Args)), Len: 2}
	f.Args = append(f.Args, lhs, rhs)
	return f.addValue(Value{Type: typ, Op: op, Args: r})
}

func (f *Func) wrap(offset int, err error) error {
	return fmt.Errorf("railssa: function %d byte %d: %w", f.Index, offset, err)
}

// Verify checks the compact value and operand tables before machine lowering.
func Verify(f *Func) error {
	if f == nil {
		return fmt.Errorf("railssa: nil function")
	}
	if len(f.Params) > len(f.Values) {
		return fmt.Errorf("railssa: %d parameters exceed %d values", len(f.Params), len(f.Values))
	}
	for id := range f.Values {
		value := &f.Values[id]
		if !scalarInt(value.Type) || value.Op == OpInvalid {
			return fmt.Errorf("railssa: value %d has invalid definition", id)
		}
		end := uint64(value.Args.Start) + uint64(value.Args.Len)
		if end > uint64(len(f.Args)) {
			return fmt.Errorf("railssa: value %d operand range is out of bounds", id)
		}
		args := f.operands(value)
		if value.Op == OpParam && (id >= len(f.Params) || value.Aux != uint64(id) || value.Type != f.Params[id]) {
			return fmt.Errorf("railssa: value %d has invalid parameter definition", id)
		}
		if value.Op >= OpAdd && value.Op <= OpXor {
			if len(args) != 2 {
				return fmt.Errorf("railssa: value %d binary arity is %d", id, len(args))
			}
			for _, arg := range args {
				if int(arg) >= id || f.Values[arg].Type != value.Type {
					return fmt.Errorf("railssa: value %d has invalid operand %d", id, arg)
				}
			}
		} else if len(args) != 0 {
			return fmt.Errorf("railssa: value %d unexpectedly has operands", id)
		}
	}
	if len(f.Results) == 1 && (int(f.Result) >= len(f.Values) || f.Values[f.Result].Type != f.Results[0]) {
		return fmt.Errorf("railssa: invalid result value %d", f.Result)
	}
	return nil
}

// Eval provides a differential oracle for the MVP subset.
func Eval(f *Func, params []uint64) (uint64, error) {
	if err := Verify(f); err != nil {
		return 0, err
	}
	if len(params) != len(f.Params) {
		return 0, fmt.Errorf("railssa: got %d parameters, want %d", len(params), len(f.Params))
	}
	values := make([]uint64, len(f.Values))
	for id := range f.Values {
		value := &f.Values[id]
		switch value.Op {
		case OpParam:
			values[id] = params[value.Aux]
		case OpConst:
			values[id] = value.Aux
		default:
			args := f.operands(value)
			lhs, rhs := values[args[0]], values[args[1]]
			switch value.Op {
			case OpAdd:
				values[id] = lhs + rhs
			case OpSub:
				values[id] = lhs - rhs
			case OpAnd:
				values[id] = lhs & rhs
			case OpOr:
				values[id] = lhs | rhs
			case OpXor:
				values[id] = lhs ^ rhs
			}
		}
		if value.Type == wasm.I32 {
			values[id] = uint64(uint32(values[id]))
		}
	}
	if len(f.Results) == 0 {
		return 0, nil
	}
	return values[f.Result], nil
}

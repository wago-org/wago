// Package plugins defines Wago's compiler-plugin instruction model independently
// of any compiler backend. Backends integrate it through small compatibility
// adapters at their own seams.
package plugins

import (
	"encoding/binary"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/machinecode"
)

// SIMDInstruction describes a pointer-based, architecture-neutral wide SIMD
// operation. The physical Wasm parameters are destination first followed by
// Arity input pointers. Backends privately select their native representation.
type SIMDInstruction struct {
	Width     uint16
	Subopcode uint32
	Arity     uint8
}

// InstructionSpec describes a language-neutral custom instruction. Input and
// Output contain logical value widths in bits. Its physical Wasm signature is
// derived entirely from those slices: every input is i32, and the result is
// void for no outputs or one i32 otherwise. With multiple logical outputs that
// i32 is a result-pack handle projected through wago:abi.result.get.
type InstructionSpec struct {
	Module  string
	Name    string
	Input   []int32
	Output  []int32
	Handler InstructionHandler
	Lower   InstructionLowerer
	// SIMD declares an architecture-neutral Wasm SIMD-style operation over
	// linear-memory vectors. Its physical signature is
	// (destination i32, input pointers...) -> (). Backends privately select
	// NEON, YMM, or another native representation.
	SIMD *SIMDInstruction
	// AMD64 is an explicitly unsafe, fully trusted machine-code lowering. It may
	// use Wago's real encoder or append arbitrary bytes through Encoder().B.
	AMD64 *machinecode.AMD64Lowering
}

// InstructionHandler implements the portable semantics of an instruction.
// Values are unsigned fixed-width bit strings; arithmetic interpretation is up
// to the instruction. The returned slice must match InstructionSpec.Output.
type InstructionHandler func(InstructionContext, []Bits) ([]Bits, error)

// InstructionContext is the per-call view exposed to a portable handler.
type InstructionContext interface {
	Memory() []byte
}

// Bits is an immutable, arbitrary-width logical instruction value. Bytes are
// little-endian and unused high bits are always zero.
type Bits struct {
	width int32
	data  []byte
}

// NewBits constructs a canonical logical value from little-endian bytes.
func NewBits(width int32, littleEndian []byte) (Bits, error) {
	if width <= 0 {
		return Bits{}, fmt.Errorf("wago: bit width must be positive, got %d", width)
	}
	n := int((width + 7) / 8)
	if len(littleEndian) > n {
		return Bits{}, fmt.Errorf("wago: %d-bit value needs at most %d byte(s), got %d", width, n, len(littleEndian))
	}
	b := Bits{width: width, data: make([]byte, n)}
	copy(b.data, littleEndian)
	if rem := uint(width & 7); rem != 0 {
		b.data[n-1] &= byte((uint16(1) << rem) - 1)
	}
	return b, nil
}

// BitsFromUint32 constructs a value from the low width bits of v.
func BitsFromUint32(width int32, v uint32) (Bits, error) {
	if width <= 0 || width > 32 {
		return Bits{}, fmt.Errorf("wago: uint32 bit width must be in [1,32], got %d", width)
	}
	var raw [4]byte
	binary.LittleEndian.PutUint32(raw[:], v)
	return NewBits(width, raw[:(width+7)/8])
}

// Width returns the logical width in bits.
func (b Bits) Width() int32 { return b.width }

// Bytes returns a detached little-endian representation.
func (b Bits) Bytes() []byte { return append([]byte(nil), b.data...) }

// Uint32 returns the low 32 bits. It is valid for every width, and is the whole
// value when Width() <= 32.
func (b Bits) Uint32() uint32 {
	var raw [4]byte
	copy(raw[:], b.data)
	return binary.LittleEndian.Uint32(raw[:])
}

// Clone returns a detached copy of b.
func (b Bits) Clone() Bits { return Bits{width: b.width, data: append([]byte(nil), b.data...)} }

// ValidFor reports whether b is the canonical representation of width bits.
func (b Bits) ValidFor(width int32) bool {
	return b.width == width && width > 0 && len(b.data) == int((width+7)/8)
}

// LowerValue is one value in a constrained custom-instruction lowering recipe.
// It is only meaningful to the LoweringContext that created it.
type LowerValue struct{ id int }

// InstructionLowerer builds a target-independent fixed-width bit-vector recipe.
// Wago may compile a supported recipe natively and otherwise uses Handler.
type InstructionLowerer func(LoweringContext) error

// LoweringContext is deliberately smaller than the compiler backend API. All
// operations wrap to the result width, so Add on 4-bit values implements i4.add.
type LoweringContext interface {
	Input(index int) LowerValue
	Const(width int32, littleEndian ...byte) LowerValue
	Add(a, b LowerValue) LowerValue
	Sub(a, b LowerValue) LowerValue
	Mul(a, b LowerValue) LowerValue
	And(a, b LowerValue) LowerValue
	Or(a, b LowerValue) LowerValue
	Xor(a, b LowerValue) LowerValue
	Not(v LowerValue) LowerValue
	Shl(v, count LowerValue) LowerValue
	ShrU(v, count LowerValue) LowerValue
	ShrS(v, count LowerValue) LowerValue
	Eq(a, b LowerValue) LowerValue
	Ne(a, b LowerValue) LowerValue
	LtU(a, b LowerValue) LowerValue
	LtS(a, b LowerValue) LowerValue
	LeU(a, b LowerValue) LowerValue
	LeS(a, b LowerValue) LowerValue
	GtU(a, b LowerValue) LowerValue
	GtS(a, b LowerValue) LowerValue
	GeU(a, b LowerValue) LowerValue
	GeS(a, b LowerValue) LowerValue
	IsZero(v LowerValue) LowerValue
	Select(ifTrue, ifFalse, condition LowerValue) LowerValue
	Output(index int, value LowerValue)
}

// InstructionOp identifies one operation in a backend-neutral scalar lowering
// recipe.
type InstructionOp uint8

const (
	InstructionInput InstructionOp = iota
	InstructionConst
	InstructionAdd
	InstructionSub
	InstructionMul
	InstructionAnd
	InstructionOr
	InstructionXor
	InstructionNot
	InstructionShl
	InstructionShrU
	InstructionShrS
	InstructionEq
	InstructionNe
	InstructionLtU
	InstructionLtS
	InstructionLeU
	InstructionLeS
	InstructionGtU
	InstructionGtS
	InstructionGeU
	InstructionGeS
	InstructionIsZero
	InstructionSelect
)

type instructionNode struct {
	op       InstructionOp
	width    int32
	a, b, c  int
	input    int
	constant Bits
}

type instructionRecipe struct {
	nodes   []instructionNode
	outputs []int
}

type loweringBuilder struct {
	inputWidths, outputWidths []int32
	recipe                    instructionRecipe
	err                       error
}

func (b *loweringBuilder) value(v LowerValue) (instructionNode, bool) {
	if v.id < 0 || v.id >= len(b.recipe.nodes) {
		b.fail("value does not belong to this lowering")
		return instructionNode{}, false
	}
	return b.recipe.nodes[v.id], true
}
func (b *loweringBuilder) fail(s string) {
	if b.err == nil {
		b.err = fmt.Errorf("wago: %s", s)
	}
}
func (b *loweringBuilder) Input(i int) LowerValue {
	if i < 0 || i >= len(b.inputWidths) {
		b.fail(fmt.Sprintf("lowering input %d out of range", i))
		return LowerValue{id: -1}
	}
	return b.add(instructionNode{op: InstructionInput, width: b.inputWidths[i], input: i})
}
func (b *loweringBuilder) Const(width int32, raw ...byte) LowerValue {
	v, err := NewBits(width, raw)
	if err != nil {
		b.fail(err.Error())
		return LowerValue{id: -1}
	}
	return b.add(instructionNode{op: InstructionConst, width: width, constant: v})
}
func (b *loweringBuilder) binary(op InstructionOp, a, c LowerValue) LowerValue {
	an, ok1 := b.value(a)
	cn, ok2 := b.value(c)
	if !ok1 || !ok2 || an.width != cn.width {
		b.fail("lowering binary operands must have the same width")
		return LowerValue{id: -1}
	}
	return b.add(instructionNode{op: op, width: an.width, a: a.id, b: c.id})
}
func (b *loweringBuilder) Add(a, c LowerValue) LowerValue { return b.binary(InstructionAdd, a, c) }
func (b *loweringBuilder) Sub(a, c LowerValue) LowerValue { return b.binary(InstructionSub, a, c) }
func (b *loweringBuilder) Mul(a, c LowerValue) LowerValue { return b.binary(InstructionMul, a, c) }
func (b *loweringBuilder) And(a, c LowerValue) LowerValue { return b.binary(InstructionAnd, a, c) }
func (b *loweringBuilder) Or(a, c LowerValue) LowerValue  { return b.binary(InstructionOr, a, c) }
func (b *loweringBuilder) Xor(a, c LowerValue) LowerValue { return b.binary(InstructionXor, a, c) }
func (b *loweringBuilder) Not(v LowerValue) LowerValue {
	n, ok := b.value(v)
	if !ok {
		return LowerValue{id: -1}
	}
	return b.add(instructionNode{op: InstructionNot, width: n.width, a: v.id})
}
func (b *loweringBuilder) Shl(v, c LowerValue) LowerValue  { return b.shift(InstructionShl, v, c) }
func (b *loweringBuilder) ShrU(v, c LowerValue) LowerValue { return b.shift(InstructionShrU, v, c) }
func (b *loweringBuilder) ShrS(v, c LowerValue) LowerValue { return b.shift(InstructionShrS, v, c) }
func (b *loweringBuilder) shift(op InstructionOp, v, c LowerValue) LowerValue {
	vn, ok1 := b.value(v)
	_, ok2 := b.value(c)
	if !ok1 || !ok2 {
		return LowerValue{id: -1}
	}
	return b.add(instructionNode{op: op, width: vn.width, a: v.id, b: c.id})
}
func (b *loweringBuilder) compare(op InstructionOp, a, c LowerValue) LowerValue {
	an, ok1 := b.value(a)
	cn, ok2 := b.value(c)
	if !ok1 || !ok2 || an.width != cn.width {
		b.fail("lowering comparison operands must have the same width")
		return LowerValue{id: -1}
	}
	return b.add(instructionNode{op: op, width: 1, a: a.id, b: c.id})
}
func (b *loweringBuilder) Eq(a, c LowerValue) LowerValue  { return b.compare(InstructionEq, a, c) }
func (b *loweringBuilder) Ne(a, c LowerValue) LowerValue  { return b.compare(InstructionNe, a, c) }
func (b *loweringBuilder) LtU(a, c LowerValue) LowerValue { return b.compare(InstructionLtU, a, c) }
func (b *loweringBuilder) LtS(a, c LowerValue) LowerValue { return b.compare(InstructionLtS, a, c) }
func (b *loweringBuilder) LeU(a, c LowerValue) LowerValue { return b.compare(InstructionLeU, a, c) }
func (b *loweringBuilder) LeS(a, c LowerValue) LowerValue { return b.compare(InstructionLeS, a, c) }
func (b *loweringBuilder) GtU(a, c LowerValue) LowerValue { return b.compare(InstructionGtU, a, c) }
func (b *loweringBuilder) GtS(a, c LowerValue) LowerValue { return b.compare(InstructionGtS, a, c) }
func (b *loweringBuilder) GeU(a, c LowerValue) LowerValue { return b.compare(InstructionGeU, a, c) }
func (b *loweringBuilder) GeS(a, c LowerValue) LowerValue { return b.compare(InstructionGeS, a, c) }
func (b *loweringBuilder) IsZero(v LowerValue) LowerValue {
	if _, ok := b.value(v); !ok {
		return LowerValue{id: -1}
	}
	return b.add(instructionNode{op: InstructionIsZero, width: 1, a: v.id})
}
func (b *loweringBuilder) Select(ifTrue, ifFalse, condition LowerValue) LowerValue {
	tn, ok1 := b.value(ifTrue)
	fn, ok2 := b.value(ifFalse)
	_, ok3 := b.value(condition)
	if !ok1 || !ok2 || !ok3 || tn.width != fn.width {
		b.fail("lowering select branches must have the same width")
		return LowerValue{id: -1}
	}
	return b.add(instructionNode{op: InstructionSelect, width: tn.width, a: ifTrue.id, b: ifFalse.id, c: condition.id})
}
func (b *loweringBuilder) Output(i int, v LowerValue) {
	n, ok := b.value(v)
	if !ok {
		return
	}
	if i < 0 || i >= len(b.outputWidths) {
		b.fail(fmt.Sprintf("lowering output %d out of range", i))
		return
	}
	if n.width != b.outputWidths[i] {
		b.fail(fmt.Sprintf("lowering output %d has width %d, want %d", i, n.width, b.outputWidths[i]))
		return
	}
	b.recipe.outputs[i] = v.id
}
func (b *loweringBuilder) add(n instructionNode) LowerValue {
	b.recipe.nodes = append(b.recipe.nodes, n)
	return LowerValue{id: len(b.recipe.nodes) - 1}
}

func buildInstructionRecipe(spec InstructionSpec) (*instructionRecipe, error) {
	if spec.Lower == nil {
		return nil, nil
	}
	b := &loweringBuilder{inputWidths: spec.Input, outputWidths: spec.Output, recipe: instructionRecipe{outputs: make([]int, len(spec.Output))}}
	for i := range b.recipe.outputs {
		b.recipe.outputs[i] = -1
	}
	if err := spec.Lower(b); err != nil {
		return nil, err
	}
	if b.err != nil {
		return nil, b.err
	}
	for i, v := range b.recipe.outputs {
		if v < 0 {
			return nil, fmt.Errorf("wago: lowering did not set output %d", i)
		}
	}
	return &b.recipe, nil
}

// InstructionNode is one node in a backend-neutral scalar lowering DAG.
type InstructionNode struct {
	Op      InstructionOp
	Width   int32
	A, B, C int
	Input   int
	Const   uint32
}

// Instruction is the canonical lowering model consumed by compiler backends.
// Unsupported lowerings are simply omitted so the ordinary portable host call
// remains available.
type Instruction struct {
	Nodes           []InstructionNode
	Output          int
	StackCompatible bool
	AMD64           *machinecode.AMD64Lowering
	SIMD            *SIMDInstruction
	InputWidths     []int32
	ResultWidth     int32
}

// Definition is a validated plugin instruction. It owns detached copies of the
// caller-provided widths and machine-specific lowering declarations.
type Definition struct {
	Spec   InstructionSpec
	recipe *instructionRecipe
	amd64  *machinecode.AMD64Lowering
	simd   *SIMDInstruction
}

// Native selects the allocation-free subset compiler backends currently
// support. False means the exact same Wasm import transparently retains its
// portable Handler.
func (d Definition) Native() (Instruction, bool) {
	if d.simd != nil {
		copy := *d.simd
		return Instruction{
			SIMD:        &copy,
			InputWidths: append([]int32(nil), d.Spec.Input...),
		}, true
	}
	if d.amd64 != nil {
		copy := *d.amd64
		var resultWidth int32
		if len(d.Spec.Output) == 1 {
			resultWidth = d.Spec.Output[0]
		}
		return Instruction{AMD64: &copy, InputWidths: append([]int32(nil), d.Spec.Input...), ResultWidth: resultWidth}, true
	}
	r := d.recipe
	if r == nil || len(d.Spec.Output) != 1 || d.Spec.Output[0] > 32 {
		return Instruction{}, false
	}
	for _, w := range d.Spec.Input {
		if w > 32 {
			return Instruction{}, false
		}
	}
	nodes := make([]InstructionNode, len(r.nodes))
	for i, n := range r.nodes {
		if n.width > 32 {
			return Instruction{}, false
		}
		if n.op == InstructionInput {
			if n.input < 0 || n.input >= len(d.Spec.Input) {
				return Instruction{}, false
			}
		}
		nodes[i] = InstructionNode{Op: n.op, Width: n.width, A: n.a, B: n.b, C: n.c, Input: n.input, Const: n.constant.Uint32()}
	}
	out := r.outputs[0]
	if out < 0 || out >= len(r.nodes) {
		return Instruction{}, false
	}
	return Instruction{Nodes: nodes, Output: out, ResultWidth: d.Spec.Output[0], StackCompatible: stackCompatibleInstructionRecipe(r, len(d.Spec.Input))}, true
}

// stackCompatibleInstructionRecipe recognizes the zero-copy subset whose input
// nodes already occur in physical Wasm operand-stack order. More general DAGs
// remain native but snapshot their arguments into compiler spill slots first.
func stackCompatibleInstructionRecipe(r *instructionRecipe, inputs int) bool {
	inputNodes := make([]int, inputs)
	for i := range inputNodes {
		inputNodes[i] = -1
	}
	for id, n := range r.nodes {
		switch n.op {
		case InstructionInput:
			if n.input < 0 || n.input >= inputs || inputNodes[n.input] >= 0 {
				return false
			}
			inputNodes[n.input] = id
		case InstructionConst, InstructionAdd, InstructionSub, InstructionMul,
			InstructionAnd, InstructionOr, InstructionXor, InstructionNot:
		default:
			return false
		}
	}
	for _, id := range inputNodes {
		if id < 0 {
			return false
		}
	}
	stack := append([]int(nil), inputNodes...)
	var walk func(int) bool
	walk = func(id int) bool {
		if id < 0 || id >= len(r.nodes) {
			return false
		}
		n := r.nodes[id]
		switch n.op {
		case InstructionInput:
			return true
		case InstructionConst:
			stack = append(stack, id)
			return true
		case InstructionNot:
			if !walk(n.a) || len(stack) < 1 || stack[len(stack)-1] != n.a {
				return false
			}
			stack[len(stack)-1] = id
			return true
		default:
			if !walk(n.a) || !walk(n.b) || len(stack) < 2 || stack[len(stack)-2] != n.a || stack[len(stack)-1] != n.b {
				return false
			}
			stack = append(stack[:len(stack)-2], id)
			return true
		}
	}
	out := r.outputs[0]
	return walk(out) && len(stack) == 1 && stack[0] == out
}

// Prepare validates and snapshots a plugin instruction once at registration.
// Runtime and compiler callers then consume the same Definition without
// repeating policy or backend-capability checks.
func Prepare(spec InstructionSpec) (Definition, error) {
	if spec.Module == "" || spec.Name == "" {
		return Definition{}, fmt.Errorf("wago: instruction requires Module and Name")
	}
	if spec.Handler == nil {
		return Definition{}, fmt.Errorf("wago: instruction %q.%q requires Handler", spec.Module, spec.Name)
	}
	for _, widths := range [][]int32{spec.Input, spec.Output} {
		for _, w := range widths {
			if w <= 0 {
				return Definition{}, fmt.Errorf("wago: instruction %q.%q has non-positive width %d", spec.Module, spec.Name, w)
			}
		}
	}
	recipe, err := buildInstructionRecipe(spec)
	if err != nil {
		return Definition{}, fmt.Errorf("wago: instruction %q.%q lowering: %w", spec.Module, spec.Name, err)
	}
	var amd64Lowering *machinecode.AMD64Lowering
	var simdLowering *SIMDInstruction
	if spec.SIMD != nil {
		if spec.AMD64 != nil || spec.Lower != nil {
			return Definition{}, fmt.Errorf("wago: instruction %q.%q SIMD lowering cannot be combined with Lower or AMD64", spec.Module, spec.Name)
		}
		if spec.SIMD.Width < 256 || spec.SIMD.Width%256 != 0 {
			return Definition{}, fmt.Errorf("wago: instruction %q.%q SIMD width %d must be a positive multiple of 256", spec.Module, spec.Name, spec.SIMD.Width)
		}
		if spec.SIMD.Arity < 1 || spec.SIMD.Arity > 3 {
			return Definition{}, fmt.Errorf("wago: instruction %q.%q SIMD arity %d must be in [1,3]", spec.Module, spec.Name, spec.SIMD.Arity)
		}
		if len(spec.Output) != 0 || len(spec.Input) != int(spec.SIMD.Arity)+1 {
			return Definition{}, fmt.Errorf("wago: instruction %q.%q SIMD signature must be (destination, %d input pointer(s)) -> ()", spec.Module, spec.Name, spec.SIMD.Arity)
		}
		for _, width := range spec.Input {
			if width != 32 {
				return Definition{}, fmt.Errorf("wago: instruction %q.%q SIMD pointer widths must all be 32 bits", spec.Module, spec.Name)
			}
		}
		copy := *spec.SIMD
		simdLowering = &copy
	}
	if spec.AMD64 != nil {
		switch spec.AMD64.Compatibility {
		case machinecode.AMD64CompatibilityManaged:
			if spec.AMD64.Managed == nil || spec.AMD64.Emit != nil {
				return Definition{}, fmt.Errorf("wago: instruction %q.%q managed amd64 lowering requires Managed and forbids Emit", spec.Module, spec.Name)
			}
		case machinecode.AMD64CompatibilityFullAccess:
			if spec.AMD64.Emit == nil || spec.AMD64.Managed != nil {
				return Definition{}, fmt.Errorf("wago: instruction %q.%q full-access amd64 lowering requires Emit and forbids Managed", spec.Module, spec.Name)
			}
		default:
			return Definition{}, fmt.Errorf("wago: instruction %q.%q requires an explicit amd64 compatibility mode", spec.Module, spec.Name)
		}
		if spec.AMD64.Features & ^(machinecode.AMD64FeatureAVX2|machinecode.AMD64FeatureAVX512) != 0 {
			return Definition{}, fmt.Errorf("wago: instruction %q.%q declares unsupported amd64 features %#x", spec.Module, spec.Name, spec.AMD64.Features)
		}
		if len(spec.Output) > 1 {
			return Definition{}, fmt.Errorf("wago: instruction %q.%q amd64 lowering supports at most one direct output", spec.Module, spec.Name)
		}
		for _, width := range append(append([]int32(nil), spec.Input...), spec.Output...) {
			if width > 32 {
				return Definition{}, fmt.Errorf("wago: instruction %q.%q amd64 lowering only supports direct values up to 32 bits", spec.Module, spec.Name)
			}
		}
		copy := *spec.AMD64
		amd64Lowering = &copy
	}
	spec.Input = append([]int32(nil), spec.Input...)
	spec.Output = append([]int32(nil), spec.Output...)
	spec.AMD64 = amd64Lowering
	spec.SIMD = simdLowering
	return Definition{Spec: spec, recipe: recipe, amd64: amd64Lowering, simd: simdLowering}, nil
}

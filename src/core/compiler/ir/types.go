package ir

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Compact identifiers used throughout the IR. The zero value is a valid ID;
// invalid references are represented by the corresponding Invalid* constant.
type ValueID uint32
type BlockID uint32
type InstID uint32

const (
	InvalidValue ValueID = ^ValueID(0)
	InvalidBlock BlockID = ^BlockID(0)
	InvalidInst  InstID  = ^InstID(0)
)

// Range indexes a contiguous sub-slice in one of Func's shared pools.
type Range struct {
	Start uint32
	Len   uint32
}

func (r Range) End() uint32 { return r.Start + r.Len }
func (r Range) Empty() bool { return r.Len == 0 }

// Module is the high-level IR view of a WebAssembly module. It intentionally
// mirrors only metadata needed by later optimization/codegen work.
type Module struct {
	Types             []wasm.FuncType
	ImportedFuncCount uint32
	FuncTypes         []uint32 // all functions, imported first, local after imports
	Globals           []wasm.GlobalType
	Memories          []wasm.MemType
	Tables            []wasm.TableType
	Elements          []ElementMeta
	Data              []DataMeta
	Funcs             []Func // local functions only, in module-local order
}

type ElementMeta struct {
	TableIdx uint32
	ElemType wasm.ValType
	Passive  bool
	Declared bool
	Len      uint32
}

type DataMeta struct {
	MemIdx  uint32
	Passive bool
	Len     uint32
}

// Func is a compact block-parameter value IR. It uses SSA values for the
// operand stack and block parameters, but WebAssembly locals intentionally remain
// explicit stateful OpLocalGet/OpLocalSet/OpLocalTee instructions for this
// high-level lowering stage. A later local-SSA pass can remove those effects;
// until then, optimizers/codegen must treat local read/write effects as ordering
// constraints.
//
// Per-block, per-instruction, and per-edge variable-length operands live in
// shared contiguous pools.
type Func struct {
	Index      uint32 // absolute wasm function index
	LocalIndex uint32 // index in Module.Code/Functions
	TypeIndex  uint32
	Sig        wasm.FuncType
	Locals     []wasm.ValType // params followed by declared locals

	Entry  BlockID
	Blocks []Block
	Insts  []Inst
	Values []Value

	ValueIDs []ValueID
	Edges    []Edge
}

type ValueDefKind uint8

const (
	ValueDefInvalid ValueDefKind = iota
	ValueDefBlockParam
	ValueDefInst
	ValueDefPoison // stack-polymorphic value in unreachable code; must not be used by reachable IR
)

type Value struct {
	Type    wasm.ValType
	DefKind ValueDefKind
	Def     uint32 // block id or instruction id depending on DefKind
}

type Block struct {
	Params Range
	Insts  Range
	Term   Term
}

type Inst struct {
	Op      Op
	Args    Range
	Results Range
	Aux     uint64
	Aux2    uint64
	Effects EffectFlags
}

type Edge struct {
	To   BlockID
	Args Range
}

type TermKind uint8

const (
	TermInvalid TermKind = iota
	TermBr
	TermCondBr
	TermSwitch
	TermReturn
	TermTrap
)

type Term struct {
	Kind  TermKind
	Cond  ValueID // TermCondBr
	Index ValueID // TermSwitch selector
	Edges Range   // branch-like terminators
	Args  Range   // TermReturn values
}

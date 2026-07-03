// Package wasm contains a structured WebAssembly binary decoder and validator
// for post-MVP proposals including reference types, exception handling tags,
// typed function references, GC, stringrefs, SIMD, atomics, bulk memory, and
// memory64 encodings.
//
// It is the single wasm frontend used by validation, public metadata, and the
// current JIT/backend support boundary.
package wasm

import "fmt"

type NumType byte

const (
	NumI32 NumType = 0x7F
	NumI64 NumType = 0x7E
	NumF32 NumType = 0x7D
	NumF64 NumType = 0x7C
)

type AbsHeapType byte

const (
	HeapString   AbsHeapType = 0x64
	HeapExn      AbsHeapType = 0x69
	HeapArray    AbsHeapType = 0x6A
	HeapStruct   AbsHeapType = 0x6B
	HeapI31      AbsHeapType = 0x6C
	HeapEq       AbsHeapType = 0x6D
	HeapAny      AbsHeapType = 0x6E
	HeapExtern   AbsHeapType = 0x6F
	HeapFunc     AbsHeapType = 0x70
	HeapNone     AbsHeapType = 0x71
	HeapNoExtern AbsHeapType = 0x72
	HeapNoFunc   AbsHeapType = 0x73
	HeapNoExn    AbsHeapType = 0x74
)

type HeapTypeKind uint8

const (
	HeapAbs HeapTypeKind = iota
	HeapTypeIndex
	HeapDefType
)

type HeapType struct {
	Kind HeapTypeKind
	Abs  AbsHeapType
	Type TypeIdx
	Def  *DefType
}

func AbsHeap(abs AbsHeapType) HeapType { return HeapType{Kind: HeapAbs, Abs: abs} }
func IndexedHeap(idx TypeIdx) HeapType { return HeapType{Kind: HeapTypeIndex, Type: idx} }

type RefType struct {
	Nullable bool
	Exact    bool
	Heap     HeapType
	// Bare is true for one-byte abstract aliases such as funcref/externref.
	Bare bool
}

func AbsRef(abs AbsHeapType) RefType { return RefType{Nullable: true, Heap: AbsHeap(abs), Bare: true} }
func Ref(nullable bool, heap HeapType, exact bool) RefType {
	return RefType{Nullable: nullable, Heap: heap, Exact: exact}
}

func (rt RefType) IsDefaultable() bool { return rt.Nullable }

func (rt RefType) String() string {
	prefix := "ref"
	if rt.Nullable {
		prefix = "ref null"
	}
	if rt.Exact {
		prefix += " exact"
	}
	return prefix + " " + rt.Heap.String()
}

func (h HeapType) String() string {
	switch h.Kind {
	case HeapAbs:
		return h.Abs.String()
	case HeapTypeIndex:
		return fmt.Sprintf("type %d", h.Type.Index)
	case HeapDefType:
		if h.Def == nil {
			return "def ?"
		}
		return fmt.Sprintf("def %d.%d", h.Def.GroupIndex, h.Def.Index)
	default:
		return "heap?"
	}
}

func (a AbsHeapType) String() string {
	switch a {
	case HeapString:
		return "string"
	case HeapExn:
		return "exn"
	case HeapArray:
		return "array"
	case HeapStruct:
		return "struct"
	case HeapI31:
		return "i31"
	case HeapEq:
		return "eq"
	case HeapAny:
		return "any"
	case HeapExtern:
		return "extern"
	case HeapFunc:
		return "func"
	case HeapNone:
		return "none"
	case HeapNoExtern:
		return "noextern"
	case HeapNoFunc:
		return "nofunc"
	case HeapNoExn:
		return "noexn"
	default:
		return "heap?"
	}
}

type ValTypeKind uint8

const (
	ValNum ValTypeKind = iota
	ValVec
	ValRef
	ValBot
)

type ValType struct {
	Kind ValTypeKind
	Num  NumType
	Ref  RefType
}

var (
	I32       = ValType{Kind: ValNum, Num: NumI32}
	I64       = ValType{Kind: ValNum, Num: NumI64}
	F32       = ValType{Kind: ValNum, Num: NumF32}
	F64       = ValType{Kind: ValNum, Num: NumF64}
	V128      = ValType{Kind: ValVec}
	Bot       = ValType{Kind: ValBot}
	FuncRef   = ValType{Kind: ValRef, Ref: AbsRef(HeapFunc)}
	ExternRef = ValType{Kind: ValRef, Ref: AbsRef(HeapExtern)}
	AnyRef    = ValType{Kind: ValRef, Ref: AbsRef(HeapAny)}
	EqRef     = ValType{Kind: ValRef, Ref: AbsRef(HeapEq)}
	I31Ref    = ValType{Kind: ValRef, Ref: AbsRef(HeapI31)}
	StringRef = ValType{Kind: ValRef, Ref: AbsRef(HeapString)}
)

func RefVal(rt RefType) ValType { return ValType{Kind: ValRef, Ref: rt} }

func (v ValType) String() string {
	switch v.Kind {
	case ValNum:
		switch v.Num {
		case NumI32:
			return "i32"
		case NumI64:
			return "i64"
		case NumF32:
			return "f32"
		case NumF64:
			return "f64"
		}
	case ValVec:
		return "v128"
	case ValRef:
		if v.Ref.Bare && v.Ref.Nullable && !v.Ref.Exact && v.Ref.Heap.Kind == HeapAbs {
			switch v.Ref.Heap.Abs {
			case HeapFunc:
				return "funcref"
			case HeapExtern:
				return "externref"
			}
		}
		return v.Ref.String()
	case ValBot:
		return "⊥"
	}
	return "val?"
}

type Mut byte

const (
	Const Mut = 0
	Var   Mut = 1
)

type PackType byte

const (
	PackI16 PackType = 0x77
	PackI8  PackType = 0x78
)

type StorageType struct {
	Packed bool
	Val    ValType
	Pack   PackType
}

type FieldType struct {
	Storage StorageType
	Mut     Mut
}

type TypeIdx struct {
	Index uint32
	Rec   bool
}
type FuncIdx uint32
type TableIdx uint32
type MemIdx uint32
type GlobalIdx uint32
type TagIdx uint32
type ElemIdx uint32
type DataIdx uint32
type LocalIdx uint32
type LabelIdx uint32
type LaneIdx byte

type CompTypeKind uint8

const (
	CompArray CompTypeKind = iota
	CompStruct
	CompFunc
)

type CompType struct {
	Kind    CompTypeKind
	Array   FieldType
	Fields  []FieldType
	Params  []ValType
	Results []ValType
}

type TypeMetadata struct {
	Describes  *TypeIdx
	Descriptor *TypeIdx
}

type SubType struct {
	Final     bool
	Supers    []TypeIdx
	Metadata  TypeMetadata
	Comp      CompType
	HasPrefix bool // false for the compact CompTypeSubType form.
}

type RecType struct{ SubTypes []SubType }

type DefType struct {
	Rec        RecType
	GroupIndex uint32
	Index      uint32
}

type Limits struct {
	Min    uint64
	Max    *uint64
	Addr64 bool
}

type TagType struct{ Type TypeIdx }
type GlobalType struct {
	Type    ValType
	Mutable bool
}
type MemType struct {
	Limits Limits
	Shared bool
}
type TableType struct {
	Ref    RefType
	Limits Limits
}

type ExternKind byte

const (
	ExternFunc ExternKind = iota
	ExternTable
	ExternMem
	ExternGlobal
	ExternTag
)

type ExternType struct {
	Kind   ExternKind
	Type   TypeIdx
	Table  TableType
	Mem    MemType
	Global GlobalType
	Tag    TagType
}
type ExternIdx struct {
	Kind  ExternKind
	Index uint32
}

type ElemModeKind uint8

const (
	ElemPassive ElemModeKind = iota
	ElemActive
	ElemDeclarative
)

type ElemMode struct {
	Kind   ElemModeKind
	Table  TableIdx
	Offset Expr
}

type ElemKindKind uint8

const (
	ElemFuncs ElemKindKind = iota
	ElemFuncExprs
	ElemTypedExprs
)

type ElemKind struct {
	Kind  ElemKindKind
	Funcs []FuncIdx
	Ref   RefType
	Exprs []Expr
}
type Elem struct {
	Mode ElemMode
	Kind ElemKind
}

// Expr is a WebAssembly expression. BodyBytes, when non-nil, is the original
// expression bytecode including its terminating end opcode. DecodeModule stores
// const expressions in BodyBytes and stores function bodies in Func.BodyBytes;
// the Instrs field remains only for programmatically constructed modules and
// encoder/validator unit tests.
type Expr struct {
	Instrs    []Instruction
	BodyBytes []byte
}

type Import struct {
	Module string
	Name   string
	Type   ExternType
}
type Table struct {
	Type TableType
	Init *Expr
}
type Global struct {
	Type GlobalType
	Init Expr
}
type Export struct {
	Name  string
	Index ExternIdx
}
type FuncType struct{ Params, Results []ValType }

type LocalRun struct {
	Count uint32
	Type  ValType
}

type LocalEntry = LocalRun

type Locals struct{ Runs []LocalRun }

type DataModeKind uint8

const (
	DataActive DataModeKind = iota
	DataPassive
)

type DataMode struct {
	Kind   DataModeKind
	Mem    MemIdx
	Offset Expr
}
type Data struct {
	Mode DataMode
	Init []byte
}

type Func struct {
	Locals Locals
	Body   Expr
	// BodyBytes is the original expression bytecode, including the terminating
	// end opcode and excluding local declarations. DecodeModule always populates
	// this field for local function bodies.
	BodyBytes []byte
}
type CustomSec struct {
	Name string
	Data []byte
}

type NameAssoc struct {
	Index uint32
	Name  string
}
type NameMap []NameAssoc
type IndirectNameAssoc struct {
	Index uint32
	Names NameMap
}
type IndirectNameMap []IndirectNameAssoc
type NameSec struct {
	ModuleName    *string
	FunctionNames NameMap
	LocalNames    IndirectNameMap
	LabelNames    IndirectNameMap
	TypeNames     NameMap
	TableNames    NameMap
	MemoryNames   NameMap
	GlobalNames   NameMap
	ElementNames  NameMap
	DataNames     NameMap
	FieldNames    IndirectNameMap
	TagNames      NameMap
}

type Module struct {
	Customs           []CustomSec
	NameSec           *NameSec
	RawNameSecPayload []byte
	Types             []RecType
	Imports           []Import
	FuncTypes         []TypeIdx
	Tables            []Table
	Memories          []MemType
	Tags              []TagType
	StringRefs        [][]byte
	Globals           []Global
	Exports           []Export
	Start             *FuncIdx
	Elements          []Elem
	DataCount         *uint32
	Code              []Func
	Data              []Data
}

func (m *Module) ImportedFuncCount() int   { return m.importCount(ExternFunc) }
func (m *Module) ImportedTableCount() int  { return m.importCount(ExternTable) }
func (m *Module) ImportedMemCount() int    { return m.importCount(ExternMem) }
func (m *Module) ImportedGlobalCount() int { return m.importCount(ExternGlobal) }
func (m *Module) ImportedTagCount() int    { return m.importCount(ExternTag) }
func (m *Module) importCount(k ExternKind) int {
	n := 0
	for _, im := range m.Imports {
		if im.Type.Kind == k {
			n++
		}
	}
	return n
}
func (m *Module) FuncCount() int   { return m.ImportedFuncCount() + len(m.FuncTypes) }
func (m *Module) TableCount() int  { return m.ImportedTableCount() + len(m.Tables) }
func (m *Module) MemCount() int    { return m.ImportedMemCount() + len(m.Memories) }
func (m *Module) GlobalCount() int { return m.ImportedGlobalCount() + len(m.Globals) }
func (m *Module) TagCount() int    { return m.ImportedTagCount() + len(m.Tags) }

type MemArg struct {
	Align  uint32
	Mem    *MemIdx
	Offset uint64
}
type AtomicOrder byte

const (
	SeqCst AtomicOrder = 0
	AcqRel AtomicOrder = 1
)

type BlockTypeKind uint8

const (
	BlockVoid BlockTypeKind = iota
	BlockVal
	BlockTypeIndex
)

type BlockType struct {
	Kind BlockTypeKind
	Val  ValType
	Type TypeIdx
}

type CatchKind uint8

const (
	CatchTag CatchKind = iota
	CatchRef
	CatchAll
	CatchAllRef
)

type Catch struct {
	Kind  CatchKind
	Tag   TagIdx
	Label LabelIdx
}
type CastOp struct {
	SourceNullable bool
	TargetNullable bool
}

type InstrKind uint16

const (
	InstrInvalid InstrKind = iota
	InstrUnreachable
	InstrNop
	InstrBlock
	InstrLoop
	InstrIf
	InstrThrow
	InstrThrowRef
	InstrBr
	InstrBrIf
	InstrBrTable
	InstrReturn
	InstrCall
	InstrCallIndirect
	InstrReturnCall
	InstrReturnCallIndirect
	InstrCallRef
	InstrReturnCallRef
	InstrDrop
	InstrSelect
	InstrTryTable
	InstrLocalGet
	InstrLocalSet
	InstrLocalTee
	InstrGlobalGet
	InstrGlobalSet
	InstrTableGet
	InstrTableSet
	InstrI32Load
	InstrI64Load
	InstrF32Load
	InstrF64Load
	InstrI32Load8S
	InstrI32Load8U
	InstrI32Load16S
	InstrI32Load16U
	InstrI64Load8S
	InstrI64Load8U
	InstrI64Load16S
	InstrI64Load16U
	InstrI64Load32S
	InstrI64Load32U
	InstrI32Store
	InstrI64Store
	InstrF32Store
	InstrF64Store
	InstrI32Store8
	InstrI32Store16
	InstrI64Store8
	InstrI64Store16
	InstrI64Store32
	InstrMemorySize
	InstrMemoryGrow
	InstrMemoryAtomicNotify
	InstrMemoryAtomicWait32
	InstrMemoryAtomicWait64
	InstrAtomicFence
	InstrI32AtomicLoad
	InstrI64AtomicLoad
	InstrI32AtomicLoad8U
	InstrI32AtomicLoad16U
	InstrI64AtomicLoad8U
	InstrI64AtomicLoad16U
	InstrI64AtomicLoad32U
	InstrI32AtomicStore
	InstrI64AtomicStore
	InstrI32AtomicStore8
	InstrI32AtomicStore16
	InstrI64AtomicStore8
	InstrI64AtomicStore16
	InstrI64AtomicStore32
	InstrAtomicRmw
	InstrAtomicCmpxchg
	InstrI32Const
	InstrI64Const
	InstrF32Const
	InstrF64Const
	InstrI32Eqz
	InstrI32Eq
	InstrI32Ne
	InstrI32LtS
	InstrI32LtU
	InstrI32GtS
	InstrI32GtU
	InstrI32LeS
	InstrI32LeU
	InstrI32GeS
	InstrI32GeU
	InstrI64Eqz
	InstrI64Eq
	InstrI64Ne
	InstrI64LtS
	InstrI64LtU
	InstrI64GtS
	InstrI64GtU
	InstrI64LeS
	InstrI64LeU
	InstrI64GeS
	InstrI64GeU
	InstrF32Eq
	InstrF32Ne
	InstrF32Lt
	InstrF32Gt
	InstrF32Le
	InstrF32Ge
	InstrF64Eq
	InstrF64Ne
	InstrF64Lt
	InstrF64Gt
	InstrF64Le
	InstrF64Ge
	InstrI32Clz
	InstrI32Ctz
	InstrI32Popcnt
	InstrI32Add
	InstrI32Sub
	InstrI32Mul
	InstrI32DivS
	InstrI32DivU
	InstrI32RemS
	InstrI32RemU
	InstrI32And
	InstrI32Or
	InstrI32Xor
	InstrI32Shl
	InstrI32ShrS
	InstrI32ShrU
	InstrI32Rotl
	InstrI32Rotr
	InstrI64Clz
	InstrI64Ctz
	InstrI64Popcnt
	InstrI64Add
	InstrI64Sub
	InstrI64Mul
	InstrI64DivS
	InstrI64DivU
	InstrI64RemS
	InstrI64RemU
	InstrI64And
	InstrI64Or
	InstrI64Xor
	InstrI64Shl
	InstrI64ShrS
	InstrI64ShrU
	InstrI64Rotl
	InstrI64Rotr
	InstrF32Abs
	InstrF32Neg
	InstrF32Ceil
	InstrF32Floor
	InstrF32Trunc
	InstrF32Nearest
	InstrF32Sqrt
	InstrF32Add
	InstrF32Sub
	InstrF32Mul
	InstrF32Div
	InstrF32Min
	InstrF32Max
	InstrF32Copysign
	InstrF64Abs
	InstrF64Neg
	InstrF64Ceil
	InstrF64Floor
	InstrF64Trunc
	InstrF64Nearest
	InstrF64Sqrt
	InstrF64Add
	InstrF64Sub
	InstrF64Mul
	InstrF64Div
	InstrF64Min
	InstrF64Max
	InstrF64Copysign
	InstrI32WrapI64
	InstrI32TruncF32S
	InstrI32TruncF32U
	InstrI32TruncF64S
	InstrI32TruncF64U
	InstrI64ExtendI32S
	InstrI64ExtendI32U
	InstrI64TruncF32S
	InstrI64TruncF32U
	InstrI64TruncF64S
	InstrI64TruncF64U
	InstrF32ConvertI32S
	InstrF32ConvertI32U
	InstrF32ConvertI64S
	InstrF32ConvertI64U
	InstrF32DemoteF64
	InstrF64ConvertI32S
	InstrF64ConvertI32U
	InstrF64ConvertI64S
	InstrF64ConvertI64U
	InstrF64PromoteF32
	InstrI32ReinterpretF32
	InstrI64ReinterpretF64
	InstrF32ReinterpretI32
	InstrF64ReinterpretI64
	InstrI32Extend8S
	InstrI32Extend16S
	InstrI64Extend8S
	InstrI64Extend16S
	InstrI64Extend32S
	InstrRefNull
	InstrRefIsNull
	InstrRefFunc
	InstrRefEq
	InstrRefAsNonNull
	InstrStringConst
	InstrBrOnNull
	InstrBrOnNonNull
	InstrStringNewUtf8Array
	InstrStringNewWtf16Array
	InstrStringEncodeUtf8Array
	InstrStringEncodeWtf16Array
	InstrStringNewLossyUtf8Array
	InstrStringNewWtf8Array
	InstrStringEncodeLossyUtf8Array
	InstrStringEncodeWtf8Array
	InstrStructNew
	InstrStructNewDefault
	InstrStructNewDesc
	InstrStructNewDefaultDesc
	InstrStructGet
	InstrStructGetS
	InstrStructGetU
	InstrStructAtomicGet
	InstrStructAtomicGetS
	InstrStructAtomicGetU
	InstrStructSet
	InstrArrayNew
	InstrArrayNewDefault
	InstrArrayNewFixed
	InstrArrayNewData
	InstrArrayNewElem
	InstrArrayGet
	InstrArrayGetS
	InstrArrayGetU
	InstrArraySet
	InstrArrayLen
	InstrArrayFill
	InstrArrayCopy
	InstrArrayInitData
	InstrArrayInitElem
	InstrRefGetDesc
	InstrRefTest
	InstrRefCast
	InstrRefTestDesc
	InstrRefCastDescEq
	InstrBrOnCast
	InstrBrOnCastFail
	InstrAnyConvertExtern
	InstrExternConvertAny
	InstrRefI31
	InstrI31GetS
	InstrI31GetU
	InstrI32TruncSatF32S
	InstrI32TruncSatF32U
	InstrI32TruncSatF64S
	InstrI32TruncSatF64U
	InstrI64TruncSatF32S
	InstrI64TruncSatF32U
	InstrI64TruncSatF64S
	InstrI64TruncSatF64U
	InstrMemoryInit
	InstrDataDrop
	InstrMemoryCopy
	InstrMemoryFill
	InstrTableInit
	InstrElemDrop
	InstrTableCopy
	InstrTableGrow
	InstrTableSize
	InstrTableFill
	InstrV128Load
	InstrV128Load8x8S
	InstrV128Load8x8U
	InstrV128Load16x4S
	InstrV128Load16x4U
	InstrV128Load32x2S
	InstrV128Load32x2U
	InstrV128Load8Splat
	InstrV128Load16Splat
	InstrV128Load32Splat
	InstrV128Load64Splat
	InstrV128Store
	InstrV128Const
	InstrByte
	InstrI8x16Shuffle
	InstrLaneIdx
	InstrI8x16Swizzle
	InstrI8x16Splat
	InstrI16x8Splat
	InstrI32x4Splat
	InstrI64x2Splat
	InstrF32x4Splat
	InstrF64x2Splat
	InstrI8x16ExtractLaneS
	InstrI8x16ExtractLaneU
	InstrI8x16ReplaceLane
	InstrI16x8ExtractLaneS
	InstrI16x8ExtractLaneU
	InstrI16x8ReplaceLane
	InstrI32x4ExtractLane
	InstrI32x4ReplaceLane
	InstrI64x2ExtractLane
	InstrI64x2ReplaceLane
	InstrF32x4ExtractLane
	InstrF32x4ReplaceLane
	InstrF64x2ExtractLane
	InstrF64x2ReplaceLane
	InstrI8x16Eq
	InstrI8x16Ne
	InstrI8x16LtS
	InstrI8x16LtU
	InstrI8x16GtS
	InstrI8x16GtU
	InstrI8x16LeS
	InstrI8x16LeU
	InstrI8x16GeS
	InstrI8x16GeU
	InstrI16x8Eq
	InstrI16x8Ne
	InstrI16x8LtS
	InstrI16x8LtU
	InstrI16x8GtS
	InstrI16x8GtU
	InstrI16x8LeS
	InstrI16x8LeU
	InstrI16x8GeS
	InstrI16x8GeU
	InstrI32x4Eq
	InstrI32x4Ne
	InstrI32x4LtS
	InstrI32x4LtU
	InstrI32x4GtS
	InstrI32x4GtU
	InstrI32x4LeS
	InstrI32x4LeU
	InstrI32x4GeS
	InstrI32x4GeU
	InstrF32x4Eq
	InstrF32x4Ne
	InstrF32x4Lt
	InstrF32x4Gt
	InstrF32x4Le
	InstrF32x4Ge
	InstrF64x2Eq
	InstrF64x2Ne
	InstrF64x2Lt
	InstrF64x2Gt
	InstrF64x2Le
	InstrF64x2Ge
	InstrV128Not
	InstrV128And
	InstrV128Andnot
	InstrV128Or
	InstrV128Xor
	InstrV128Bitselect
	InstrV128AnyTrue
	InstrV128Load8Lane
	InstrV128Load16Lane
	InstrV128Load32Lane
	InstrV128Load64Lane
	InstrV128Store8Lane
	InstrV128Store16Lane
	InstrV128Store32Lane
	InstrV128Store64Lane
	InstrV128Load32Zero
	InstrV128Load64Zero
	InstrF32x4DemoteF64x2Zero
	InstrF64x2PromoteLowF32x4
	InstrI8x16Abs
	InstrI8x16Neg
	InstrI8x16Popcnt
	InstrI8x16AllTrue
	InstrI8x16Bitmask
	InstrI8x16NarrowI16x8S
	InstrI8x16NarrowI16x8U
	InstrF32x4Ceil
	InstrF32x4Floor
	InstrF32x4Trunc
	InstrF32x4Nearest
	InstrI8x16Shl
	InstrI8x16ShrS
	InstrI8x16ShrU
	InstrI8x16Add
	InstrI8x16AddSatS
	InstrI8x16AddSatU
	InstrI8x16Sub
	InstrI8x16SubSatS
	InstrI8x16SubSatU
	InstrF64x2Ceil
	InstrF64x2Floor
	InstrI8x16MinS
	InstrI8x16MinU
	InstrI8x16MaxS
	InstrI8x16MaxU
	InstrF64x2Trunc
	InstrI8x16AvgrU
	InstrI16x8ExtaddPairwiseI8x16S
	InstrI16x8ExtaddPairwiseI8x16U
	InstrI32x4ExtaddPairwiseI16x8S
	InstrI32x4ExtaddPairwiseI16x8U
	InstrI16x8Abs
	InstrI16x8Neg
	InstrI16x8Q15mulrSatS
	InstrI16x8AllTrue
	InstrI16x8Bitmask
	InstrI16x8NarrowI32x4S
	InstrI16x8NarrowI32x4U
	InstrI16x8ExtendLowI8x16S
	InstrI16x8ExtendHighI8x16S
	InstrI16x8ExtendLowI8x16U
	InstrI16x8ExtendHighI8x16U
	InstrI16x8Shl
	InstrI16x8ShrS
	InstrI16x8ShrU
	InstrI16x8Add
	InstrI16x8AddSatS
	InstrI16x8AddSatU
	InstrI16x8Sub
	InstrI16x8SubSatS
	InstrI16x8SubSatU
	InstrF64x2Nearest
	InstrI16x8Mul
	InstrI16x8MinS
	InstrI16x8MinU
	InstrI16x8MaxS
	InstrI16x8MaxU
	InstrI16x8AvgrU
	InstrI16x8ExtmulLowI8x16S
	InstrI16x8ExtmulHighI8x16S
	InstrI16x8ExtmulLowI8x16U
	InstrI16x8ExtmulHighI8x16U
	InstrI32x4Abs
	InstrI32x4Neg
	InstrI32x4AllTrue
	InstrI32x4Bitmask
	InstrI32x4ExtendLowI16x8S
	InstrI32x4ExtendHighI16x8S
	InstrI32x4ExtendLowI16x8U
	InstrI32x4ExtendHighI16x8U
	InstrI32x4Shl
	InstrI32x4ShrS
	InstrI32x4ShrU
	InstrI32x4Add
	InstrI32x4Sub
	InstrI32x4Mul
	InstrI32x4MinS
	InstrI32x4MinU
	InstrI32x4MaxS
	InstrI32x4MaxU
	InstrI32x4DotI16x8S
	InstrI32x4ExtmulLowI16x8S
	InstrI32x4ExtmulHighI16x8S
	InstrI32x4ExtmulLowI16x8U
	InstrI32x4ExtmulHighI16x8U
	InstrI64x2Abs
	InstrI64x2Neg
	InstrI64x2AllTrue
	InstrI64x2Bitmask
	InstrI64x2ExtendLowI32x4S
	InstrI64x2ExtendHighI32x4S
	InstrI64x2ExtendLowI32x4U
	InstrI64x2ExtendHighI32x4U
	InstrI64x2Shl
	InstrI64x2ShrS
	InstrI64x2ShrU
	InstrI64x2Add
	InstrI64x2Sub
	InstrI64x2Mul
	InstrI64x2Eq
	InstrI64x2Ne
	InstrI64x2LtS
	InstrI64x2GtS
	InstrI64x2LeS
	InstrI64x2GeS
	InstrI64x2ExtmulLowI32x4S
	InstrI64x2ExtmulHighI32x4S
	InstrI64x2ExtmulLowI32x4U
	InstrI64x2ExtmulHighI32x4U
	InstrF32x4Abs
	InstrF32x4Neg
	InstrF32x4Sqrt
	InstrF32x4Add
	InstrF32x4Sub
	InstrF32x4Mul
	InstrF32x4Div
	InstrF32x4Min
	InstrF32x4Max
	InstrF32x4Pmin
	InstrF32x4Pmax
	InstrF64x2Abs
	InstrF64x2Neg
	InstrF64x2Sqrt
	InstrF64x2Add
	InstrF64x2Sub
	InstrF64x2Mul
	InstrF64x2Div
	InstrF64x2Min
	InstrF64x2Max
	InstrF64x2Pmin
	InstrF64x2Pmax
	InstrI32x4TruncSatF32x4S
	InstrI32x4TruncSatF32x4U
	InstrF32x4ConvertI32x4S
	InstrF32x4ConvertI32x4U
	InstrI32x4TruncSatF64x2SZero
	InstrI32x4TruncSatF64x2UZero
	InstrF64x2ConvertLowI32x4S
	InstrF64x2ConvertLowI32x4U
	InstrI8x16RelaxedSwizzle
	InstrI32x4RelaxedTruncF32x4S
	InstrI32x4RelaxedTruncF32x4U
	InstrI32x4RelaxedTruncZeroF64x2S
	InstrI32x4RelaxedTruncZeroF64x2U
	InstrF32x4RelaxedMadd
	InstrF32x4RelaxedNmadd
	InstrF64x2RelaxedMadd
	InstrF64x2RelaxedNmadd
	InstrI8x16RelaxedLaneselect
	InstrI16x8RelaxedLaneselect
	InstrI32x4RelaxedLaneselect
	InstrI64x2RelaxedLaneselect
	InstrF32x4RelaxedMin
	InstrF32x4RelaxedMax
	InstrF64x2RelaxedMin
	InstrF64x2RelaxedMax
	InstrI16x8RelaxedQ15mulrS
	InstrI16x8RelaxedDotI8x16I7x16S
	InstrI32x4RelaxedDotI8x16I7x16AddS
)

var instrKindNames = [...]string{
	InstrInvalid:                       "Invalid",
	InstrUnreachable:                   "Unreachable",
	InstrNop:                           "Nop",
	InstrBlock:                         "Block",
	InstrLoop:                          "Loop",
	InstrIf:                            "If",
	InstrThrow:                         "Throw",
	InstrThrowRef:                      "ThrowRef",
	InstrBr:                            "Br",
	InstrBrIf:                          "BrIf",
	InstrBrTable:                       "BrTable",
	InstrReturn:                        "Return",
	InstrCall:                          "Call",
	InstrCallIndirect:                  "CallIndirect",
	InstrReturnCall:                    "ReturnCall",
	InstrReturnCallIndirect:            "ReturnCallIndirect",
	InstrCallRef:                       "CallRef",
	InstrReturnCallRef:                 "ReturnCallRef",
	InstrDrop:                          "Drop",
	InstrSelect:                        "Select",
	InstrTryTable:                      "TryTable",
	InstrLocalGet:                      "LocalGet",
	InstrLocalSet:                      "LocalSet",
	InstrLocalTee:                      "LocalTee",
	InstrGlobalGet:                     "GlobalGet",
	InstrGlobalSet:                     "GlobalSet",
	InstrTableGet:                      "TableGet",
	InstrTableSet:                      "TableSet",
	InstrI32Load:                       "I32Load",
	InstrI64Load:                       "I64Load",
	InstrF32Load:                       "F32Load",
	InstrF64Load:                       "F64Load",
	InstrI32Load8S:                     "I32Load8S",
	InstrI32Load8U:                     "I32Load8U",
	InstrI32Load16S:                    "I32Load16S",
	InstrI32Load16U:                    "I32Load16U",
	InstrI64Load8S:                     "I64Load8S",
	InstrI64Load8U:                     "I64Load8U",
	InstrI64Load16S:                    "I64Load16S",
	InstrI64Load16U:                    "I64Load16U",
	InstrI64Load32S:                    "I64Load32S",
	InstrI64Load32U:                    "I64Load32U",
	InstrI32Store:                      "I32Store",
	InstrI64Store:                      "I64Store",
	InstrF32Store:                      "F32Store",
	InstrF64Store:                      "F64Store",
	InstrI32Store8:                     "I32Store8",
	InstrI32Store16:                    "I32Store16",
	InstrI64Store8:                     "I64Store8",
	InstrI64Store16:                    "I64Store16",
	InstrI64Store32:                    "I64Store32",
	InstrMemorySize:                    "MemorySize",
	InstrMemoryGrow:                    "MemoryGrow",
	InstrMemoryAtomicNotify:            "MemoryAtomicNotify",
	InstrMemoryAtomicWait32:            "MemoryAtomicWait32",
	InstrMemoryAtomicWait64:            "MemoryAtomicWait64",
	InstrAtomicFence:                   "AtomicFence",
	InstrI32AtomicLoad:                 "I32AtomicLoad",
	InstrI64AtomicLoad:                 "I64AtomicLoad",
	InstrI32AtomicLoad8U:               "I32AtomicLoad8U",
	InstrI32AtomicLoad16U:              "I32AtomicLoad16U",
	InstrI64AtomicLoad8U:               "I64AtomicLoad8U",
	InstrI64AtomicLoad16U:              "I64AtomicLoad16U",
	InstrI64AtomicLoad32U:              "I64AtomicLoad32U",
	InstrI32AtomicStore:                "I32AtomicStore",
	InstrI64AtomicStore:                "I64AtomicStore",
	InstrI32AtomicStore8:               "I32AtomicStore8",
	InstrI32AtomicStore16:              "I32AtomicStore16",
	InstrI64AtomicStore8:               "I64AtomicStore8",
	InstrI64AtomicStore16:              "I64AtomicStore16",
	InstrI64AtomicStore32:              "I64AtomicStore32",
	InstrAtomicRmw:                     "AtomicRmw",
	InstrAtomicCmpxchg:                 "AtomicCmpxchg",
	InstrI32Const:                      "I32Const",
	InstrI64Const:                      "I64Const",
	InstrF32Const:                      "F32Const",
	InstrF64Const:                      "F64Const",
	InstrI32Eqz:                        "I32Eqz",
	InstrI32Eq:                         "I32Eq",
	InstrI32Ne:                         "I32Ne",
	InstrI32LtS:                        "I32LtS",
	InstrI32LtU:                        "I32LtU",
	InstrI32GtS:                        "I32GtS",
	InstrI32GtU:                        "I32GtU",
	InstrI32LeS:                        "I32LeS",
	InstrI32LeU:                        "I32LeU",
	InstrI32GeS:                        "I32GeS",
	InstrI32GeU:                        "I32GeU",
	InstrI64Eqz:                        "I64Eqz",
	InstrI64Eq:                         "I64Eq",
	InstrI64Ne:                         "I64Ne",
	InstrI64LtS:                        "I64LtS",
	InstrI64LtU:                        "I64LtU",
	InstrI64GtS:                        "I64GtS",
	InstrI64GtU:                        "I64GtU",
	InstrI64LeS:                        "I64LeS",
	InstrI64LeU:                        "I64LeU",
	InstrI64GeS:                        "I64GeS",
	InstrI64GeU:                        "I64GeU",
	InstrF32Eq:                         "F32Eq",
	InstrF32Ne:                         "F32Ne",
	InstrF32Lt:                         "F32Lt",
	InstrF32Gt:                         "F32Gt",
	InstrF32Le:                         "F32Le",
	InstrF32Ge:                         "F32Ge",
	InstrF64Eq:                         "F64Eq",
	InstrF64Ne:                         "F64Ne",
	InstrF64Lt:                         "F64Lt",
	InstrF64Gt:                         "F64Gt",
	InstrF64Le:                         "F64Le",
	InstrF64Ge:                         "F64Ge",
	InstrI32Clz:                        "I32Clz",
	InstrI32Ctz:                        "I32Ctz",
	InstrI32Popcnt:                     "I32Popcnt",
	InstrI32Add:                        "I32Add",
	InstrI32Sub:                        "I32Sub",
	InstrI32Mul:                        "I32Mul",
	InstrI32DivS:                       "I32DivS",
	InstrI32DivU:                       "I32DivU",
	InstrI32RemS:                       "I32RemS",
	InstrI32RemU:                       "I32RemU",
	InstrI32And:                        "I32And",
	InstrI32Or:                         "I32Or",
	InstrI32Xor:                        "I32Xor",
	InstrI32Shl:                        "I32Shl",
	InstrI32ShrS:                       "I32ShrS",
	InstrI32ShrU:                       "I32ShrU",
	InstrI32Rotl:                       "I32Rotl",
	InstrI32Rotr:                       "I32Rotr",
	InstrI64Clz:                        "I64Clz",
	InstrI64Ctz:                        "I64Ctz",
	InstrI64Popcnt:                     "I64Popcnt",
	InstrI64Add:                        "I64Add",
	InstrI64Sub:                        "I64Sub",
	InstrI64Mul:                        "I64Mul",
	InstrI64DivS:                       "I64DivS",
	InstrI64DivU:                       "I64DivU",
	InstrI64RemS:                       "I64RemS",
	InstrI64RemU:                       "I64RemU",
	InstrI64And:                        "I64And",
	InstrI64Or:                         "I64Or",
	InstrI64Xor:                        "I64Xor",
	InstrI64Shl:                        "I64Shl",
	InstrI64ShrS:                       "I64ShrS",
	InstrI64ShrU:                       "I64ShrU",
	InstrI64Rotl:                       "I64Rotl",
	InstrI64Rotr:                       "I64Rotr",
	InstrF32Abs:                        "F32Abs",
	InstrF32Neg:                        "F32Neg",
	InstrF32Ceil:                       "F32Ceil",
	InstrF32Floor:                      "F32Floor",
	InstrF32Trunc:                      "F32Trunc",
	InstrF32Nearest:                    "F32Nearest",
	InstrF32Sqrt:                       "F32Sqrt",
	InstrF32Add:                        "F32Add",
	InstrF32Sub:                        "F32Sub",
	InstrF32Mul:                        "F32Mul",
	InstrF32Div:                        "F32Div",
	InstrF32Min:                        "F32Min",
	InstrF32Max:                        "F32Max",
	InstrF32Copysign:                   "F32Copysign",
	InstrF64Abs:                        "F64Abs",
	InstrF64Neg:                        "F64Neg",
	InstrF64Ceil:                       "F64Ceil",
	InstrF64Floor:                      "F64Floor",
	InstrF64Trunc:                      "F64Trunc",
	InstrF64Nearest:                    "F64Nearest",
	InstrF64Sqrt:                       "F64Sqrt",
	InstrF64Add:                        "F64Add",
	InstrF64Sub:                        "F64Sub",
	InstrF64Mul:                        "F64Mul",
	InstrF64Div:                        "F64Div",
	InstrF64Min:                        "F64Min",
	InstrF64Max:                        "F64Max",
	InstrF64Copysign:                   "F64Copysign",
	InstrI32WrapI64:                    "I32WrapI64",
	InstrI32TruncF32S:                  "I32TruncF32S",
	InstrI32TruncF32U:                  "I32TruncF32U",
	InstrI32TruncF64S:                  "I32TruncF64S",
	InstrI32TruncF64U:                  "I32TruncF64U",
	InstrI64ExtendI32S:                 "I64ExtendI32S",
	InstrI64ExtendI32U:                 "I64ExtendI32U",
	InstrI64TruncF32S:                  "I64TruncF32S",
	InstrI64TruncF32U:                  "I64TruncF32U",
	InstrI64TruncF64S:                  "I64TruncF64S",
	InstrI64TruncF64U:                  "I64TruncF64U",
	InstrF32ConvertI32S:                "F32ConvertI32S",
	InstrF32ConvertI32U:                "F32ConvertI32U",
	InstrF32ConvertI64S:                "F32ConvertI64S",
	InstrF32ConvertI64U:                "F32ConvertI64U",
	InstrF32DemoteF64:                  "F32DemoteF64",
	InstrF64ConvertI32S:                "F64ConvertI32S",
	InstrF64ConvertI32U:                "F64ConvertI32U",
	InstrF64ConvertI64S:                "F64ConvertI64S",
	InstrF64ConvertI64U:                "F64ConvertI64U",
	InstrF64PromoteF32:                 "F64PromoteF32",
	InstrI32ReinterpretF32:             "I32ReinterpretF32",
	InstrI64ReinterpretF64:             "I64ReinterpretF64",
	InstrF32ReinterpretI32:             "F32ReinterpretI32",
	InstrF64ReinterpretI64:             "F64ReinterpretI64",
	InstrI32Extend8S:                   "I32Extend8S",
	InstrI32Extend16S:                  "I32Extend16S",
	InstrI64Extend8S:                   "I64Extend8S",
	InstrI64Extend16S:                  "I64Extend16S",
	InstrI64Extend32S:                  "I64Extend32S",
	InstrRefNull:                       "RefNull",
	InstrRefIsNull:                     "RefIsNull",
	InstrRefFunc:                       "RefFunc",
	InstrRefEq:                         "RefEq",
	InstrRefAsNonNull:                  "RefAsNonNull",
	InstrStringConst:                   "StringConst",
	InstrBrOnNull:                      "BrOnNull",
	InstrBrOnNonNull:                   "BrOnNonNull",
	InstrStringNewUtf8Array:            "StringNewUtf8Array",
	InstrStringNewWtf16Array:           "StringNewWtf16Array",
	InstrStringEncodeUtf8Array:         "StringEncodeUtf8Array",
	InstrStringEncodeWtf16Array:        "StringEncodeWtf16Array",
	InstrStringNewLossyUtf8Array:       "StringNewLossyUtf8Array",
	InstrStringNewWtf8Array:            "StringNewWtf8Array",
	InstrStringEncodeLossyUtf8Array:    "StringEncodeLossyUtf8Array",
	InstrStringEncodeWtf8Array:         "StringEncodeWtf8Array",
	InstrStructNew:                     "StructNew",
	InstrStructNewDefault:              "StructNewDefault",
	InstrStructNewDesc:                 "StructNewDesc",
	InstrStructNewDefaultDesc:          "StructNewDefaultDesc",
	InstrStructGet:                     "StructGet",
	InstrStructGetS:                    "StructGetS",
	InstrStructGetU:                    "StructGetU",
	InstrStructAtomicGet:               "StructAtomicGet",
	InstrStructAtomicGetS:              "StructAtomicGetS",
	InstrStructAtomicGetU:              "StructAtomicGetU",
	InstrStructSet:                     "StructSet",
	InstrArrayNew:                      "ArrayNew",
	InstrArrayNewDefault:               "ArrayNewDefault",
	InstrArrayNewFixed:                 "ArrayNewFixed",
	InstrArrayNewData:                  "ArrayNewData",
	InstrArrayNewElem:                  "ArrayNewElem",
	InstrArrayGet:                      "ArrayGet",
	InstrArrayGetS:                     "ArrayGetS",
	InstrArrayGetU:                     "ArrayGetU",
	InstrArraySet:                      "ArraySet",
	InstrArrayLen:                      "ArrayLen",
	InstrArrayFill:                     "ArrayFill",
	InstrArrayCopy:                     "ArrayCopy",
	InstrArrayInitData:                 "ArrayInitData",
	InstrArrayInitElem:                 "ArrayInitElem",
	InstrRefGetDesc:                    "RefGetDesc",
	InstrRefTest:                       "RefTest",
	InstrRefCast:                       "RefCast",
	InstrRefTestDesc:                   "RefTestDesc",
	InstrRefCastDescEq:                 "RefCastDescEq",
	InstrBrOnCast:                      "BrOnCast",
	InstrBrOnCastFail:                  "BrOnCastFail",
	InstrAnyConvertExtern:              "AnyConvertExtern",
	InstrExternConvertAny:              "ExternConvertAny",
	InstrRefI31:                        "RefI31",
	InstrI31GetS:                       "I31GetS",
	InstrI31GetU:                       "I31GetU",
	InstrI32TruncSatF32S:               "I32TruncSatF32S",
	InstrI32TruncSatF32U:               "I32TruncSatF32U",
	InstrI32TruncSatF64S:               "I32TruncSatF64S",
	InstrI32TruncSatF64U:               "I32TruncSatF64U",
	InstrI64TruncSatF32S:               "I64TruncSatF32S",
	InstrI64TruncSatF32U:               "I64TruncSatF32U",
	InstrI64TruncSatF64S:               "I64TruncSatF64S",
	InstrI64TruncSatF64U:               "I64TruncSatF64U",
	InstrMemoryInit:                    "MemoryInit",
	InstrDataDrop:                      "DataDrop",
	InstrMemoryCopy:                    "MemoryCopy",
	InstrMemoryFill:                    "MemoryFill",
	InstrTableInit:                     "TableInit",
	InstrElemDrop:                      "ElemDrop",
	InstrTableCopy:                     "TableCopy",
	InstrTableGrow:                     "TableGrow",
	InstrTableSize:                     "TableSize",
	InstrTableFill:                     "TableFill",
	InstrV128Load:                      "V128Load",
	InstrV128Load8x8S:                  "V128Load8x8S",
	InstrV128Load8x8U:                  "V128Load8x8U",
	InstrV128Load16x4S:                 "V128Load16x4S",
	InstrV128Load16x4U:                 "V128Load16x4U",
	InstrV128Load32x2S:                 "V128Load32x2S",
	InstrV128Load32x2U:                 "V128Load32x2U",
	InstrV128Load8Splat:                "V128Load8Splat",
	InstrV128Load16Splat:               "V128Load16Splat",
	InstrV128Load32Splat:               "V128Load32Splat",
	InstrV128Load64Splat:               "V128Load64Splat",
	InstrV128Store:                     "V128Store",
	InstrV128Const:                     "V128Const",
	InstrByte:                          "Byte",
	InstrI8x16Shuffle:                  "I8x16Shuffle",
	InstrLaneIdx:                       "LaneIdx",
	InstrI8x16Swizzle:                  "I8x16Swizzle",
	InstrI8x16Splat:                    "I8x16Splat",
	InstrI16x8Splat:                    "I16x8Splat",
	InstrI32x4Splat:                    "I32x4Splat",
	InstrI64x2Splat:                    "I64x2Splat",
	InstrF32x4Splat:                    "F32x4Splat",
	InstrF64x2Splat:                    "F64x2Splat",
	InstrI8x16ExtractLaneS:             "I8x16ExtractLaneS",
	InstrI8x16ExtractLaneU:             "I8x16ExtractLaneU",
	InstrI8x16ReplaceLane:              "I8x16ReplaceLane",
	InstrI16x8ExtractLaneS:             "I16x8ExtractLaneS",
	InstrI16x8ExtractLaneU:             "I16x8ExtractLaneU",
	InstrI16x8ReplaceLane:              "I16x8ReplaceLane",
	InstrI32x4ExtractLane:              "I32x4ExtractLane",
	InstrI32x4ReplaceLane:              "I32x4ReplaceLane",
	InstrI64x2ExtractLane:              "I64x2ExtractLane",
	InstrI64x2ReplaceLane:              "I64x2ReplaceLane",
	InstrF32x4ExtractLane:              "F32x4ExtractLane",
	InstrF32x4ReplaceLane:              "F32x4ReplaceLane",
	InstrF64x2ExtractLane:              "F64x2ExtractLane",
	InstrF64x2ReplaceLane:              "F64x2ReplaceLane",
	InstrI8x16Eq:                       "I8x16Eq",
	InstrI8x16Ne:                       "I8x16Ne",
	InstrI8x16LtS:                      "I8x16LtS",
	InstrI8x16LtU:                      "I8x16LtU",
	InstrI8x16GtS:                      "I8x16GtS",
	InstrI8x16GtU:                      "I8x16GtU",
	InstrI8x16LeS:                      "I8x16LeS",
	InstrI8x16LeU:                      "I8x16LeU",
	InstrI8x16GeS:                      "I8x16GeS",
	InstrI8x16GeU:                      "I8x16GeU",
	InstrI16x8Eq:                       "I16x8Eq",
	InstrI16x8Ne:                       "I16x8Ne",
	InstrI16x8LtS:                      "I16x8LtS",
	InstrI16x8LtU:                      "I16x8LtU",
	InstrI16x8GtS:                      "I16x8GtS",
	InstrI16x8GtU:                      "I16x8GtU",
	InstrI16x8LeS:                      "I16x8LeS",
	InstrI16x8LeU:                      "I16x8LeU",
	InstrI16x8GeS:                      "I16x8GeS",
	InstrI16x8GeU:                      "I16x8GeU",
	InstrI32x4Eq:                       "I32x4Eq",
	InstrI32x4Ne:                       "I32x4Ne",
	InstrI32x4LtS:                      "I32x4LtS",
	InstrI32x4LtU:                      "I32x4LtU",
	InstrI32x4GtS:                      "I32x4GtS",
	InstrI32x4GtU:                      "I32x4GtU",
	InstrI32x4LeS:                      "I32x4LeS",
	InstrI32x4LeU:                      "I32x4LeU",
	InstrI32x4GeS:                      "I32x4GeS",
	InstrI32x4GeU:                      "I32x4GeU",
	InstrF32x4Eq:                       "F32x4Eq",
	InstrF32x4Ne:                       "F32x4Ne",
	InstrF32x4Lt:                       "F32x4Lt",
	InstrF32x4Gt:                       "F32x4Gt",
	InstrF32x4Le:                       "F32x4Le",
	InstrF32x4Ge:                       "F32x4Ge",
	InstrF64x2Eq:                       "F64x2Eq",
	InstrF64x2Ne:                       "F64x2Ne",
	InstrF64x2Lt:                       "F64x2Lt",
	InstrF64x2Gt:                       "F64x2Gt",
	InstrF64x2Le:                       "F64x2Le",
	InstrF64x2Ge:                       "F64x2Ge",
	InstrV128Not:                       "V128Not",
	InstrV128And:                       "V128And",
	InstrV128Andnot:                    "V128Andnot",
	InstrV128Or:                        "V128Or",
	InstrV128Xor:                       "V128Xor",
	InstrV128Bitselect:                 "V128Bitselect",
	InstrV128AnyTrue:                   "V128AnyTrue",
	InstrV128Load8Lane:                 "V128Load8Lane",
	InstrV128Load16Lane:                "V128Load16Lane",
	InstrV128Load32Lane:                "V128Load32Lane",
	InstrV128Load64Lane:                "V128Load64Lane",
	InstrV128Store8Lane:                "V128Store8Lane",
	InstrV128Store16Lane:               "V128Store16Lane",
	InstrV128Store32Lane:               "V128Store32Lane",
	InstrV128Store64Lane:               "V128Store64Lane",
	InstrV128Load32Zero:                "V128Load32Zero",
	InstrV128Load64Zero:                "V128Load64Zero",
	InstrF32x4DemoteF64x2Zero:          "F32x4DemoteF64x2Zero",
	InstrF64x2PromoteLowF32x4:          "F64x2PromoteLowF32x4",
	InstrI8x16Abs:                      "I8x16Abs",
	InstrI8x16Neg:                      "I8x16Neg",
	InstrI8x16Popcnt:                   "I8x16Popcnt",
	InstrI8x16AllTrue:                  "I8x16AllTrue",
	InstrI8x16Bitmask:                  "I8x16Bitmask",
	InstrI8x16NarrowI16x8S:             "I8x16NarrowI16x8S",
	InstrI8x16NarrowI16x8U:             "I8x16NarrowI16x8U",
	InstrF32x4Ceil:                     "F32x4Ceil",
	InstrF32x4Floor:                    "F32x4Floor",
	InstrF32x4Trunc:                    "F32x4Trunc",
	InstrF32x4Nearest:                  "F32x4Nearest",
	InstrI8x16Shl:                      "I8x16Shl",
	InstrI8x16ShrS:                     "I8x16ShrS",
	InstrI8x16ShrU:                     "I8x16ShrU",
	InstrI8x16Add:                      "I8x16Add",
	InstrI8x16AddSatS:                  "I8x16AddSatS",
	InstrI8x16AddSatU:                  "I8x16AddSatU",
	InstrI8x16Sub:                      "I8x16Sub",
	InstrI8x16SubSatS:                  "I8x16SubSatS",
	InstrI8x16SubSatU:                  "I8x16SubSatU",
	InstrF64x2Ceil:                     "F64x2Ceil",
	InstrF64x2Floor:                    "F64x2Floor",
	InstrI8x16MinS:                     "I8x16MinS",
	InstrI8x16MinU:                     "I8x16MinU",
	InstrI8x16MaxS:                     "I8x16MaxS",
	InstrI8x16MaxU:                     "I8x16MaxU",
	InstrF64x2Trunc:                    "F64x2Trunc",
	InstrI8x16AvgrU:                    "I8x16AvgrU",
	InstrI16x8ExtaddPairwiseI8x16S:     "I16x8ExtaddPairwiseI8x16S",
	InstrI16x8ExtaddPairwiseI8x16U:     "I16x8ExtaddPairwiseI8x16U",
	InstrI32x4ExtaddPairwiseI16x8S:     "I32x4ExtaddPairwiseI16x8S",
	InstrI32x4ExtaddPairwiseI16x8U:     "I32x4ExtaddPairwiseI16x8U",
	InstrI16x8Abs:                      "I16x8Abs",
	InstrI16x8Neg:                      "I16x8Neg",
	InstrI16x8Q15mulrSatS:              "I16x8Q15mulrSatS",
	InstrI16x8AllTrue:                  "I16x8AllTrue",
	InstrI16x8Bitmask:                  "I16x8Bitmask",
	InstrI16x8NarrowI32x4S:             "I16x8NarrowI32x4S",
	InstrI16x8NarrowI32x4U:             "I16x8NarrowI32x4U",
	InstrI16x8ExtendLowI8x16S:          "I16x8ExtendLowI8x16S",
	InstrI16x8ExtendHighI8x16S:         "I16x8ExtendHighI8x16S",
	InstrI16x8ExtendLowI8x16U:          "I16x8ExtendLowI8x16U",
	InstrI16x8ExtendHighI8x16U:         "I16x8ExtendHighI8x16U",
	InstrI16x8Shl:                      "I16x8Shl",
	InstrI16x8ShrS:                     "I16x8ShrS",
	InstrI16x8ShrU:                     "I16x8ShrU",
	InstrI16x8Add:                      "I16x8Add",
	InstrI16x8AddSatS:                  "I16x8AddSatS",
	InstrI16x8AddSatU:                  "I16x8AddSatU",
	InstrI16x8Sub:                      "I16x8Sub",
	InstrI16x8SubSatS:                  "I16x8SubSatS",
	InstrI16x8SubSatU:                  "I16x8SubSatU",
	InstrF64x2Nearest:                  "F64x2Nearest",
	InstrI16x8Mul:                      "I16x8Mul",
	InstrI16x8MinS:                     "I16x8MinS",
	InstrI16x8MinU:                     "I16x8MinU",
	InstrI16x8MaxS:                     "I16x8MaxS",
	InstrI16x8MaxU:                     "I16x8MaxU",
	InstrI16x8AvgrU:                    "I16x8AvgrU",
	InstrI16x8ExtmulLowI8x16S:          "I16x8ExtmulLowI8x16S",
	InstrI16x8ExtmulHighI8x16S:         "I16x8ExtmulHighI8x16S",
	InstrI16x8ExtmulLowI8x16U:          "I16x8ExtmulLowI8x16U",
	InstrI16x8ExtmulHighI8x16U:         "I16x8ExtmulHighI8x16U",
	InstrI32x4Abs:                      "I32x4Abs",
	InstrI32x4Neg:                      "I32x4Neg",
	InstrI32x4AllTrue:                  "I32x4AllTrue",
	InstrI32x4Bitmask:                  "I32x4Bitmask",
	InstrI32x4ExtendLowI16x8S:          "I32x4ExtendLowI16x8S",
	InstrI32x4ExtendHighI16x8S:         "I32x4ExtendHighI16x8S",
	InstrI32x4ExtendLowI16x8U:          "I32x4ExtendLowI16x8U",
	InstrI32x4ExtendHighI16x8U:         "I32x4ExtendHighI16x8U",
	InstrI32x4Shl:                      "I32x4Shl",
	InstrI32x4ShrS:                     "I32x4ShrS",
	InstrI32x4ShrU:                     "I32x4ShrU",
	InstrI32x4Add:                      "I32x4Add",
	InstrI32x4Sub:                      "I32x4Sub",
	InstrI32x4Mul:                      "I32x4Mul",
	InstrI32x4MinS:                     "I32x4MinS",
	InstrI32x4MinU:                     "I32x4MinU",
	InstrI32x4MaxS:                     "I32x4MaxS",
	InstrI32x4MaxU:                     "I32x4MaxU",
	InstrI32x4DotI16x8S:                "I32x4DotI16x8S",
	InstrI32x4ExtmulLowI16x8S:          "I32x4ExtmulLowI16x8S",
	InstrI32x4ExtmulHighI16x8S:         "I32x4ExtmulHighI16x8S",
	InstrI32x4ExtmulLowI16x8U:          "I32x4ExtmulLowI16x8U",
	InstrI32x4ExtmulHighI16x8U:         "I32x4ExtmulHighI16x8U",
	InstrI64x2Abs:                      "I64x2Abs",
	InstrI64x2Neg:                      "I64x2Neg",
	InstrI64x2AllTrue:                  "I64x2AllTrue",
	InstrI64x2Bitmask:                  "I64x2Bitmask",
	InstrI64x2ExtendLowI32x4S:          "I64x2ExtendLowI32x4S",
	InstrI64x2ExtendHighI32x4S:         "I64x2ExtendHighI32x4S",
	InstrI64x2ExtendLowI32x4U:          "I64x2ExtendLowI32x4U",
	InstrI64x2ExtendHighI32x4U:         "I64x2ExtendHighI32x4U",
	InstrI64x2Shl:                      "I64x2Shl",
	InstrI64x2ShrS:                     "I64x2ShrS",
	InstrI64x2ShrU:                     "I64x2ShrU",
	InstrI64x2Add:                      "I64x2Add",
	InstrI64x2Sub:                      "I64x2Sub",
	InstrI64x2Mul:                      "I64x2Mul",
	InstrI64x2Eq:                       "I64x2Eq",
	InstrI64x2Ne:                       "I64x2Ne",
	InstrI64x2LtS:                      "I64x2LtS",
	InstrI64x2GtS:                      "I64x2GtS",
	InstrI64x2LeS:                      "I64x2LeS",
	InstrI64x2GeS:                      "I64x2GeS",
	InstrI64x2ExtmulLowI32x4S:          "I64x2ExtmulLowI32x4S",
	InstrI64x2ExtmulHighI32x4S:         "I64x2ExtmulHighI32x4S",
	InstrI64x2ExtmulLowI32x4U:          "I64x2ExtmulLowI32x4U",
	InstrI64x2ExtmulHighI32x4U:         "I64x2ExtmulHighI32x4U",
	InstrF32x4Abs:                      "F32x4Abs",
	InstrF32x4Neg:                      "F32x4Neg",
	InstrF32x4Sqrt:                     "F32x4Sqrt",
	InstrF32x4Add:                      "F32x4Add",
	InstrF32x4Sub:                      "F32x4Sub",
	InstrF32x4Mul:                      "F32x4Mul",
	InstrF32x4Div:                      "F32x4Div",
	InstrF32x4Min:                      "F32x4Min",
	InstrF32x4Max:                      "F32x4Max",
	InstrF32x4Pmin:                     "F32x4Pmin",
	InstrF32x4Pmax:                     "F32x4Pmax",
	InstrF64x2Abs:                      "F64x2Abs",
	InstrF64x2Neg:                      "F64x2Neg",
	InstrF64x2Sqrt:                     "F64x2Sqrt",
	InstrF64x2Add:                      "F64x2Add",
	InstrF64x2Sub:                      "F64x2Sub",
	InstrF64x2Mul:                      "F64x2Mul",
	InstrF64x2Div:                      "F64x2Div",
	InstrF64x2Min:                      "F64x2Min",
	InstrF64x2Max:                      "F64x2Max",
	InstrF64x2Pmin:                     "F64x2Pmin",
	InstrF64x2Pmax:                     "F64x2Pmax",
	InstrI32x4TruncSatF32x4S:           "I32x4TruncSatF32x4S",
	InstrI32x4TruncSatF32x4U:           "I32x4TruncSatF32x4U",
	InstrF32x4ConvertI32x4S:            "F32x4ConvertI32x4S",
	InstrF32x4ConvertI32x4U:            "F32x4ConvertI32x4U",
	InstrI32x4TruncSatF64x2SZero:       "I32x4TruncSatF64x2SZero",
	InstrI32x4TruncSatF64x2UZero:       "I32x4TruncSatF64x2UZero",
	InstrF64x2ConvertLowI32x4S:         "F64x2ConvertLowI32x4S",
	InstrF64x2ConvertLowI32x4U:         "F64x2ConvertLowI32x4U",
	InstrI8x16RelaxedSwizzle:           "I8x16RelaxedSwizzle",
	InstrI32x4RelaxedTruncF32x4S:       "I32x4RelaxedTruncF32x4S",
	InstrI32x4RelaxedTruncF32x4U:       "I32x4RelaxedTruncF32x4U",
	InstrI32x4RelaxedTruncZeroF64x2S:   "I32x4RelaxedTruncZeroF64x2S",
	InstrI32x4RelaxedTruncZeroF64x2U:   "I32x4RelaxedTruncZeroF64x2U",
	InstrF32x4RelaxedMadd:              "F32x4RelaxedMadd",
	InstrF32x4RelaxedNmadd:             "F32x4RelaxedNmadd",
	InstrF64x2RelaxedMadd:              "F64x2RelaxedMadd",
	InstrF64x2RelaxedNmadd:             "F64x2RelaxedNmadd",
	InstrI8x16RelaxedLaneselect:        "I8x16RelaxedLaneselect",
	InstrI16x8RelaxedLaneselect:        "I16x8RelaxedLaneselect",
	InstrI32x4RelaxedLaneselect:        "I32x4RelaxedLaneselect",
	InstrI64x2RelaxedLaneselect:        "I64x2RelaxedLaneselect",
	InstrF32x4RelaxedMin:               "F32x4RelaxedMin",
	InstrF32x4RelaxedMax:               "F32x4RelaxedMax",
	InstrF64x2RelaxedMin:               "F64x2RelaxedMin",
	InstrF64x2RelaxedMax:               "F64x2RelaxedMax",
	InstrI16x8RelaxedQ15mulrS:          "I16x8RelaxedQ15mulrS",
	InstrI16x8RelaxedDotI8x16I7x16S:    "I16x8RelaxedDotI8x16I7x16S",
	InstrI32x4RelaxedDotI8x16I7x16AddS: "I32x4RelaxedDotI8x16I7x16AddS",
}

func (k InstrKind) String() string {
	if int(k) >= 0 && int(k) < len(instrKindNames) && instrKindNames[k] != "" {
		return instrKindNames[k]
	}
	return "Invalid"
}

// Instruction is a decoded wasm instruction payload used by the streaming
// validator, encoder helpers, and programmatic tests. DecodeModule does not
// build retained function-body instruction trees. The high-frequency scalar
// fields stay inline, while bulky or rare-opcode payloads live in a lazily
// allocated *instrExt. The boxed fields are reached through the accessor methods
// below, which return zero values when ext is nil.
type Instruction struct {
	ext         *instrExt
	I64         int64
	F64Bits     uint64
	Index       uint32
	Index2      uint32
	I32         int32
	F32Bits     uint32
	AtomicOp    uint32
	Kind        InstrKind
	Lane        LaneIdx
	AtomicOrder AtomicOrder
	Cast        CastOp
}

// instrExt holds the Instruction payloads that only a minority of opcodes use:
// control-flow bodies, memory operands, and the rare reference/SIMD/EH fields.
// It is allocated lazily by the decoder for the instructions that need it.
type instrExt struct {
	BlockType BlockType
	Body      Expr
	Then      []Instruction
	Else      []Instruction
	Catches   []Catch
	Indices   []uint32
	ValTypes  []ValType
	MemArg    MemArg
	Bytes     []byte
	Lanes     [16]LaneIdx
	RefType   RefType
	HeapType  HeapType
	HeapType2 HeapType
}

func (in *Instruction) BlockType() BlockType {
	if in.ext == nil {
		return BlockType{}
	}
	return in.ext.BlockType
}
func (in *Instruction) Body() Expr {
	if in.ext == nil {
		return Expr{}
	}
	return in.ext.Body
}
func (in *Instruction) Then() []Instruction {
	if in.ext == nil {
		return nil
	}
	return in.ext.Then
}
func (in *Instruction) Else() []Instruction {
	if in.ext == nil {
		return nil
	}
	return in.ext.Else
}
func (in *Instruction) Catches() []Catch {
	if in.ext == nil {
		return nil
	}
	return in.ext.Catches
}
func (in *Instruction) Indices() []uint32 {
	if in.ext == nil {
		return nil
	}
	return in.ext.Indices
}
func (in *Instruction) ValTypes() []ValType {
	if in.ext == nil {
		return nil
	}
	return in.ext.ValTypes
}
func (in *Instruction) MemArg() MemArg {
	if in.ext == nil {
		return MemArg{}
	}
	return in.ext.MemArg
}
func (in *Instruction) Bytes() []byte {
	if in.ext == nil {
		return nil
	}
	return in.ext.Bytes
}
func (in *Instruction) Lanes() [16]LaneIdx {
	if in.ext == nil {
		return [16]LaneIdx{}
	}
	return in.ext.Lanes
}
func (in *Instruction) RefType() RefType {
	if in.ext == nil {
		return RefType{}
	}
	return in.ext.RefType
}
func (in *Instruction) HeapType() HeapType {
	if in.ext == nil {
		return HeapType{}
	}
	return in.ext.HeapType
}
func (in *Instruction) HeapType2() HeapType {
	if in.ext == nil {
		return HeapType{}
	}
	return in.ext.HeapType2
}

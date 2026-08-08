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

// HeapType, RefType, ValType, StorageType, and FieldType share one canonical,
// pointer-free two-word encoding. The second word is necessary only because a
// resolved definition carries two full uint32 coordinates; retaining the full
// accepted domain leaves no room for a variant tag in one uint64. Ordinary
// decoded types use only a uint32 payload, but keeping one representation makes
// values directly comparable and avoids pointer-rich tagged unions.
//
// lo bit layout:
//
//	0..1   heap kind      8..15  abstract heap type
//	16     recursive TypeIdx
//	17     defined-type coordinate is present
//	18..19 defined component kind
//	20     nullable      21 exact      22 bare binary alias
//	23     defined component kind is valid
//	24..25 value kind    32..39 numeric type
//	40     packed        41..48 packed storage type
//	49     mutable
//
// hi contains a uint32 TypeIdx, or group/member uint32 coordinates for a
// resolved definition. Scalar operations are endian-independent.
type HeapType struct{ lo, hi uint64 }

const (
	heapKindShift   = 0
	heapKindMask    = uint64(0x3)
	heapAbsShift    = 8
	heapAbsMask     = uint64(0xff) << heapAbsShift
	typeRecBit      = uint64(1) << 16
	defPresentBit   = uint64(1) << 17
	defKindShift    = 18
	defKindMask     = uint64(0x3) << defKindShift
	nullableBit     = uint64(1) << 20
	exactBit        = uint64(1) << 21
	bareBit         = uint64(1) << 22
	defKindValidBit = uint64(1) << 23
	valKindShift    = 24
	valKindMask     = uint64(0x3) << valKindShift
	numTypeShift    = 32
	numTypeMask     = uint64(0xff) << numTypeShift
	packedBit       = uint64(1) << 40
	packTypeShift   = 41
	packTypeMask    = uint64(0xff) << packTypeShift
	mutableBit      = uint64(1) << 49
)

func AbsHeap(abs AbsHeapType) HeapType {
	return HeapType{lo: uint64(HeapAbs)<<heapKindShift | uint64(abs)<<heapAbsShift}
}
func IndexedHeap(idx TypeIdx) HeapType {
	lo := uint64(HeapTypeIndex) << heapKindShift
	if idx.Rec {
		lo |= typeRecBit
	}
	return HeapType{lo: lo, hi: uint64(idx.Index)}
}
func DefinedHeap(def *DefType) HeapType {
	h := HeapType{lo: uint64(HeapDefType) << heapKindShift}
	if def == nil {
		return h
	}
	h.lo |= defPresentBit
	h.hi = uint64(def.GroupIndex) | uint64(def.Index)<<32
	if def.Index < uint32(len(def.Rec.SubTypes)) {
		h.lo |= defKindValidBit | uint64(def.Rec.SubTypes[def.Index].Comp.Kind)<<defKindShift
	}
	return h
}
func (h HeapType) Kind() HeapTypeKind { return HeapTypeKind((h.lo >> heapKindShift) & heapKindMask) }
func (h HeapType) Abs() AbsHeapType   { return AbsHeapType((h.lo & heapAbsMask) >> heapAbsShift) }
func (h HeapType) Type() TypeIdx {
	return TypeIdx{Index: uint32(h.hi), Rec: h.lo&typeRecBit != 0}
}
func (h HeapType) Def() (group, member uint32, kind CompTypeKind, valid bool) {
	return uint32(h.hi), uint32(h.hi >> 32), CompTypeKind((h.lo & defKindMask) >> defKindShift), h.lo&defPresentBit != 0
}
func (h HeapType) DefCompKind() (CompTypeKind, bool) {
	return CompTypeKind((h.lo & defKindMask) >> defKindShift), h.lo&defKindValidBit != 0
}

type RefType struct{ lo, hi uint64 }

func AbsRef(abs AbsHeapType) RefType {
	h := AbsHeap(abs)
	return RefType{lo: h.lo | nullableBit | bareBit, hi: h.hi}
}
func Ref(nullable bool, heap HeapType, exact bool) RefType {
	lo := heap.lo
	if nullable {
		lo |= nullableBit
	}
	if exact {
		lo |= exactBit
	}
	return RefType{lo: lo, hi: heap.hi}
}

func (rt RefType) Nullable() bool { return rt.lo&nullableBit != 0 }
func (rt RefType) Exact() bool    { return rt.lo&exactBit != 0 }
func (rt RefType) Bare() bool     { return rt.lo&bareBit != 0 }
func (rt RefType) Heap() HeapType {
	return HeapType{lo: rt.lo & (heapKindMask | heapAbsMask | typeRecBit | defPresentBit | defKindMask | defKindValidBit), hi: rt.hi}
}
func (rt RefType) WithNullable(nullable bool) RefType {
	if nullable {
		rt.lo |= nullableBit
	} else {
		rt.lo &^= nullableBit
	}
	return rt
}
func (rt RefType) WithHeap(heap HeapType) RefType {
	return RefType{lo: heap.lo | rt.lo&(nullableBit|exactBit|bareBit), hi: heap.hi}
}
func (rt RefType) IsDefaultable() bool { return rt.Nullable() }

func (rt RefType) String() string {
	prefix := "ref"
	if rt.Nullable() {
		prefix = "ref null"
	}
	if rt.Exact() {
		prefix += " exact"
	}
	return prefix + " " + rt.Heap().String()
}

func (h HeapType) String() string {
	switch h.Kind() {
	case HeapAbs:
		return h.Abs().String()
	case HeapTypeIndex:
		return fmt.Sprintf("type %d", h.Type().Index)
	case HeapDefType:
		group, member, _, valid := h.Def()
		if !valid {
			return "def ?"
		}
		return fmt.Sprintf("def %d.%d", group, member)
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

type ValType struct{ lo, hi uint64 }

func newValType(kind ValTypeKind, num NumType) ValType {
	return ValType{lo: uint64(kind)<<valKindShift | uint64(num)<<numTypeShift}
}

var (
	I32       = newValType(ValNum, NumI32)
	I64       = newValType(ValNum, NumI64)
	F32       = newValType(ValNum, NumF32)
	F64       = newValType(ValNum, NumF64)
	V128      = newValType(ValVec, 0)
	Bot       = newValType(ValBot, 0)
	FuncRef   = RefVal(AbsRef(HeapFunc))
	ExternRef = RefVal(AbsRef(HeapExtern))
	AnyRef    = RefVal(AbsRef(HeapAny))
	EqRef     = RefVal(AbsRef(HeapEq))
	I31Ref    = RefVal(AbsRef(HeapI31))
	StringRef = RefVal(AbsRef(HeapString))
)

func RefVal(rt RefType) ValType {
	return ValType{lo: rt.lo | uint64(ValRef)<<valKindShift, hi: rt.hi}
}
func (v ValType) Kind() ValTypeKind { return ValTypeKind((v.lo & valKindMask) >> valKindShift) }
func (v ValType) Num() NumType      { return NumType((v.lo & numTypeMask) >> numTypeShift) }
func (v ValType) Ref() RefType {
	return RefType{lo: v.lo &^ (valKindMask | numTypeMask | packedBit | packTypeMask | mutableBit), hi: v.hi}
}

func (v ValType) String() string {
	switch v.Kind() {
	case ValNum:
		switch v.Num() {
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
		rt := v.Ref()
		if rt.Bare() && rt.Nullable() && !rt.Exact() && rt.Heap().Kind() == HeapAbs {
			switch rt.Heap().Abs() {
			case HeapFunc:
				return "funcref"
			case HeapExtern:
				return "externref"
			}
		}
		return rt.String()
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

type StorageType struct{ lo, hi uint64 }

func StorageVal(v ValType) StorageType { return StorageType(v) }
func StoragePacked(pack PackType) StorageType {
	return StorageType{lo: packedBit | uint64(pack)<<packTypeShift}
}
func (s StorageType) Packed() bool { return s.lo&packedBit != 0 }
func (s StorageType) Val() ValType {
	return ValType{lo: s.lo &^ (packedBit | packTypeMask | mutableBit), hi: s.hi}
}
func (s StorageType) Pack() PackType { return PackType((s.lo & packTypeMask) >> packTypeShift) }

type FieldType struct{ lo, hi uint64 }

func NewFieldType(storage StorageType, mut Mut) FieldType {
	lo := storage.lo
	if mut == Var {
		lo |= mutableBit
	}
	return FieldType{lo: lo, hi: storage.hi}
}
func (f FieldType) Storage() StorageType {
	return StorageType{lo: f.lo &^ mutableBit, hi: f.hi}
}
func (f FieldType) Mut() Mut {
	if f.lo&mutableBit != 0 {
		return Var
	}
	return Const
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

// OptionalTypeIdx stores a full TypeIdx and explicit presence without a Go
// pointer. Bits 0..31 hold Index, bit 32 holds Rec, and bit 33 holds presence.
type OptionalTypeIdx struct{ bits uint64 }

const (
	optionalTypeIdxRec     = uint64(1) << 32
	optionalTypeIdxPresent = uint64(1) << 33
)

func SomeTypeIdx(idx TypeIdx) OptionalTypeIdx {
	b := optionalTypeIdxPresent | uint64(idx.Index)
	if idx.Rec {
		b |= optionalTypeIdxRec
	}
	return OptionalTypeIdx{bits: b}
}

func (o OptionalTypeIdx) Get() (TypeIdx, bool) {
	return TypeIdx{Index: uint32(o.bits), Rec: o.bits&optionalTypeIdxRec != 0}, o.bits&optionalTypeIdxPresent != 0
}

func (o OptionalTypeIdx) Present() bool { return o.bits&optionalTypeIdxPresent != 0 }

type TypeMetadata struct {
	Describes  OptionalTypeIdx
	Descriptor OptionalTypeIdx
}

type SubType struct {
	Final     bool
	Supers    []TypeIdx
	Metadata  TypeMetadata
	Comp      CompType
	HasPrefix bool // false for the compact CompTypeSubType form.
}

type RecType struct {
	SubTypes []SubType
}

type DefType struct {
	Rec        RecType
	GroupIndex uint32
	Index      uint32
}

type Limits struct {
	Min    uint64
	Max    uint64
	HasMax bool
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

// ExternType stores the payload for exactly one external kind. value holds a
// global value or table reference type, index holds a function/tag type index,
// and min/max hold table/memory limits. flags represents optional maxima and
// scalar booleans without retaining Go pointers.
type ExternType struct {
	Kind  ExternKind
	flags uint8
	index uint32
	value ValType
	min   uint64
	max   uint64
}

const (
	externTypeRecursive = uint8(1 << iota)
	externTypeHasMax
	externTypeAddr64
	externTypeShared
	externTypeMutable
)

func NewFuncExternType(t TypeIdx) ExternType {
	e := ExternType{Kind: ExternFunc, index: t.Index}
	if t.Rec {
		e.flags |= externTypeRecursive
	}
	return e
}

func NewTableExternType(t TableType) ExternType {
	e := ExternType{Kind: ExternTable, value: RefVal(t.Ref), min: t.Limits.Min, max: t.Limits.Max}
	e.setLimitFlags(t.Limits)
	return e
}

func NewMemExternType(t MemType) ExternType {
	e := ExternType{Kind: ExternMem, min: t.Limits.Min, max: t.Limits.Max}
	e.setLimitFlags(t.Limits)
	if t.Shared {
		e.flags |= externTypeShared
	}
	return e
}

func NewGlobalExternType(t GlobalType) ExternType {
	e := ExternType{Kind: ExternGlobal, value: t.Type}
	if t.Mutable {
		e.flags |= externTypeMutable
	}
	return e
}

func NewTagExternType(t TagType) ExternType {
	e := ExternType{Kind: ExternTag, index: t.Type.Index}
	if t.Type.Rec {
		e.flags |= externTypeRecursive
	}
	return e
}

func (e *ExternType) setLimitFlags(l Limits) {
	if l.HasMax {
		e.flags |= externTypeHasMax
	}
	if l.Addr64 {
		e.flags |= externTypeAddr64
	}
}

func (e ExternType) limits() Limits {
	return Limits{
		Min: e.min, Max: e.max,
		HasMax: e.flags&externTypeHasMax != 0,
		Addr64: e.flags&externTypeAddr64 != 0,
	}
}

func (e ExternType) FuncType() TypeIdx {
	return TypeIdx{Index: e.index, Rec: e.flags&externTypeRecursive != 0}
}

func (e ExternType) TableType() TableType {
	return TableType{Ref: e.value.Ref(), Limits: e.limits()}
}

func (e ExternType) MemType() MemType {
	return MemType{Limits: e.limits(), Shared: e.flags&externTypeShared != 0}
}

func (e ExternType) GlobalType() GlobalType {
	return GlobalType{Type: e.value, Mutable: e.flags&externTypeMutable != 0}
}

func (e ExternType) TagType() TagType {
	return TagType{Type: TypeIdx{Index: e.index, Rec: e.flags&externTypeRecursive != 0}}
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
	// LocalDeclBytes is the length of the encoded local declarations. Branch-hint
	// offsets are relative to the start of those declarations, rather than to
	// BodyBytes (which starts immediately after them).
	LocalDeclBytes uint32
	// BodyBytes is the original expression bytecode, including the terminating
	// end opcode and excluding local declarations. DecodeModule always populates
	// this field for local function bodies.
	BodyBytes []byte
}
type CustomSec struct {
	Name string
	Data []byte
}

// BranchHint describes the likelihood of the condition of an if or br_if.
// Offset is relative to the beginning of the function's local declarations.
// A false Likely value means the condition is unlikely to be true.
type BranchHint struct {
	Offset uint32
	Likely bool
}

// FuncBranchHints groups the ordered branch hints for one defined function.
// FuncIndex uses the module's (import-inclusive) function index space.
type FuncBranchHints struct {
	FuncIndex uint32
	Hints     []BranchHint
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

	UsesCompactImports bool

	// BranchHints is the validated metadata.code.branch_hint custom section.
	// Nil means that the module did not contain the section.
	BranchHints []FuncBranchHints

	// structuralTypeCache is module-lifetime compiler state. Decoded/validated
	// modules are immutable while compiling; the cache fingerprints the outer
	// type slice so ordinary test/module copies with replacement type sections
	// cannot reuse stale identities.
	structuralTypeCache *structuralTypeKeyCache
}

func (m *Module) ImportedFuncCount() int { return m.importCount(ExternFunc) }

// BranchHintsForFunc returns the ordered metadata hints for funcIndex. It does
// not allocate; most modules have no branch-hint section, and the section's
// function entries are already required to be sorted.
func (m *Module) BranchHintsForFunc(funcIndex uint32) []BranchHint {
	for i := range m.BranchHints {
		if m.BranchHints[i].FuncIndex == funcIndex {
			return m.BranchHints[i].Hints
		}
		if m.BranchHints[i].FuncIndex > funcIndex {
			break
		}
	}
	return nil
}
func (m *Module) ImportedTableCount() int  { return m.importCount(ExternTable) }
func (m *Module) ImportedMemCount() int    { return m.importCount(ExternMem) }
func (m *Module) ImportedGlobalCount() int { return m.importCount(ExternGlobal) }
func (m *Module) ImportedTagCount() int    { return m.importCount(ExternTag) }
func (m *Module) importCount(k ExternKind) int {
	// Index-based iteration: Import is a large struct (~208 bytes), and these
	// counters are called frequently on the compile hot path, so ranging by value
	// would copy every import per call (shows up as runtime.duffcopy).
	n := 0
	for i := range m.Imports {
		if m.Imports[i].Type.Kind == k {
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

//go:generate go run ./internal/geninstrnames -in types.go -out instr_names_gen.go
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

	numInstrKinds // sentinel: count of InstrKind values, for lookup-table sizing
)

func (k InstrKind) String() string {
	if k < numInstrKinds {
		start, end := instrKindNameOffsets[k], instrKindNameOffsets[k+1]
		if start != end {
			return instrKindNameBlob[start:end]
		}
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

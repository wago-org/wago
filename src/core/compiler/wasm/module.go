package wasm

// WebAssembly value type encodings.
type ValType byte

const (
	I32       ValType = 0x7F
	I64       ValType = 0x7E
	F32       ValType = 0x7D
	F64       ValType = 0x7C
	V128      ValType = 0x7B
	FuncRef   ValType = 0x70
	ExternRef ValType = 0x6F
	typeVoid  ValType = 0x40 // empty block type
)

func (t ValType) String() string {
	switch t {
	case I32:
		return "i32"
	case I64:
		return "i64"
	case F32:
		return "f32"
	case F64:
		return "f64"
	case V128:
		return "v128"
	case FuncRef:
		return "funcref"
	case ExternRef:
		return "externref"
	default:
		return "invalid"
	}
}

func isNumOrVecType(t ValType) bool {
	return t == I32 || t == I64 || t == F32 || t == F64 || t == V128
}
func isRefType(t ValType) bool { return t == FuncRef || t == ExternRef }
func isValType(t ValType) bool { return isNumOrVecType(t) || isRefType(t) }

const (
	secCustom    = 0
	secType      = 1
	secImport    = 2
	secFunction  = 3
	secTable     = 4
	secMemory    = 5
	secGlobal    = 6
	secExport    = 7
	secStart     = 8
	secElement   = 9
	secCode      = 10
	secData      = 11
	secDataCount = 12
)

type FuncType struct {
	Params  []ValType
	Results []ValType
}

type Limits struct {
	Min    uint32
	Max    uint32
	HasMax bool
}

type TableType struct {
	Elem   ValType
	Limits Limits
}

type MemType struct {
	Limits Limits
}

type GlobalType struct {
	Val     ValType
	Mutable bool
}

type ExternKind byte

const (
	ExternFunc   ExternKind = 0
	ExternTable  ExternKind = 1
	ExternMem    ExternKind = 2
	ExternGlobal ExternKind = 3
)

type Import struct {
	Module    string
	Name      string
	Kind      ExternKind
	TypeIndex uint32     // ExternFunc
	Table     TableType  // ExternTable
	Mem       MemType    // ExternMem
	Global    GlobalType // ExternGlobal
}

type Global struct {
	Type GlobalType
	Init []byte
}

type Export struct {
	Name  string
	Kind  ExternKind
	Index uint32
}

type LocalEntry struct {
	Count uint32
	Type  ValType
}

type Code struct {
	Locals []LocalEntry
	Body   []byte
}

type Element struct {
	Flags    uint32
	TableIdx uint32
	Offset   []byte   // raw const expr (active segments)
	ElemType ValType  // funcref by default
	FuncIdx  []uint32 // for the func-index forms
	Exprs    [][]byte // raw element exprs (expression forms)
	Passive  bool
	Declared bool
}

type DataSegment struct {
	MemIdx  uint32
	Offset  []byte // raw const expr (active)
	Init    []byte
	Passive bool
}

type Custom struct {
	Name string
	Data []byte
}

type Module struct {
	Version uint32

	Types     []FuncType
	Imports   []Import
	Functions []uint32 // type index per locally-defined function
	Tables    []TableType
	Memories  []MemType
	Globals   []Global
	Exports   []Export
	Start     *uint32
	Elements  []Element
	Code      []Code
	Data      []DataSegment
	DataCount *uint32
	Customs   []Custom
}

func (m *Module) ImportedFuncCount() int {
	n := 0
	for i := range m.Imports {
		if m.Imports[i].Kind == ExternFunc {
			n++
		}
	}
	return n
}

func FuncTypeEqual(a, b *FuncType) bool {
	if len(a.Params) != len(b.Params) || len(a.Results) != len(b.Results) {
		return false
	}
	for i := range a.Params {
		if a.Params[i] != b.Params[i] {
			return false
		}
	}
	for i := range a.Results {
		if a.Results[i] != b.Results[i] {
			return false
		}
	}
	return true
}

// CanonicalTypeID returns the stable signature id used by call_indirect checks.
func (m *Module) CanonicalTypeID(typeIdx uint32) uint32 {
	target := &m.Types[typeIdx]
	for j := range m.Types {
		if FuncTypeEqual(&m.Types[j], target) {
			return uint32(j)
		}
	}
	return typeIdx
}

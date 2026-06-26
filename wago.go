// Package wago exposes compile, instantiate, and run helpers for WebAssembly modules.
package wago

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"math"
	"time"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/amd64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
)

// Value is a typed wasm call argument or result.
type Value struct {
	Type wasm.ValType
	Bits uint64
}

func I32(v int32) Value   { return Value{wasm.I32, uint64(uint32(v))} }
func I64(v int64) Value   { return Value{wasm.I64, uint64(v)} }
func F32(v float32) Value { return Value{wasm.F32, uint64(math.Float32bits(v))} }
func F64(v float64) Value { return Value{wasm.F64, math.Float64bits(v)} }

func (v Value) AsI32() int32   { return int32(uint32(v.Bits)) }
func (v Value) AsI64() int64   { return int64(v.Bits) }
func (v Value) AsF32() float32 { return math.Float32frombits(uint32(v.Bits)) }
func (v Value) AsF64() float64 { return math.Float64frombits(v.Bits) }

func (v Value) String() string {
	switch v.Type {
	case wasm.I64:
		return fmt.Sprintf("%d", v.AsI64())
	case wasm.F32:
		return fmt.Sprintf("%g", v.AsF32())
	case wasm.F64:
		return fmt.Sprintf("%g", v.AsF64())
	default:
		return fmt.Sprintf("%d", v.AsI32())
	}
}

// HostFunc handles a void host import with one i32 argument.
type HostFunc func(arg int32)

// Imports supplies host functions and globals by "module.name" import key.
type Imports struct {
	Funcs   map[string]HostFunc
	Globals map[string]GlobalImport
}

// GlobalImport is the initial value and type contract for an imported global.
// Bits uses wasm's raw numeric encoding: i32/f32 consume the low 32 bits
// (integer bits or IEEE-754 f32 bits), while i64/f64 consume all 64 bits
// (integer bits or IEEE-754 f64 bits). InstantiateWithImports copies Bits into
// an instance-local slot; the import is not retained or aliased for later
// host-side mutation.
type GlobalImport struct {
	Type    wasm.ValType
	Mutable bool
	Bits    uint64
}

type FuncSig struct{ Params, Results []wasm.ValType }

// ElemInit is active element-segment metadata. Base is the literal i32 offset.
// When HasOffsetGlobal is true, OffsetGlobal names an imported immutable global
// whose current instance slot is read during instantiation instead, after import
// values have been copied into globals storage.
type ElemInit struct {
	Base            uint32
	HasOffsetGlobal bool
	OffsetGlobal    int
	Funcs           []uint32
}

// DataInit is active data-segment metadata. Offset is the literal i32 offset.
// When HasOffsetGlobal is true, OffsetGlobal names an imported immutable global
// whose current instance slot is read during instantiation instead, after import
// values have been copied into globals storage.
type DataInit struct {
	Offset          uint32
	HasOffsetGlobal bool
	OffsetGlobal    int
	Bytes           []byte
}

// GlobalDef is the compact instantiate-time metadata for one wasm global.
// Each instance stores one 8-byte slot per global; i32/f32 use the low 32 bits.
// Bits is the literal initializer. When HasInitGlobal is true, InitGlobal names
// an earlier imported immutable global whose value is copied into this global's
// instance-local slot during instantiation; it is not a slot alias.
type GlobalDef struct {
	Type          wasm.ValType
	Mutable       bool
	Bits          uint64
	HasInitGlobal bool
	InitGlobal    int
}

// GlobalImportDef identifies one imported global slot in wasm global-index order.
type GlobalImportDef struct {
	Module  string
	Name    string
	Type    wasm.ValType
	Mutable bool
}

// Compiled is emitted machine code plus instantiate-time metadata.
type Compiled struct {
	Code       []byte
	Entry      []int          // entry offset per local function
	Funcs      []FuncSig      // signature per local function
	Imports    []string       // "module.name" per imported function
	Exports    map[string]int // exported function name -> global function index
	NumImports int

	GlobalImports []GlobalImportDef // imported global slots, preceding local globals
	Globals       []GlobalDef       // global slots in wasm global-index order
	GlobalExports map[string]int    // exported global name -> global index

	TableSize  int        // initial table length
	FuncTypeID []uint32   // canonical signature id per global function index
	Elems      []ElemInit // active element segments

	Data []DataInit // active data segments (copied into linear memory at instantiate)
}

type Timings struct{ Decode, Validate, Compile time.Duration }

// ImportedGlobalCount returns the number of imported globals at the front of
// the wasm global-index space.
func (c *Compiled) ImportedGlobalCount() int { return len(c.GlobalImports) }

// LocalGlobalCount returns the number of module-defined globals.
func (c *Compiled) LocalGlobalCount() int { return len(c.Globals) - len(c.GlobalImports) }

// GlobalSlot maps a wasm global index to its byte offset in instance storage.
func (c *Compiled) GlobalSlot(idx int) int { return idx * 8 }

// ExportedGlobal returns metadata for a named exported global.
func (c *Compiled) ExportedGlobal(name string) (GlobalDef, bool) {
	idx, ok := c.GlobalExports[name]
	if !ok || idx < 0 || idx >= len(c.Globals) {
		return GlobalDef{}, false
	}
	return c.Globals[idx], true
}

// Compile decodes, validates, and compiles a wasm module to native code.
func Compile(wasmBytes []byte) (*Compiled, error) {
	c, _, err := compile(wasmBytes, false)
	return c, err
}

// CompileTimed is Compile with per-phase timings.
func CompileTimed(wasmBytes []byte) (*Compiled, Timings, error) {
	return compile(wasmBytes, true)
}

// compile builds the serialized metadata needed to instantiate without re-decoding.
func compile(wasmBytes []byte, timed bool) (*Compiled, Timings, error) {
	var t Timings
	t0 := time.Now()
	m, err := wasm.Decode(wasmBytes)
	if err != nil {
		return nil, t, fmt.Errorf("decode: %w", err)
	}
	t1 := time.Now()
	if err := wasm.Validate(m); err != nil {
		return nil, t, fmt.Errorf("validate: %w", err)
	}
	t2 := time.Now()
	if err := rejectUnsupportedGlobalTypes(m); err != nil {
		return nil, t, err
	}
	cm, err := amd64.CompileModule(m)
	if err != nil {
		return nil, t, fmt.Errorf("compile: %w", err)
	}
	if timed {
		t = Timings{t1.Sub(t0), t2.Sub(t1), time.Since(t2)}
	}

	c := &Compiled{Code: cm.Code, Entry: cm.Entry, NumImports: m.ImportedFuncCount(), Exports: map[string]int{}, GlobalExports: map[string]int{}}
	for i := range m.Imports {
		switch m.Imports[i].Kind {
		case wasm.ExternFunc:
			c.Imports = append(c.Imports, m.Imports[i].Module+"."+m.Imports[i].Name)
		case wasm.ExternGlobal:
			imp := GlobalImportDef{Module: m.Imports[i].Module, Name: m.Imports[i].Name, Type: m.Imports[i].Global.Val, Mutable: m.Imports[i].Global.Mutable}
			c.GlobalImports = append(c.GlobalImports, imp)
			c.Globals = append(c.Globals, GlobalDef{Type: imp.Type, Mutable: imp.Mutable})
		}
	}
	for li := range m.Functions {
		ft := &m.Types[m.Functions[li]]
		c.Funcs = append(c.Funcs, FuncSig{ft.Params, ft.Results})
	}
	for i := range m.Globals {
		v, err := evalConstExprWithModule(m.Globals[i].Init, m.Globals[i].Type.Val, m)
		if err != nil {
			return nil, t, fmt.Errorf("global %d initializer: %w", i, err)
		}
		g := GlobalDef{Type: m.Globals[i].Type.Val, Mutable: m.Globals[i].Type.Mutable, Bits: v.Bits}
		if v.GlobalIndex >= 0 {
			g.HasInitGlobal = true
			g.InitGlobal = v.GlobalIndex
		}
		c.Globals = append(c.Globals, g)
	}
	for i := range m.Exports {
		switch m.Exports[i].Kind {
		case wasm.ExternFunc:
			c.Exports[m.Exports[i].Name] = int(m.Exports[i].Index)
		case wasm.ExternGlobal:
			c.GlobalExports[m.Exports[i].Name] = int(m.Exports[i].Index)
		}
	}

	if len(m.Tables) > 0 {
		c.TableSize = int(m.Tables[0].Limits.Min)
	}
	// Table 0 is the only table wired through the current runtime ABI.
	for i := range m.Imports {
		if m.Imports[i].Kind == wasm.ExternFunc {
			c.FuncTypeID = append(c.FuncTypeID, m.CanonicalTypeID(m.Imports[i].TypeIndex))
		}
	}
	for li := range m.Functions {
		c.FuncTypeID = append(c.FuncTypeID, m.CanonicalTypeID(m.Functions[li]))
	}
	for i := range m.Elements {
		e := &m.Elements[i]
		if e.Passive || e.Declared || len(e.FuncIdx) == 0 {
			continue // only active func-index segments
		}
		base, err := evalConstExprWithModule(e.Offset, wasm.I32, m)
		if err != nil {
			return nil, t, fmt.Errorf("element %d offset: %w", i, err)
		}
		init := ElemInit{Base: uint32(base.Bits), Funcs: e.FuncIdx}
		if base.GlobalIndex >= 0 {
			init.HasOffsetGlobal = true
			init.OffsetGlobal = base.GlobalIndex
		}
		c.Elems = append(c.Elems, init)
	}
	for i := range m.Data {
		d := &m.Data[i]
		if d.Passive {
			continue
		}
		off, err := evalConstExprWithModule(d.Offset, wasm.I32, m)
		if err != nil {
			return nil, t, fmt.Errorf("data %d offset: %w", i, err)
		}
		init := DataInit{Offset: uint32(off.Bits), Bytes: d.Init}
		if off.GlobalIndex >= 0 {
			init.HasOffsetGlobal = true
			init.OffsetGlobal = off.GlobalIndex
		}
		c.Data = append(c.Data, init)
	}
	return c, t, nil
}

func rejectUnsupportedGlobalTypes(m *wasm.Module) error {
	for i := range m.Imports {
		if m.Imports[i].Kind == wasm.ExternGlobal && !wasm.IsNumericGlobalType(m.Imports[i].Global.Val) {
			return fmt.Errorf("unsupported global type %s for import %q.%q", m.Imports[i].Global.Val, m.Imports[i].Module, m.Imports[i].Name)
		}
	}
	for i := range m.Globals {
		if !wasm.IsNumericGlobalType(m.Globals[i].Type.Val) {
			return fmt.Errorf("unsupported global type %s for global %d", m.Globals[i].Type.Val, i)
		}
	}
	return nil
}

func evalI32ConstExpr(b []byte) (uint32, error) {
	v, err := evalConstExpr(b, wasm.I32)
	if err != nil {
		return 0, err
	}
	return uint32(v.Bits), nil
}

type constExprResult struct {
	Value
	GlobalIndex int
}

func evalConstExpr(b []byte, want wasm.ValType) (Value, error) {
	res, err := evalConstExprWithModule(b, want, nil)
	return res.Value, err
}

// evalConstExprWithModule intentionally stays narrower than full wasm validation:
// wasm.Validate checks const-expression shape/type rules before compile reaches
// here, while this helper decodes the supported MVP operators into
// instantiate-time bits or clear unsupported-expression errors.
func evalConstExprWithModule(b []byte, want wasm.ValType, m *wasm.Module) (constExprResult, error) {
	r := wasm.NewReader(b)
	op, err := r.Byte()
	if err != nil {
		return constExprResult{}, err
	}
	got := constExprResult{GlobalIndex: -1}
	switch op {
	case 0x41: // i32.const
		v, err := r.I32()
		if err != nil {
			return constExprResult{}, err
		}
		got.Value = Value{Type: wasm.I32, Bits: uint64(uint32(v))}
	case 0x42: // i64.const
		v, err := r.I64()
		if err != nil {
			return constExprResult{}, err
		}
		got.Value = Value{Type: wasm.I64, Bits: uint64(v)}
	case 0x43: // f32.const
		bb, err := r.Bytes(4)
		if err != nil {
			return constExprResult{}, err
		}
		got.Value = Value{Type: wasm.F32, Bits: uint64(binary.LittleEndian.Uint32(bb))}
	case 0x44: // f64.const
		bb, err := r.Bytes(8)
		if err != nil {
			return constExprResult{}, err
		}
		got.Value = Value{Type: wasm.F64, Bits: binary.LittleEndian.Uint64(bb)}
	case 0x23: // global.get
		if m == nil {
			return constExprResult{}, fmt.Errorf("unsupported const expression opcode 0x%02x", op)
		}
		x, err := r.U32()
		if err != nil {
			return constExprResult{}, err
		}
		gt, ok := m.GlobalType(x)
		if !ok || int(x) >= m.ImportedGlobalCount() || gt.Mutable {
			return constExprResult{}, fmt.Errorf("unsupported const expression global.get %d", x)
		}
		got.Value = Value{Type: gt.Val}
		got.GlobalIndex = int(x)
	default:
		return constExprResult{}, fmt.Errorf("unsupported const expression opcode 0x%02x", op)
	}
	end, err := r.Byte()
	if err != nil {
		return constExprResult{}, fmt.Errorf("const expression missing end: %w", err)
	}
	if end != 0x0B {
		return constExprResult{}, fmt.Errorf("const expression missing end")
	}
	if r.BytesLeft() != 0 {
		return constExprResult{}, fmt.Errorf("const expression has trailing bytes")
	}
	if got.Type != want {
		return constExprResult{}, fmt.Errorf("const expression type %s, want %s", got.Type, want)
	}
	return got, nil
}

// Signature returns the parameter and result types of an exported function.
func (c *Compiled) Signature(export string) (params, results []wasm.ValType, err error) {
	li, err := c.localIndex(export)
	if err != nil {
		return nil, nil, err
	}
	return c.Funcs[li].Params, c.Funcs[li].Results, nil
}

func (c *Compiled) localIndex(export string) (int, error) {
	gfi, ok := c.Exports[export]
	if !ok {
		return 0, fmt.Errorf("no exported function %q", export)
	}
	li := gfi - c.NumImports
	if li < 0 || li >= len(c.Funcs) {
		return 0, fmt.Errorf("export %q is an imported function", export)
	}
	return li, nil
}

func (c *Compiled) importedGlobalBits(imports Imports) ([]uint64, error) {
	bits := make([]uint64, len(c.GlobalImports))
	for i, imp := range c.GlobalImports {
		key := imp.Module + "." + imp.Name
		provided, ok := imports.Globals[key]
		if !ok {
			return nil, fmt.Errorf("missing imported global %q", key)
		}
		if provided.Type != imp.Type {
			return nil, fmt.Errorf("imported global %q has type %s, want %s", key, provided.Type, imp.Type)
		}
		if provided.Mutable != imp.Mutable {
			return nil, fmt.Errorf("imported global %q mutability mismatch", key)
		}
		bits[i] = normalizeGlobalBits(imp.Type, provided.Bits)
	}
	return bits, nil
}

func normalizeGlobalBits(t wasm.ValType, bits uint64) uint64 {
	if t == wasm.I32 || t == wasm.F32 {
		return uint64(uint32(bits))
	}
	return bits
}

func readGlobalSlot(globals []byte, idx int, t wasm.ValType) uint64 {
	return normalizeGlobalBits(t, binary.LittleEndian.Uint64(globals[idx*8:]))
}

func writeGlobalSlot(globals []byte, idx int, t wasm.ValType, bits uint64) {
	binary.LittleEndian.PutUint64(globals[idx*8:], normalizeGlobalBits(t, bits))
}

const wagoMagic = "WAGO"
const wagoVersion = 3

// plain avoids recursive gob encoding through MarshalBinary.
type plain Compiled

// MarshalBinary serializes the precompiled module to a ".wago" blob.
func (c *Compiled) MarshalBinary() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(wagoMagic)
	buf.WriteByte(wagoVersion)
	if err := gob.NewEncoder(&buf).Encode((*plain)(c)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// UnmarshalBinary loads a ".wago" blob produced by MarshalBinary.
func (c *Compiled) UnmarshalBinary(data []byte) error {
	if !IsCompiled(data) {
		return fmt.Errorf("not a wago module")
	}
	if data[4] != wagoVersion {
		return fmt.Errorf("wago module version %d unsupported (want %d)", data[4], wagoVersion)
	}
	return gob.NewDecoder(bytes.NewReader(data[5:])).Decode((*plain)(c))
}

// IsCompiled reports whether b is a precompiled wago module (vs raw wasm).
func IsCompiled(b []byte) bool { return len(b) >= 5 && string(b[:4]) == wagoMagic }

// Load returns a *Compiled from either a precompiled ".wago" blob or raw wasm
// (which it compiles).
func Load(b []byte) (*Compiled, error) {
	if IsCompiled(b) {
		c := &Compiled{}
		return c, c.UnmarshalBinary(b)
	}
	return Compile(b)
}

// Instance is ready for repeated Invoke calls.
type Instance struct {
	c                      *Compiled
	eng                    *runtime.Engine
	jm                     *runtime.JobMemory
	ar                     *runtime.Arena
	base                   uintptr
	mem                    []byte
	hosts                  map[string]HostFunc
	hostLog                []byte
	globals                []byte
	serArgs, results, trap []byte
}

// Instantiate maps code, initializes memory/table state, and allocates call buffers.
func Instantiate(c *Compiled, hosts map[string]HostFunc) (*Instance, error) {
	return InstantiateWithImports(c, Imports{Funcs: hosts})
}

// InstantiateWithImports maps code and supplies host functions and globals.
func InstantiateWithImports(c *Compiled, imports Imports) (*Instance, error) {
	importGlobalBits, err := c.importedGlobalBits(imports)
	if err != nil {
		return nil, err
	}
	eng, err := runtime.NewEngine()
	if err != nil {
		return nil, err
	}
	jm, err := runtime.NewJobMemory(1 << 16)
	if err != nil {
		eng.Close()
		return nil, err
	}
	ar, err := runtime.NewArena(1 << 20)
	if err != nil {
		jm.Close()
		eng.Close()
		return nil, err
	}
	mem, base, err := runtime.MapCode(c.Code)
	if err != nil {
		ar.Close()
		jm.Close()
		eng.Close()
		return nil, err
	}
	success := false
	defer func() {
		if success {
			return
		}
		runtime.Unmap(mem)
		ar.Close()
		jm.Close()
		eng.Close()
	}()
	const maxEntries = (1 << 16) / 8
	hostLog := ar.Alloc(8 + maxEntries*8)
	jm.SetCustomCtx(uintptr(unsafe.Pointer(&hostLog[0])))

	var globals []byte
	if len(c.Globals) > 0 {
		globals = ar.Alloc(8 * len(c.Globals))
		for i, g := range c.Globals {
			bits := g.Bits
			if i < len(importGlobalBits) {
				bits = importGlobalBits[i]
			} else if g.HasInitGlobal {
				if g.InitGlobal < 0 || g.InitGlobal >= i {
					return nil, fmt.Errorf("global %d initializer references unavailable global %d", i, g.InitGlobal)
				}
				bits = readGlobalSlot(globals, g.InitGlobal, g.Type)
			}
			writeGlobalSlot(globals, i, g.Type, bits)
		}
		jm.SetGlobalsPtr(uintptr(unsafe.Pointer(&globals[0])))
	}

	// Table descriptor: [len u32][pad][entry...], entry {codePtr u64, sigID u32, pad u32}.
	if c.TableSize > 0 || len(c.Elems) > 0 {
		size := c.TableSize
		desc := ar.Alloc(8 + size*16)
		binary.LittleEndian.PutUint32(desc, uint32(size))
		for seg, el := range c.Elems {
			elemBase := el.Base
			if el.HasOffsetGlobal {
				if el.OffsetGlobal < 0 || el.OffsetGlobal >= len(c.Globals) || el.OffsetGlobal*8+8 > len(globals) {
					return nil, fmt.Errorf("element offset global %d out of range", el.OffsetGlobal)
				}
				elemBase = uint32(readGlobalSlot(globals, el.OffsetGlobal, c.Globals[el.OffsetGlobal].Type))
			}
			end := uint64(elemBase) + uint64(len(el.Funcs))
			if end > uint64(size) {
				return nil, fmt.Errorf("active element segment %d out of bounds: offset %d + length %d > table size %d", seg, elemBase, len(el.Funcs), size)
			}
			for k, fidx := range el.Funcs {
				slot := int(elemBase) + k
				off := 8 + slot*16
				if li := int(fidx) - c.NumImports; li >= 0 && li < len(c.Entry) {
					binary.LittleEndian.PutUint64(desc[off:], uint64(base)+uint64(c.Entry[li]))
				}
				if int(fidx) < len(c.FuncTypeID) {
					binary.LittleEndian.PutUint32(desc[off+8:], c.FuncTypeID[fidx])
				}
			}
		}
		jm.SetTablePtr(uintptr(unsafe.Pointer(&desc[0])))
	}

	if len(c.Data) > 0 {
		lin := jm.LinearMemory()
		for seg, d := range c.Data {
			off := d.Offset
			if d.HasOffsetGlobal {
				if d.OffsetGlobal < 0 || d.OffsetGlobal >= len(c.Globals) || d.OffsetGlobal*8+8 > len(globals) {
					return nil, fmt.Errorf("data offset global %d out of range", d.OffsetGlobal)
				}
				off = uint32(readGlobalSlot(globals, d.OffsetGlobal, c.Globals[d.OffsetGlobal].Type))
			}
			end := uint64(off) + uint64(len(d.Bytes))
			if end > uint64(len(lin)) {
				return nil, fmt.Errorf("active data segment %d out of bounds: offset %d + length %d > memory size %d", seg, off, len(d.Bytes), len(lin))
			}
			copy(lin[off:end], d.Bytes)
		}
	}

	success = true
	return &Instance{
		c: c, eng: eng, jm: jm, ar: ar, base: base, mem: mem, hosts: imports.Funcs, hostLog: hostLog, globals: globals,
		serArgs: ar.Alloc(512), results: ar.Alloc(512), trap: ar.Alloc(8),
	}, nil
}

// Close releases the instance's mapped code and memory.
func (in *Instance) Close() {
	runtime.Unmap(in.mem)
	in.ar.Close()
	in.jm.Close()
	in.eng.Close()
}

// LinearMemory exposes the instance's linear memory for zero-copy access.
func (in *Instance) LinearMemory() []byte { return in.jm.LinearMemory() }

// Global returns the current value of an exported global.
func (in *Instance) Global(name string) (Value, error) {
	idx, ok := in.c.GlobalExports[name]
	if !ok {
		if _, isFunc := in.c.Exports[name]; isFunc {
			return Value{}, fmt.Errorf("export %q is a function, not a global", name)
		}
		return Value{}, fmt.Errorf("no exported global %q", name)
	}
	if idx < 0 || idx >= len(in.c.Globals) || idx*8+8 > len(in.globals) {
		return Value{}, fmt.Errorf("exported global %q index %d out of range", name, idx)
	}
	g := in.c.Globals[idx]
	return Value{Type: g.Type, Bits: readGlobalSlot(in.globals, idx, g.Type)}, nil
}

// SetGlobal updates an exported mutable global.
func (in *Instance) SetGlobal(name string, v Value) error {
	idx, ok := in.c.GlobalExports[name]
	if !ok {
		if _, isFunc := in.c.Exports[name]; isFunc {
			return fmt.Errorf("export %q is a function, not a global", name)
		}
		return fmt.Errorf("no exported global %q", name)
	}
	if idx < 0 || idx >= len(in.c.Globals) || idx*8+8 > len(in.globals) {
		return fmt.Errorf("exported global %q index %d out of range", name, idx)
	}
	g := in.c.Globals[idx]
	if !g.Mutable {
		return fmt.Errorf("exported global %q is immutable", name)
	}
	if v.Type != g.Type {
		return fmt.Errorf("exported global %q has type %s, got %s", name, g.Type, v.Type)
	}
	writeGlobalSlot(in.globals, idx, g.Type, v.Bits)
	return nil
}

// Invoke marshals slot-based arguments/results around one native WasmWrapper call.
func (in *Instance) Invoke(export string, args ...Value) ([]Value, error) {
	li, err := in.c.localIndex(export)
	if err != nil {
		return nil, err
	}
	sig := in.c.Funcs[li]
	if len(args) != len(sig.Params) {
		return nil, fmt.Errorf("%s expects %d arg(s), got %d", export, len(sig.Params), len(args))
	}
	for i, a := range args {
		binary.LittleEndian.PutUint64(in.serArgs[i*8:], a.Bits)
	}
	binary.LittleEndian.PutUint32(in.hostLog, 0) // reset host-call log
	entry := in.base + uintptr(in.c.Entry[li])
	if err := in.eng.Call(entry, in.serArgs, in.jm.LinearMemory(), in.trap, in.results); err != nil {
		return nil, err
	}
	n := binary.LittleEndian.Uint32(in.hostLog)
	for i := uint32(0); i < n; i++ {
		off := 8 + i*8
		imp := binary.LittleEndian.Uint32(in.hostLog[off:])
		arg := int32(binary.LittleEndian.Uint32(in.hostLog[off+4:]))
		if int(imp) < len(in.c.Imports) {
			if fn := in.hosts[in.c.Imports[imp]]; fn != nil {
				fn(arg)
			}
		}
	}
	out := make([]Value, len(sig.Results))
	for i, rt := range sig.Results {
		switch rt {
		case wasm.I64, wasm.F64:
			out[i] = Value{rt, binary.LittleEndian.Uint64(in.results[i*8:])}
		default: // i32 / f32 (4-byte)
			out[i] = Value{rt, uint64(binary.LittleEndian.Uint32(in.results[i*8:]))}
		}
	}
	return out, nil
}

// RunValuesWithHost compiles (or loads) and invokes an export in one shot.
func RunValuesWithHost(wasmBytes []byte, hosts map[string]HostFunc, export string, args ...Value) ([]Value, error) {
	c, err := Load(wasmBytes)
	if err != nil {
		return nil, err
	}
	in, err := Instantiate(c, hosts)
	if err != nil {
		return nil, err
	}
	defer in.Close()
	return in.Invoke(export, args...)
}

// RunValues is RunValuesWithHost with no host imports.
func RunValues(wasmBytes []byte, export string, args ...Value) ([]Value, error) {
	return RunValuesWithHost(wasmBytes, nil, export, args...)
}

// Run is a convenience wrapper for i32 arguments and int64 results.
func Run(wasmBytes []byte, export string, args ...int32) ([]int64, error) {
	return RunWithHost(wasmBytes, nil, export, args...)
}

// RunWithHost is Run with host imports wired by "module.name".
func RunWithHost(wasmBytes []byte, hosts map[string]HostFunc, export string, args ...int32) ([]int64, error) {
	vals := make([]Value, len(args))
	for i, a := range args {
		vals[i] = I32(a)
	}
	res, err := RunValuesWithHost(wasmBytes, hosts, export, vals...)
	if err != nil {
		return nil, err
	}
	out := make([]int64, len(res))
	for i, v := range res {
		switch v.Type {
		case wasm.I64, wasm.F64:
			out[i] = int64(v.Bits)
		case wasm.F32:
			out[i] = int64(uint32(v.Bits))
		default:
			out[i] = int64(int32(uint32(v.Bits)))
		}
	}
	return out, nil
}

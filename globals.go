package wago

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/wago-org/wago/src/core/compiler/wasm"
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

package wago

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
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

// Global is a wasm global object that can be imported by one or more module
// instances. Mutable imported globals are shared by object identity: writes from
// wasm, host accessors, or another instance importing the same *Global observe
// the same storage.
type Global struct {
	Type    wasm.ValType
	Mutable bool
	cell    []byte
	arena   *coreruntime.Arena
}

// NewGlobal constructs a host-owned wasm global object in stable off-heap
// storage suitable for native code. Close releases that storage when no
// instance can access this global anymore.
func NewGlobal(v Value, mutable bool) *Global {
	arena, err := coreruntime.NewArena(8)
	if err != nil {
		panic(fmt.Sprintf("global allocation failed: %v", err))
	}
	return newGlobalInCell(v, mutable, arena.Alloc(8), arena)
}

func newGlobalInCell(v Value, mutable bool, cell []byte, arena *coreruntime.Arena) *Global {
	g := &Global{Type: v.Type, Mutable: mutable, cell: cell, arena: arena}
	writeGlobalObject(g, v.Type, v.Bits)
	return g
}

// Close releases storage owned by a host-created global. It must only be called
// after all instances importing this global have been closed.
func (g *Global) Close() error {
	if g == nil || g.arena == nil {
		return nil
	}
	err := g.arena.Close()
	g.arena = nil
	g.cell = nil
	return err
}

// Value returns the current raw typed value of the global.
func (g *Global) Value() Value {
	if g == nil {
		return Value{}
	}
	return Value{Type: g.Type, Bits: readGlobalObject(g, g.Type)}
}

// Set updates a mutable host-owned global object.
func (g *Global) Set(v Value) error {
	if g == nil {
		return fmt.Errorf("global is nil")
	}
	if !g.Mutable {
		return fmt.Errorf("global is immutable")
	}
	if v.Type != g.Type {
		return fmt.Errorf("global has type %s, got %s", g.Type, v.Type)
	}
	writeGlobalObject(g, g.Type, v.Bits)
	return nil
}

// GlobalImport supplies an imported global. Prefer Global for mutable imports
// so aliases across duplicate imports and instances share one wasm global
// object. Type/Mutable/Bits remain as a convenience for immutable globals and
// one-shot tests; InstantiateWithImports materializes one shared object per
// import key for those values.
type GlobalImport struct {
	Type    wasm.ValType
	Mutable bool
	Bits    uint64
	Global  *Global
}

type FuncSig struct{ Params, Results []wasm.ValType }

// OffsetInit is active data/element offset metadata. Base is the literal i32
// offset. When HasGlobal is true, Global names an imported immutable i32 global
// whose current instance cell is read during instantiation instead, after import
// values have been resolved.
type OffsetInit struct {
	Base      uint32
	HasGlobal bool
	Global    int
}

// ElemInit is active element-segment metadata.
type ElemInit struct {
	Offset OffsetInit
	Funcs  []uint32
}

// DataInit is active data-segment metadata.
type DataInit struct {
	Offset OffsetInit
	Bytes  []byte
}

// GlobalDef is the compact instantiate-time metadata for one wasm global.
// Each instance stores one pointer-table entry per global; i32/f32 use the low
// 32 bits of the pointed-to 8-byte cell. Bits is the literal initializer. When
// HasInitGlobal is true, InitGlobal names an earlier imported immutable global
// whose current value is copied into this global's own local cell during
// instantiation; it is not a slot alias.
type GlobalDef struct {
	Type          wasm.ValType
	Mutable       bool
	Bits          uint64
	HasInitGlobal bool
	InitGlobal    int
}

// GlobalImportDef identifies one imported global entry in wasm global-index order.
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

	GlobalImports []GlobalImportDef // imported global entries, preceding local globals
	Globals       []GlobalDef       // global entries in wasm global-index order
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

// GlobalSlot maps a wasm global index to its pointer-table byte offset.
func (c *Compiled) GlobalSlot(idx int) int { return idx * 8 }

// ExportedGlobal returns metadata for a named exported global.
func (c *Compiled) ExportedGlobal(name string) (GlobalDef, bool) {
	idx, ok := c.GlobalExports[name]
	if !ok || idx < 0 || idx >= len(c.Globals) {
		return GlobalDef{}, false
	}
	return c.Globals[idx], true
}

type resolvedGlobalImport struct {
	global  *Global
	initial Value
	mutable bool
}

func (c *Compiled) importedGlobals(imports Imports) ([]*resolvedGlobalImport, error) {
	// Global imports use the public API's "module.name" map key. Duplicate
	// imports of the same key intentionally resolve to the same descriptor so
	// wasm global object identity is preserved.
	globals := make([]*resolvedGlobalImport, len(c.GlobalImports))
	byKey := map[string]*resolvedGlobalImport{}
	for i, imp := range c.GlobalImports {
		key := imp.Module + "." + imp.Name
		if g := byKey[key]; g != nil {
			if err := validateResolvedImportedGlobal(key, g, imp); err != nil {
				return nil, err
			}
			globals[i] = g
			continue
		}
		provided, ok := imports.Globals[key]
		if !ok {
			return nil, fmt.Errorf("missing imported global %q", key)
		}
		g := &resolvedGlobalImport{global: provided.Global, initial: Value{Type: provided.Type, Bits: provided.Bits}, mutable: provided.Mutable}
		if err := validateResolvedImportedGlobal(key, g, imp); err != nil {
			return nil, err
		}
		byKey[key] = g
		globals[i] = g
	}
	return globals, nil
}

func validateResolvedImportedGlobal(key string, g *resolvedGlobalImport, imp GlobalImportDef) error {
	if g == nil {
		return fmt.Errorf("imported global %q is nil", key)
	}
	if g.global != nil {
		return validateImportedGlobal(key, g.global, imp)
	}
	if g.initial.Type != imp.Type {
		return fmt.Errorf("imported global %q has type %s, want %s", key, g.initial.Type, imp.Type)
	}
	if g.mutable != imp.Mutable {
		return fmt.Errorf("imported global %q mutability mismatch", key)
	}
	return nil
}

func validateImportedGlobal(key string, g *Global, imp GlobalImportDef) error {
	if g == nil {
		return fmt.Errorf("imported global %q is nil", key)
	}
	if len(g.cell) < 8 {
		return fmt.Errorf("imported global %q storage is closed", key)
	}
	if g.Type != imp.Type {
		return fmt.Errorf("imported global %q has type %s, want %s", key, g.Type, imp.Type)
	}
	if g.Mutable != imp.Mutable {
		return fmt.Errorf("imported global %q mutability mismatch", key)
	}
	return nil
}

func normalizeGlobalBits(t wasm.ValType, bits uint64) uint64 {
	if t == wasm.I32 || t == wasm.F32 {
		return uint64(uint32(bits))
	}
	return bits
}

func readGlobalObject(g *Global, t wasm.ValType) uint64 {
	if g == nil || len(g.cell) < 8 {
		return 0
	}
	return normalizeGlobalBits(t, binary.LittleEndian.Uint64(g.cell))
}

func writeGlobalObject(g *Global, t wasm.ValType, bits uint64) {
	binary.LittleEndian.PutUint64(g.cell, normalizeGlobalBits(t, bits))
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
	if idx < 0 || idx >= len(in.c.Globals) || idx >= len(in.globalCells) || in.globalCells[idx] == nil {
		return Value{}, fmt.Errorf("exported global %q index %d out of range", name, idx)
	}
	g := in.c.Globals[idx]
	return Value{Type: g.Type, Bits: readGlobalObject(in.globalCells[idx], g.Type)}, nil
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
	if idx < 0 || idx >= len(in.c.Globals) || idx >= len(in.globalCells) || in.globalCells[idx] == nil {
		return fmt.Errorf("exported global %q index %d out of range", name, idx)
	}
	g := in.c.Globals[idx]
	if !g.Mutable {
		return fmt.Errorf("exported global %q is immutable", name)
	}
	if v.Type != g.Type {
		return fmt.Errorf("exported global %q has type %s, got %s", name, g.Type, v.Type)
	}
	writeGlobalObject(in.globalCells[idx], g.Type, v.Bits)
	return nil
}

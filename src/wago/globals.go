package wago

import (
	"encoding/binary"
	"fmt"
	"math"
	"sync"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/gc"
)

// Call arguments and results are raw uint64s, wazero-style: the function
// signature defines how each is interpreted (i32 in the low 32 bits, floats as
// their IEEE-754 bits). These helpers encode a typed value into / decode it from
// that representation.
func I32(v int32) uint64   { return uint64(uint32(v)) }
func I64(v int64) uint64   { return uint64(v) }
func F32(v float32) uint64 { return uint64(math.Float32bits(v)) }
func F64(v float64) uint64 { return math.Float64bits(v) }

func AsI32(b uint64) int32   { return int32(uint32(b)) }
func AsI64(b uint64) int64   { return int64(b) }
func AsF32(b uint64) float32 { return math.Float32frombits(uint32(b)) }
func AsF64(b uint64) float64 { return math.Float64frombits(b) }

func valTypeEqual(a, b wasm.ValType) bool { return wasm.EqualValType(a, b) }

func valTypeCode(t wasm.ValType) byte {
	b, _ := wasm.EncodeValType(t)
	return b
}

// HostFunc handles a void host import with one i32 argument.
type HostFunc func(arg int32)

// Imports supplies a module's imports by "module.name" key, JS-style: one
// namespace whose values may be a HostFunc, a GlobalImport or *Global, or a
// *Memory — mirroring the WebAssembly JS API's single imports object.
type Imports map[string]any

// hostFuncs extracts the HostFunc entries (the import-function wiring).
func (im Imports) hostFuncs() map[string]HostFunc {
	var m map[string]HostFunc
	for k, v := range im {
		if fn, ok := v.(HostFunc); ok {
			if m == nil {
				m = make(map[string]HostFunc, len(im))
			}
			m[k] = fn
		}
	}
	return m
}

// global returns the imported global for key, accepting either a GlobalImport
// value or a *Global object.
func (im Imports) global(key string) (GlobalImport, bool) {
	switch g := im[key].(type) {
	case GlobalImport:
		return g, true
	case *Global:
		return GlobalImport{Type: g.Type, Mutable: g.Mutable, Global: g}, true
	default:
		return GlobalImport{}, false
	}
}

// Global is a wasm global object that can be imported by one or more module
// instances. Mutable imported globals are shared by object identity: writes from
// wasm, host accessors, or another instance importing the same *Global observe
// the same storage.
type Global struct {
	Type    ValType
	Mutable bool
	cell    []byte
	arena   *coreruntime.Arena
}

// NewGlobalI32/I64/F32/F64 construct a host-owned wasm global of the named type.
// Close releases its storage when no instance can access it anymore.
func NewGlobalI32(v int32, mutable bool) *Global   { return newGlobal(ValI32, I32(v), mutable) }
func NewGlobalI64(v int64, mutable bool) *Global   { return newGlobal(ValI64, I64(v), mutable) }
func NewGlobalF32(v float32, mutable bool) *Global { return newGlobal(ValF32, F32(v), mutable) }
func NewGlobalF64(v float64, mutable bool) *Global { return newGlobal(ValF64, F64(v), mutable) }

func newGlobal(t ValType, bits uint64, mutable bool) *Global {
	arena, err := coreruntime.NewArena(8)
	if err != nil {
		panic(fmt.Sprintf("global allocation failed: %v", err))
	}
	return newGlobalInCell(t, bits, mutable, arena.Alloc(8), arena)
}

func newGlobalInCell(t ValType, bits uint64, mutable bool, cell []byte, arena *coreruntime.Arena) *Global {
	g := &Global{Type: t, Mutable: mutable, cell: cell, arena: arena}
	writeGlobalObject(g, t, bits)
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

// Get returns the global's current value as raw bits (decode with AsI32/etc).
func (g *Global) Get() uint64 {
	if g == nil {
		return 0
	}
	return readGlobalObject(g, g.Type)
}

// Set updates a mutable host-owned global; bits are interpreted as the global's
// type.
func (g *Global) Set(bits uint64) error {
	if g == nil {
		return fmt.Errorf("global is nil")
	}
	if !g.Mutable {
		return fmt.Errorf("global is immutable")
	}
	writeGlobalObject(g, g.Type, bits)
	return nil
}

// GlobalImport supplies an imported global value. Prefer a *Global for mutable
// imports so aliases across duplicate imports and instances share one wasm
// global object; Type/Mutable/Bits are a convenience for immutable globals.
type GlobalImport struct {
	Type    ValType
	Mutable bool
	Bits    uint64
	Global  *Global
}

type FuncSig struct{ Params, Results []ValType }

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
	Type          ValType
	Mutable       bool
	Bits          uint64
	HasInitGlobal bool
	InitGlobal    int
}

// GlobalImportDef identifies one imported global entry in wasm global-index order.
type GlobalImportDef struct {
	Module  string
	Name    string
	Type    ValType
	Mutable bool
}

// Compiled is emitted machine code plus instantiate-time metadata.
type Compiled struct {
	Code  []byte
	Entry []int // entry offset per local function
	// InternalEntry mirrors Entry with each function's register-ABI internal
	// entry offset (== Entry[i] when none): indirect calls to compatible
	// signatures bypass the wrapper adapter via the table's delta field.
	InternalEntry []int
	Funcs         []FuncSig      // signature per local function
	Imports       []string       // "module.name" per imported function
	Exports       map[string]int // exported function name -> global function index
	NumImports    int
	Names         *wasm.NameSec // parsed debug names from the wasm name custom section

	GlobalImports []GlobalImportDef // imported global entries, preceding local globals
	Globals       []GlobalDef       // global entries in wasm global-index order
	GlobalExports map[string]int    // exported global name -> global index

	HasTable   bool       // true when table 0 is declared, even with minimum length 0
	TableSize  int        // initial table length
	FuncTypeID []uint32   // canonical signature id per global function index
	Elems      []ElemInit // active element segments

	Data []DataInit // active data segments (copied into linear memory at instantiate)

	HasMemory   bool   // module declares a linear memory
	MemMinPages uint32 // initial linear-memory size (pages); allocated at instantiate
	MemMaxPages uint32 // grow ceiling (pages); 0 means use the engine default

	HasStart       bool // module declares a start function to run at instantiate
	StartLocalFunc int  // its local function index (valid when HasStart && !StartIsImport)
	StartIsImport  bool // start is an imported function (run its host binding at instantiate)
	StartImportIdx int  // its imported-function index (valid when HasStart && StartIsImport)

	// boundsMode records how this code was compiled: BoundsChecksSignalsBased
	// means the inline checks were elided and execution requires a guard-page
	// memory + trap handler (Instantiate wires this up). Not serialized:
	// MarshalBinary rejects signals-based modules, so a loaded Compiled is always
	// explicit-checks.
	boundsMode BoundsCheckMode

	// memoryImport is the "module.name" key of the module's imported memory, if it
	// imports one; Instantiate then requires a *Memory for that key.
	memoryImport string

	// tableImport is the "module.name" key of the module's imported table, if it
	// imports one (cross-instance shared table); Instantiate then requires a *Table.
	tableImport string

	// wasmBytes retains the raw module for the link-time recompile that lowers
	// cross-instance calls (set only when the module has function imports, the
	// recompile candidates). needsLink marks a module whose codegen was deferred
	// because it has a returning import that must be bound to another instance's
	// function at Instantiate; its Code/Entry are empty until then.
	wasmBytes     []byte
	needsLink     bool
	boundsElide   bool // cached ElideBoundsChecks decision, for the link-time recompile
	noDeferBounds bool // cached DeferBoundsChecks=false decision, for the link-time recompile

	// syncHostImports is set by linkModule when the module has a returning host
	// import: all its host calls use the synchronous control frame and Invoke
	// drives the CallWithHost re-entry loop. importFuncSigs holds the function
	// imports' signatures (imports first), needed to bind host functions. Both are
	// instance-specific and never serialized.
	syncHostImports bool
	importFuncSigs  []FuncSig

	GCTypeDescs []gc.TypeDesc // immutable Wasm GC descriptor metadata; per-instance heaps own collection state

	// Cached during validateArenaFootprint.
	maxParamSlots        int
	maxResultSlots       int
	instantiateArenaNeed int

	// validateMemo memoizes the instantiate-boundary metadata validation for
	// modules produced by Compile/UnmarshalBinary, which are immutable: the full
	// check (which loops all funcs/globals/exports/GC descs) then only runs once
	// instead of on every Instantiate. It is a pointer so a by-value Compiled copy
	// (the link-time recompile) doesn't copy a lock; a nil memo means "validate
	// every time" — which is what a hand-constructed Compiled (exported fields,
	// no memo) gets, preserving its first-use validation.
	validateMemo *validateMemo

	codeCache *compiledCodeCache
}

type validateMemo struct {
	once sync.Once
	err  error
}

// validateCached returns the metadata-validation result, running the full check
// once per compiler-produced Compiled and every time for a hand-constructed one.
func (c *Compiled) validateCached() error {
	if c == nil || c.validateMemo == nil {
		return c.validate()
	}
	c.validateMemo.once.Do(func() { c.validateMemo.err = c.validate() })
	return c.validateMemo.err
}

// memorySizeBytes returns the initial and maximum (grow ceiling) linear-memory
// sizes in bytes for instantiation. A module without a declared memory still
// gets one page (legacy behavior). An unbounded or oversized max is capped at
// the engine ceiling (65535 pages, the largest u32-representable byte size).
func (c *Compiled) memorySizeBytes() (initial, max int) {
	const pageBytes = 65536
	const maxPagesCeil = 65535
	if !c.HasMemory {
		return pageBytes, pageBytes
	}
	maxPages := c.MemMaxPages
	if maxPages > maxPagesCeil {
		maxPages = maxPagesCeil
	}
	// Honor the declared minimum exactly, including 0: a (memory 0) module has
	// no in-bounds pages and memory.size reports 0 until it grows.
	initialPages := c.MemMinPages
	if initialPages > maxPages {
		maxPages = initialPages
	}
	return int(initialPages) * pageBytes, int(maxPages) * pageBytes
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
	global      *Global
	initialType ValType
	initialBits uint64
	mutable     bool
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
		provided, ok := imports.global(key)
		if !ok {
			return nil, fmt.Errorf("missing imported global %q", key)
		}
		g := &resolvedGlobalImport{global: provided.Global, initialType: provided.Type, initialBits: provided.Bits, mutable: provided.Mutable}
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
	if g.initialType != imp.Type {
		return fmt.Errorf("imported global %q has type %s, want %s", key, g.initialType, imp.Type)
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

func normalizeGlobalBits(t ValType, bits uint64) uint64 {
	if t == ValI32 || t == ValF32 {
		return uint64(uint32(bits))
	}
	return bits
}

func readGlobalObject(g *Global, t ValType) uint64 {
	if g == nil || len(g.cell) < 8 {
		return 0
	}
	return normalizeGlobalBits(t, binary.LittleEndian.Uint64(g.cell))
}

func writeGlobalObject(g *Global, t ValType, bits uint64) {
	binary.LittleEndian.PutUint64(g.cell, normalizeGlobalBits(t, bits))
}

// Global returns the current value of an exported global as raw bits (decode
// with AsI32/etc); its type is available via Signature/metadata.
func (in *Instance) Global(name string) (uint64, error) {
	idx, ok := in.c.GlobalExports[name]
	if !ok {
		if _, isFunc := in.c.Exports[name]; isFunc {
			return 0, fmt.Errorf("export %q is a function, not a global", name)
		}
		return 0, fmt.Errorf("no exported global %q", name)
	}
	if idx < 0 || idx >= len(in.c.Globals) || idx >= len(in.globalCells) || in.globalCells[idx] == nil {
		return 0, fmt.Errorf("exported global %q index %d out of range", name, idx)
	}
	g := in.c.Globals[idx]
	return readGlobalObject(in.globalCells[idx], g.Type), nil
}

// SetGlobal updates an exported mutable global; bits are interpreted as the
// global's type.
func (in *Instance) SetGlobal(name string, bits uint64) error {
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
	writeGlobalObject(in.globalCells[idx], g.Type, bits)
	return nil
}

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

// NewGlobalI32/I64/F32/F64/V128 construct a host-owned wasm global of the named
// type. Close releases its storage when no instance can access it anymore.
func NewGlobalI32(v int32, mutable bool) *Global   { return newGlobal(ValI32, I32(v), V128{}, mutable) }
func NewGlobalI64(v int64, mutable bool) *Global   { return newGlobal(ValI64, I64(v), V128{}, mutable) }
func NewGlobalF32(v float32, mutable bool) *Global { return newGlobal(ValF32, F32(v), V128{}, mutable) }
func NewGlobalF64(v float64, mutable bool) *Global { return newGlobal(ValF64, F64(v), V128{}, mutable) }
func NewGlobalV128(v V128, mutable bool) *Global   { return newGlobal(ValV128, 0, v, mutable) }

func newGlobal(t ValType, bits uint64, vec V128, mutable bool) *Global {
	arena, err := coreruntime.NewArena(globalCellSize(t))
	if err != nil {
		panic(fmt.Sprintf("global allocation failed: %v", err))
	}
	return newGlobalInCell(t, bits, vec, mutable, arena.Alloc(globalCellSize(t)), arena)
}

func newGlobalInCell(t ValType, bits uint64, vec V128, mutable bool, cell []byte, arena *coreruntime.Arena) *Global {
	g := &Global{Type: t, Mutable: mutable, cell: cell, arena: arena}
	writeGlobalObject(g, t, bits)
	if t == ValV128 {
		writeGlobalObjectV128(g, vec)
	}
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

// Get returns the global's current numeric scalar value as raw bits (decode
// with AsI32/etc). It returns zero for reference globals so descriptor addresses
// never cross the public boundary. For v128 globals use GetV128.
func (g *Global) Get() uint64 {
	if g == nil || isReferenceValType(g.Type) {
		return 0
	}
	return readGlobalObject(g, g.Type)
}

// GetV128 returns the global's current v128 value. Non-v128 globals return the
// low scalar bits in bytes 0..7 for debugging convenience; callers should prefer
// Type metadata when choosing this accessor.
func (g *Global) GetV128() V128 {
	if g == nil {
		return V128{}
	}
	return readGlobalObjectV128(g)
}

// Set updates a mutable host-owned scalar global; bits are interpreted as the
// global's type. For v128 globals use SetV128.
func (g *Global) Set(bits uint64) error {
	if g == nil {
		return fmt.Errorf("global is nil")
	}
	if !g.Mutable {
		return fmt.Errorf("global is immutable")
	}
	if g.Type == ValV128 {
		return fmt.Errorf("global is v128; use SetV128")
	}
	if isReferenceValType(g.Type) {
		return fmt.Errorf("global is a reference type; use an instance typed accessor")
	}
	writeGlobalObject(g, g.Type, bits)
	return nil
}

// SetV128 updates a mutable host-owned v128 global.
func (g *Global) SetV128(v V128) error {
	if g == nil {
		return fmt.Errorf("global is nil")
	}
	if !g.Mutable {
		return fmt.Errorf("global is immutable")
	}
	if g.Type != ValV128 {
		return fmt.Errorf("global is %s, not v128", g.Type)
	}
	writeGlobalObjectV128(g, v)
	return nil
}

// GlobalImport supplies an imported global value. Prefer a *Global for mutable
// imports so aliases across duplicate imports and instances share one wasm
// global object; Type/Mutable/Bits are a convenience for immutable globals.
type GlobalImport struct {
	Type    ValType
	Mutable bool
	Bits    uint64 // scalar initializer for i32/i64/f32/f64 imports
	V128    V128   // vector initializer for v128 imports
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

const nullFuncRefIndex = ^uint32(0)

// ElemInit is element-segment metadata. TableIndex names the active destination
// table; passive/declarative state leaves it zero. Funcs stores nullable funcref
// payloads: nullFuncRefIndex is ref.null, otherwise the wasm function index.
type ElemInit struct {
	TableIndex uint32
	Offset     OffsetInit
	Funcs      []uint32
}

// tableDef is compact instantiate-time metadata for local tables after table 0.
// Table 0 retains the legacy direct fields on Compiled so its hot path and codec
// layout stay unchanged during the multiple-table closeout.
type tableDef struct {
	ImportKey    string  // non-empty only for imported nonzero table indexes
	Size         int     // local size, or imported minimum when ImportKey is non-empty
	Max          int     // local capacity, or imported declared maximum
	Type         ValType // zero is the codec-v19/hand-built legacy funcref shape
	HasInitFunc  bool
	ImportHasMax bool
	InitFunc     uint32
}

type tableImportDef struct {
	Key    string
	Min    int
	Max    int
	Type   ValType
	HasMax bool
}

// DataInit is active data-segment metadata.
type DataInit struct {
	Offset OffsetInit
	Bytes  []byte
}

// PassiveDataInit is data-segment state metadata for memory.init/data.drop.
// Passive Bytes are immutable for a compiled module; active slots have nil Bytes
// and therefore start with a zero-length (already-dropped) instance descriptor.
type PassiveDataInit struct {
	Bytes []byte
}

// GlobalDef is the compact instantiate-time metadata for one wasm global.
// Each instance stores one pointer-table entry per global; scalar globals use an
// 8-byte cell (i32/f32 in the low 32 bits) and v128 globals use a 16-byte cell.
// Bits/V128 hold literal initializers. When HasInitGlobal is true, InitGlobal
// names an earlier imported immutable global whose current value is copied into
// this global's own local cell during instantiation; it is not a slot alias.
// When HasInitFunc is true, InitFunc is a structural Wasm function index that is
// resolved to this instance's canonical descriptor after code mapping.
type GlobalDef struct {
	Type          ValType
	Mutable       bool
	Bits          uint64
	V128          V128
	HasInitGlobal bool
	InitGlobal    int
	HasInitFunc   bool
	InitFunc      uint32
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

	GlobalImports          []GlobalImportDef // imported global entries, preceding local globals
	Globals                []GlobalDef       // global entries in wasm global-index order
	GlobalExports          map[string]int    // exported global name -> global index
	tableExports           map[string]int    // exported table name -> table index; allocated only when non-empty
	hasTableExportMetadata bool              // false only for legacy hand-built Compiled values

	HasTable          bool       // true when table 0 is declared, even with minimum length 0
	TableType         ValType    // table-0 element type; zero is legacy funcref metadata
	TableSize         int        // initial/current table-0 length
	TableMax          int        // table-0 allocated capacity/max; zero means TableSize for older hand-built metadata
	HasTableInitFunc  bool       // table-0 initializer is a non-null ref.func payload
	TableInitFunc     uint32     // wasm function index used to prefill table 0 when HasTableInitFunc
	extraTables       []tableDef // table indexes 1..N; imported positions carry indexed import metadata
	FuncTypeID        []uint32   // canonical signature id per global function index
	NeedsFuncRefDescs bool       // true when instantiation requires the canonical per-function descriptor arena
	Elems             []ElemInit // active element segments

	passiveElems []ElemInit // element-state descriptors keyed by original index; active/declarative slots start dropped

	Data        []DataInit        // active data segments (copied into linear memory at instantiate)
	PassiveData []PassiveDataInit // data-state descriptors keyed by original index; active slots start dropped

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

	// tableImport preserves codec-v19 and API metadata for imported table 0.
	// Additional imported tables occupy the leading extraTables entries; codec
	// v19 rejects every shape with more than one table import.
	tableImport       string
	tableImportMin    int
	tableImportMax    int
	tableImportHasMax bool

	// wasmBytes retains the raw module for the link-time recompile that lowers
	// cross-instance calls (set only when the module has function imports, the
	// recompile candidates). needsLink marks a module whose codegen was deferred
	// because it has a returning import that must be bound to another instance's
	// function at Instantiate; its Code/Entry are empty until then.
	wasmBytes     []byte
	needsLink     bool
	boundsElide   bool // cached ElideBoundsChecks decision, for the link-time recompile
	noDeferBounds bool // cached DeferBoundsChecks=false decision, for the link-time recompile
	requiresSIMD  bool // emitted code/ABI metadata requires the runtime SIMD CPU baseline

	// hostLink caches the host-only link recompile. A needsLink module (returning
	// import) defers codegen to Instantiate; when every import binds to a host
	// function (no cross-instance) the recompiled code is IDENTICAL regardless of
	// which host functions are supplied (host dispatch is a runtime table, not baked
	// into code), so it is produced once and reused — turning repeated Instantiate
	// of a WASI/host module from "re-run the whole backend" into "reuse the code +
	// its executable mapping". Non-deferred modules with function imports use the
	// same cache when non-legacy host bindings force a host-only sync recompile.
	// A pointer so the link-time `linked := *c` copy carries no lock. nil for
	// modules with no function imports (or hand-built/deserialized).
	hostLink *hostLinkCache

	// syncHostImports is set by linkModule when the module has a returning host
	// import: all its host calls use the synchronous control frame and Invoke
	// drives the CallWithHost re-entry loop. importFuncSigs holds the function
	// imports' signatures (imports first), needed to bind host functions and to
	// keep legacy HostFunc validation sound after compiled-code serialization.
	// syncHostImports is instance-specific and never serialized; importFuncSigs is
	// serialized with the rest of the immutable import metadata.
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

// hostLinkCache memoizes the host-only link recompile of a needsLink module (see
// Compiled.hostLink). once guards the single recompile; c/err hold its result,
// shared by every subsequent host Instantiate.
type hostLinkCache struct {
	once sync.Once
	c    *Compiled
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
	initialV128 V128
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
		g := &resolvedGlobalImport{global: provided.Global, initialType: provided.Type, initialBits: provided.Bits, initialV128: provided.V128, mutable: provided.Mutable}
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
	if len(g.cell) < globalCellSize(g.Type) {
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

func globalCellSize(t ValType) int {
	if t == ValV128 {
		return 16
	}
	return 8
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

func readGlobalObjectV128(g *Global) V128 {
	var out V128
	if g == nil || len(g.cell) == 0 {
		return out
	}
	copy(out[:], g.cell)
	return out
}

func writeGlobalObject(g *Global, t ValType, bits uint64) {
	binary.LittleEndian.PutUint64(g.cell, normalizeGlobalBits(t, bits))
}

func writeGlobalObjectV128(g *Global, v V128) {
	copy(g.cell, v[:])
}

// Global returns the current value of an exported numeric scalar global as raw
// bits (decode with AsI32/etc). Reference globals require GlobalValue so opaque
// token translation cannot expose an internal descriptor address. For v128
// globals use GlobalV128.
func (in *Instance) Global(name string) (uint64, error) {
	idx, err := in.exportedGlobalIndex(name)
	if err != nil {
		return 0, err
	}
	g := in.c.Globals[idx]
	if g.Type == ValV128 {
		return 0, fmt.Errorf("exported global %q is v128; use GlobalV128", name)
	}
	if isReferenceValType(g.Type) {
		return 0, fmt.Errorf("exported global %q is a reference type; use GlobalValue", name)
	}
	return readGlobalObject(in.globalCells[idx], g.Type), nil
}

// GlobalV128 returns the current value of an exported v128 global.
func (in *Instance) GlobalV128(name string) (V128, error) {
	idx, err := in.exportedGlobalIndex(name)
	if err != nil {
		return V128{}, err
	}
	g := in.c.Globals[idx]
	if g.Type != ValV128 {
		return V128{}, fmt.Errorf("exported global %q is %s, not v128", name, g.Type)
	}
	return readGlobalObjectV128(in.globalCells[idx]), nil
}

// SetGlobal updates an exported mutable numeric scalar global; bits are
// interpreted as the global's type. Reference globals require SetGlobalValue so
// opaque tokens are validated and translated. For v128 globals use SetGlobalV128.
func (in *Instance) SetGlobal(name string, bits uint64) error {
	idx, err := in.exportedGlobalIndex(name)
	if err != nil {
		return err
	}
	g := in.c.Globals[idx]
	if !g.Mutable {
		return fmt.Errorf("exported global %q is immutable", name)
	}
	if g.Type == ValV128 {
		return fmt.Errorf("exported global %q is v128; use SetGlobalV128", name)
	}
	if isReferenceValType(g.Type) {
		return fmt.Errorf("exported global %q is a reference type; use SetGlobalValue", name)
	}
	writeGlobalObject(in.globalCells[idx], g.Type, bits)
	return nil
}

// SetGlobalV128 updates an exported mutable v128 global.
func (in *Instance) SetGlobalV128(name string, v V128) error {
	idx, err := in.exportedGlobalIndex(name)
	if err != nil {
		return err
	}
	g := in.c.Globals[idx]
	if !g.Mutable {
		return fmt.Errorf("exported global %q is immutable", name)
	}
	if g.Type != ValV128 {
		return fmt.Errorf("exported global %q is %s, not v128", name, g.Type)
	}
	writeGlobalObjectV128(in.globalCells[idx], v)
	return nil
}

func (in *Instance) exportedGlobalIndex(name string) (int, error) {
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
	return idx, nil
}

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
		var fn HostFunc
		switch value := v.(type) {
		case HostFunc:
			fn = value
		case *HostFuncRef:
			if value != nil {
				value.mu.Lock()
				fn = value.fn
				value.mu.Unlock()
			}
		}
		if fn != nil {
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
	owner   *globalOwner
}

type globalOwner struct {
	mu        sync.Mutex
	arena     *coreruntime.Arena
	store     *referenceStore
	instance  *Instance
	typ       ValType
	mutable   bool
	importers int
	closed    bool
	// retained holds producer instances whose local funcref is currently stored
	// in this global's cell (funcref globals only). Each retained instance keeps a
	// resource root so its code/arena outlives the raw descriptor; roots are
	// released when the descriptor is overwritten (next scan) or the global closes.
	retained map[*Instance]struct{}
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
	var owner *globalOwner
	if arena != nil {
		owner = &globalOwner{arena: arena, typ: t, mutable: mutable}
	}
	g := &Global{Type: t, Mutable: mutable, cell: cell, owner: owner}
	writeGlobalObject(g, t, bits)
	if t == ValV128 {
		writeGlobalObjectV128(g, vec)
	}
	return g
}

// retainProducerInstance transfers an instance's resource lifetime to this
// funcref global when the instance's local funcref is the value currently held
// in the cell — mirroring Table.retainProducerInstance for the single-slot
// global case. The raw descriptor embeds the producer's code pointer and home
// linear-memory address, so a producer that wrote it via global.set and then
// closed must be kept alive for other importers that read the global. Before
// adding the root it drops any previously retained producer no longer named by
// the current cell value, keeping retention bounded to the live descriptor.
func (g *Global) retainProducerInstance(in *Instance) bool {
	if g == nil || g.owner == nil || g.owner.typ != ValFuncRef || in == nil || !in.retainResourceRoot() {
		return false
	}
	o := g.owner
	var release []*Instance
	o.mu.Lock()
	if o.closed || len(g.cell) < 8 {
		o.mu.Unlock()
		in.releaseResourceRoot()
		return false
	}
	current := readGlobalObject(g, ValFuncRef)
	for root := range o.retained {
		if !root.ownsLocalFuncrefDescriptor(current) {
			delete(o.retained, root)
			release = append(release, root)
		}
	}
	if !in.ownsLocalFuncrefDescriptor(current) {
		o.mu.Unlock()
		in.releaseResourceRoot()
		for _, root := range release {
			root.releaseResourceRoot()
		}
		return false
	}
	if o.retained == nil {
		o.retained = make(map[*Instance]struct{})
	}
	_, exists := o.retained[in]
	if !exists {
		o.retained[in] = struct{}{}
	}
	o.mu.Unlock()
	if exists {
		in.releaseResourceRoot()
	}
	for _, root := range release {
		root.releaseResourceRoot()
	}
	return true
}

// NewFuncRefGlobal creates a host-owned funcref global bound to this Runtime's
// exact reference store. The initial token must be null or have been issued by
// the same Runtime. A non-null host-function token can originate only from an
// explicit HostFuncRef owner; raw HostFunc descriptors remain fail-closed.
func (rt *Runtime) NewFuncRefGlobal(initial FuncRef, mutable bool) (*Global, error) {
	if rt == nil || rt.refStore == nil {
		return nil, fmt.Errorf("wago: nil runtime")
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.closed {
		return nil, fmt.Errorf("wago: NewFuncRefGlobal on a closed runtime")
	}
	descriptor := uint64(0)
	if initial.token != 0 {
		var ok bool
		descriptor, ok = rt.refStore.resolve(initial.token)
		if !ok {
			return nil, fmt.Errorf("wago: invalid funcref token for global initializer")
		}
	}
	arena, err := coreruntime.NewArena(8)
	if err != nil {
		return nil, err
	}
	if err := rt.refStore.registerStoreObject(); err != nil {
		_ = arena.Close()
		return nil, err
	}
	g := newGlobalInCell(ValFuncRef, descriptor, V128{}, mutable, arena.Alloc(8), arena)
	g.owner.store = rt.refStore
	return g, nil
}

// NewExternRefGlobal creates a host-owned externref global bound to this
// Runtime's exact reference store. The initial token must be null or have been
// issued by the same Runtime.
func (rt *Runtime) NewExternRefGlobal(initial ExternRef, mutable bool) (*Global, error) {
	if rt == nil || rt.refStore == nil {
		return nil, fmt.Errorf("wago: nil runtime")
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.closed {
		return nil, fmt.Errorf("wago: NewExternRefGlobal on a closed runtime")
	}
	if initial.token != 0 {
		if _, ok := rt.refStore.resolveExternref(initial.token); !ok {
			return nil, fmt.Errorf("wago: invalid externref token for global initializer")
		}
	}
	arena, err := coreruntime.NewArena(8)
	if err != nil {
		return nil, err
	}
	if err := rt.refStore.registerStoreObject(); err != nil {
		_ = arena.Close()
		return nil, err
	}
	g := newGlobalInCell(ValExternRef, initial.token, V128{}, mutable, arena.Alloc(8), arena)
	g.owner.store = rt.refStore
	return g, nil
}

// Close releases storage owned by a host-created global after every reference-
// global importer closes. Instance-owned exported globals remain no-ops because
// their producer instance owns the cell.
func (g *Global) Close() error {
	if g == nil || g.owner == nil || g.owner.arena == nil {
		return nil
	}
	o := g.owner
	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		return nil
	}
	if o.importers != 0 {
		count := o.importers
		o.mu.Unlock()
		return fmt.Errorf("wago: global has %d live importer(s); close consumers before the global", count)
	}
	o.closed = true
	arena, store := o.arena, o.store
	o.arena = nil
	roots := make([]*Instance, 0, len(o.retained))
	for root := range o.retained {
		roots = append(roots, root)
	}
	o.retained = nil
	o.mu.Unlock()
	for _, root := range roots {
		root.releaseResourceRoot()
	}
	err := arena.Close()
	g.cell = nil
	if store != nil {
		store.storeObjectClosed()
	}
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

// GetValue returns a reference global through its exact owner store. Numeric and
// vector globals keep their existing Get/GetV128 accessors.
func (g *Global) GetValue() (Value, error) {
	if g == nil || len(g.cell) < 8 {
		return Value{}, fmt.Errorf("global storage is closed")
	}
	if g.owner == nil {
		return Value{}, fmt.Errorf("global has no compatible reference owner")
	}
	o := g.owner
	o.mu.Lock()
	typ, store, source, closed := o.typ, o.store, o.instance, o.closed
	consistent := g.Type == typ && g.Mutable == o.mutable
	o.mu.Unlock()
	if closed || !consistent || !isReferenceValType(typ) {
		return Value{}, fmt.Errorf("global reference owner metadata is invalid")
	}
	bits := readGlobalObject(g, typ)
	if bits == 0 {
		return Value{typ: typ}, nil
	}
	if store == nil {
		return Value{}, fmt.Errorf("global has no compatible reference store")
	}
	if typ == ValExternRef {
		if _, ok := store.resolveExternref(bits); !ok {
			return Value{}, fmt.Errorf("global contains an invalid externref value")
		}
		return Value{typ: ValExternRef, bits: bits}, nil
	}
	token, err := store.issue(source, bits)
	if err != nil {
		return Value{}, fmt.Errorf("global contains an invalid funcref value: %w", err)
	}
	return Value{typ: ValFuncRef, bits: token}, nil
}

// SetValue updates a mutable reference global after exact token validation.
func (g *Global) SetValue(v Value) error {
	if g == nil || len(g.cell) < 8 {
		return fmt.Errorf("global storage is closed")
	}
	if g.owner == nil {
		return fmt.Errorf("global has no compatible reference owner")
	}
	o := g.owner
	o.mu.Lock()
	typ, mutable, store, closed := o.typ, o.mutable, o.store, o.closed
	consistent := g.Type == typ && g.Mutable == mutable
	o.mu.Unlock()
	if closed || !consistent || !isReferenceValType(typ) {
		return fmt.Errorf("global reference owner metadata is invalid")
	}
	if v.typ != typ {
		return fmt.Errorf("global is %s, got %s", typ, v.typ)
	}
	if !mutable {
		return fmt.Errorf("global is immutable")
	}
	bits := v.bits
	if bits != 0 {
		if store == nil {
			return fmt.Errorf("global has no compatible reference store")
		}
		if typ == ValExternRef {
			if _, ok := store.resolveExternref(bits); !ok {
				return fmt.Errorf("invalid externref token")
			}
		} else {
			descriptor, ok := store.resolve(bits)
			if !ok {
				return fmt.Errorf("invalid funcref token")
			}
			bits = descriptor
		}
	}
	writeGlobalObject(g, typ, bits)
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
// global object; Type/Mutable/Bits are a convenience for immutable numeric/vector
// globals. Reference imports require an explicit compatible-store *Global.
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

const nullFuncRefIndex = ^uint32(0) // internal sentinel while decoding table initializer expressions

// ElemMode records the declared segment mode instead of inferring it from which
// compiled slice happens to contain the metadata.
type ElemMode uint8

const (
	ElemModeActive ElemMode = iota
	ElemModePassive
	ElemModeDeclarative
)

// RefInit is one typed element initializer. Null is explicit so ref.null never
// aliases an ordinary uint32 function index. Non-null initializers are ref.func
// and therefore valid only for a funcref segment.
type RefInit struct {
	FuncIndex uint32
	Null      bool
}

// ElemInit is typed element-segment metadata. TableIndex names an active
// destination, RefType selects the 32-byte funcref or 8-byte externref runtime
// representation, Mode preserves active/passive/declarative semantics, and
// Values carries structural null/ref.func payloads without live addresses.
type ElemInit struct {
	TableIndex uint32
	RefType    ValType
	Mode       ElemMode
	Offset     OffsetInit
	Values     []RefInit
}

// tableDef is compact instantiate-time metadata for local tables after table 0.
// Table 0 retains the legacy direct fields on Compiled so its hot path and codec
// layout stay unchanged during the multiple-table closeout.
type tableDef struct {
	ImportKey    string  // non-empty only for imported nonzero table indexes
	Size         int     // local size, or imported minimum when ImportKey is non-empty
	Max          int     // local runtime capacity, or imported declared maximum
	Type         ValType // zero is the hand-built legacy funcref shape
	HasInitFunc  bool
	ImportHasMax bool
	HasMax       bool // local declaration has an explicit maximum; Max is exact when true
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
	TableHasMax       bool       // local table-0 declaration has an explicit maximum
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

	// tableImport preserves the direct table-0 API/runtime metadata. Additional
	// imported tables occupy the leading extraTables entries, and codec v20 writes
	// every declaration in exact Wasm index order.
	tableImport       string
	tableImportMin    int
	tableImportMax    int
	tableImportHasMax bool

	// wasmBytes retains the caller-transferred raw module for the link-time
	// recompile that lowers cross-instance calls (set only when the module has
	// function imports, the recompile candidates). needsLink marks a module whose
	// codegen was deferred
	// because it has a returning import that must be bound to another instance's
	// function at Instantiate; its Code/Entry are empty until then.
	wasmBytes        []byte
	needsLink        bool
	boundsElide      bool   // cached ElideBoundsChecks decision, for the link-time recompile
	noDeferBounds    bool   // cached DeferBoundsChecks=false decision, for the link-time recompile
	compileWorkers   uint16 // capped compile policy for link-time recompilation; never serialized
	requiredFeatures uint8  // exact optional core-feature bits required by code/metadata

	// hostLink caches the host-only link recompile. A needsLink module (returning
	// import) defers codegen to Instantiate; when every import binds to a host
	// function (no cross-instance) the recompiled code is IDENTICAL regardless of
	// which host functions are supplied (host dispatch is a runtime table, not baked
	// into code), so it is produced once and reused — turning repeated Instantiate
	// of a host module from "re-run the whole backend" into "reuse the code +
	// its executable mapping". Non-deferred modules with function imports use the
	// same cache when non-legacy host bindings force a host-only sync recompile.
	// A pointer so the link-time `linked := *c` copy carries no lock. nil for
	// modules with no function imports (or hand-built/deserialized).
	hostLink *hostLinkCache

	// syncHostImports is set by linkModule when the module has a returning/v128
	// host import or exact caller resolution requires call-site callbacks: all host
	// calls then use the synchronous control frame and Invoke
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

// hostLinkCache memoizes host-only link recompiles of a needsLink module (see
// Compiled.hostLink). Normal and forced-synchronous host modes are cached
// separately so caller-resolution authority cannot reuse async replay code.
type hostLinkCache struct {
	once sync.Once
	c    *Compiled
	err  error

	syncOnce sync.Once
	syncC    *Compiled
	syncErr  error
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
	if isReferenceValType(imp.Type) {
		return fmt.Errorf("imported reference global %q requires an explicit store-bound *Global", key)
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
	actualType, actualMutable := g.Type, g.Mutable
	if g.owner != nil {
		actualType, actualMutable = g.owner.typ, g.owner.mutable
		if g.Type != actualType || g.Mutable != actualMutable {
			return fmt.Errorf("imported global %q public metadata does not match its exact owner type", key)
		}
	}
	if actualType != imp.Type {
		return fmt.Errorf("imported global %q has type %s, want %s", key, actualType, imp.Type)
	}
	if actualMutable != imp.Mutable {
		return fmt.Errorf("imported global %q mutability mismatch", key)
	}
	if isReferenceValType(imp.Type) && g.owner == nil {
		return fmt.Errorf("imported global %q has no explicit reference owner", key)
	}
	return nil
}

func (g *Global) validateReferenceImport(store *referenceStore) error {
	if g == nil || g.owner == nil || len(g.cell) < 8 {
		return fmt.Errorf("reference global descriptor is invalid")
	}
	o := g.owner
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return fmt.Errorf("reference global owner is closed")
	}
	if !isReferenceValType(o.typ) || o.typ != g.Type || o.mutable != g.Mutable {
		return fmt.Errorf("reference global owner metadata is inconsistent")
	}
	if store == nil || o.store == nil || o.store != store {
		return fmt.Errorf("reference global belongs to an incompatible reference store")
	}
	if o.instance != nil && !o.instance.hasPhysicalResources() {
		return fmt.Errorf("reference global owner instance is closed")
	}
	bits := readGlobalObject(g, o.typ)
	if bits == 0 {
		return nil
	}
	if o.typ == ValExternRef {
		if _, ok := store.resolveExternref(bits); !ok {
			return fmt.Errorf("reference global contains an invalid externref token")
		}
		return nil
	}
	store.mu.Lock()
	var ok bool
	if o.instance == nil {
		entry := store.byDescriptor[bits]
		ok = entry != nil && entry.descriptor == bits
	} else {
		_, _, ok = store.canonicalFuncrefOwnerLocked(o.instance, bits)
	}
	store.mu.Unlock()
	if !ok {
		return fmt.Errorf("reference global contains an invalid funcref descriptor")
	}
	return nil
}

func (g *Global) attachReferenceImporter(store *referenceStore) error {
	if err := g.validateReferenceImport(store); err != nil {
		return err
	}
	o := g.owner
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return fmt.Errorf("reference global owner is closed")
	}
	if o.instance != nil && !o.instance.retainResourceRoot() {
		return fmt.Errorf("reference global owner instance is closed")
	}
	o.importers++
	return nil
}

func (g *Global) detachReferenceImporter() {
	if g == nil || g.owner == nil {
		return
	}
	o := g.owner
	var instance *Instance
	o.mu.Lock()
	if o.importers > 0 {
		o.importers--
		instance = o.instance
	}
	o.mu.Unlock()
	if instance != nil {
		instance.releaseResourceRoot()
	}
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

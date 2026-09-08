package wago

import (
	"encoding/binary"
	"fmt"
	"math"
	"sync"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

// Call arguments and results are raw uint64 value slots: the function
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
		if g == nil {
			return GlobalImport{}, false
		}
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
	mu           sync.Mutex
	arena        *coreruntime.Arena
	store        *referenceStore
	instance     *Instance
	typ          ValType
	mutable      bool
	valueType    ValueTypeDescriptor
	types        []DefinedTypeDescriptor
	hasValueType bool
	importers    int
	closed       bool
	// retained holds writer instances whose reachable funcref is currently stored
	// in this global's cell (funcref globals only). Each root preserves the writer's
	// descriptor arena and transitive import attachments until overwrite or close.
	retained map[*Instance]*retainedInstanceRoot
}

func (g *Global) instanceOwner() *Instance {
	if g == nil || g.owner == nil {
		return nil
	}
	g.owner.mu.Lock()
	owner := g.owner.instance
	g.owner.mu.Unlock()
	return owner
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
// funcref global when the current descriptor is reachable through that
// instance's function-index space. Retaining the writer covers local,
// canonical-import, bare-producer proxy, and HostFuncRef proxy descriptors while
// preserving established transitive attachments. A later scan drops any root
// no longer represented by the single live cell.
func (g *Global) retainProducerInstance(in *Instance) bool {
	return g.retainProducerInstanceMode(in, false)
}

func (g *Global) retainProducerInstanceForFinalization(in *Instance) bool {
	return g.retainProducerInstanceMode(in, true)
}

func (g *Global) retainProducerInstanceMode(in *Instance, finalization bool) bool {
	if g == nil || g.owner == nil || g.owner.typ != ValFuncRef || in == nil {
		return false
	}
	g.owner.mu.Lock()
	selfOwned := g.owner.closed || g.owner.instance == in
	g.owner.mu.Unlock()
	if selfOwned {
		return false
	}
	var retained bool
	if finalization {
		retained = in.retainResourceRootForFinalization()
	} else {
		retained = in.retainResourceRoot()
	}
	if !retained {
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
	for root, state := range o.retained {
		state.precise = state.precise && root.reachesFuncrefDescriptor(current)
		for descriptor := range state.proxyDescriptors {
			if descriptor != current {
				delete(state.proxyDescriptors, descriptor)
			}
		}
		if !state.precise && len(state.proxyDescriptors) == 0 {
			delete(o.retained, root)
			release = append(release, root)
		}
	}
	if !in.reachesFuncrefDescriptor(current) {
		o.mu.Unlock()
		in.releaseResourceRoot()
		for _, root := range release {
			root.releaseResourceRoot()
		}
		return false
	}
	if o.retained == nil {
		o.retained = make(map[*Instance]*retainedInstanceRoot)
	}
	state, exists := o.retained[in]
	if !exists {
		state = &retainedInstanceRoot{}
		o.retained[in] = state
	}
	state.precise = true
	for root, retained := range o.retained {
		if in.reachesFuncrefDescriptor(current) {
			delete(retained.proxyDescriptors, current)
		}
		if !retained.precise && len(retained.proxyDescriptors) == 0 {
			delete(o.retained, root)
			release = append(release, root)
		}
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

// funcrefProducerRoots snapshots the instances that keep the current global
// descriptor reachable. Active element initialization can copy that descriptor
// into a different shared owner, which must retain these actual producers before
// the global importer detaches.
func (g *Global) funcrefProducerRoots() []*Instance {
	if g == nil || g.owner == nil {
		return nil
	}
	o := g.owner
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed || o.typ != ValFuncRef {
		return nil
	}
	roots := make([]*Instance, 0, len(o.retained)+1)
	seen := make(map[*Instance]struct{}, len(o.retained)+1)
	if o.instance != nil {
		roots = append(roots, o.instance)
		seen[o.instance] = struct{}{}
	}
	for root := range o.retained {
		if root == nil {
			continue
		}
		if _, ok := seen[root]; ok {
			continue
		}
		seen[root] = struct{}{}
		roots = append(roots, root)
	}
	return roots
}

// retainDescriptorOwnerForFinalization resolves the current cell descriptor
// without holding globalOwner.mu across store or instance locks. If resolution
// fails, proxy conservatively owns the descriptor through its existing import
// attachment chain until overwrite, later precise resolution, or global close.
func (g *Global) retainDescriptorOwnerForFinalization(store *referenceStore, proxy *Instance) descriptorRetentionResult {
	if g == nil || g.owner == nil {
		return descriptorRetentionResult{}
	}
	o := g.owner
	o.mu.Lock()
	if o.closed || o.typ != ValFuncRef || len(g.cell) < 8 {
		o.mu.Unlock()
		return descriptorRetentionResult{}
	}
	descriptor := readGlobalObject(g, ValFuncRef)
	containerOwner := o.instance
	o.mu.Unlock()

	var owner *Instance
	if descriptor != 0 && store != nil {
		owner = store.retainDescriptorOwnerForFinalization(descriptor)
	}
	if owner != nil && owner == containerOwner {
		owner.releaseResourceRoot()
		owner = nil
	}
	unresolved := descriptor != 0 && owner == nil
	proxyAcquired := unresolved && proxy != nil && proxy != containerOwner && proxy.retainResourceRootForFinalization()

	var release []*Instance
	o.mu.Lock()
	if o.closed || len(g.cell) < 8 || readGlobalObject(g, ValFuncRef) != descriptor {
		o.mu.Unlock()
		if owner != nil {
			owner.releaseResourceRoot()
		}
		if proxyAcquired {
			proxy.releaseResourceRoot()
		}
		return descriptorRetentionResult{}
	}
	preciseCoverage := false
	for root, state := range o.retained {
		state.precise = root == owner || (unresolved && root.reachesFuncrefDescriptor(descriptor))
		preciseCoverage = preciseCoverage || state.precise
		for candidate := range state.proxyDescriptors {
			if !unresolved || candidate != descriptor {
				delete(state.proxyDescriptors, candidate)
			}
		}
	}
	if preciseCoverage {
		for _, state := range o.retained {
			delete(state.proxyDescriptors, descriptor)
		}
	}
	if owner != nil {
		if o.retained == nil {
			o.retained = make(map[*Instance]*retainedInstanceRoot)
		}
		state := o.retained[owner]
		if state == nil {
			state = &retainedInstanceRoot{precise: true}
			o.retained[owner] = state
			owner.markNativeControlShared()
		} else {
			state.precise = true
			release = append(release, owner)
		}
	}
	if proxyAcquired {
		covered := false
		for root, state := range o.retained {
			_, proxied := state.proxyDescriptors[descriptor]
			if proxied || (state.precise && root.reachesFuncrefDescriptor(descriptor)) {
				covered = true
				break
			}
		}
		if covered {
			release = append(release, proxy)
		} else {
			if o.retained == nil {
				o.retained = make(map[*Instance]*retainedInstanceRoot)
			}
			state := o.retained[proxy]
			if state == nil {
				state = &retainedInstanceRoot{}
				o.retained[proxy] = state
				proxy.markNativeControlShared()
			} else {
				release = append(release, proxy)
			}
			if state.proxyDescriptors == nil {
				state.proxyDescriptors = make(map[uint64]struct{}, 1)
			}
			state.proxyDescriptors[descriptor] = struct{}{}
		}
	}
	for root, state := range o.retained {
		if !state.precise && len(state.proxyDescriptors) == 0 {
			delete(o.retained, root)
			release = append(release, root)
		}
	}
	result := descriptorRetentionResult{retained: len(o.retained) != 0, unresolved: unresolved}
	o.mu.Unlock()
	for _, root := range release {
		root.releaseResourceRoot()
	}
	return result
}

// NewFuncRefGlobal creates a host-owned funcref global bound to this Runtime's
// exact reference store. The initial token must be null or have been issued by
// the same Runtime. A non-null host-function token can originate only from an
// explicit HostFuncRef owner; raw HostFunc descriptors remain fail-closed.
func (g *Global) pruneRetainedInstances() {
	if g == nil || g.owner == nil {
		return
	}
	o := g.owner
	var release []*Instance
	o.mu.Lock()
	if !o.closed && len(g.cell) >= 8 && o.typ == ValFuncRef {
		descriptor := readGlobalObject(g, ValFuncRef)
		for root, state := range o.retained {
			state.precise = descriptor != 0 && root.reachesFuncrefDescriptor(descriptor)
			for candidate := range state.proxyDescriptors {
				if candidate != descriptor {
					delete(state.proxyDescriptors, candidate)
				}
			}
			if !state.precise && len(state.proxyDescriptors) == 0 {
				delete(o.retained, root)
				release = append(release, root)
			}
		}
	}
	o.mu.Unlock()
	for _, root := range release {
		root.releaseResourceRoot()
	}
}

func (rt *Runtime) NewFuncRefGlobal(initial FuncRef, mutable bool) (*Global, error) {
	if rt == nil || rt.refStore == nil {
		return nil, fmt.Errorf("wago: nil runtime")
	}
	operation, err := rt.beginOperation("NewFuncRefGlobal", false)
	if err != nil {
		return nil, err
	}
	defer operation.end()
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
	operation, err := rt.beginOperation("NewExternRefGlobal", false)
	if err != nil {
		return nil, err
	}
	defer operation.end()
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
	if g == nil || g.owner == nil {
		return nil
	}
	o := g.owner
	o.mu.Lock()
	if o.closed || o.arena == nil {
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
	g.cell = nil
	roots := make([]*Instance, 0, len(o.retained))
	for root := range o.retained {
		roots = append(roots, root)
	}
	o.retained = nil
	o.mu.Unlock()
	// Root release may re-enter instance finalization, so it must not happen
	// while globalOwner.mu is held.
	for _, root := range roots {
		root.releaseResourceRoot()
	}
	err := arena.Close()
	if store != nil {
		store.storeObjectClosed()
	}
	return err
}

// accessMetadata returns canonical storage metadata and rejects public-field edits.
// Owner type and mutability are fixed when the cell is created.
func (g *Global) accessMetadata() (ValType, bool, bool) {
	if g == nil {
		return 0, false, false
	}
	if g.owner != nil {
		return g.owner.typ, g.owner.mutable, g.Type == g.owner.typ && g.Mutable == g.owner.mutable
	}
	return g.Type, g.Mutable, true
}

// Get returns the global's current numeric scalar value as raw bits (decode
// with AsI32/etc). It returns zero for reference globals so descriptor addresses
// never cross the public boundary. For v128 globals use GetV128.
func (g *Global) Get() uint64 {
	typ, _, valid := g.accessMetadata()
	if !valid || isReferenceValType(typ) {
		return 0
	}
	end, ok := g.beginOwnerAccess()
	if !ok {
		return 0
	}
	defer end()
	if g.owner != nil {
		g.owner.mu.Lock()
		defer g.owner.mu.Unlock()
		if g.owner.closed || len(g.cell) < globalCellSize(typ) {
			return 0
		}
	}
	return readGlobalObject(g, typ)
}

// GetV128 returns the global's current v128 value. Non-v128 globals return the
// low scalar bits in bytes 0..7 for debugging convenience. Reference globals
// and inconsistent public metadata return zero.
func (g *Global) GetV128() V128 {
	typ, _, valid := g.accessMetadata()
	if !valid || isReferenceValType(typ) {
		return V128{}
	}
	end, ok := g.beginOwnerAccess()
	if !ok {
		return V128{}
	}
	defer end()
	if g.owner != nil {
		g.owner.mu.Lock()
		defer g.owner.mu.Unlock()
		if g.owner.closed || len(g.cell) < globalCellSize(typ) {
			return V128{}
		}
	}
	return readGlobalObjectV128(g)
}

// Set updates a mutable host-owned scalar global; bits are interpreted as the
// global's type. For v128 globals use SetV128.
func (g *Global) Set(bits uint64) error {
	typ, mutable, valid := g.accessMetadata()
	if !valid {
		return fmt.Errorf("global owner metadata is invalid")
	}
	end, ok := g.beginOwnerAccess()
	if !ok {
		return fmt.Errorf("global owner instance is closed")
	}
	defer end()
	if !mutable {
		return fmt.Errorf("global is immutable")
	}
	if typ == ValV128 {
		return fmt.Errorf("global is v128; use SetV128")
	}
	if isReferenceValType(typ) {
		return fmt.Errorf("global is a reference type; use an instance typed accessor")
	}
	if g.owner != nil {
		g.owner.mu.Lock()
		defer g.owner.mu.Unlock()
		if g.owner.closed || len(g.cell) < globalCellSize(typ) {
			return fmt.Errorf("global storage is closed")
		}
	}
	writeGlobalObject(g, typ, bits)
	return nil
}

// GetValue returns a reference global through its exact owner store. Numeric and
// vector globals keep their existing Get/GetV128 accessors.
func (g *Global) GetValue() (Value, error) {
	end, ok := g.beginOwnerAccess()
	if !ok {
		return Value{}, fmt.Errorf("global owner instance is closed")
	}
	defer end()
	return g.getValueNoLease()
}

func (g *Global) getValueNoLease() (Value, error) {
	if g == nil || g.owner == nil {
		return Value{}, fmt.Errorf("global has no compatible reference owner")
	}
	o := g.owner
	o.mu.Lock()
	typ, store, source, closed := o.typ, o.store, o.instance, o.closed
	exact, exactTypes, hasExact := o.valueType, o.types, o.hasValueType
	consistent := g.Type == typ && g.Mutable == o.mutable
	if closed || len(g.cell) < 8 || !consistent || !isReferenceValType(typ) {
		o.mu.Unlock()
		return Value{}, fmt.Errorf("global reference owner metadata is invalid")
	}
	bits := readGlobalObject(g, typ)
	o.mu.Unlock()
	if bits == 0 {
		if hasExact && exact.Kind == ValueTypeReference && !exact.Ref.Nullable {
			return Value{}, fmt.Errorf("global contains null for a non-null reference type")
		}
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
	if hasExact {
		actual, actualTypes, ok := store.descriptorFuncrefExactType(source, bits)
		if !ok || !valueTypeSubtype(actual, actualTypes, exact, exactTypes) {
			return Value{}, fmt.Errorf("global contains a funcref with an incompatible exact structural type")
		}
	}
	token, err := store.issue(source, bits)
	if err != nil {
		return Value{}, fmt.Errorf("global contains an invalid funcref value: %w", err)
	}
	return Value{typ: ValFuncRef, bits: token}, nil
}

// SetValue updates a mutable reference global after exact token validation.
func (g *Global) SetValue(v Value) error {
	end, ok := g.beginOwnerAccess()
	if !ok {
		return fmt.Errorf("global owner instance is closed")
	}
	defer end()
	return g.setValueNoLease(v)
}

func (g *Global) setValueNoLease(v Value) error {
	if g == nil || g.owner == nil {
		return fmt.Errorf("global has no compatible reference owner")
	}
	o := g.owner
	o.mu.Lock()
	typ, mutable, store, containerOwner, closed := o.typ, o.mutable, o.store, o.instance, o.closed
	exact, exactTypes, hasExact := o.valueType, o.types, o.hasValueType
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
	var retainedOwner *Instance
	if bits == 0 && hasExact && exact.Kind == ValueTypeReference && !exact.Ref.Nullable {
		return fmt.Errorf("global requires a non-null reference value")
	}
	if bits != 0 {
		if store == nil {
			return fmt.Errorf("global has no compatible reference store")
		}
		if typ == ValExternRef {
			if _, ok := store.resolveExternref(bits); !ok {
				return fmt.Errorf("invalid externref token")
			}
		} else {
			if hasExact {
				actual, actualTypes, ok := store.tokenFuncrefExactType(bits)
				if !ok {
					return fmt.Errorf("invalid funcref token")
				}
				if !valueTypeSubtype(actual, actualTypes, exact, exactTypes) {
					return fmt.Errorf("funcref token does not match the global's exact structural type")
				}
			}
			descriptor, owner, ok := store.resolveFuncrefTokenOwner(bits)
			if !ok {
				return fmt.Errorf("invalid funcref token owner")
			}
			bits = descriptor
			retainedOwner = owner
			if retainedOwner == containerOwner {
				retainedOwner.releaseResourceRoot()
				retainedOwner = nil
			}
		}
	}
	var release []*Instance
	o.mu.Lock()
	if o.closed || len(g.cell) < 8 || o.typ != typ || o.mutable != mutable || g.Type != typ || g.Mutable != mutable {
		o.mu.Unlock()
		if retainedOwner != nil {
			retainedOwner.releaseResourceRoot()
		}
		return fmt.Errorf("global reference owner metadata is invalid")
	}
	writeGlobalObject(g, typ, bits)
	if typ == ValFuncRef {
		for root := range o.retained {
			if root == retainedOwner {
				continue
			}
			delete(o.retained, root)
			release = append(release, root)
		}
		if retainedOwner != nil {
			if o.retained == nil {
				o.retained = make(map[*Instance]*retainedInstanceRoot)
			}
			if state := o.retained[retainedOwner]; state != nil {
				state.precise = true
				state.proxyDescriptors = nil
				release = append(release, retainedOwner)
			} else {
				o.retained[retainedOwner] = &retainedInstanceRoot{precise: true}
				retainedOwner.markNativeControlShared()
			}
		}
	}
	o.mu.Unlock()
	for _, root := range release {
		root.releaseResourceRoot()
	}
	return nil
}

// SetV128 updates a mutable host-owned v128 global.
func (g *Global) SetV128(v V128) error {
	typ, mutable, valid := g.accessMetadata()
	if !valid {
		return fmt.Errorf("global owner metadata is invalid")
	}
	end, ok := g.beginOwnerAccess()
	if !ok {
		return fmt.Errorf("global owner instance is closed")
	}
	defer end()
	if !mutable {
		return fmt.Errorf("global is immutable")
	}
	if typ != ValV128 {
		return fmt.Errorf("global is %s, not v128", typ)
	}
	if g.owner != nil {
		g.owner.mu.Lock()
		defer g.owner.mu.Unlock()
		if g.owner.closed || len(g.cell) < 16 {
			return fmt.Errorf("global storage is closed")
		}
	}
	writeGlobalObjectV128(g, v)
	return nil
}

func (g *Global) beginOwnerAccess() (func(), bool) {
	if g == nil {
		return nil, false
	}
	var owner *Instance
	if g.owner != nil {
		owner = g.owner.instance
	}
	if owner != nil {
		if err := owner.beginInvocation(); err != nil {
			return nil, false
		}
	}
	var unlockNative func()
	if owner != nil {
		unlockNative = owner.lockInstanceNativeStateForHostAccess()
	} else {
		unlockNative = lockNativeExecutionForHostAccess()
	}
	return func() {
		unlockNative()
		if owner != nil {
			owner.endInvocation()
		}
	}, true
}

func (g *Global) instanceOwnerClosed(in *Instance) {
	if g == nil || g.owner == nil || in == nil {
		return
	}
	o := g.owner
	var roots []*Instance
	o.mu.Lock()
	if o.instance != in || o.closed {
		o.mu.Unlock()
		return
	}
	o.closed = true
	for root := range o.retained {
		roots = append(roots, root)
	}
	o.retained = nil
	g.cell = nil
	o.mu.Unlock()
	for _, root := range roots {
		root.releaseResourceRoot()
	}
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

type FuncSig struct {
	Params, Results []ValType
	TypeIndex       uint32
	HasTypeIndex    bool
	unsafeCrossTail bool // imported return_call use exceeds the admitted cross-instance tail ABI
}

// OffsetInit is active data/element offset metadata. Base is the literal i32
// offset. When HasGlobal is true, Global names an imported immutable i32 global
// whose current instance cell is read during instantiation. Expr holds a
// validated extended constant-expression program when the offset needs integer
// arithmetic; it is evaluated after imported globals have been resolved.
type OffsetInit struct {
	Base      uint32
	HasGlobal bool
	Global    int
	Expr      []byte
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
// aliases an ordinary uint32 payload. Funcref segments interpret FuncIndex as a
// function index; exact i31 segments interpret it as the tagged compact immediate.
type RefInit struct {
	FuncIndex   uint32
	GlobalIndex uint32
	Expr        []byte
	Null        bool
	HasGlobal   bool
	I31Wrap     bool
}

// ElemInit is typed element-segment metadata. TableIndex names an active
// destination, RefType selects the 32-byte funcref or 8-byte externref runtime
// representation, Mode preserves active/passive/declarative semantics, and
// Values carries structural null/ref.func payloads without live addresses.
// HasValueType is false for the legacy function-index segment encoding, whose
// exact element type is non-null (ref func).
type ElemInit struct {
	TableIndex     uint32
	RefType        ValType
	ValueTypeIndex uint32
	HasValueType   bool
	Mode           ElemMode
	Offset         OffsetInit
	Values         []RefInit
}

// tableDef is compact instantiate-time metadata for local tables after table 0.
// Table 0 retains the legacy direct fields on Compiled so its hot path and codec
// layout stay unchanged during the multiple-table closeout.
type tableDef struct {
	ImportKey      string  // non-empty only for imported nonzero table indexes
	Size           int     // local size, or imported minimum when ImportKey is non-empty
	Max            uint64  // exact local declared maximum when HasMax; otherwise runtime reserve; imported maximum when ImportKey is non-empty
	Type           ValType // zero is the hand-built legacy funcref shape
	ValueTypeIndex uint32
	HasValueType   bool
	HasInitFunc    bool
	ImportHasMax   bool
	HasMax         bool // local declaration has an explicit maximum; Max is exact when true
	Addr64         bool // table indexes and limits use the Core 3 i64 address form
	InitFunc       uint32
}

type tableImportDef struct {
	Key string
	Min uint64 // exact declared minimum; executable sizing uses checked int conversion
	Max uint64 // exact declared maximum when HasMax

	Type           ValType
	ValueTypeIndex uint32
	HasValueType   bool
	HasMax         bool
	Addr64         bool
}

// DataInit is active data-segment metadata.
type DataInit struct {
	MemoryIndex uint32
	Offset      OffsetInit
	Bytes       []byte
}

// PassiveDataInit is data-segment state metadata for memory.init/data.drop.
// Passive Bytes are immutable for a compiled module; active slots have nil Bytes
// and therefore start with a zero-length (already-dropped) instance descriptor.
type PassiveDataInit struct {
	Bytes []byte
}

// memoryDef is exact declaration metadata in Wasm memory-index order. Imported
// memories precede local definitions. The legacy memory-0 fields on Compiled
// remain the direct execution cache until indexed codegen is complete.
type memoryDef struct {
	ImportKey string
	Min       uint64
	Max       uint64
	HasMax    bool
	Addr64    bool
	Shared    bool
}

func (c *Compiled) funcTypeKey(index int) uint64 {
	if c != nil && index >= 0 && index < len(c.FuncTypeID) {
		return c.FuncTypeID[index]
	}
	return 0
}

type compiledMemoryDirectory struct {
	defs         []memoryDef
	exports      map[string]int
	exactExports bool
	staged       bool // internal multi-memory execution gate; never serialized

	stagedMemory64  bool                   // internal bounded memory64 execution gate; never serialized
	gcStructGlobals []gcStructGlobalInit   // exact staged GC constant initializers; never serialized
	gcArrayGlobals  []gcArrayGlobalInit    // exact staged numeric array globals; never serialized
	gcArrayElement  *gcArrayElementInit    // exact passive GC element constructors; never serialized
	gcI31TableInit  *gcI31TableInitializer // exact imported-global i31 table initializer; never serialized
	ehTags          []compiledTagDef       // staged EH product metadata in tag-index order; never serialized
	ehTagExports    map[string]int         // exact tag export name -> tag index; never serialized
}

// GlobalDef is the compact instantiate-time metadata for one wasm global.
// Each instance stores one pointer-table entry per global; scalar globals use an
// 8-byte cell (i32/f32 in the low 32 bits) and v128 globals use a 16-byte cell.
// Bits/V128 hold literal initializers. When HasInitGlobal is true, InitGlobal
// names an earlier immutable global whose current value is copied into this
// global's own local cell during instantiation; it is not a slot alias. InitExpr
// holds a validated scalar extended constant expression evaluated against
// earlier immutable globals. When HasInitFunc is true, InitFunc is a structural
// Wasm function index resolved to this instance's canonical descriptor.
type GlobalDef struct {
	Type           ValType
	ValueTypeIndex uint32
	HasValueType   bool
	Mutable        bool
	Bits           uint64
	V128           V128
	HasInitGlobal  bool
	InitGlobal     int
	HasInitFunc    bool
	InitFunc       uint32
	InitExpr       []byte
}

// GlobalImportDef identifies one imported global entry in wasm global-index order.
type GlobalImportDef struct {
	Module         string
	Name           string
	Type           ValType
	ValueTypeIndex uint32
	HasValueType   bool
	Mutable        bool
}

type compiledTagDef struct {
	ImportKey string
	TypeIndex uint32
}

// Compiled owns emitted machine code plus instantiate-time metadata. Native
// bytes are intentionally private; use CodeSize or WriteCodeTo for diagnostics.
// Public metadata is a compatibility view; changing it after compilation or
// loading does not change the private execution snapshot. Call Close when
// no new instances are needed; existing instances retain their executable image
// until they close.
type Compiled struct {
	code  []byte
	Entry []int // host-wrapper offset, or the internal offset for direct-only functions
	// InternalEntry mirrors Entry with each function's register-ABI internal
	// entry offset. Entry[i] == InternalEntry[i] for a direct-only register-ABI
	// function whose unreachable host adapter was omitted.
	InternalEntry []int
	Funcs         []FuncSig               // signature per local function
	Types         []DefinedTypeDescriptor // flattened structural type graph for indexed references
	ValueTypes    []ValueTypeDescriptor   // deduplicated exact global/table/element types
	Imports       []string                // "module.name" per imported function
	Exports       map[string]int          // exported function name -> global function index
	NumImports    int
	Names         *wasm.NameSec // parsed debug names from the wasm name custom section

	GlobalImports          []GlobalImportDef // imported global entries, preceding local globals
	Globals                []GlobalDef       // global entries in wasm global-index order
	GlobalExports          map[string]int    // exported global name -> global index
	tableExports           map[string]int    // exported table name -> index
	hasTableExportMetadata bool              // false only for legacy hand-built Compiled values

	HasTable            bool    // true when table 0 is declared, even with minimum length 0
	TableType           ValType // table-0 element type; zero is legacy funcref metadata
	TableValueTypeIndex uint32
	TableHasValueType   bool
	TableSize           int        // initial/current table-0 length
	TableMax            uint64     // exact declared maximum when TableHasMax; otherwise runtime reserve; zero means TableSize for older hand-built metadata
	HasTableInitFunc    bool       // table-0 initializer is a non-null ref.func payload
	TableHasMax         bool       // local table-0 declaration has an explicit maximum
	TableAddr64         bool       // table-0 indexes and limits use the Core 3 i64 address form
	stagedTable64       bool       // compile-only admission sidecar; never serialized
	TableInitFunc       uint32     // wasm function index used to prefill table 0 when HasTableInitFunc
	extraTables         []tableDef // table indexes 1..N; imported positions carry indexed import metadata
	FuncTypeID          []uint64   // collision-resistant native signature key; legacy field name
	NeedsFuncRefDescs   bool       // true when instantiation requires the canonical per-function descriptor arena
	Elems               []ElemInit // active element segments

	passiveElems []ElemInit // element-state descriptors keyed by original index; active/declarative slots start dropped

	Data        []DataInit        // active data segments (copied into linear memory at instantiate)
	PassiveData []PassiveDataInit // data-state descriptors keyed by original index; active slots start dropped

	HasMemory   bool   // module declares memory 0; direct execution cache
	MemMinPages uint32 // memory-0 initial size (pages); allocated at instantiate
	MemMaxPages uint32 // memory-0 reservation ceiling; 0 means use the engine default
	MemHasMax   bool   // memory-0 declaration/import has an explicit maximum
	memoryDir   *compiledMemoryDirectory

	HasStart       bool // module declares a start function to run at instantiate
	StartLocalFunc int  // its local function index (valid when HasStart && !StartIsImport)
	StartIsImport  bool // start is an imported function (run its host binding at instantiate)
	StartImportIdx int  // its imported-function index (valid when HasStart && StartIsImport)

	// boundsMode records how this code was compiled: BoundsChecksSignalsBased
	// means eligible memory-0 memory32 checks were elided and execution requires
	// a guard-page memory + trap handler (Instantiate wires this up). Indexed
	// memories and memory64 may still contain explicit checks. Not serialized:
	// MarshalBinary rejects signals-based modules, so a loaded Compiled is always
	// explicit-checks.
	boundsMode BoundsCheckMode

	// memoryImport is the "module.name" key of the module's imported memory, if it
	// imports one; Instantiate then requires a *Memory for that key.
	memoryImport string

	// tableImport preserves the direct table-0 API/runtime metadata. Additional
	// imported tables occupy the leading extraTables entries, and codec version 2 writes
	// every declaration in exact Wasm index order.
	tableImport       string
	tableImportMin    int
	tableImportMax    int
	tableImportHasMax bool

	// Imported calls use wrapper targets from a per-instance dispatch table. The
	// code image is therefore complete at Compile time and independent of concrete
	// host or cross-instance bindings.
	dynamicImports            bool
	dynamicFuncrefEscape      bool
	needsFuncRefContextHeader bool
	// registerABIDisabled keeps descriptor publication aligned with the actual
	// compile policy. False preserves legacy hand-built Compiled behavior.
	registerABIDisabled bool
	requiredFeatures    CoreFeatures
	importFuncSigs      []FuncSig

	GCTypeDescs []gc.TypeDesc // immutable Wasm GC descriptor metadata; per-instance heaps own collection state

	gcCodeTelemetry    gc.NativeCodeTelemetry
	hasGCCodeTelemetry bool

	// Cached during validateArenaFootprint.
	maxParamSlots        int
	maxResultSlots       int
	instantiateArenaNeed int

	// validateMemo owns immutable validation sidecars and memoizes the
	// instantiate-boundary metadata validation for modules produced by
	// Compile/UnmarshalBinary: the full check (which loops all
	// funcs/globals/exports/GC descs) then only runs once instead of on every
	// Instantiate. A nil memo means "validate every time" — which is what a
	// hand-constructed Compiled (exported fields, no memo) gets, preserving its
	// first-use validation.
	validateMemo *validateMemo

	codeCache          *compiledCodeCache
	customInstructions map[uint32]railshot.CustomInstruction
	requiresBMI2       bool
	requiresAVX2       bool
	requiresAVX512     bool
	syncHostSlots      uint16
	// independentInstances allows instances without cross-instance Wasm imports
	// to use instance-local native execution leases. It is intentionally not
	// serialized because it is runtime policy rather than a module property.
	independentInstances bool
	// preparedIsolatedTables is a fresh-compilation proof that every table is
	// local, unexported, immutable, and limited to local function descriptors.
	// Like direct-prepared entry selection, codecs conservatively discard it.
	preparedIsolatedTables bool
}

// The sign bit of a fresh compilation's internal-entry offset carries the
// optional direct-prepared selection without growing Compiled. Native code
// offsets are non-negative and bounded far below the host int range. The codec
// strips this compile-only bit, so decoded artifacts retain the wrapper fallback.
var directPreparedEntryMask = ^(^uint(0) >> 1)

func markDirectPreparedEntry(off int) int { return int(uint(off) | directPreparedEntryMask) }
func directPreparedEntry(off int) bool    { return uint(off)&directPreparedEntryMask != 0 }
func internalEntryOffset(off int) int     { return int(uint(off) &^ directPreparedEntryMask) }

// RequiresBMI2 reports whether compilation selected BMI2 instructions.
func (c *Compiled) RequiresBMI2() bool { return c != nil && c.requiresBMI2 }

// RequiresAVX2 reports whether compilation selected an AVX2 plugin lowering.
func (c *Compiled) RequiresAVX2() bool { return c != nil && c.requiresAVX2 }

// RequiresAVX512 reports whether compilation selected an AVX-512 plugin lowering.
func (c *Compiled) RequiresAVX512() bool { return c != nil && c.requiresAVX512 }

type validateMemo struct {
	execution     *Compiled // private deeply owned execution metadata
	snapshotLimit uint64    // source admission policy; zero selects the default
	snapshotBytes uint64    // protected by the code-cache lock

	once                     sync.Once
	err                      error
	gcFrameRoots             *compiledGCFrameRoots // immutable compiled/codec native safepoint and callsite map
	structuralCallIdentities *structuralCallIdentityCache
	// importModuleEnds stores one plus the module-name byte length for each
	// non-global import, grouped as functions, tables, memories, then tags. A
	// zero entry retains the legacy first-dot interpretation for hand-built
	// Compiled values; source compilation always records an exact nonzero end.
	importModuleEnds []uint64

	// Fresh low-level compilation records runtime-only quotas here for a later
	// package-level Instantiate without growing Compiled. Decoded cache artifacts
	// receive the destination Runtime's current policy through InstantiateOptions.
	memoryLimitPages         uint32
	maxInstanceMetadataBytes uint64
}

// validateCached returns the metadata-validation result, running the full check
// once per compiler-produced Compiled and every time for a hand-constructed one.
func (c *Compiled) validateCached() error {
	c = c.executionView()
	memo := c.loadValidateMemo()
	if memo == nil {
		return c.validate()
	}
	memo.once.Do(func() { memo.err = c.validate() })
	return memo.err
}

// memorySizeBytes returns the initial and maximum (grow ceiling) linear-memory
// sizes in bytes for instantiation. A module without a declared memory still
// gets one page (legacy behavior). An unbounded or oversized max is capped at
// the memory32 engine ceiling of 65,536 pages (4 GiB).
func (c *Compiled) memorySizeBytes() (initial, max int) {
	const pageBytes = 65536
	const maxPagesCeil = 65536
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
func (c *Compiled) ImportedGlobalCount() int { return len(c.executionView().GlobalImports) }

// LocalGlobalCount returns the number of module-defined globals.
func (c *Compiled) LocalGlobalCount() int {
	c = c.executionView()
	return len(c.Globals) - len(c.GlobalImports)
}

// GlobalSlot maps a wasm global index to its pointer-table byte offset.
func (c *Compiled) GlobalSlot(idx int) int { return idx * 8 }

// ExportedGlobal returns metadata for a named exported global.
func (c *Compiled) ExportedGlobal(name string) (GlobalDef, bool) {
	c = c.executionView()
	idx, ok := c.GlobalExports[name]
	if !ok || idx < 0 || idx >= len(c.Globals) {
		return GlobalDef{}, false
	}
	def := c.Globals[idx]
	def.InitExpr = append([]byte(nil), def.InitExpr...)
	return def, true
}

func (c *Compiled) globalExactType(index int) (ValueTypeDescriptor, error) {
	if c == nil || index < 0 || index >= len(c.Globals) {
		return ValueTypeDescriptor{}, fmt.Errorf("global index %d out of range", index)
	}
	g := c.Globals[index]
	return exactValueType(g.Type, g.HasValueType, g.ValueTypeIndex, c.ValueTypes, c.Types)
}

func (c *Compiled) tableExactType(index int) (ValueTypeDescriptor, error) {
	if c == nil || index < 0 || index >= c.tableCount() {
		return ValueTypeDescriptor{}, fmt.Errorf("table index %d out of range", index)
	}
	def := c.tableDef(index)
	return exactValueType(c.tableElementType(index), def.HasValueType, def.ValueTypeIndex, c.ValueTypes, c.Types)
}

func (c *Compiled) elemExactType(elem ElemInit) (ValueTypeDescriptor, error) {
	// The legacy function-index encoding declares a non-null (ref func) segment,
	// but predates structural value-type metadata. Keep HasValueType false as its
	// persisted marker so MVP artifacts remain feature-free while Core 3 can use
	// the segment to initialize a non-null function table. A null initializer
	// identifies older hand-built nullable metadata instead.
	if !elem.HasValueType && normalizedElemRefType(elem.RefType) == ValFuncRef {
		legacy := true
		for _, value := range elem.Values {
			if value.Null || value.HasGlobal || value.I31Wrap || len(value.Expr) != 0 {
				legacy = false
				break
			}
		}
		if legacy {
			exact, _ := valueTypeDescriptorFromValType(ValFuncRef)
			exact.Ref.Nullable = false
			return exact, nil
		}
	}
	return exactValueType(normalizedElemRefType(elem.RefType), elem.HasValueType, elem.ValueTypeIndex, c.ValueTypes, c.Types)
}

func (c *Compiled) functionRefExactType(index uint32) (ValueTypeDescriptor, error) {
	var sig FuncSig
	if int(index) < c.NumImports {
		if int(index) >= len(c.importFuncSigs) {
			return ValueTypeDescriptor{}, fmt.Errorf("function index %d out of range", index)
		}
		sig = c.importFuncSigs[index]
	} else {
		local := int(index) - c.NumImports
		if local < 0 || local >= len(c.Funcs) {
			return ValueTypeDescriptor{}, fmt.Errorf("function index %d out of range", index)
		}
		sig = c.Funcs[local]
	}
	if sig.HasTypeIndex {
		if int(sig.TypeIndex) >= len(c.Types) || c.Types[sig.TypeIndex].Kind != CompositeTypeFunction {
			return ValueTypeDescriptor{}, fmt.Errorf("function index %d has invalid declared type %d", index, sig.TypeIndex)
		}
		return ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true, TypeIndex: sig.TypeIndex}}}, nil
	}
	return ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Abstract: AbstractHeapFunc}}}, nil
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
			if err := c.validateResolvedImportedGlobal(key, g, imp); err != nil {
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
		if err := c.validateResolvedImportedGlobal(key, g, imp); err != nil {
			return nil, err
		}
		byKey[key] = g
		globals[i] = g
	}
	return globals, nil
}

func (c *Compiled) validateResolvedImportedGlobal(key string, g *resolvedGlobalImport, imp GlobalImportDef) error {
	if g == nil {
		return fmt.Errorf("imported global %q is nil", key)
	}
	if g.global != nil {
		return c.validateImportedGlobal(key, g.global, imp)
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

func (c *Compiled) validateImportedGlobal(key string, g *Global, imp GlobalImportDef) error {
	if g == nil {
		return fmt.Errorf("imported global %q is nil", key)
	}
	actualType, actualMutable := g.Type, g.Mutable
	var actual ValueTypeDescriptor
	var actualTypes []DefinedTypeDescriptor
	var hasExact bool
	if g.owner != nil {
		o := g.owner
		o.mu.Lock()
		if o.closed || len(g.cell) < globalCellSize(o.typ) {
			o.mu.Unlock()
			return fmt.Errorf("imported global %q storage is closed", key)
		}
		actualType, actualMutable = o.typ, o.mutable
		actual, actualTypes, hasExact = o.valueType, o.types, o.hasValueType
		o.mu.Unlock()
		if g.Type != actualType || g.Mutable != actualMutable {
			return fmt.Errorf("imported global %q public metadata does not match its exact owner type", key)
		}
	} else if len(g.cell) < globalCellSize(g.Type) {
		return fmt.Errorf("imported global %q storage is closed", key)
	}
	if actualType != imp.Type {
		return fmt.Errorf("imported global %q has type %s, want %s", key, actualType, imp.Type)
	}
	if actualMutable != imp.Mutable {
		return fmt.Errorf("imported global %q mutability mismatch", key)
	}
	if !isReferenceValType(imp.Type) {
		return nil
	}
	if g.owner == nil {
		return fmt.Errorf("imported global %q has no explicit reference owner", key)
	}
	required, err := exactValueType(imp.Type, imp.HasValueType, imp.ValueTypeIndex, c.ValueTypes, c.Types)
	if err != nil {
		return fmt.Errorf("imported global %q required type: %w", key, err)
	}
	if !hasExact {
		actual, _ = valueTypeDescriptorFromValType(actualType)
	}
	compatible := valueTypeSubtype(actual, actualTypes, required, c.Types)
	if imp.Mutable {
		compatible = compatible && valueTypeSubtype(required, c.Types, actual, actualTypes)
	}
	if !compatible {
		return fmt.Errorf("imported global %q exact type is incompatible with required structural type", key)
	}
	return nil
}

func (g *Global) validateNumericImport() error {
	if g == nil || g.owner == nil {
		return fmt.Errorf("numeric global descriptor is invalid")
	}
	o := g.owner
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed || len(g.cell) < globalCellSize(o.typ) {
		return fmt.Errorf("numeric global owner is closed")
	}
	if isReferenceValType(o.typ) || o.typ != g.Type || o.mutable != g.Mutable {
		return fmt.Errorf("numeric global owner metadata is inconsistent")
	}
	if o.instance != nil && !o.instance.hasPhysicalResources() {
		return fmt.Errorf("numeric global owner instance is closed")
	}
	return nil
}

func (g *Global) attachNumericImporter() error {
	if err := g.validateNumericImport(); err != nil {
		return err
	}
	o := g.owner
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return fmt.Errorf("numeric global owner is closed")
	}
	if o.instance != nil && !o.instance.retainResourceRoot() {
		return fmt.Errorf("numeric global owner instance is closed")
	}
	o.importers++
	return nil
}

//lint:ignore U1000 retained as the collector-free reference import validation entrypoint
func (g *Global) validateReferenceImport(store *referenceStore) error {
	return g.validateReferenceImportWithCollector(store, nil)
}

func (g *Global) validateReferenceImportWithCollector(store *referenceStore, collector *gc.Collector) error {
	if g == nil || g.owner == nil {
		return fmt.Errorf("reference global descriptor is invalid")
	}
	type retainedSnapshot struct {
		root    *Instance
		precise bool
		proxy   bool
	}
	o := g.owner
	o.mu.Lock()
	if o.closed || len(g.cell) < 8 {
		o.mu.Unlock()
		return fmt.Errorf("reference global owner is closed")
	}
	if !isReferenceValType(o.typ) || o.typ != g.Type || o.mutable != g.Mutable {
		o.mu.Unlock()
		return fmt.Errorf("reference global owner metadata is inconsistent")
	}
	if store == nil || o.store == nil || o.store != store {
		o.mu.Unlock()
		return fmt.Errorf("reference global belongs to an incompatible reference store")
	}
	typ, source := o.typ, o.instance
	bits := readGlobalObject(g, typ)
	retained := make([]retainedSnapshot, 0, len(o.retained))
	for root, state := range o.retained {
		_, proxy := state.proxyDescriptors[bits]
		retained = append(retained, retainedSnapshot{root: root, precise: state.precise, proxy: proxy})
	}
	o.mu.Unlock()

	if source != nil && !source.hasPhysicalResources() {
		return fmt.Errorf("reference global owner instance is closed")
	}
	if isGCRefValType(typ) {
		if source == nil || source.gc == nil || store == nil || collector == nil || source.gc != collector || source.refStore != store || !store.ownsGCCollector(collector) {
			return fmt.Errorf("collector-reference global requires producer and importer in the same Runtime GC domain")
		}
		ref := gc.Ref(uint32(bits))
		if bits != uint64(ref) {
			return fmt.Errorf("collector-reference global contains non-compact reference %#x", bits)
		}
		if ref.IsNull() || ref.IsI31() {
			return nil
		}
		if !ref.IsObj() {
			return fmt.Errorf("collector-reference global contains invalid reference %#x", bits)
		}
		if _, err := collector.ObjectType(ref); err != nil {
			return fmt.Errorf("collector-reference global contains stale or foreign object: %w", err)
		}
		return nil
	}
	if bits == 0 {
		return nil
	}
	if typ == ValExternRef {
		if _, ok := store.resolveExternref(bits); !ok {
			return fmt.Errorf("reference global contains an invalid externref token")
		}
		return nil
	}
	store.mu.Lock()
	var ok bool
	if source == nil {
		entry := store.byIdentity[funcrefIdentity{descriptor: bits}]
		ok = entry != nil && entry.descriptor == bits
	} else {
		_, _, ok = store.canonicalFuncrefOwnerLocked(source, bits)
	}
	store.mu.Unlock()
	if !ok && source == nil {
		// Store-neutral funcref descriptors may originate in another runtime or a
		// package-level instance. A precise retained root validates the descriptor
		// directly; an unresolved proxy validates the attachment chain that kept it
		// callable. No container lock is held while instance lifetime is inspected.
		for _, candidate := range retained {
			if candidate.root == nil || !candidate.root.hasPhysicalResources() {
				continue
			}
			if candidate.proxy || (candidate.precise && candidate.root.reachesFuncrefDescriptor(bits)) {
				ok = true
				break
			}
		}
	}
	if !ok {
		return fmt.Errorf("reference global contains an invalid funcref descriptor")
	}
	return nil
}

func (g *Global) attachReferenceImporter(store *referenceStore) error {
	return g.attachReferenceImporterWithCollector(store, nil)
}

func (g *Global) attachReferenceImporterWithCollector(store *referenceStore, collector *gc.Collector) error {
	if err := g.validateReferenceImportWithCollector(store, collector); err != nil {
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
	if err := in.beginInvocation(); err != nil {
		return 0, fmt.Errorf("global %q: %w", name, err)
	}
	defer in.endInvocation()
	unlockNative := in.lockInstanceNativeStateForHostAccess()
	defer unlockNative()
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
	if err := in.beginInvocation(); err != nil {
		return V128{}, fmt.Errorf("global %q: %w", name, err)
	}
	defer in.endInvocation()
	unlockNative := in.lockInstanceNativeStateForHostAccess()
	defer unlockNative()
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
	if err := in.beginInvocation(); err != nil {
		return fmt.Errorf("global %q: %w", name, err)
	}
	defer in.endInvocation()
	unlockNative := in.lockInstanceNativeStateForHostAccess()
	defer unlockNative()
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
	if err := in.beginInvocation(); err != nil {
		return fmt.Errorf("global %q: %w", name, err)
	}
	defer in.endInvocation()
	unlockNative := in.lockInstanceNativeStateForHostAccess()
	defer unlockNative()
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

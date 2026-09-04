package wago

import (
	"errors"
	"fmt"
	"math"
	"sync/atomic"

	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

// GuestStorageAccess describes whether a host callback only observes guest
// storage or may modify it. Wago uses the distinction to reject writes through
// immutable Wasm GC arrays.
type GuestStorageAccess uint8

const (
	GuestStorageRead GuestStorageAccess = iota
	GuestStorageWrite
)

// GuestMemoryAddressType is the address width declared by one WebAssembly
// linear memory.
type GuestMemoryAddressType uint8

const (
	GuestMemory32 GuestMemoryAddressType = 32
	GuestMemory64 GuestMemoryAddressType = 64
)

// GuestMemoryInfo describes one memory in Wasm memory-index order.
type GuestMemoryInfo struct {
	AddressType GuestMemoryAddressType
	ByteLength  uint64
}

// GuestGCArrayStorage describes the physical element storage of a Wasm GC
// array. Reference arrays are deliberately separate from raw byte views.
type GuestGCArrayStorage uint8

const (
	GuestGCArrayI8 GuestGCArrayStorage = iota + 1
	GuestGCArrayI16
	GuestGCArrayI32
	GuestGCArrayI64
	GuestGCArrayF32
	GuestGCArrayF64
	GuestGCArrayV128
	GuestGCArrayRef
	GuestGCArrayFuncRef
	GuestGCArrayExternRef
)

// GuestGCRef is an opaque callback-scoped collector reference. Its zero value
// is null. A value obtained from one GuestStorage view MUST NOT be retained or
// used with another view.
type GuestGCRef struct {
	ref  gc.Ref
	view *guestStorageView
}

// IsNull reports whether this callback-scoped reference is null.
func (r GuestGCRef) IsNull() bool { return r.ref.IsNull() }

// GuestGCArrayInfo describes the actual dynamic array selected by a
// callback-scoped reference.
type GuestGCArrayInfo struct {
	Storage   GuestGCArrayStorage
	Length    uint32
	Mutable   bool
	TypeIndex uint32 // producer-local flattened defined-type index
}

// GuestStorage is a callback-scoped view of the calling Wasm instance. Direct
// slices and every GuestGCRef returned by this interface expire when the
// surrounding WithGuestStorage callback returns. Implementations keep native
// execution and collector relocation serialized for that complete lifetime, so
// multiple array views can be held at once for scatter/gather validation and
// I/O. GCArrayBytes returns a detached copy for immutable arrays because Go
// slices cannot enforce read-only access.
type GuestStorage interface {
	MemoryInfo(index uint32) (GuestMemoryInfo, error)
	MemoryRange(index uint32, offset, length uint64, access GuestStorageAccess) ([]byte, error)

	GCRef(slot uint64) (GuestGCRef, error)
	GCArrayInfo(ref GuestGCRef) (GuestGCArrayInfo, error)
	GCArrayBytes(ref GuestGCRef, access GuestStorageAccess) ([]byte, GuestGCArrayInfo, error)
	GCArrayRef(ref GuestGCRef, index uint32) (GuestGCRef, error)

	ImportParamType(index int) (ValueTypeDescriptor, bool)
	ImportResultType(index int) (ValueTypeDescriptor, bool)
	DefinedType(index uint32) (DefinedTypeDescriptor, bool)
}

// GuestStorageHostModule is the optional synchronous host-callback surface for
// direct, indexed guest storage access. HostModule intentionally remains small
// and backwards compatible; plugins opt into this interface when they need
// multi-memory, Memory64, or Wasm GC storage.
//
// Only the callback-scoped HostModule value handed to a live synchronous host
// import implements this interface. Wago rejects guest re-entry and public
// instance operations that require overlapping native-state synchronization while
// WithGuestStorage is active. They could otherwise grow a linear memory, move GC
// objects, or self-deadlock while a host view still refers to the prior storage.
type GuestStorageHostModule interface {
	HostModule
	WithGuestStorage(func(GuestStorage) error) error
}

type guestStorageView struct {
	in      *Instance
	params  []ValueTypeDescriptor
	results []ValueTypeDescriptor
	types   []DefinedTypeDescriptor
	active  atomic.Bool
}

func (v *guestStorageView) ensureActive() error {
	if v == nil || !v.active.Load() || v.in == nil {
		return fmt.Errorf("wago: guest-storage view is no longer active: %w", ErrPermissionDenied)
	}
	return nil
}

func (in *Instance) guestStorageBorrowed() bool {
	if in == nil {
		return false
	}
	state := in.pluginState.Load()
	return state != nil && state.guestStorageBorrow.Load() != 0
}

func beginGuestStorageBorrow(in *Instance) (func(), error) {
	if in == nil {
		return nil, fmt.Errorf("wago: guest storage has no instance")
	}
	state := in.ensurePluginState()
	if !state.guestStorageBorrow.CompareAndSwap(0, 1) {
		return nil, fmt.Errorf("wago: guest storage is already borrowed: %w", ErrPermissionDenied)
	}
	return func() { state.guestStorageBorrow.Store(0) }, nil
}

func (h instanceHostModule) WithGuestStorage(fn func(GuestStorage) error) error {
	if fn == nil {
		return fmt.Errorf("wago: nil guest-storage callback")
	}
	if !h.valid() || h.in == nil {
		return fmt.Errorf("wago: guest storage is outside its active host callback: %w", ErrPermissionDenied)
	}
	endBorrow, err := beginGuestStorageBorrow(h.in)
	if err != nil {
		return err
	}
	defer endBorrow()
	unlockNative := h.in.lockInstanceNativeStateForHostAccess()
	defer unlockNative()
	if h.in.gc != nil {
		lockedDomain := h.in.lockGCCollector()
		defer unlockGCCollector(lockedDomain)
	}
	var types []DefinedTypeDescriptor
	if h.ephemeralGCResults != nil && h.ephemeralGCResults.exactTypes != nil {
		types = *h.ephemeralGCResults.exactTypes
	} else if h.in.c != nil {
		types = h.in.c.Types
	}
	view := &guestStorageView{in: h.in, params: h.exactParams, results: h.exactResults, types: types}
	view.active.Store(true)
	defer view.active.Store(false)
	return fn(view)
}

func (v *guestStorageView) memory(index uint32) (*Memory, error) {
	if err := v.ensureActive(); err != nil {
		return nil, err
	}
	if v.in.c == nil {
		return nil, fmt.Errorf("wago: guest storage has no compiled metadata")
	}
	if uint64(index) >= uint64(v.in.c.memoryCount()) {
		return nil, fmt.Errorf("wago: memory index %d is out of range", index)
	}
	if v.in.memoryDir != nil {
		if int(index) >= len(v.in.memoryDir.memories) || v.in.memoryDir.memories[index] == nil {
			return nil, fmt.Errorf("wago: memory index %d is unavailable", index)
		}
		return v.in.memoryDir.memories[index], nil
	}
	if index == 0 && v.in.memory != nil {
		return v.in.memory, nil
	}
	return nil, fmt.Errorf("wago: memory index %d is unavailable", index)
}

func (v *guestStorageView) MemoryInfo(index uint32) (GuestMemoryInfo, error) {
	memory, err := v.memory(index)
	if err != nil {
		return GuestMemoryInfo{}, err
	}
	bytes := memory.UnsafeBytes()
	if bytes == nil && memory.jobMemory() == nil {
		return GuestMemoryInfo{}, fmt.Errorf("wago: memory index %d is closed", index)
	}
	addressType := GuestMemory32
	def := v.in.c.memoryDef(int(index))
	if def.Addr64 {
		addressType = GuestMemory64
	}
	return GuestMemoryInfo{AddressType: addressType, ByteLength: uint64(len(bytes))}, nil
}

func (v *guestStorageView) MemoryRange(index uint32, offset, length uint64, access GuestStorageAccess) ([]byte, error) {
	if access != GuestStorageRead && access != GuestStorageWrite {
		return nil, fmt.Errorf("wago: invalid guest-storage access %d", access)
	}
	memory, err := v.memory(index)
	if err != nil {
		return nil, err
	}
	bytes := memory.UnsafeBytes()
	if bytes == nil && memory.jobMemory() == nil {
		return nil, fmt.Errorf("wago: memory index %d is closed", index)
	}
	if offset > math.MaxUint64-length {
		return nil, fmt.Errorf("wago: memory range overflows")
	}
	end := offset + length
	if end > uint64(len(bytes)) {
		return nil, fmt.Errorf("wago: memory range [%d,%d) exceeds %d bytes", offset, end, len(bytes))
	}
	return bytes[int(offset):int(end):int(end)], nil
}

func (v *guestStorageView) GCRef(slot uint64) (GuestGCRef, error) {
	if err := v.ensureActive(); err != nil {
		return GuestGCRef{}, err
	}
	if slot == 0 {
		return GuestGCRef{view: v}, nil
	}
	if v.in.gc == nil || v.in.refStore == nil {
		return GuestGCRef{}, fmt.Errorf("wago: guest has no live Wasm GC collector")
	}
	state := v.in.existingPublicGCState()
	if state == nil {
		return GuestGCRef{}, fmt.Errorf("wago: GC reference token state is unavailable")
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	v.in.refStore.mu.Lock()
	entry, ok := v.in.refStore.gcByToken[slot]
	_, registered := v.in.refStore.instances[v.in]
	v.in.refStore.mu.Unlock()
	ownerIndex := entry.ownerIndex
	if !ok || entry.owner != v.in || !registered || ownerIndex >= state.resultRootsMade || state.resultToken(ownerIndex) != slot || state.resultRootSlot(ownerIndex) != entry.slot {
		return GuestGCRef{}, fmt.Errorf("wago: invalid, stale, or foreign GC reference token")
	}
	ref := v.in.gc.GlobalSlot(entry.slot)
	if ref.IsNull() {
		return GuestGCRef{}, fmt.Errorf("wago: GC reference token has no live object root")
	}
	return GuestGCRef{ref: ref, view: v}, nil
}

func guestArrayStorage(kind gc.StorageKind) (GuestGCArrayStorage, bool) {
	switch kind {
	case gc.StorageI8:
		return GuestGCArrayI8, true
	case gc.StorageI16:
		return GuestGCArrayI16, true
	case gc.StorageI32:
		return GuestGCArrayI32, true
	case gc.StorageI64:
		return GuestGCArrayI64, true
	case gc.StorageF32:
		return GuestGCArrayF32, true
	case gc.StorageF64:
		return GuestGCArrayF64, true
	case gc.StorageV128:
		return GuestGCArrayV128, true
	case gc.StorageRef, gc.StorageRefNull:
		return GuestGCArrayRef, true
	case gc.StorageFuncRef, gc.StorageFuncRefNull:
		return GuestGCArrayFuncRef, true
	case gc.StorageExternRef, gc.StorageExternRefNull:
		return GuestGCArrayExternRef, true
	default:
		return 0, false
	}
}

func (v *guestStorageView) gcArrayInfo(ref GuestGCRef) (GuestGCArrayInfo, gc.TypeID, error) {
	if err := v.ensureActive(); err != nil {
		return GuestGCArrayInfo{}, 0, err
	}
	if v.in.gc == nil || v.in.c == nil {
		return GuestGCArrayInfo{}, 0, fmt.Errorf("wago: guest has no live Wasm GC collector")
	}
	if ref.view != nil && ref.view != v {
		return GuestGCArrayInfo{}, 0, fmt.Errorf("wago: GC reference belongs to a different guest-storage view: %w", ErrPermissionDenied)
	}
	if ref.ref.IsNull() {
		return GuestGCArrayInfo{}, 0, fmt.Errorf("wago: null GC reference")
	}
	domainType, err := v.in.gc.ObjectType(ref.ref)
	if err != nil {
		return GuestGCArrayInfo{}, 0, err
	}
	localType, ok := v.in.gcLocalType(domainType)
	if !ok || int(localType) >= len(v.in.c.Types) || int(localType) >= len(v.in.c.GCTypeDescs) {
		return GuestGCArrayInfo{}, 0, fmt.Errorf("wago: GC object type %d has no local descriptor", domainType)
	}
	defined := v.in.c.Types[localType]
	gcDesc := v.in.c.GCTypeDescs[localType]
	if defined.Kind != CompositeTypeArray || gcDesc.Kind != gc.KindArray {
		return GuestGCArrayInfo{}, 0, fmt.Errorf("wago: GC reference is not an array")
	}
	storage, ok := guestArrayStorage(gcDesc.Elem)
	if !ok {
		return GuestGCArrayInfo{}, 0, fmt.Errorf("wago: GC array has unsupported storage %d", gcDesc.Elem)
	}
	length, err := v.in.gc.ArrayLen(ref.ref)
	if err != nil {
		return GuestGCArrayInfo{}, 0, err
	}
	return GuestGCArrayInfo{Storage: storage, Length: length, Mutable: defined.Array.Mutable, TypeIndex: localType}, domainType, nil
}

func (v *guestStorageView) GCArrayInfo(ref GuestGCRef) (GuestGCArrayInfo, error) {
	info, _, err := v.gcArrayInfo(ref)
	return info, err
}

func (v *guestStorageView) GCArrayBytes(ref GuestGCRef, access GuestStorageAccess) ([]byte, GuestGCArrayInfo, error) {
	if access != GuestStorageRead && access != GuestStorageWrite {
		return nil, GuestGCArrayInfo{}, fmt.Errorf("wago: invalid guest-storage access %d", access)
	}
	info, _, err := v.gcArrayInfo(ref)
	if err != nil {
		return nil, GuestGCArrayInfo{}, err
	}
	if info.Storage == GuestGCArrayRef || info.Storage == GuestGCArrayFuncRef || info.Storage == GuestGCArrayExternRef {
		return nil, info, fmt.Errorf("wago: reference arrays do not expose raw byte views")
	}
	if access == GuestStorageWrite && !info.Mutable {
		return nil, info, fmt.Errorf("wago: GC array is immutable")
	}
	payload, err := v.in.gc.ArrayPayload(ref.ref)
	if err != nil {
		return nil, info, err
	}
	if !info.Mutable {
		return append([]byte(nil), payload.Bytes...), info, nil
	}
	return payload.Bytes, info, nil
}

func (v *guestStorageView) GCArrayRef(ref GuestGCRef, index uint32) (GuestGCRef, error) {
	info, _, err := v.gcArrayInfo(ref)
	if err != nil {
		return GuestGCRef{}, err
	}
	if info.Storage != GuestGCArrayRef {
		return GuestGCRef{}, fmt.Errorf("wago: GC array does not contain collector references")
	}
	if index >= info.Length {
		return GuestGCRef{}, fmt.Errorf("wago: GC array index %d is out of range", index)
	}
	value, err := v.in.gc.ArrayGet(ref.ref, index)
	if err != nil {
		return GuestGCRef{}, err
	}
	if value.Kind != gc.StorageRef && value.Kind != gc.StorageRefNull {
		return GuestGCRef{}, errors.New("wago: GC array reference storage changed during host view")
	}
	return GuestGCRef{ref: value.Ref, view: v}, nil
}

func (v *guestStorageView) ImportParamType(index int) (ValueTypeDescriptor, bool) {
	if v == nil || !v.active.Load() || index < 0 || index >= len(v.params) {
		return ValueTypeDescriptor{}, false
	}
	return v.params[index], true
}

func (v *guestStorageView) ImportResultType(index int) (ValueTypeDescriptor, bool) {
	if v == nil || !v.active.Load() || index < 0 || index >= len(v.results) {
		return ValueTypeDescriptor{}, false
	}
	return v.results[index], true
}

func (v *guestStorageView) DefinedType(index uint32) (DefinedTypeDescriptor, bool) {
	if v == nil || !v.active.Load() || int(index) >= len(v.types) {
		return DefinedTypeDescriptor{}, false
	}
	return v.types[index], true
}

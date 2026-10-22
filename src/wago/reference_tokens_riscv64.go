//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// Called with store.mu held after descriptor membership/index validation. The
// importer owns the framed wrapper; its frozen attachment retains the producer.
// Do not take a native execution or instance lifetime lock under the store lock.
func railshotCanonicalImportedFuncref(store *referenceStore, source *Instance, ex *InstanceExport, index int, descriptor uint64) bool {
	if source.refStore != store || !store.hasInstanceResourcesLocked(source) {
		return false
	}
	// The descriptor is the importer's own framed wrapper, including for a
	// foreign Runtime producer. Its exact frozen native attachment owns the
	// producer physically; no foreign store lookup or raw pointer is authority.
	if source.importReferenceNative(index) != ex {
		return false
	}
	if ex.inst.refStore == store && !store.hasInstanceResourcesLocked(ex.inst) {
		return false
	}
	return railshotPublishedFuncref(store, source, index, descriptor)
}

// All fields come from retained arenas and frozen publication metadata.
func railshotPublishedFuncref(store *referenceStore, source *Instance, index int, descriptor uint64) bool {
	if source.refStore != store || !store.hasInstanceResourcesLocked(source) {
		return false
	}
	entries := source.importReferenceEntries()
	if index < 0 || index >= len(entries) || entries[index].address == 0 || entries[index].address&15 != 0 || source.nativeContext == 0 || source.jm == nil {
		return false
	}
	off := (index + 1) * coreruntime.FuncRefDescBytes
	if off < coreruntime.FuncRefDescBytes || off+coreruntime.FuncRefDescBytes > len(source.funcRefDescs) {
		return false
	}
	entry := source.funcRefDescs[off : off+coreruntime.FuncRefDescBytes]
	key := rv64AttachedFunctionTypeKey(source, index)
	return key != 0 && binary.LittleEndian.Uint64(entry[coreruntime.TableEntryCodePtrOffset:]) == entries[index].address &&
		binary.LittleEndian.Uint64(entry[coreruntime.TableEntrySigKeyOffset:]) == key &&
		binary.LittleEndian.Uint64(entry[coreruntime.TableEntryHomeLinMemOffset:]) == uint64(source.jm.LinMemBase())|abi.FuncRefLocalWrapperHomeTag &&
		binary.LittleEndian.Uint64(entry[coreruntime.TableEntryRefSlotOffset:]) == descriptor &&
		binary.LittleEndian.Uint64(entry[coreruntime.FuncRefContextOffset:]) == uint64(source.nativeContext)
}

// Plain Go host imports have no HostFuncRef owner. Their published framed
// wrapper and synchronous callback both belong to the importing instance.
func railshotCanonicalPlainHostFuncref(store *referenceStore, source *Instance, index int, descriptor uint64) bool {
	entries := source.importReferenceEntries()
	return index >= 0 && index < len(entries) && index < len(source.syncHosts) &&
		entries[index].native == nil && entries[index].host == nil && source.syncHosts[index].callable() &&
		railshotPublishedFuncref(store, source, index, descriptor)
}

// Store lock precedes the host lock. Canonicalization may choose an already
// token-rooted importer; authenticate that retained slot as well as the input.
func railshotCanonicalHostFuncref(store *referenceStore, source *Instance, host *HostFuncRef, index int, descriptor uint64) (*Instance, uint64, bool) {
	if host.store != store || !railshotPublishedFuncref(store, source, index, descriptor) {
		return nil, 0, false
	}
	owner, canonical, valid := host.canonicalDescriptor(source, descriptor, source.c.importFuncSigs[index])
	if !valid || owner == nil {
		return nil, 0, false
	}
	canonicalIndex, valid := owner.funcrefDescriptorIndex(canonical)
	if !valid || owner.importReferenceHost(canonicalIndex) != host || !railshotPublishedFuncref(store, owner, canonicalIndex, canonical) {
		return nil, 0, false
	}
	return owner, canonical, true
}

// Pin the caller before taking store.mu, without holding lifeMu across it.
// Token roots pin the actual owner; identity lookup uses frozen metadata.
func railshotFuncRefMatchesFunction(in *Instance, ref FuncRef, index uint32) bool {
	if !in.retainResourceRoot() {
		return false
	}
	defer in.releaseResourceRoot()
	in.lifeMu.Lock()
	store := in.refStore
	validCaller := !in.closed && !in.resourcesClosed && int(index) < len(in.c.FuncTypeID) && !railshotNonCallableFunction(in.c, int(index))
	in.lifeMu.Unlock()
	if store == nil || !validCaller {
		return false
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	entry := store.byToken[ref.token]
	if entry == nil || entry.owner == nil || !store.hasInstanceResourcesLocked(entry.owner) {
		return false
	}
	if _, _, valid := store.canonicalFuncrefOwnerLocked(entry.owner, entry.descriptor); !valid {
		return false
	}
	actualHost := entry.owner.hostFuncRefForDescriptor(entry.descriptor)
	var expectedHost *HostFuncRef
	expected := funcrefIdentity{instance: in, localIdx: int(index) - in.c.NumImports}
	if int(index) < in.c.NumImports {
		off := (int(index) + 1) * coreruntime.FuncRefDescBytes
		if off+coreruntime.FuncRefDescBytes > len(in.funcRefDescs) {
			return false
		}
		descriptor := uint64(offHeapSlicePtr(in.funcRefDescs)) + uint64(off)
		if _, _, valid := store.canonicalFuncrefOwnerLocked(in, descriptor); !valid {
			return false
		}
		expectedHost = in.importReferenceHost(int(index))
		if actualHost != nil || expectedHost != nil {
			return actualHost != nil && actualHost == expectedHost
		}
		var valid bool
		expected, valid = in.funcrefFunctionIdentity(descriptor)
		if !valid {
			return false
		}
	}
	if actualHost != nil {
		return false
	}
	actual, valid := entry.owner.funcrefFunctionIdentity(entry.descriptor)
	return valid && actual == expected
}

// The store lock proves physical lifetime before the bounded native row read.
func railshotValidFuncrefPublication(store *referenceStore, source *Instance, descriptor uint64) bool {
	if source == nil || source.refStore != store || !store.hasInstanceResourcesLocked(source) {
		return false
	}
	_, valid := rv64PublishedDescriptor(source, descriptor)
	return valid
}

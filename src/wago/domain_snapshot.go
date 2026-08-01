package wago

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/gc"
)

// DomainSnapshot is an immutable in-memory capture of every live instance in one
// Runtime WasmGC collector domain. Member order is supplied by CaptureDomain and
// is preserved on restore so internal imports have stable member identities.
type DomainSnapshot struct {
	members []domainSnapshotMember
	objects []domainGCObjectSnapshot
	gc      GCConfig
}

type domainSnapshotMember struct {
	state        *Snapshot
	imports      []domainSnapshotImport
	globalRefs   []gcSnapshotRef
	tableRoots   [][]gcSnapshotRef
	elementRoots [][]gcSnapshotRef
}

type domainSnapshotImport struct {
	key    string
	kind   uint8
	member uint32
	index  uint32
}

const (
	domainImportFunction uint8 = iota + 1
	domainImportGlobal
	domainImportTable
	domainImportMemory
	domainImportTag
)

type domainGCObjectSnapshot struct {
	typeMember uint32
	typeID     gc.TypeID
	arrayLen   uint32
	values     []gcSnapshotValue
}

// CaptureDomain captures an exhaustive, explicitly ordered set of instances
// sharing one Runtime GC collector. Calls are quiesced process-wide during the
// copy. The bounded product rejects active calls, live public GC tokens, external
// imports, shared memories, opaque references, independently owned passive GC
// payloads, and an incomplete member list. Same-domain imported memory32/memory64
// aliases and exception-tag aliases are captured through their owning member.
// Live passive GC values may depend on immutable internal GC globals; WGDN v3
// persists their exact stable-ID roots. Immutable tag directories carry no
// post-instantiation mutable state.
func CaptureDomain(instances ...*Instance) (*DomainSnapshot, error) {
	if len(instances) == 0 {
		return nil, errors.New("wago: domain snapshot requires at least one instance")
	}
	first := instances[0]
	if first == nil || first.refStore == nil || first.refStore.private || first.gc == nil {
		return nil, errors.New("wago: domain snapshot requires Runtime-owned WasmGC instances")
	}
	store, collector := first.refStore, first.gc
	memberIndex := make(map[*Instance]uint32, len(instances))
	for i, in := range instances {
		if in == nil || in.refStore != store || in.gc != collector {
			return nil, fmt.Errorf("wago: domain snapshot member %d is outside the selected Runtime GC domain", i)
		}
		if _, duplicate := memberIndex[in]; duplicate {
			return nil, fmt.Errorf("wago: domain snapshot member %d is duplicated", i)
		}
		memberIndex[in] = uint32(i)
	}

	unlockNative := lockNativeExecutionForHostAccess()
	defer unlockNative()
	domain := store.lockGCCollector(collector)
	if domain == nil {
		return nil, errors.New("wago: domain snapshot collector is no longer live")
	}
	defer unlockGCCollector(domain)

	store.mu.Lock()
	live := 0
	for in, state := range store.instances {
		if state == nil || state.resourcesReleased || in == nil || in.gc != collector {
			continue
		}
		live++
		if _, ok := memberIndex[in]; !ok {
			store.mu.Unlock()
			return nil, errors.New("wago: domain snapshot member list is incomplete")
		}
		if state.closeAccounted || state.quiesced {
			store.mu.Unlock()
			return nil, errors.New("wago: domain snapshot contains a closing instance")
		}
	}
	for _, token := range store.gcByToken {
		if token.owner != nil && token.owner.gc == collector {
			store.mu.Unlock()
			return nil, errors.New("wago: domain snapshot rejects live public GC reference tokens")
		}
	}
	store.mu.Unlock()
	if live != len(instances) {
		return nil, errors.New("wago: domain snapshot member list is incomplete")
	}

	out := &DomainSnapshot{members: make([]domainSnapshotMember, len(instances)), gc: domain.config}
	for i, in := range instances {
		if in.invocationState.Load()&instanceInvocationCount != 0 {
			return nil, fmt.Errorf("wago: domain snapshot member %d has an active invocation", i)
		}
		if err := validateDomainSnapshotMember(in); err != nil {
			return nil, fmt.Errorf("wago: domain snapshot member %d: %w", i, err)
		}
		state := captureInstanceSnapshot(in, SnapshotOptions{Kind: SnapshotInit, GC: domain.config})
		state.imports = nil
		out.members[i].state = state
		imports, err := captureDomainMemberImports(in, instances, memberIndex)
		if err != nil {
			return nil, fmt.Errorf("wago: domain snapshot member %d imports: %w", i, err)
		}
		out.members[i].imports = imports
	}
	if err := captureDomainGCGraph(instances, out); err != nil {
		return nil, fmt.Errorf("wago: domain snapshot GC graph: %w", err)
	}
	return out, nil
}

func validateDomainSnapshotMember(in *Instance) error {
	if in == nil || in.c == nil || in.c.boundsMode == BoundsChecksSignalsBased {
		return errors.New("signals-based or unavailable modules are unsupported")
	}
	if state := in.existingPublicGCState(); state != nil {
		state.mu.Lock()
		activeArgs := state.argumentRootCount
		state.mu.Unlock()
		if activeArgs != 0 {
			return errors.New("checked host argument roots are active")
		}
	}
	for i := 0; i < in.c.memoryCount(); i++ {
		def := in.c.memoryDef(i)
		if def.Shared {
			return fmt.Errorf("memory %d is shared", i)
		}
	}
	if lens := capturePassiveElemLens(in); len(lens) != 0 {
		if err := validateDomainPassiveElements(in.c, lens); err != nil {
			return fmt.Errorf("passive element state: %w", err)
		}
	}
	for i := range in.c.Globals {
		if isReferenceValType(in.c.Globals[i].Type) && !isGCRefValType(in.c.Globals[i].Type) {
			return fmt.Errorf("global %d has opaque reference storage", i)
		}
	}
	for i := 0; i < in.c.tableCount(); i++ {
		if !isGCRefValType(in.c.tableElementType(i)) {
			return fmt.Errorf("table %d has opaque reference storage", i)
		}
	}
	return nil
}

func validateDomainPassiveElements(c *Compiled, lens []uint32) error {
	if err := validatePassiveElemLens(c, lens); err != nil {
		return err
	}
	totalFuncs := c.NumImports + len(c.Funcs)
	for i, length := range lens {
		if length == 0 {
			continue
		}
		elem := &c.passiveElems[i]
		refType := normalizedElemRefType(elem.RefType)
		required, err := c.elemExactType(*elem)
		if err != nil {
			return fmt.Errorf("live element %d exact type: %w", i, err)
		}
		for valueIndex, value := range elem.Values {
			if value.HasGlobal {
				if int(value.GlobalIndex) >= len(c.Globals) || c.Globals[value.GlobalIndex].Mutable || !isGCRefValType(c.Globals[value.GlobalIndex].Type) {
					return fmt.Errorf("live element %d value %d reference global %d is unavailable, mutable, or externally owned", i, valueIndex, value.GlobalIndex)
				}
				actual, actualErr := c.globalExactType(int(value.GlobalIndex))
				if actualErr != nil || !valueTypeSubtype(actual, c.Types, required, c.Types) {
					return fmt.Errorf("live element %d value %d reference global type mismatch", i, valueIndex)
				}
				continue
			}
			switch refType {
			case ValFuncRef:
				if !value.Null && int(value.FuncIndex) >= totalFuncs {
					return fmt.Errorf("live element %d value %d has unavailable function %d", i, valueIndex, value.FuncIndex)
				}
			case ValExternRef:
				if !value.Null {
					return fmt.Errorf("live element %d value %d contains an opaque externref", i, valueIndex)
				}
			case ValI31Ref:
				if !value.Null && value.FuncIndex&1 == 0 {
					return fmt.Errorf("live element %d value %d contains an invalid i31 immediate", i, valueIndex)
				}
			default:
				if !value.Null {
					return fmt.Errorf("live element %d value %d has unsupported non-global reference type %s", i, valueIndex, elem.RefType)
				}
			}
		}
	}
	return nil
}

func captureDomainMemberImports(in *Instance, members []*Instance, indexes map[*Instance]uint32) ([]domainSnapshotImport, error) {
	var out []domainSnapshotImport
	for _, key := range in.c.Imports {
		ex, ok := in.imports[key].(*InstanceExport)
		if !ok || ex == nil || ex.inst == nil || ex.localIdx < 0 {
			return nil, fmt.Errorf("function import %q is external or invalid", key)
		}
		member, ok := indexes[ex.inst]
		if !ok {
			return nil, fmt.Errorf("function import %q owner is outside the domain", key)
		}
		out = append(out, domainSnapshotImport{key: key, kind: domainImportFunction, member: member, index: uint32(ex.localIdx)})
	}
	for _, imp := range in.c.GlobalImports {
		key := imp.Module + "." + imp.Name
		value, ok := in.imports.global(key)
		if !ok || value.Global == nil {
			return nil, fmt.Errorf("global import %q is external", key)
		}
		member, index, ok := findDomainOwnedGlobal(value.Global, members)
		if !ok {
			return nil, fmt.Errorf("global import %q owner is outside the domain", key)
		}
		out = append(out, domainSnapshotImport{key: key, kind: domainImportGlobal, member: member, index: index})
	}
	for tableIndex := 0; tableIndex < in.c.tableCount(); tableIndex++ {
		def, imported := in.c.tableImportAt(tableIndex)
		if !imported {
			continue
		}
		table, ok := in.imports.table(def.Key)
		if !ok || table == nil {
			return nil, fmt.Errorf("table import %q is external", def.Key)
		}
		member, index, ok := findDomainOwnedTable(table, members)
		if !ok {
			return nil, fmt.Errorf("table import %q owner is outside the domain", def.Key)
		}
		out = append(out, domainSnapshotImport{key: def.Key, kind: domainImportTable, member: member, index: index})
	}
	memoryLinks := make(map[string]domainSnapshotImport)
	for memoryIndex := 0; memoryIndex < in.c.memoryCount(); memoryIndex++ {
		def, imported := in.c.memoryImportAt(memoryIndex)
		if !imported {
			continue
		}
		memory, ok := in.imports.memory(def.ImportKey)
		if !ok || memory == nil {
			return nil, fmt.Errorf("memory import %q is external", def.ImportKey)
		}
		member, index, ok := findDomainOwnedMemory(memory, members)
		if !ok {
			return nil, fmt.Errorf("memory import %q owner is outside the domain", def.ImportKey)
		}
		link := domainSnapshotImport{key: def.ImportKey, kind: domainImportMemory, member: member, index: index}
		if previous, duplicate := memoryLinks[def.ImportKey]; duplicate {
			if previous != link {
				return nil, fmt.Errorf("memory import %q resolves inconsistently", def.ImportKey)
			}
			continue
		}
		memoryLinks[def.ImportKey] = link
		out = append(out, link)
	}
	tagLinks := make(map[string]domainSnapshotImport)
	for tagIndex := 0; tagIndex < in.c.tagImportCount(); tagIndex++ {
		def := in.c.memoryDir.ehTags[tagIndex]
		tag, ok := in.imports.tag(def.ImportKey)
		if !ok {
			return nil, fmt.Errorf("tag import %q is external", def.ImportKey)
		}
		member, index, ok := findDomainOwnedTag(tag, members)
		if !ok {
			return nil, fmt.Errorf("tag import %q owner is outside the domain", def.ImportKey)
		}
		link := domainSnapshotImport{key: def.ImportKey, kind: domainImportTag, member: member, index: index}
		if previous, duplicate := tagLinks[def.ImportKey]; duplicate {
			if previous != link {
				return nil, fmt.Errorf("tag import %q resolves inconsistently", def.ImportKey)
			}
			continue
		}
		tagLinks[def.ImportKey] = link
		out = append(out, link)
	}
	return out, nil
}

func findDomainOwnedGlobal(global *Global, members []*Instance) (uint32, uint32, bool) {
	for member, in := range members {
		for i := len(in.c.GlobalImports); i < len(in.globalCells); i++ {
			if in.globalCells[i] == global {
				return uint32(member), uint32(i), true
			}
		}
	}
	return 0, 0, false
}

func findDomainOwnedTag(tag *Tag, members []*Instance) (uint32, uint32, bool) {
	if tag == nil || tag.owner == nil || tag.index < 0 {
		return 0, 0, false
	}
	for member, in := range members {
		if in != tag.owner || in.c == nil || in.c.memoryDir == nil || tag.index >= len(in.c.memoryDir.ehTags) || in.c.memoryDir.ehTags[tag.index].ImportKey != "" {
			continue
		}
		return uint32(member), uint32(tag.index), true
	}
	return 0, 0, false
}

func findDomainOwnedMemory(memory *Memory, members []*Instance) (uint32, uint32, bool) {
	for member, in := range members {
		for i := 0; i < in.c.memoryCount(); i++ {
			if _, imported := in.c.memoryImportAt(i); imported {
				continue
			}
			candidate, owned := in.instanceMemoryAt(i)
			if owned && candidate == memory {
				return uint32(member), uint32(i), true
			}
		}
	}
	return 0, 0, false
}

func findDomainOwnedTable(table *Table, members []*Instance) (uint32, uint32, bool) {
	if table == nil || len(table.desc) == 0 {
		return 0, 0, false
	}
	for member, in := range members {
		for i := 0; i < in.c.tableCount(); i++ {
			if _, imported := in.c.tableImportAt(i); imported {
				continue
			}
			desc := in.tableDescriptor(i)
			if len(desc) != 0 && &desc[0] == &table.desc[0] {
				return uint32(member), uint32(i), true
			}
		}
	}
	return 0, 0, false
}

func captureDomainGCGraph(instances []*Instance, out *DomainSnapshot) error {
	collector := instances[0].gc
	ids := make(map[gc.Ref]uint32)
	queue := make([]gc.Ref, 0, 16)
	encode := func(ref gc.Ref) (gcSnapshotRef, error) {
		if ref.IsNull() {
			return gcSnapshotRef{}, nil
		}
		if ref.IsI31() {
			return gcSnapshotRef{kind: gcSnapshotRefI31, value: uint32(ref)}, nil
		}
		if id, ok := ids[ref]; ok {
			return gcSnapshotRef{kind: gcSnapshotRefObject, value: id}, nil
		}
		if _, err := collector.ObjectType(ref); err != nil {
			return gcSnapshotRef{}, err
		}
		id := uint32(len(queue) + 1)
		ids[ref], queue = id, append(queue, ref)
		return gcSnapshotRef{kind: gcSnapshotRefObject, value: id}, nil
	}
	for member, in := range instances {
		roots := make([]gcSnapshotRef, len(in.globalCells))
		for i, cell := range in.globalCells {
			if cell == nil || i >= len(in.c.Globals) || !isGCRefValType(in.c.Globals[i].Type) {
				continue
			}
			bits := readGlobalObject(cell, in.c.Globals[i].Type)
			ref := gc.Ref(uint32(bits))
			if bits != uint64(ref) {
				return fmt.Errorf("member %d global %d contains non-compact reference", member, i)
			}
			encoded, err := encode(ref)
			if err != nil {
				return fmt.Errorf("member %d global %d: %w", member, i, err)
			}
			roots[i] = encoded
			out.members[member].state.globals[i].bits = 0
		}
		out.members[member].globalRefs = roots
		tables := make([][]gcSnapshotRef, in.c.tableCount())
		for tableIndex := range tables {
			desc := in.tableDescriptor(tableIndex)
			if len(desc) < 8 {
				return fmt.Errorf("member %d table %d descriptor is unavailable", member, tableIndex)
			}
			length := int(binary.LittleEndian.Uint32(desc))
			if length > (len(desc)-8)/8 {
				return fmt.Errorf("member %d table %d descriptor is invalid", member, tableIndex)
			}
			tables[tableIndex] = make([]gcSnapshotRef, length)
			for i := 0; i < length; i++ {
				bits := binary.LittleEndian.Uint64(desc[8+i*8:])
				ref := gc.Ref(uint32(bits))
				if bits != uint64(ref) {
					return fmt.Errorf("member %d table %d slot %d contains non-compact reference", member, tableIndex, i)
				}
				encoded, err := encode(ref)
				if err != nil {
					return err
				}
				tables[tableIndex][i] = encoded
			}
		}
		out.members[member].tableRoots = tables

		lens := snapshotPassiveElemLens(out.members[member].state)
		elements := make([][]gcSnapshotRef, len(in.c.passiveElems))
		if len(elements) != 0 {
			descBase := in.jm.PassiveElemPtr()
			if descBase == 0 {
				return fmt.Errorf("member %d passive element descriptors are unavailable", member)
			}
			descBytes := runtime.PassiveElemDescBytes * len(elements)
			descs := unsafe.Slice((*byte)(offHeapPtr(descBase)), descBytes)
			for segment, elem := range in.c.passiveElems {
				length := lens[segment]
				needsRoots := false
				for _, value := range elem.Values {
					needsRoots = needsRoots || value.HasGlobal
				}
				if length == 0 || !needsRoots || !isGCRefValType(normalizedElemRefType(elem.RefType)) {
					continue
				}
				off := segment * runtime.PassiveElemDescBytes
				entryPtr := uintptr(binary.LittleEndian.Uint64(descs[off:]))
				if entryPtr == 0 {
					return fmt.Errorf("member %d passive element %d has no live payload", member, segment)
				}
				entries := unsafe.Slice((*byte)(offHeapPtr(entryPtr)), int(length)*8)
				elements[segment] = make([]gcSnapshotRef, length)
				for i := uint32(0); i < length; i++ {
					bits := binary.LittleEndian.Uint64(entries[i*8:])
					ref := gc.Ref(uint32(bits))
					if bits != uint64(ref) {
						return fmt.Errorf("member %d passive element %d value %d contains a non-compact reference", member, segment, i)
					}
					encoded, err := encode(ref)
					if err != nil {
						return fmt.Errorf("member %d passive element %d value %d: %w", member, segment, i, err)
					}
					elements[segment][i] = encoded
				}
			}
		}
		out.members[member].elementRoots = elements
	}
	for pos := 0; pos < len(queue); pos++ {
		ref := queue[pos]
		domainType, err := collector.ObjectType(ref)
		if err != nil {
			return err
		}
		originMember, originType, found := uint32(0), uint32(0), false
		for i, in := range instances {
			if local, ok := in.gcLocalType(domainType); ok {
				originMember, originType, found = uint32(i), local, true
				break
			}
		}
		if !found {
			return fmt.Errorf("object %d canonical type %d has no member identity", pos+1, domainType)
		}
		desc, ok := snapshotGCDescriptor(instances[originMember].c, gc.TypeID(originType))
		if !ok || (desc.Kind != gc.KindStruct && desc.Kind != gc.KindArray) {
			return fmt.Errorf("object %d has unavailable type", pos+1)
		}
		record := domainGCObjectSnapshot{typeMember: originMember, typeID: gc.TypeID(originType)}
		count := uint32(len(desc.Fields))
		if desc.Kind == gc.KindArray {
			count, err = collector.ArrayLen(ref)
			if err != nil {
				return err
			}
			record.arrayLen = count
		}
		record.values = make([]gcSnapshotValue, count)
		for i := uint32(0); i < count; i++ {
			var value gc.Value
			if desc.Kind == gc.KindStruct {
				value, err = collector.StructGet(ref, i)
			} else {
				value, err = collector.ArrayGet(ref, i)
			}
			if err != nil {
				return err
			}
			entry := gcSnapshotValue{kind: value.Kind, bits: value.Bits, bitsHi: value.BitsHi}
			if value.Kind == gc.StorageRef || value.Kind == gc.StorageRefNull {
				entry.ref, err = encode(value.Ref)
				entry.bits = 0
				if err != nil {
					return err
				}
			} else if (value.Kind == gc.StorageFuncRef || value.Kind == gc.StorageFuncRefNull || value.Kind == gc.StorageExternRef || value.Kind == gc.StorageExternRefNull) && value.Bits != 0 {
				return fmt.Errorf("object %d value %d contains a non-null opaque reference", pos+1, i)
			}
			record.values[i] = entry
		}
		out.objects = append(out.objects, record)
	}
	return nil
}

// Instantiate restores every member into rt and publishes the returned slice only
// after all modules, aliases, and the shared object graph have been reconstructed.
func (s *DomainSnapshot) Instantiate(rt *Runtime) ([]*Instance, error) {
	if err := validateDomainSnapshot(s); err != nil {
		return nil, err
	}
	if rt == nil || rt.refStore == nil {
		return nil, errors.New("wago: domain snapshot restore requires a Runtime")
	}
	rt.refStore.mu.Lock()
	if rt.refStore.runtimeClosed {
		rt.refStore.mu.Unlock()
		return nil, errors.New("wago: domain snapshot restore Runtime is closed")
	}
	if rt.refStore.domainSnapshotRestoring || rt.refStore.gcDomains != nil {
		rt.refStore.mu.Unlock()
		return nil, errors.New("wago: domain snapshot restore requires a Runtime without a live GC domain")
	}
	rt.refStore.domainSnapshotRestoring = true
	rt.refStore.mu.Unlock()
	defer func() {
		rt.refStore.mu.Lock()
		rt.refStore.domainSnapshotRestoring = false
		rt.refStore.mu.Unlock()
	}()
	restored := make([]*Instance, len(s.members))
	remaining := len(restored)
	for remaining != 0 {
		progress := false
		for i := range s.members {
			if restored[i] != nil {
				continue
			}
			ready := true
			imports := make(Imports, len(s.members[i].imports))
			for _, imp := range s.members[i].imports {
				if int(imp.member) >= len(restored) || restored[imp.member] == nil {
					ready = false
					break
				}
				owner := restored[imp.member]
				switch imp.kind {
				case domainImportFunction:
					if int(imp.index) >= len(owner.c.Funcs) {
						return closeDomainRestore(restored, fmt.Errorf("member %d function import target is unavailable", i))
					}
					imports[imp.key] = &InstanceExport{inst: owner, localIdx: int(imp.index), params: owner.c.Funcs[imp.index].Params, results: owner.c.Funcs[imp.index].Results}
				case domainImportGlobal:
					global, err := owner.domainSnapshotGlobal(int(imp.index))
					if err != nil {
						return closeDomainRestore(restored, err)
					}
					imports[imp.key] = global
				case domainImportTable:
					table, err := owner.domainSnapshotTable(int(imp.index))
					if err != nil {
						return closeDomainRestore(restored, err)
					}
					imports[imp.key] = table
				case domainImportMemory:
					memory, err := owner.domainSnapshotMemory(int(imp.index))
					if err != nil {
						return closeDomainRestore(restored, err)
					}
					imports[imp.key] = memory
				case domainImportTag:
					tag, err := owner.domainSnapshotTag(int(imp.index))
					if err != nil {
						return closeDomainRestore(restored, err)
					}
					imports[imp.key] = tag
				default:
					return closeDomainRestore(restored, fmt.Errorf("member %d has unknown import kind %d", i, imp.kind))
				}
			}
			if !ready {
				continue
			}
			state := *s.members[i].state
			state.gcGlobalRefs, state.gcTableRefs, state.gcTableRoots, state.gcObjects = nil, nil, nil, nil
			in, err := instantiateCore(state.c, InstantiateOptions{Imports: imports, GC: s.gc, store: rt.refStore, restore: &state, runtime: rt, domainRestore: true})
			if err != nil {
				return closeDomainRestore(restored, fmt.Errorf("wago: restore domain member %d: %w", i, err))
			}
			restored[i], remaining, progress = in, remaining-1, true
		}
		if !progress {
			return closeDomainRestore(restored, errors.New("wago: domain snapshot import graph is cyclic or incomplete"))
		}
	}
	if err := restoreDomainGCGraph(restored, s); err != nil {
		return closeDomainRestore(restored, fmt.Errorf("wago: restore domain GC graph: %w", err))
	}
	return restored, nil
}

func (in *Instance) instanceMemoryAt(index int) (*Memory, bool) {
	if in == nil || in.c == nil || index < 0 || index >= in.c.memoryCount() {
		return nil, false
	}
	if index == 0 {
		return in.memory, in.ownsMem
	}
	if in.memoryDir == nil || index >= len(in.memoryDir.memories) || index >= len(in.memoryDir.owns) {
		return nil, false
	}
	return in.memoryDir.memories[index], in.memoryDir.owns[index]
}

func (in *Instance) domainSnapshotTag(index int) (*Tag, error) {
	if in == nil || in.c == nil || in.c.memoryDir == nil || index < 0 || index >= len(in.c.memoryDir.ehTags) {
		return nil, fmt.Errorf("domain snapshot tag %d is unavailable", index)
	}
	def := in.c.memoryDir.ehTags[index]
	if def.ImportKey != "" {
		return nil, fmt.Errorf("domain snapshot tag %d is not locally owned", index)
	}
	state := in.ensurePluginState()
	in.lifeMu.Lock()
	defer in.lifeMu.Unlock()
	if in.closed || in.resourcesClosed || state.tagIdentityBase == 0 {
		return nil, fmt.Errorf("domain snapshot tag %d owner is unavailable", index)
	}
	if state.tagExports == nil {
		state.tagExports = make(map[int]*Tag)
	}
	if tag := state.tagExports[index]; tag != nil {
		return tag, nil
	}
	tag := &Tag{owner: in, index: index, typeIndex: def.TypeIndex, identity: uint64(state.tagIdentityBase + uintptr(index*8))}
	state.tagExports[index] = tag
	return tag, nil
}

func (in *Instance) domainSnapshotMemory(index int) (*Memory, error) {
	memory, owned := in.instanceMemoryAt(index)
	if memory == nil || !owned {
		return nil, fmt.Errorf("domain snapshot memory %d is unavailable", index)
	}
	if err := memory.share(in, in.c.memoryDef(index)); err != nil {
		return nil, fmt.Errorf("domain snapshot memory %d: %w", index, err)
	}
	return memory, nil
}

func (in *Instance) domainSnapshotGlobal(index int) (*Global, error) {
	if in == nil || in.c == nil || index < len(in.c.GlobalImports) || index < 0 || index >= len(in.globalCells) || in.globalCells[index] == nil {
		return nil, fmt.Errorf("domain snapshot global %d is unavailable", index)
	}
	global := in.globalCells[index]
	exact, err := in.c.globalExactType(index)
	if err != nil {
		return nil, fmt.Errorf("domain snapshot global %d exact type: %w", index, err)
	}
	in.lifeMu.Lock()
	if global.owner == nil {
		global.owner = &globalOwner{store: in.refStore, instance: in, typ: global.Type, mutable: global.Mutable, valueType: exact, types: in.c.Types, hasValueType: true}
	}
	in.lifeMu.Unlock()
	return global, nil
}

func (in *Instance) domainSnapshotTable(index int) (*Table, error) {
	if in == nil || in.c == nil || index < 0 || index >= in.c.tableCount() {
		return nil, fmt.Errorf("domain snapshot table %d is unavailable", index)
	}
	desc := in.tableDescriptor(index)
	exact, err := in.c.tableExactType(index)
	if err != nil || len(desc) < 8 {
		return nil, fmt.Errorf("domain snapshot table %d metadata is unavailable", index)
	}
	in.lifeMu.Lock()
	defer in.lifeMu.Unlock()
	for table := in.table; table != nil; table = table.next {
		if len(table.desc) != 0 && &table.desc[0] == &desc[0] {
			return table, nil
		}
	}
	def := in.c.tableDef(index)
	table := &Table{desc: desc, owner: &tableOwner{store: in.refStore, instance: in, elementType: in.c.tableElementType(index), valueType: exact, types: in.c.Types, hasValueType: true, declaredHasMax: def.HasMax, addr64: def.Addr64}, next: in.table}
	in.table = table
	return table, nil
}

func restoreDomainGCGraph(instances []*Instance, s *DomainSnapshot) error {
	collector := instances[0].gc
	domain := instances[0].lockGCCollector()
	if domain == nil {
		return errors.New("restored collector domain is unavailable")
	}
	defer unlockGCCollector(domain)
	for member, in := range instances {
		public := in.publicGCState()
		for i, encoded := range s.members[member].globalRefs {
			if i >= len(in.c.Globals) || !isGCRefValType(in.c.Globals[i].Type) || in.globalCells[i] == nil {
				if encoded.kind != gcSnapshotRefNull {
					return fmt.Errorf("member %d global %d root is invalid", member, i)
				}
				continue
			}
			writeGlobalObject(in.globalCells[i], in.c.Globals[i].Type, 0)
			for _, root := range public.globalRoots {
				if root.GlobalIndex == uint32(i) {
					if err := collector.SetGlobalSlot(root.SlotIndex, gc.Null()); err != nil {
						return err
					}
				}
			}
		}
		for tableIndex, roots := range s.members[member].tableRoots {
			desc := in.tableDescriptor(tableIndex)
			if len(desc) < 8 || len(roots) > (len(desc)-8)/8 {
				return fmt.Errorf("member %d table %d roots exceed capacity", member, tableIndex)
			}
			binary.LittleEndian.PutUint32(desc, uint32(len(roots)))
			clear(desc[8:])
		}
	}
	refs := make(gc.RefSliceRoots, len(s.objects))
	for i, object := range s.objects {
		if int(object.typeMember) >= len(instances) {
			return fmt.Errorf("object %d type member is unavailable", i+1)
		}
		owner := instances[object.typeMember]
		domainType, ok := owner.gcDomainType(uint32(object.typeID))
		if !ok {
			return fmt.Errorf("object %d type is unavailable", i+1)
		}
		desc, ok := snapshotGCDescriptor(owner.c, object.typeID)
		if !ok {
			return fmt.Errorf("object %d descriptor is unavailable", i+1)
		}
		var ref gc.Ref
		var err error
		if desc.Kind == gc.KindStruct {
			ref, err = collector.NewStructUninitializedWithRoots(domainType, refs)
		} else {
			ref, err = collector.NewArrayUninitializedWithRoots(domainType, object.arrayLen, refs)
		}
		if err != nil {
			return err
		}
		refs[i] = ref
	}
	decode := func(encoded gcSnapshotRef) gc.Ref {
		if encoded.kind == gcSnapshotRefI31 {
			return gc.Ref(encoded.value)
		}
		if encoded.kind == gcSnapshotRefObject {
			return refs[encoded.value-1]
		}
		return gc.Null()
	}
	for i, object := range s.objects {
		owner := instances[object.typeMember]
		desc, _ := snapshotGCDescriptor(owner.c, object.typeID)
		for j, encoded := range object.values {
			value := gc.Value{Kind: encoded.kind, Bits: encoded.bits, BitsHi: encoded.bitsHi}
			if encoded.kind == gc.StorageRef || encoded.kind == gc.StorageRefNull {
				value.Ref = decode(encoded.ref)
			}
			var err error
			if desc.Kind == gc.KindStruct {
				err = collector.StructSet(refs[i], uint32(j), value)
			} else {
				err = collector.ArraySet(refs[i], uint32(j), value)
			}
			if err != nil {
				return err
			}
		}
	}
	for member, in := range instances {
		public := in.publicGCState()
		for i, encoded := range s.members[member].globalRefs {
			if i >= len(in.c.Globals) || !isGCRefValType(in.c.Globals[i].Type) {
				continue
			}
			ref := decode(encoded)
			writeGlobalObject(in.globalCells[i], in.c.Globals[i].Type, uint64(ref))
			found := false
			for _, root := range public.globalRoots {
				if root.GlobalIndex == uint32(i) {
					if err := collector.SetGlobalSlot(root.SlotIndex, ref); err != nil {
						return err
					}
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("member %d global %d has no checked root", member, i)
			}
		}
		for tableIndex, roots := range s.members[member].tableRoots {
			desc := in.tableDescriptor(tableIndex)
			for i, encoded := range roots {
				ref := decode(encoded)
				binary.LittleEndian.PutUint64(desc[8+i*8:], uint64(ref))
				collector.WriteBarrierRoot(ref)
			}
		}
		if len(s.members[member].elementRoots) != 0 {
			descBase := in.jm.PassiveElemPtr()
			if len(in.c.passiveElems) != 0 && descBase == 0 {
				return fmt.Errorf("member %d passive element descriptors are unavailable", member)
			}
			descBytes := runtime.PassiveElemDescBytes * len(in.c.passiveElems)
			descs := unsafe.Slice((*byte)(offHeapPtr(descBase)), descBytes)
			for segment, roots := range s.members[member].elementRoots {
				if len(roots) == 0 {
					continue
				}
				off := segment * runtime.PassiveElemDescBytes
				entryPtr := uintptr(binary.LittleEndian.Uint64(descs[off:]))
				if entryPtr == 0 {
					return fmt.Errorf("member %d passive element %d has no payload storage", member, segment)
				}
				entries := unsafe.Slice((*byte)(offHeapPtr(entryPtr)), len(roots)*8)
				for i, encoded := range roots {
					binary.LittleEndian.PutUint64(entries[i*8:], uint64(decode(encoded)))
				}
			}
		}
	}
	return nil
}

func validateDomainSnapshot(s *DomainSnapshot) error {
	if s == nil || len(s.members) == 0 {
		return errors.New("wago: empty domain snapshot")
	}
	validateRef := func(ref gcSnapshotRef) error {
		switch ref.kind {
		case gcSnapshotRefNull:
			if ref.value != 0 {
				return fmt.Errorf("null reference has payload %d", ref.value)
			}
		case gcSnapshotRefI31:
			if !gc.Ref(ref.value).IsI31() {
				return fmt.Errorf("invalid i31 payload %#x", ref.value)
			}
		case gcSnapshotRefObject:
			if ref.value == 0 || int(ref.value) > len(s.objects) {
				return fmt.Errorf("object reference %d exceeds object count %d", ref.value, len(s.objects))
			}
		default:
			return fmt.Errorf("unknown reference kind %d", ref.kind)
		}
		return nil
	}
	actualType := func(ref gcSnapshotRef) (ValueTypeDescriptor, []DefinedTypeDescriptor, error) {
		if ref.kind == gcSnapshotRefI31 {
			return ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Exact: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapI31}}}, nil, nil
		}
		if ref.kind != gcSnapshotRefObject || ref.value == 0 || int(ref.value) > len(s.objects) {
			return ValueTypeDescriptor{}, nil, errors.New("reference has no object type")
		}
		object := s.objects[ref.value-1]
		if int(object.typeMember) >= len(s.members) || s.members[object.typeMember].state == nil || s.members[object.typeMember].state.c == nil {
			return ValueTypeDescriptor{}, nil, errors.New("object type member is unavailable")
		}
		types := s.members[object.typeMember].state.c.Types
		if int(object.typeID) >= len(types) {
			return ValueTypeDescriptor{}, nil, errors.New("object type is unavailable")
		}
		return ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Exact: true, Heap: HeapTypeDescriptor{Defined: true, TypeIndex: uint32(object.typeID)}}}, types, nil
	}
	validateTyped := func(ref gcSnapshotRef, required ValueTypeDescriptor, requiredTypes []DefinedTypeDescriptor) error {
		if err := validateRef(ref); err != nil {
			return err
		}
		if ref.kind == gcSnapshotRefNull {
			if !required.Ref.Nullable {
				return errors.New("null does not match non-null reference type")
			}
			return nil
		}
		actual, types, err := actualType(ref)
		if err != nil || !valueTypeSubtype(actual, types, required, requiredTypes) {
			return errors.New("reference does not match structural type")
		}
		return nil
	}
	reachable := make([]bool, len(s.objects))
	queue := make([]uint32, 0, len(s.objects))
	mark := func(ref gcSnapshotRef) {
		if ref.kind == gcSnapshotRefObject && !reachable[ref.value-1] {
			reachable[ref.value-1] = true
			queue = append(queue, ref.value)
		}
	}
	if err := gc.ValidateConfig(s.gc); err != nil {
		return fmt.Errorf("wago: domain snapshot GC config: %w", err)
	}
	for member, entry := range s.members {
		if entry.state == nil || entry.state.c == nil {
			return fmt.Errorf("wago: domain snapshot member %d has no module", member)
		}
		c := entry.state.c
		if err := validateDomainSnapshotCompiledMember(c); err != nil {
			return fmt.Errorf("wago: domain snapshot member %d: %w", member, err)
		}
		if err := validateSnapshotMemories(c, snapshotMemories(entry.state)); err != nil {
			return fmt.Errorf("wago: domain snapshot member %d memories: %w", member, err)
		}
		if len(entry.state.globals) != len(c.Globals) || len(entry.globalRefs) != len(c.Globals) {
			return fmt.Errorf("wago: domain snapshot member %d global count mismatch", member)
		}
		if err := validatePassiveDataLens(c, snapshotPassiveDataLens(entry.state)); err != nil {
			return fmt.Errorf("wago: domain snapshot member %d passive data: %w", member, err)
		}
		elementLens := snapshotPassiveElemLens(entry.state)
		if err := validateDomainPassiveElements(c, elementLens); err != nil {
			return fmt.Errorf("wago: domain snapshot member %d passive elements: %w", member, err)
		}
		if len(entry.elementRoots) != 0 && len(entry.elementRoots) != len(c.passiveElems) {
			return fmt.Errorf("wago: domain snapshot member %d passive element root count mismatch", member)
		}
		if len(entry.elementRoots) == 0 {
			for segment, length := range elementLens {
				if length == 0 {
					continue
				}
				for _, value := range c.passiveElems[segment].Values {
					if value.HasGlobal {
						return fmt.Errorf("wago: domain snapshot member %d passive element %d requires persisted global-dependent roots", member, segment)
					}
				}
			}
		} else {
			for segment, elem := range c.passiveElems {
				roots := entry.elementRoots[segment]
				length := elementLens[segment]
				refType := normalizedElemRefType(elem.RefType)
				needsRoots := false
				for _, value := range elem.Values {
					needsRoots = needsRoots || value.HasGlobal
				}
				if length == 0 || !needsRoots || !isGCRefValType(refType) {
					if len(roots) != 0 {
						return fmt.Errorf("wago: domain snapshot member %d passive element %d has unexpected roots", member, segment)
					}
					continue
				}
				if len(roots) != int(length) || len(elem.Values) != int(length) {
					return fmt.Errorf("wago: domain snapshot member %d passive element %d root length mismatch", member, segment)
				}
				required, err := c.elemExactType(elem)
				if err != nil {
					return fmt.Errorf("wago: domain snapshot member %d passive element %d exact type: %w", member, segment, err)
				}
				for i, ref := range roots {
					if err := validateTyped(ref, required, c.Types); err != nil {
						return fmt.Errorf("wago: domain snapshot member %d passive element %d value %d: %w", member, segment, i, err)
					}
					value := elem.Values[i]
					switch {
					case value.HasGlobal:
						if int(value.GlobalIndex) >= len(entry.globalRefs) || ref != entry.globalRefs[value.GlobalIndex] {
							return fmt.Errorf("wago: domain snapshot member %d passive element %d value %d does not preserve global identity", member, segment, i)
						}
					case value.Null:
						if ref.kind != gcSnapshotRefNull {
							return fmt.Errorf("wago: domain snapshot member %d passive element %d value %d is not null", member, segment, i)
						}
					case refType == ValI31Ref:
						if ref.kind != gcSnapshotRefI31 || ref.value != value.FuncIndex {
							return fmt.Errorf("wago: domain snapshot member %d passive element %d value %d changed i31 payload", member, segment, i)
						}
					default:
						return fmt.Errorf("wago: domain snapshot member %d passive element %d value %d has unsupported independent ownership", member, segment, i)
					}
					mark(ref)
				}
			}
		}
		if len(entry.tableRoots) != c.tableCount() {
			return fmt.Errorf("wago: domain snapshot member %d table count mismatch", member)
		}
		for i, ref := range entry.globalRefs {
			if !isGCRefValType(c.Globals[i].Type) {
				if ref.kind != gcSnapshotRefNull {
					return fmt.Errorf("wago: domain snapshot member %d non-GC global %d has a root", member, i)
				}
				continue
			}
			required, err := c.globalExactType(i)
			if err != nil || validateTyped(ref, required, c.Types) != nil {
				return fmt.Errorf("wago: domain snapshot member %d global %d has an invalid root", member, i)
			}
			mark(ref)
		}
		for tableIndex, roots := range entry.tableRoots {
			def := c.tableDef(tableIndex)
			max := c.tableRuntimeCapacity(tableIndex)
			if _, imported := c.tableImportAt(tableIndex); imported {
				max = int(^uint(0) >> 1)
				if def.HasMax && def.Max <= uint64(max) {
					max = int(def.Max)
				}
			}
			if len(roots) < def.Size || len(roots) > max {
				return fmt.Errorf("wago: domain snapshot member %d table %d length is invalid", member, tableIndex)
			}
			required, err := c.tableExactType(tableIndex)
			if err != nil {
				return err
			}
			for slot, ref := range roots {
				if err := validateTyped(ref, required, c.Types); err != nil {
					return fmt.Errorf("wago: domain snapshot member %d table %d slot %d: %w", member, tableIndex, slot, err)
				}
				mark(ref)
			}
		}
		expectedImports := make(map[string]uint8, len(c.Imports)+len(c.GlobalImports)+c.tableImportCount()+c.memoryImportCount()+c.tagImportCount())
		for _, key := range c.Imports {
			expectedImports[key] = domainImportFunction
		}
		for _, imp := range c.GlobalImports {
			expectedImports[imp.Module+"."+imp.Name] = domainImportGlobal
		}
		for tableIndex := 0; tableIndex < c.tableCount(); tableIndex++ {
			if def, imported := c.tableImportAt(tableIndex); imported {
				expectedImports[def.Key] = domainImportTable
			}
		}
		for memoryIndex := 0; memoryIndex < c.memoryCount(); memoryIndex++ {
			if def, imported := c.memoryImportAt(memoryIndex); imported {
				expectedImports[def.ImportKey] = domainImportMemory
			}
		}
		for tagIndex := 0; tagIndex < c.tagImportCount(); tagIndex++ {
			expectedImports[c.memoryDir.ehTags[tagIndex].ImportKey] = domainImportTag
		}
		seenImports := make(map[string]uint8, len(entry.imports))
		for _, imp := range entry.imports {
			if imp.key == "" || int(imp.member) >= len(s.members) || imp.member == uint32(member) || seenImports[imp.key] != 0 {
				return fmt.Errorf("wago: domain snapshot member %d has an invalid import record", member)
			}
			if expectedImports[imp.key] != imp.kind {
				return fmt.Errorf("wago: domain snapshot member %d import %q has the wrong kind", member, imp.key)
			}
			seenImports[imp.key] = imp.kind
			targetEntry := s.members[imp.member]
			target := targetEntry.state.c
			switch imp.kind {
			case domainImportFunction:
				if int(imp.index) >= len(target.Funcs) {
					return fmt.Errorf("wago: domain snapshot member %d function target is unavailable", member)
				}
			case domainImportGlobal:
				if int(imp.index) >= len(target.Globals) {
					return fmt.Errorf("wago: domain snapshot member %d global target is unavailable", member)
				}
				consumerGlobal := -1
				for i, definition := range c.GlobalImports {
					if definition.Module+"."+definition.Name == imp.key {
						consumerGlobal = i
						break
					}
				}
				if consumerGlobal < 0 || entry.globalRefs[consumerGlobal] != targetEntry.globalRefs[imp.index] {
					return fmt.Errorf("wago: domain snapshot member %d global import %q does not preserve alias state", member, imp.key)
				}
			case domainImportTable:
				if int(imp.index) >= target.tableCount() {
					return fmt.Errorf("wago: domain snapshot member %d table target is unavailable", member)
				}
				consumerTable := -1
				for i := 0; i < c.tableCount(); i++ {
					if definition, imported := c.tableImportAt(i); imported && definition.Key == imp.key {
						consumerTable = i
						break
					}
				}
				if consumerTable < 0 || !equalGCSnapshotRefs(entry.tableRoots[consumerTable], targetEntry.tableRoots[imp.index]) {
					return fmt.Errorf("wago: domain snapshot member %d table import %q does not preserve alias state", member, imp.key)
				}
			case domainImportMemory:
				if int(imp.index) >= target.memoryCount() {
					return fmt.Errorf("wago: domain snapshot member %d memory target is unavailable", member)
				}
				for i := 0; i < c.memoryCount(); i++ {
					definition, imported := c.memoryImportAt(i)
					if !imported || definition.ImportKey != imp.key {
						continue
					}
					if !equalMemorySnapshots(snapshotMemories(entry.state)[i], snapshotMemories(targetEntry.state)[imp.index]) {
						return fmt.Errorf("wago: domain snapshot member %d memory import %q does not preserve alias state", member, imp.key)
					}
				}
			case domainImportTag:
				if target.memoryDir == nil || int(imp.index) >= len(target.memoryDir.ehTags) || target.memoryDir.ehTags[imp.index].ImportKey != "" {
					return fmt.Errorf("wago: domain snapshot member %d tag target is unavailable", member)
				}
				consumerTag := -1
				for i := 0; i < c.tagImportCount(); i++ {
					if c.memoryDir.ehTags[i].ImportKey == imp.key {
						consumerTag = i
						break
					}
				}
				if consumerTag < 0 || !tagTypeEquivalent(target.memoryDir.ehTags[imp.index].TypeIndex, target.Types, c.memoryDir.ehTags[consumerTag].TypeIndex, c.Types) {
					return fmt.Errorf("wago: domain snapshot member %d tag import %q does not preserve structural identity", member, imp.key)
				}
			default:
				return fmt.Errorf("wago: domain snapshot member %d has unknown import kind %d", member, imp.kind)
			}
		}
		if len(seenImports) != len(expectedImports) {
			return fmt.Errorf("wago: domain snapshot member %d import graph is incomplete", member)
		}
	}
	for i, object := range s.objects {
		if int(object.typeMember) >= len(s.members) {
			return fmt.Errorf("wago: domain snapshot object %d type member is unavailable", i+1)
		}
		c := s.members[object.typeMember].state.c
		desc, ok := snapshotGCDescriptor(c, object.typeID)
		if !ok || (desc.Kind != gc.KindStruct && desc.Kind != gc.KindArray) {
			return fmt.Errorf("wago: domain snapshot object %d type is unavailable", i+1)
		}
		want := len(desc.Fields)
		if desc.Kind == gc.KindArray {
			want = int(object.arrayLen)
		}
		if len(object.values) != want {
			return fmt.Errorf("wago: domain snapshot object %d value count mismatch", i+1)
		}
		structural := c.Types[object.typeID]
		for j, value := range object.values {
			kind := desc.Elem
			var storage StorageTypeDescriptor
			if desc.Kind == gc.KindStruct {
				kind, storage = desc.Fields[j].Kind, structural.Fields[j].Storage
			} else {
				storage = structural.Array.Storage
			}
			if value.kind != kind {
				return fmt.Errorf("wago: domain snapshot object %d value %d storage mismatch", i+1, j)
			}
			if kind == gc.StorageRef || kind == gc.StorageRefNull {
				if err := validateTyped(value.ref, storage.Value, c.Types); err != nil {
					return fmt.Errorf("wago: domain snapshot object %d value %d: %w", i+1, j, err)
				}
			} else if value.ref.kind != 0 || value.ref.value != 0 {
				return fmt.Errorf("wago: domain snapshot object %d value %d has an unexpected reference", i+1, j)
			}
			if kind == gc.StorageFuncRef || kind == gc.StorageFuncRefNull || kind == gc.StorageExternRef || kind == gc.StorageExternRefNull {
				if value.bits != 0 {
					return fmt.Errorf("wago: domain snapshot object %d value %d has a non-null opaque reference", i+1, j)
				}
				if kind == gc.StorageFuncRef || kind == gc.StorageExternRef {
					return fmt.Errorf("wago: domain snapshot object %d value %d is null in non-null opaque storage", i+1, j)
				}
			}
		}
	}
	for pos := 0; pos < len(queue); pos++ {
		for _, value := range s.objects[queue[pos]-1].values {
			if value.kind == gc.StorageRef || value.kind == gc.StorageRefNull {
				mark(value.ref)
			}
		}
	}
	for i, live := range reachable {
		if !live {
			return fmt.Errorf("wago: domain snapshot object %d is unreachable", i+1)
		}
	}
	return nil
}

func validateDomainSnapshotCompiledMember(c *Compiled) error {
	if c == nil || c.boundsMode == BoundsChecksSignalsBased || !c.usesGenericGCExecution() || c.genericGCFrameRoots() == nil {
		return errors.New("module is outside exact explicit-bounds generic GC execution")
	}
	for i := 0; i < c.memoryCount(); i++ {
		def := c.memoryDef(i)
		if def.Shared {
			return fmt.Errorf("memory %d is shared", i)
		}
	}
	for i, global := range c.Globals {
		if isReferenceValType(global.Type) && !isGCRefValType(global.Type) {
			return fmt.Errorf("global %d has opaque reference storage", i)
		}
	}
	for i := 0; i < c.tableCount(); i++ {
		if !isGCRefValType(c.tableElementType(i)) {
			return fmt.Errorf("table %d has opaque reference storage", i)
		}
	}
	return nil
}

func equalMemorySnapshots(a, b memorySnap) bool {
	return a.pages == b.pages && bytes.Equal(a.image, b.image)
}

func equalGCSnapshotRefs(a, b []gcSnapshotRef) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func closeDomainRestore(instances []*Instance, err error) ([]*Instance, error) {
	for i := len(instances) - 1; i >= 0; i-- {
		if instances[i] != nil {
			_ = instances[i].Close()
		}
	}
	return nil, err
}

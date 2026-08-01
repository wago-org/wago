package wago

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"

	"github.com/wago-org/wago/src/core/runtime/gc"
)

const (
	domainSnapshotMagic      = "WGDN"
	domainSnapshotVersionMin = 1
	domainSnapshotVersion    = 2
)

// IsDomainSnapshot reports whether b starts with the whole-domain snapshot wire
// header.
func IsDomainSnapshot(b []byte) bool {
	return len(b) >= len(domainSnapshotMagic)+1 && string(b[:len(domainSnapshotMagic)]) == domainSnapshotMagic
}

// MarshalBinary encodes the complete member set, internal import graph, shared
// stable-ID GC graph, and exact collector configuration.
func (s *DomainSnapshot) MarshalBinary() ([]byte, error) {
	return s.marshalBinaryVersion(domainSnapshotVersion)
}

func (s *DomainSnapshot) marshalBinaryVersion(version byte) ([]byte, error) {
	if version < domainSnapshotVersionMin || version > domainSnapshotVersion {
		return nil, fmt.Errorf("wago: domain snapshot version %d unsupported", version)
	}
	if err := validateDomainSnapshot(s); err != nil {
		return nil, err
	}
	out := append([]byte(domainSnapshotMagic), version)
	out = appendDomainGCConfig(out, s.gc)
	out = binary.AppendUvarint(out, uint64(len(s.members)))
	for member, entry := range s.members {
		compiled, err := entry.state.c.MarshalBinary()
		if err != nil {
			return nil, fmt.Errorf("wago: domain snapshot member %d module: %w", member, err)
		}
		out = appendSized(out, compiled)
		memories := snapshotMemories(entry.state)
		out = binary.AppendUvarint(out, uint64(len(memories)))
		for _, memory := range memories {
			out = binary.AppendUvarint(out, uint64(memory.pages))
			out = appendSized(out, memory.image)
		}
		out = binary.AppendUvarint(out, uint64(len(entry.state.globals)))
		for _, global := range entry.state.globals {
			out = append(out, byte(global.typ))
			if global.typ == ValV128 {
				out = append(out, global.vec[:]...)
			} else {
				out = binary.LittleEndian.AppendUint64(out, global.bits)
				out = append(out, make([]byte, 8)...)
			}
		}
		passive := snapshotPassiveDataLens(entry.state)
		out = binary.AppendUvarint(out, uint64(len(passive)))
		for _, length := range passive {
			out = binary.AppendUvarint(out, uint64(length))
		}
		if version >= 2 {
			elements := snapshotPassiveElemLens(entry.state)
			out = binary.AppendUvarint(out, uint64(len(elements)))
			for _, length := range elements {
				out = binary.AppendUvarint(out, uint64(length))
			}
		} else if len(entry.state.c.passiveElems) != 0 {
			return nil, fmt.Errorf("wago: domain snapshot v1 cannot encode member %d passive element state", member)
		}
		out = binary.AppendUvarint(out, uint64(len(entry.imports)))
		for _, imp := range entry.imports {
			out = appendSized(out, []byte(imp.key))
			out = append(out, imp.kind)
			out = binary.AppendUvarint(out, uint64(imp.member))
			out = binary.AppendUvarint(out, uint64(imp.index))
		}
		out = appendDomainRefs(out, entry.globalRefs)
		out = binary.AppendUvarint(out, uint64(len(entry.tableRoots)))
		for _, roots := range entry.tableRoots {
			out = appendDomainRefs(out, roots)
		}
	}
	out = binary.AppendUvarint(out, uint64(len(s.objects)))
	for _, object := range s.objects {
		out = binary.AppendUvarint(out, uint64(object.typeMember))
		out = binary.AppendUvarint(out, uint64(object.typeID))
		out = binary.AppendUvarint(out, uint64(object.arrayLen))
		out = binary.AppendUvarint(out, uint64(len(object.values)))
		for _, value := range object.values {
			out = append(out, byte(value.kind), value.ref.kind)
			out = binary.LittleEndian.AppendUint32(out, value.ref.value)
			out = binary.LittleEndian.AppendUint64(out, value.bits)
			out = binary.LittleEndian.AppendUint64(out, value.bitsHi)
		}
	}
	return out, nil
}

func appendSized(out, value []byte) []byte {
	out = binary.AppendUvarint(out, uint64(len(value)))
	return append(out, value...)
}

func appendDomainRefs(out []byte, refs []gcSnapshotRef) []byte {
	out = binary.AppendUvarint(out, uint64(len(refs)))
	for _, ref := range refs {
		out = append(out, ref.kind)
		out = binary.LittleEndian.AppendUint32(out, ref.value)
	}
	return out
}

// LoadDomainSnapshot decodes a self-contained whole-domain snapshot.
func LoadDomainSnapshot(data []byte) (*DomainSnapshot, error) {
	if !IsDomainSnapshot(data) {
		return nil, errors.New("wago: not a domain snapshot blob")
	}
	version := data[len(domainSnapshotMagic)]
	if version < domainSnapshotVersionMin || version > domainSnapshotVersion {
		return nil, fmt.Errorf("wago: domain snapshot version %d unsupported", version)
	}
	rd := &snapReader{buf: data[len(domainSnapshotMagic)+1:]}
	cfg := readDomainGCConfig(rd)
	members := make([]domainSnapshotMember, rd.count("domain member", 1))
	loaded := make([]*Compiled, 0, len(members))
	fail := func(err error) (*DomainSnapshot, error) {
		for _, compiled := range loaded {
			_ = compiled.Close()
		}
		return nil, err
	}
	for member := range members {
		compiledBytes := rd.sizedBytes(fmt.Sprintf("member %d compiled module", member))
		compiled, err := Load(compiledBytes)
		if err != nil {
			return fail(fmt.Errorf("wago: domain snapshot member %d module: %w", member, err))
		}
		loaded = append(loaded, compiled)
		memories := make([]memorySnap, rd.count(fmt.Sprintf("member %d memory", member), 2))
		for i := range memories {
			pages := rd.uvarint()
			if pages > uint64(^uint32(0)) {
				rd.err = fmt.Errorf("member %d memory %d pages overflow u32", member, i)
				break
			}
			memories[i] = memorySnap{pages: uint32(pages), image: rd.sizedBytes(fmt.Sprintf("member %d memory %d image", member, i))}
		}
		globals := make([]globalSnap, rd.count(fmt.Sprintf("member %d global", member), 17))
		for i := range globals {
			globals[i].typ = ValType(rd.byte())
			raw := rd.bytes(16)
			if globals[i].typ == ValV128 {
				copy(globals[i].vec[:], raw)
			} else if len(raw) == 16 {
				globals[i].bits = binary.LittleEndian.Uint64(raw)
			}
		}
		passive := make([]uint32, rd.count(fmt.Sprintf("member %d passive data", member), 1))
		for i := range passive {
			value := rd.uvarint()
			if value > uint64(^uint32(0)) {
				rd.err = fmt.Errorf("member %d passive data %d length overflows u32", member, i)
				break
			}
			passive[i] = uint32(value)
		}
		var elements []uint32
		if version >= 2 {
			elements = make([]uint32, rd.count(fmt.Sprintf("member %d passive element", member), 1))
			for i := range elements {
				value := rd.uvarint()
				if value > uint64(^uint32(0)) {
					rd.err = fmt.Errorf("member %d passive element %d length overflows u32", member, i)
					break
				}
				elements[i] = uint32(value)
			}
		} else {
			elements = compiledPassiveElemLens(compiled)
		}
		imports := make([]domainSnapshotImport, rd.count(fmt.Sprintf("member %d import", member), 4))
		for i := range imports {
			imports[i].key = string(rd.sizedBytes(fmt.Sprintf("member %d import %d key", member, i)))
			imports[i].kind = rd.byte()
			owner, index := rd.uvarint(), rd.uvarint()
			if owner > uint64(^uint32(0)) || index > uint64(^uint32(0)) {
				rd.err = fmt.Errorf("member %d import %d target overflows u32", member, i)
				break
			}
			imports[i].member, imports[i].index = uint32(owner), uint32(index)
		}
		globalRefs := readDomainRefs(rd, fmt.Sprintf("member %d global root", member))
		tableRoots := make([][]gcSnapshotRef, rd.count(fmt.Sprintf("member %d table", member), 1))
		for i := range tableRoots {
			tableRoots[i] = readDomainRefs(rd, fmt.Sprintf("member %d table %d root", member, i))
		}
		members[member] = domainSnapshotMember{
			state:   &Snapshot{c: compiled, gc: cfg, kind: SnapshotInit, memories: memories, globals: globals, passiveDataLens: passive, passiveElemLens: elements},
			imports: imports, globalRefs: globalRefs, tableRoots: tableRoots,
		}
		if len(memories) != 0 {
			members[member].state.memPages, members[member].state.memory = memories[0].pages, memories[0].image
		}
	}
	objects := make([]domainGCObjectSnapshot, rd.count("domain GC object", 4))
	for i := range objects {
		member, typeID, arrayLen := rd.uvarint(), rd.uvarint(), rd.uvarint()
		if member > uint64(^uint32(0)) || typeID > uint64(^uint32(0)) || arrayLen > uint64(^uint32(0)) {
			rd.err = fmt.Errorf("domain GC object %d metadata overflows u32", i)
			break
		}
		values := make([]gcSnapshotValue, rd.count(fmt.Sprintf("domain GC object %d value", i), 22))
		for j := range values {
			values[j] = gcSnapshotValue{kind: gc.StorageKind(rd.byte()), ref: gcSnapshotRef{kind: rd.byte(), value: rd.u32()}, bits: rd.u64(), bitsHi: rd.u64()}
		}
		objects[i] = domainGCObjectSnapshot{typeMember: uint32(member), typeID: gc.TypeID(typeID), arrayLen: uint32(arrayLen), values: values}
	}
	if rd.err == nil && len(rd.buf) != 0 {
		rd.err = fmt.Errorf("%d trailing domain snapshot bytes", len(rd.buf))
	}
	if rd.err != nil {
		return fail(fmt.Errorf("wago: invalid domain snapshot: %w", rd.err))
	}
	out := &DomainSnapshot{members: members, objects: objects, gc: cfg}
	if err := validateDomainSnapshot(out); err != nil {
		return fail(err)
	}
	return out, nil
}

func readDomainRefs(rd *snapReader, name string) []gcSnapshotRef {
	refs := make([]gcSnapshotRef, rd.count(name, 5))
	for i := range refs {
		refs[i] = gcSnapshotRef{kind: rd.byte(), value: rd.u32()}
	}
	return refs
}

func appendDomainGCConfig(out []byte, cfg GCConfig) []byte {
	out = append(out, byte(cfg.Profile), byte(cfg.Allocator), byte(cfg.Runtime))
	for _, value := range []uint32{cfg.NurseryBytes, cfg.OldBlockBytes, cfg.LargeObjectBytes, cfg.StressNurseryBytes, cfg.TinyHeapBytes, cfg.TinyBlockBytes, cfg.TinyStepBudget, cfg.ThroughputHeapBytes, cfg.ThroughputPageBytes, cfg.ThroughputClassLimit} {
		out = binary.LittleEndian.AppendUint32(out, value)
	}
	for _, value := range []bool{cfg.CollectEveryAlloc, cfg.ForceMajorEveryMinor, cfg.VerifyAfterCollect, cfg.PoisonFreed, cfg.StressBarriers, cfg.DisableMovingNursery, cfg.DisableCollection, cfg.TinyCollectEveryAlloc, cfg.TinyStepEveryAlloc} {
		if value {
			out = append(out, 1)
		} else {
			out = append(out, 0)
		}
	}
	return out
}

// WriteFile marshals the domain snapshot and writes it to path.
func (s *DomainSnapshot) WriteFile(path string) error {
	data, err := s.MarshalBinary()
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// ReadDomainSnapshotFile reads and validates a whole-domain snapshot blob.
func ReadDomainSnapshotFile(path string) (*DomainSnapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return LoadDomainSnapshot(data)
}

func readDomainGCConfig(rd *snapReader) GCConfig {
	cfg := GCConfig{Profile: GCProfile(rd.byte()), Allocator: gc.AllocatorKind(rd.byte()), Runtime: gc.RuntimeKind(rd.byte())}
	values := []*uint32{&cfg.NurseryBytes, &cfg.OldBlockBytes, &cfg.LargeObjectBytes, &cfg.StressNurseryBytes, &cfg.TinyHeapBytes, &cfg.TinyBlockBytes, &cfg.TinyStepBudget, &cfg.ThroughputHeapBytes, &cfg.ThroughputPageBytes, &cfg.ThroughputClassLimit}
	for _, value := range values {
		*value = rd.u32()
	}
	bools := []*bool{&cfg.CollectEveryAlloc, &cfg.ForceMajorEveryMinor, &cfg.VerifyAfterCollect, &cfg.PoisonFreed, &cfg.StressBarriers, &cfg.DisableMovingNursery, &cfg.DisableCollection, &cfg.TinyCollectEveryAlloc, &cfg.TinyStepEveryAlloc}
	for _, value := range bools {
		b := rd.byte()
		if b > 1 {
			rd.err = fmt.Errorf("invalid domain GC config boolean %d", b)
		}
		*value = b == 1
	}
	return cfg
}

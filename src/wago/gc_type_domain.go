package wago

import (
	"fmt"
	"reflect"

	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

// gcTypeMapping translates immutable module-local flattened type indexes to the
// canonical IDs used by one Runtime collector domain. Private collectors also
// canonicalize structurally equivalent local types so exact references have the
// same semantics with and without a shared store.
type gcTypeMapping struct {
	localToDomain []gc.TypeID
	domainToLocal []uint32
}

const unavailableLocalGCType = ^uint32(0)

func (m *gcTypeMapping) domain(local uint32) (gc.TypeID, bool) {
	if m == nil {
		return gc.TypeID(local), true
	}
	if int(local) >= len(m.localToDomain) {
		return 0, false
	}
	return m.localToDomain[local], true
}

func (m *gcTypeMapping) local(domain gc.TypeID) (uint32, bool) {
	if m == nil {
		return uint32(domain), true
	}
	if int(domain) >= len(m.domainToLocal) || m.domainToLocal[domain] == unavailableLocalGCType {
		return 0, false
	}
	return m.domainToLocal[domain], true
}

func (m *gcTypeMapping) canonicalTypes(local []gc.TypeID) ([]gc.TypeID, error) {
	if local == nil || m == nil {
		return local, nil
	}
	if len(local) != len(m.localToDomain) {
		return nil, fmt.Errorf("wago: local canonical type count %d, want %d", len(local), len(m.localToDomain))
	}
	domain := make([]gc.TypeID, len(m.domainToLocal))
	seen := make([]bool, len(domain))
	for i := range domain {
		domain[i] = gc.TypeID(i)
	}
	for localType, representative := range local {
		if int(representative) >= len(m.localToDomain) {
			return nil, fmt.Errorf("wago: local canonical type %d maps to unavailable representative %d", localType, representative)
		}
		domainType := m.localToDomain[localType]
		domainRepresentative := m.localToDomain[representative]
		if int(domainType) >= len(domain) || int(domainRepresentative) >= len(domain) {
			return nil, fmt.Errorf("wago: local canonical type %d has unavailable Runtime-domain identity", localType)
		}
		if seen[domainType] && domain[domainType] != domainRepresentative {
			return nil, fmt.Errorf("wago: Runtime-domain type %d has conflicting canonical representatives %d and %d", domainType, domain[domainType], domainRepresentative)
		}
		domain[domainType] = domainRepresentative
		seen[domainType] = true
	}
	return domain, nil
}

type gcDomainTypeRepresentative struct {
	types []DefinedTypeDescriptor
	index uint32
}

func gcTypeEquivalentToRepresentative(c *Compiled, local uint32, rep gcDomainTypeRepresentative) bool {
	return c != nil && int(local) < len(c.Types) && int(rep.index) < len(rep.types) && definedTypeEquivalent(local, c.Types, rep.index, rep.types)
}

func hasEquivalentLocalGCHeapTypes(c *Compiled) bool {
	if c == nil {
		return false
	}
	for i := 1; i < len(c.Types); i++ {
		kind := c.Types[i].Kind
		if kind != CompositeTypeStruct && kind != CompositeTypeArray {
			continue
		}
		for j := 0; j < i; j++ {
			if c.Types[j].Kind == kind && definedTypeEquivalent(uint32(i), c.Types, uint32(j), c.Types) {
				return true
			}
		}
	}
	return false
}

func gcCanonicalTypePlan(c *Compiled, reps []gcDomainTypeRepresentative, domainTypes []gc.TypeDesc, allowAppend bool) (*gcTypeMapping, []gc.TypeDesc, []gcDomainTypeRepresentative, error) {
	if c == nil || len(c.Types) != len(c.GCTypeDescs) {
		return nil, nil, nil, fmt.Errorf("wago: canonical GC type metadata is incomplete")
	}
	mapping := &gcTypeMapping{localToDomain: make([]gc.TypeID, len(c.Types))}
	newReps := append([]gcDomainTypeRepresentative(nil), reps...)
	newDescs := append([]gc.TypeDesc(nil), domainTypes...)
	for local := range c.Types {
		found := -1
		for domain, rep := range newReps {
			if gcTypeEquivalentToRepresentative(c, uint32(local), rep) {
				found = domain
				break
			}
		}
		if found < 0 {
			if !allowAppend {
				return nil, nil, nil, fmt.Errorf("wago: module type %d is absent from the selected Runtime GC domain", local)
			}
			found = len(newReps)
			newReps = append(newReps, gcDomainTypeRepresentative{types: c.Types, index: uint32(local)})
			newDescs = append(newDescs, gc.TypeDesc{})
		}
		mapping.localToDomain[local] = gc.TypeID(found)
	}
	if len(newReps) > int(^uint32(0)) {
		return nil, nil, nil, fmt.Errorf("wago: Runtime GC domain has too many canonical types")
	}
	for local, domainID := range mapping.localToDomain {
		if int(domainID) < len(domainTypes) {
			candidate := c.GCTypeDescs[local]
			candidate.ID = domainID
			if candidate.HasSuper {
				if int(candidate.Super) >= len(mapping.localToDomain) {
					return nil, nil, nil, fmt.Errorf("wago: module type %d has unavailable supertype %d", local, candidate.Super)
				}
				candidate.Super = mapping.localToDomain[candidate.Super]
			}
			if !reflect.DeepEqual(candidate, domainTypes[domainID]) {
				return nil, nil, nil, fmt.Errorf("wago: structurally equivalent module type %d has incompatible collector layout", local)
			}
			continue
		}
		desc := c.GCTypeDescs[local]
		desc.ID = domainID
		if desc.HasSuper {
			if int(desc.Super) >= len(mapping.localToDomain) {
				return nil, nil, nil, fmt.Errorf("wago: module type %d has unavailable supertype %d", local, desc.Super)
			}
			desc.Super = mapping.localToDomain[desc.Super]
		}
		newDescs[domainID] = desc
	}
	mapping.domainToLocal = make([]uint32, len(newReps))
	for i := range mapping.domainToLocal {
		mapping.domainToLocal[i] = unavailableLocalGCType
	}
	for local, domain := range mapping.localToDomain {
		if mapping.domainToLocal[domain] == unavailableLocalGCType {
			mapping.domainToLocal[domain] = uint32(local)
		}
	}
	return mapping, newDescs, newReps, nil
}

func gcModuleFitsDomain(c *Compiled, domain *gcStoreDomain) bool {
	if c == nil || domain == nil {
		return false
	}
	mapping, descs, reps, err := gcCanonicalTypePlan(c, domain.typeReps, domain.types, false)
	if err != nil || len(descs) != len(domain.types) || len(reps) != len(domain.typeReps) {
		return false
	}
	for _, local := range mapping.domainToLocal {
		if local == unavailableLocalGCType {
			return false
		}
	}
	return true
}

func preferredGCCollectorFromImports(c *Compiled, imports Imports, store *referenceStore) (*gc.Collector, error) {
	var collector *gc.Collector
	consider := func(candidate *Instance) error {
		if candidate == nil || candidate.gc == nil {
			return nil
		}
		if candidate.refStore != store {
			return fmt.Errorf("wago: imported GC owner requires the same Runtime GC domain")
		}
		if collector != nil && collector != candidate.gc {
			return fmt.Errorf("wago: imports require the same Runtime GC domain")
		}
		collector = candidate.gc
		return nil
	}
	// Function imports are index-aligned with their signatures. Walk them once
	// so large import sets remain linear instead of rescanning the complete key
	// list for every InstanceExport in the imports map.
	if c != nil {
		for i, key := range c.Imports {
			if i >= len(c.importFuncSigs) || !funcSigHasGCRefs(c.importFuncSigs[i]) {
				continue
			}
			v, ok := imports[key].(*InstanceExport)
			if ok && v != nil {
				if err := consider(v.inst); err != nil {
					return nil, err
				}
			}
		}
	}
	for _, value := range imports {
		switch v := value.(type) {
		case *InstanceExport:
			// Function owners were handled by the index-aligned pass above.
		case *Global:
			if v != nil && v.owner != nil {
				if err := consider(v.owner.instance); err != nil {
					return nil, err
				}
			}
		case GlobalImport:
			if v.Global != nil && v.Global.owner != nil {
				if err := consider(v.Global.owner.instance); err != nil {
					return nil, err
				}
			}
		case *Table:
			if v != nil && v.owner != nil {
				if err := consider(v.owner.instance); err != nil {
					return nil, err
				}
			}
		}
	}
	return collector, nil
}

func (in *Instance) gcDomainType(local uint32) (gc.TypeID, bool) {
	if in == nil {
		return 0, false
	}
	return in.gcTypeMap.domain(local)
}

func (in *Instance) gcLocalType(domain gc.TypeID) (uint32, bool) {
	if in == nil {
		return 0, false
	}
	return in.gcTypeMap.local(domain)
}

func (in *Instance) requireGCDomainType(local uint32) gc.TypeID {
	domain, ok := in.gcDomainType(local)
	if !ok {
		panic(gcStructHelperError{err: fmt.Errorf("gc module type %d has no Runtime-domain identity", local)})
	}
	return domain
}

func mappedGCType(mapping *gcTypeMapping, local uint32) (gc.TypeID, error) {
	domain, ok := mapping.domain(local)
	if !ok {
		return 0, fmt.Errorf("gc module type %d has no Runtime-domain identity", local)
	}
	return domain, nil
}

func (in *Instance) gcRefMatchesValueType(ref gc.Ref, required ValueTypeDescriptor) bool {
	if in == nil || required.Kind != ValueTypeReference {
		return false
	}
	if ref.IsNull() {
		return required.Ref.Nullable
	}
	if ref.IsI31() {
		if required.Ref.Heap.Defined {
			return false
		}
		switch required.Ref.Heap.Abstract {
		case AbstractHeapAny, AbstractHeapEq, AbstractHeapI31:
			return true
		default:
			return false
		}
	}
	if in.gc == nil {
		return false
	}
	var target gc.RefTestTarget
	target.Nullable = required.Ref.Nullable
	if required.Ref.Heap.Defined {
		domain, ok := in.gcDomainType(required.Ref.Heap.TypeIndex)
		if !ok {
			return false
		}
		target.Kind, target.Type = gc.RefTestDefined, domain
	} else {
		switch required.Ref.Heap.Abstract {
		case AbstractHeapAny:
			target.Kind = gc.RefTestAny
		case AbstractHeapEq:
			target.Kind = gc.RefTestEq
		case AbstractHeapI31:
			target.Kind = gc.RefTestI31
		case AbstractHeapStruct:
			target.Kind = gc.RefTestStruct
		case AbstractHeapArray:
			target.Kind = gc.RefTestArray
		case AbstractHeapNone:
			target.Kind = gc.RefTestNone
		default:
			return false
		}
	}
	matched, err := in.gc.RefTest(ref, target)
	return err == nil && matched
}

package wago

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

type structuralCallIdentityCache struct {
	identities []byte
	spans      []structuralCallIdentitySpan
}

type structuralCallIdentitySpan struct {
	start uint32
	end   uint32
}

type structuralTypeGroupBounds struct {
	start int
	count int
}

func compiledStructuralTypeGroups(types []DefinedTypeDescriptor) map[uint32]structuralTypeGroupBounds {
	groups := make(map[uint32]structuralTypeGroupBounds)
	for i := range types {
		group := types[i].RecGroup
		bounds, exists := groups[group]
		if !exists {
			bounds.start = i
		}
		bounds.count++
		groups[group] = bounds
	}
	return groups
}

var structuralCallIdentitySeenSentinel = &structuralCallIdentityCache{}

const (
	maxStructuralCallIdentityCacheBytes    = 64 << 10
	structuralCallIdentityCacheHeaderBytes = 48
	structuralCallIdentitySpanBytes        = 8
	uncachedStructuralCallIdentityEnd      = ^uint32(0)
)

func (c *Compiled) prepareStructuralCallIdentities() error {
	if c.validateMemo == nil || len(c.FuncTypeID) == 0 || len(c.Types) == 0 {
		return nil
	}
	c.ensureCodeCache()
	cc := c.codeCache
	cc.mu.Lock()
	defer cc.mu.Unlock()
	memo := c.validateMemo
	published := memo.structuralCallIdentities.Load()
	if published != nil && published != structuralCallIdentitySeenSentinel {
		return nil
	}
	if published == nil {
		memo.structuralCallIdentities.Store(structuralCallIdentitySeenSentinel)
		return nil
	}
	spanBytes := uint64(len(c.Types)) * structuralCallIdentitySpanBytes
	if spanBytes > maxStructuralCallIdentityCacheBytes-structuralCallIdentityCacheHeaderBytes {
		// A non-nil empty cache records that this module exceeded the bounded
		// retention budget. Registrations keep reconstructing identities exactly.
		memo.structuralCallIdentities.Store(&structuralCallIdentityCache{})
		return nil
	}
	identityBudget := maxStructuralCallIdentityCacheBytes - structuralCallIdentityCacheHeaderBytes - int(spanBytes)
	cache := &structuralCallIdentityCache{spans: make([]structuralCallIdentitySpan, len(c.Types))}
	groups := compiledStructuralTypeGroups(c.Types)
	for i := range c.FuncTypeID {
		sig, ok := compiledFunctionSignature(c, i)
		if !ok || !sig.HasTypeIndex || int(sig.TypeIndex) >= len(c.Types) {
			continue
		}
		if cache.spans[sig.TypeIndex].end != 0 {
			continue
		}
		canonical, err := compiledStructuralCallIdentityWithGroups(c, i, groups)
		if err != nil {
			return fmt.Errorf("function %d exact type: %w", i, err)
		}
		if len(canonical) > identityBudget-len(cache.identities) {
			cache.spans[sig.TypeIndex].end = uncachedStructuralCallIdentityEnd
			// Later entries cannot all be retained under this budget. Let
			// registration construct them once with its per-type memo instead of
			// spending another full graph walk merely to discard each identity.
			break
		}
		start := len(cache.identities)
		cache.identities = append(cache.identities, canonical...)
		if cap(cache.identities) > identityBudget {
			bounded := make([]byte, len(cache.identities))
			copy(bounded, cache.identities)
			cache.identities = bounded
		}
		end := len(cache.identities)
		cache.spans[sig.TypeIndex] = structuralCallIdentitySpan{start: uint32(start), end: uint32(end)}
	}
	memo.structuralCallIdentities.Store(cache)
	return nil
}

func (c *Compiled) cachedStructuralCallIdentity(functionIndex int) ([]byte, bool) {
	if c.validateMemo == nil {
		return nil, false
	}
	// A single acquire load observes one fully constructed immutable cache.
	cache := c.validateMemo.structuralCallIdentities.Load()
	if cache == nil || cache == structuralCallIdentitySeenSentinel {
		return nil, false
	}
	sig, ok := compiledFunctionSignature(c, functionIndex)
	if !ok || !sig.HasTypeIndex || int(sig.TypeIndex) >= len(cache.spans) {
		return nil, false
	}
	span := cache.spans[sig.TypeIndex]
	if span.end == 0 || span.end == uncachedStructuralCallIdentityEnd {
		return nil, false
	}
	if span.start > span.end || int(span.end) > len(cache.identities) {
		return nil, false
	}
	return cache.identities[span.start:span.end], true
}

// compiledStructuralCallIdentity reconstructs the exact canonical byte program
// underlying native structural-key generation from persisted compiled metadata.
// The store compares these bytes after a fast-key match; no hash width is treated
// as proof of type equality.
func compiledStructuralCallIdentity(c *Compiled, functionIndex int) ([]byte, error) {
	return compiledStructuralCallIdentityWithGroups(c, functionIndex, nil)
}

func compiledStructuralCallIdentityWithGroups(c *Compiled, functionIndex int, groups map[uint32]structuralTypeGroupBounds) ([]byte, error) {
	sig, ok := compiledFunctionSignature(c, functionIndex)
	if !ok {
		return nil, fmt.Errorf("function %d signature is unavailable", functionIndex)
	}
	params, results, err := exactFuncSignature(sig, c.Types)
	if err != nil {
		return nil, err
	}
	if !sig.HasTypeIndex {
		return encodeFlatCallIdentity(params, results)
	}
	if int(sig.TypeIndex) >= len(c.Types) || c.Types[sig.TypeIndex].Kind != CompositeTypeFunction {
		return nil, fmt.Errorf("function %d type index %d is unavailable", functionIndex, sig.TypeIndex)
	}
	rootGroup := c.Types[sig.TypeIndex].RecGroup
	if groups == nil {
		groups = compiledStructuralTypeGroups(c.Types)
	}
	rootBounds, foundGroup := groups[rootGroup]
	if !foundGroup {
		return nil, fmt.Errorf("function %d recursive type group is unavailable", functionIndex)
	}
	rootStart, rootCount := rootBounds.start, rootBounds.count
	indexed := false
	for _, values := range [][]ValueTypeDescriptor{params, results} {
		for _, value := range values {
			indexed = indexed || (value.Kind == ValueTypeReference && value.Ref.Heap.Defined)
		}
	}
	if !indexed && rootCount <= 1 {
		return encodeFlatCallIdentity(params, results)
	}

	const maxIdentityBytes = 1 << 20
	type canonicalGroup struct {
		encoding []byte
		digest   [32]byte
	}
	groupsByID := groups
	groupIDs := make(map[uint32]uint32)
	building := make(map[uint32]bool)
	uniqueGroups := make([]canonicalGroup, 0, 8)
	groupsByDigest := make(map[[32]byte][]uint32)
	uniqueGraphBytes := 0
	var buildGroup func(uint32) (uint32, error)
	var writeValue func(*[]byte, ValueTypeDescriptor, uint32) error
	var writeField func(*[]byte, FieldTypeDescriptor, uint32) error
	appendByte := func(dst *[]byte, b byte) error {
		if len(*dst) >= maxIdentityBytes {
			return fmt.Errorf("structural call identity exceeds %d bytes", maxIdentityBytes)
		}
		*dst = append(*dst, b)
		return nil
	}
	appendU32 := func(dst *[]byte, v uint32) error {
		if len(*dst) > maxIdentityBytes-4 {
			return fmt.Errorf("structural call identity exceeds %d bytes", maxIdentityBytes)
		}
		*dst = binary.LittleEndian.AppendUint32(*dst, v)
		return nil
	}
	writeRef := func(dst *[]byte, ownerGroup, index uint32) error {
		if int(index) >= len(c.Types) {
			return fmt.Errorf("structural type index %d out of range", index)
		}
		targetGroup := c.Types[index].RecGroup
		bounds, ok := groupsByID[targetGroup]
		if !ok || index < uint32(bounds.start) || uint64(index) >= uint64(bounds.start+bounds.count) {
			return fmt.Errorf("structural type index %d has no recursive-group member", index)
		}
		member := index - uint32(bounds.start)
		if targetGroup == ownerGroup {
			if err := appendByte(dst, 0xf2); err != nil {
				return err
			}
			return appendU32(dst, member)
		}
		id, err := buildGroup(targetGroup)
		if err != nil {
			return err
		}
		if err := appendByte(dst, 0xf4); err != nil {
			return err
		}
		if err := appendU32(dst, id); err != nil {
			return err
		}
		return appendU32(dst, member)
	}
	writeValue = func(dst *[]byte, value ValueTypeDescriptor, ownerGroup uint32) error {
		if err := appendByte(dst, byte(value.Kind)); err != nil {
			return err
		}
		if value.Kind != ValueTypeReference {
			return nil
		}
		for _, flag := range []bool{value.Ref.Nullable, value.Ref.Exact} {
			b := byte(0)
			if flag {
				b = 1
			}
			if err := appendByte(dst, b); err != nil {
				return err
			}
		}
		if value.Ref.Heap.Defined {
			if err := appendByte(dst, 1); err != nil {
				return err
			}
			return writeRef(dst, ownerGroup, value.Ref.Heap.TypeIndex)
		}
		if err := appendByte(dst, 0); err != nil {
			return err
		}
		return appendByte(dst, byte(value.Ref.Heap.Abstract))
	}
	writeField = func(dst *[]byte, field FieldTypeDescriptor, ownerGroup uint32) error {
		packed := byte(0)
		if field.Storage.Packed {
			packed = 1
		}
		if err := appendByte(dst, packed); err != nil {
			return err
		}
		if field.Storage.Packed {
			if err := appendByte(dst, byte(field.Storage.PackedType)); err != nil {
				return err
			}
		} else if err := writeValue(dst, field.Storage.Value, ownerGroup); err != nil {
			return err
		}
		mutable := byte(0)
		if field.Mutable {
			mutable = 1
		}
		return appendByte(dst, mutable)
	}
	buildGroup = func(group uint32) (uint32, error) {
		if id, ok := groupIDs[group]; ok {
			return id, nil
		}
		bounds, ok := groupsByID[group]
		if !ok || bounds.start < 0 || bounds.count <= 0 || bounds.start+bounds.count > len(c.Types) {
			return 0, fmt.Errorf("structural recursive type group %d is unavailable", group)
		}
		if building[group] {
			return 0, fmt.Errorf("structural recursive type groups contain a cross-group cycle")
		}
		building[group] = true
		defer delete(building, group)
		encoded := make([]byte, 0, 64)
		if err := appendU32(&encoded, uint32(bounds.count)); err != nil {
			return 0, err
		}
		for i := 0; i < bounds.count; i++ {
			index := uint32(bounds.start + i)
			d := &c.Types[index]
			if err := appendByte(&encoded, 0xf1); err != nil {
				return 0, err
			}
			final := byte(0)
			if d.Final {
				final = 1
			}
			if err := appendByte(&encoded, final); err != nil {
				return 0, err
			}
			if err := appendU32(&encoded, uint32(len(d.Supers))); err != nil {
				return 0, err
			}
			for _, super := range d.Supers {
				if err := writeRef(&encoded, group, super); err != nil {
					return 0, err
				}
			}
			for _, metadata := range []struct {
				has   bool
				index uint32
			}{{d.HasDescribes, d.Describes}, {d.HasDescriptor, d.Descriptor}} {
				if !metadata.has {
					if err := appendByte(&encoded, 0); err != nil {
						return 0, err
					}
				} else if err := appendByte(&encoded, 1); err != nil {
					return 0, err
				} else if err := writeRef(&encoded, group, metadata.index); err != nil {
					return 0, err
				}
			}
			if err := appendByte(&encoded, byte(d.Kind)); err != nil {
				return 0, err
			}
			switch d.Kind {
			case CompositeTypeFunction:
				if err := appendU32(&encoded, uint32(len(d.Params))); err != nil {
					return 0, err
				}
				for _, value := range d.Params {
					if err := writeValue(&encoded, value, group); err != nil {
						return 0, err
					}
				}
				if err := appendU32(&encoded, uint32(len(d.Results))); err != nil {
					return 0, err
				}
				for _, value := range d.Results {
					if err := writeValue(&encoded, value, group); err != nil {
						return 0, err
					}
				}
			case CompositeTypeStruct:
				if err := appendU32(&encoded, uint32(len(d.Fields))); err != nil {
					return 0, err
				}
				for _, field := range d.Fields {
					if err := writeField(&encoded, field, group); err != nil {
						return 0, err
					}
				}
			case CompositeTypeArray:
				if err := writeField(&encoded, d.Array, group); err != nil {
					return 0, err
				}
			default:
				return 0, fmt.Errorf("structural type %d has unknown kind %d", index, d.Kind)
			}
		}
		digest := sha256.Sum256(encoded)
		for _, candidate := range groupsByDigest[digest] {
			if bytes.Equal(uniqueGroups[candidate].encoding, encoded) {
				groupIDs[group] = candidate
				return candidate, nil
			}
		}
		if len(encoded)+4 > maxIdentityBytes-uniqueGraphBytes {
			return 0, fmt.Errorf("structural call identity exceeds %d bytes", maxIdentityBytes)
		}
		uniqueGraphBytes += len(encoded) + 4
		id := uint32(len(uniqueGroups))
		uniqueGroups = append(uniqueGroups, canonicalGroup{encoding: encoded, digest: digest})
		groupsByDigest[digest] = append(groupsByDigest[digest], id)
		groupIDs[group] = id
		return id, nil
	}
	rootID, err := buildGroup(rootGroup)
	if err != nil {
		return nil, err
	}
	if uniqueGraphBytes > maxIdentityBytes-13 {
		return nil, fmt.Errorf("structural call identity exceeds %d bytes", maxIdentityBytes)
	}
	out := make([]byte, 0, 64)
	appendOut := func(b byte) error {
		if len(out) >= maxIdentityBytes {
			return fmt.Errorf("structural call identity exceeds %d bytes", maxIdentityBytes)
		}
		out = append(out, b)
		return nil
	}
	appendOutU32 := func(v uint32) error {
		for shift := uint(0); shift < 32; shift += 8 {
			if err := appendOut(byte(v >> shift)); err != nil {
				return err
			}
		}
		return nil
	}
	if err := appendOut(0xf5); err != nil {
		return nil, err
	}
	if err := appendOutU32(uint32(len(uniqueGroups))); err != nil {
		return nil, err
	}
	if err := appendOutU32(rootID); err != nil {
		return nil, err
	}
	if err := appendOutU32(sig.TypeIndex - uint32(rootStart)); err != nil {
		return nil, err
	}
	for _, group := range uniqueGroups {
		if err := appendOutU32(uint32(len(group.encoding))); err != nil {
			return nil, err
		}
		for _, b := range group.encoding {
			if err := appendOut(b); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

func encodeFlatCallIdentity(params, results []ValueTypeDescriptor) ([]byte, error) {
	out := make([]byte, 0, 16+4*(len(params)+len(results)))
	appendU32 := func(v uint32) { out = append(out, byte(v), byte(v>>8), byte(v>>16), byte(v>>24)) }
	write := func(value ValueTypeDescriptor) error {
		out = append(out, byte(value.Kind))
		if value.Kind != ValueTypeReference {
			return nil
		}
		if value.Ref.Heap.Defined {
			return fmt.Errorf("flat structural call identity contains defined reference")
		}
		flags := byte(0)
		if value.Ref.Nullable {
			flags |= 1
		}
		if value.Ref.Exact {
			flags |= 2
		}
		out = append(out, flags, byte(value.Ref.Heap.Abstract))
		return nil
	}
	appendU32(uint32(len(params)))
	for _, value := range params {
		if err := write(value); err != nil {
			return nil, err
		}
	}
	out = append(out, 0xfe)
	appendU32(uint32(len(results)))
	for _, value := range results {
		if err := write(value); err != nil {
			return nil, err
		}
	}
	return out, nil
}

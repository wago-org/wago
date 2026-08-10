package wago

import "fmt"

type structuralCallIdentityCache struct {
	identities []byte
	spans      []structuralCallIdentitySpan
}

type structuralCallIdentitySpan struct {
	start uint32
	end   uint32
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
	if memo.structuralCallIdentities != nil && memo.structuralCallIdentities != structuralCallIdentitySeenSentinel {
		return nil
	}
	if memo.structuralCallIdentities == nil {
		memo.structuralCallIdentities = structuralCallIdentitySeenSentinel
		return nil
	}
	spanBytes := uint64(len(c.Types)) * structuralCallIdentitySpanBytes
	if spanBytes > maxStructuralCallIdentityCacheBytes-structuralCallIdentityCacheHeaderBytes {
		// A non-nil empty cache records that this module exceeded the bounded
		// retention budget. Registrations keep reconstructing identities exactly.
		memo.structuralCallIdentities = &structuralCallIdentityCache{}
		return nil
	}
	identityBudget := maxStructuralCallIdentityCacheBytes - structuralCallIdentityCacheHeaderBytes - int(spanBytes)
	cache := &structuralCallIdentityCache{spans: make([]structuralCallIdentitySpan, len(c.Types))}
	for i := range c.FuncTypeID {
		sig, ok := compiledFunctionSignature(c, i)
		if !ok || !sig.HasTypeIndex || int(sig.TypeIndex) >= len(c.Types) {
			continue
		}
		if cache.spans[sig.TypeIndex].end != 0 {
			continue
		}
		canonical, err := compiledStructuralCallIdentity(c, i)
		if err != nil {
			return fmt.Errorf("function %d exact type: %w", i, err)
		}
		if len(canonical) > identityBudget-len(cache.identities) {
			cache.spans[sig.TypeIndex].end = uncachedStructuralCallIdentityEnd
			continue
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
	memo.structuralCallIdentities = cache
	return nil
}

func (c *Compiled) cachedStructuralCallIdentity(functionIndex int) ([]byte, bool) {
	if c.validateMemo == nil || c.validateMemo.structuralCallIdentities == nil || c.validateMemo.structuralCallIdentities == structuralCallIdentitySeenSentinel {
		return nil, false
	}
	cache := c.validateMemo.structuralCallIdentities
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
	rootStart, rootCount := -1, 0
	indexed := false
	for i := range c.Types {
		if c.Types[i].RecGroup == rootGroup {
			if rootStart < 0 {
				rootStart = i
			}
			rootCount++
		}
	}
	for _, values := range [][]ValueTypeDescriptor{params, results} {
		for _, value := range values {
			indexed = indexed || (value.Kind == ValueTypeReference && value.Ref.Heap.Defined)
		}
	}
	if !indexed && rootCount <= 1 {
		return encodeFlatCallIdentity(params, results)
	}

	const maxIdentityBytes = 1 << 20
	out := make([]byte, 0, 256)
	appendByte := func(b byte) error {
		if len(out) >= maxIdentityBytes {
			return fmt.Errorf("structural call identity exceeds %d bytes", maxIdentityBytes)
		}
		out = append(out, b)
		return nil
	}
	appendU32 := func(v uint32) error {
		for shift := uint(0); shift < 32; shift += 8 {
			if err := appendByte(byte(v >> shift)); err != nil {
				return err
			}
		}
		return nil
	}
	path := make([]uint32, 0, 8)
	active := make(map[uint32]int)
	var writeValue func(ValueTypeDescriptor) error
	var writeField func(FieldTypeDescriptor) error
	var writeType func(uint32) error
	var writeDefinition func(uint32) error

	writeValue = func(value ValueTypeDescriptor) error {
		if err := appendByte(byte(value.Kind)); err != nil {
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
			if err := appendByte(b); err != nil {
				return err
			}
		}
		if value.Ref.Heap.Defined {
			if err := appendByte(1); err != nil {
				return err
			}
			return writeType(value.Ref.Heap.TypeIndex)
		}
		if err := appendByte(0); err != nil {
			return err
		}
		return appendByte(byte(value.Ref.Heap.Abstract))
	}
	writeField = func(field FieldTypeDescriptor) error {
		packed := byte(0)
		if field.Storage.Packed {
			packed = 1
		}
		if err := appendByte(packed); err != nil {
			return err
		}
		if field.Storage.Packed {
			if err := appendByte(byte(field.Storage.PackedType)); err != nil {
				return err
			}
		} else if err := writeValue(field.Storage.Value); err != nil {
			return err
		}
		mutable := byte(0)
		if field.Mutable {
			mutable = 1
		}
		return appendByte(mutable)
	}
	writeType = func(index uint32) error {
		if int(index) >= len(c.Types) {
			return fmt.Errorf("structural type index %d out of range", index)
		}
		if rootCount > 1 && int(index) >= rootStart && int(index) < rootStart+rootCount {
			if err := appendByte(0xf2); err != nil {
				return err
			}
			return appendU32(index - uint32(rootStart))
		}
		return writeDefinition(index)
	}
	writeDefinition = func(index uint32) error {
		if depth, ok := active[index]; ok {
			if err := appendByte(0xf0); err != nil {
				return err
			}
			return appendU32(uint32(len(path) - depth))
		}
		if int(index) >= len(c.Types) {
			return fmt.Errorf("structural type index %d out of range", index)
		}
		d := &c.Types[index]
		active[index] = len(path)
		path = append(path, index)
		defer func() {
			path = path[:len(path)-1]
			delete(active, index)
		}()
		if err := appendByte(0xf1); err != nil {
			return err
		}
		final := byte(0)
		if d.Final {
			final = 1
		}
		if err := appendByte(final); err != nil {
			return err
		}
		if err := appendU32(uint32(len(d.Supers))); err != nil {
			return err
		}
		for _, super := range d.Supers {
			if err := writeType(super); err != nil {
				return err
			}
		}
		for _, metadata := range []struct {
			has   bool
			index uint32
		}{{d.HasDescribes, d.Describes}, {d.HasDescriptor, d.Descriptor}} {
			if !metadata.has {
				if err := appendByte(0); err != nil {
					return err
				}
			} else {
				if err := appendByte(1); err != nil {
					return err
				}
				if err := writeType(metadata.index); err != nil {
					return err
				}
			}
		}
		if err := appendByte(byte(d.Kind)); err != nil {
			return err
		}
		switch d.Kind {
		case CompositeTypeFunction:
			if err := appendU32(uint32(len(d.Params))); err != nil {
				return err
			}
			for _, value := range d.Params {
				if err := writeValue(value); err != nil {
					return err
				}
			}
			if err := appendU32(uint32(len(d.Results))); err != nil {
				return err
			}
			for _, value := range d.Results {
				if err := writeValue(value); err != nil {
					return err
				}
			}
		case CompositeTypeStruct:
			if err := appendU32(uint32(len(d.Fields))); err != nil {
				return err
			}
			for _, field := range d.Fields {
				if err := writeField(field); err != nil {
					return err
				}
			}
		case CompositeTypeArray:
			if err := writeField(d.Array); err != nil {
				return err
			}
		default:
			return fmt.Errorf("structural type %d has unknown kind %d", index, d.Kind)
		}
		return nil
	}

	if rootCount == 1 {
		if err := writeType(sig.TypeIndex); err != nil {
			return nil, err
		}
		return out, nil
	}
	if err := appendByte(0xf3); err != nil {
		return nil, err
	}
	if err := appendU32(uint32(rootCount)); err != nil {
		return nil, err
	}
	if err := appendU32(sig.TypeIndex - uint32(rootStart)); err != nil {
		return nil, err
	}
	for i := 0; i < rootCount; i++ {
		if err := writeDefinition(uint32(rootStart + i)); err != nil {
			return nil, err
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

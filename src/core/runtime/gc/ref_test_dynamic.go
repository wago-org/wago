package gc

import "fmt"

// RefTestKind identifies the bounded runtime heap categories accepted by
// Collector.RefTest. Defined targets use RefTestTarget.Type; abstract targets
// ignore it.
type RefTestKind uint8

const (
	RefTestAny RefTestKind = iota + 1
	RefTestEq
	RefTestI31
	RefTestStruct
	RefTestArray
	RefTestNone
	RefTestDefined
)

// RefTestTarget describes one ordinary dynamic reference test. Nullable
// controls only the null result. Defined targets name a collector descriptor;
// the descriptor and all traversed supers were validated at collector creation.
type RefTestTarget struct {
	Type     TypeID
	Kind     RefTestKind
	Nullable bool
}

// TypeCanonicalization is a collector-bound, immutable map from declared type
// IDs to canonical representatives. It is built once at product instantiation
// and consumed without allocation by dynamic tests.
type TypeCanonicalization struct {
	collector *Collector
	types     []TypeID
}

// NewTypeCanonicalization validates and copies one representative per collector
// descriptor. Representatives must preserve the descriptor kind.
func (c *Collector) NewTypeCanonicalization(types []TypeID) (*TypeCanonicalization, error) {
	if err := c.errIfClosed(); err != nil {
		return nil, err
	}
	if len(types) != len(c.types) {
		return nil, fmt.Errorf("gc: canonical type count %d, want %d", len(types), len(c.types))
	}
	for i, representative := range types {
		if int(representative) >= len(c.types) {
			return nil, fmt.Errorf("gc: canonical type %d maps to unavailable representative %d", i, representative)
		}
		if c.types[i].Kind != c.types[representative].Kind {
			return nil, fmt.Errorf("gc: canonical type %d kind %d maps to representative %d kind %d", i, c.types[i].Kind, representative, c.types[representative].Kind)
		}
	}
	return &TypeCanonicalization{collector: c, types: append([]TypeID(nil), types...)}, nil
}

// RefTest implements the collector-owned portion of ordinary WebAssembly
// ref.test. It accepts only compact refs owned by this collector, never public
// tokens. Invalid, stale, forged, or closed-collector object refs return an
// error instead of being classified as a failed test.
func (c *Collector) RefTest(r Ref, target RefTestTarget) (bool, error) {
	return c.refTest(r, target, nil)
}

// RefTestCanonical applies the same dynamic test while comparing defined types
// through a collector-bound canonicalization map.
func (c *Collector) RefTestCanonical(r Ref, target RefTestTarget, canonical *TypeCanonicalization) (bool, error) {
	if canonical == nil || canonical.collector != c {
		return false, fmt.Errorf("gc: ref.test canonicalization does not belong to collector")
	}
	return c.refTest(r, target, canonical)
}

func (c *Collector) refTest(r Ref, target RefTestTarget, canonical *TypeCanonicalization) (bool, error) {
	if err := c.errIfClosed(); err != nil {
		return false, err
	}
	defined, err := c.refTestTargetDesc(target)
	if err != nil {
		return false, err
	}
	if r.IsNull() {
		return target.Nullable, nil
	}
	if r.IsI31() {
		switch target.Kind {
		case RefTestAny, RefTestEq, RefTestI31:
			return true, nil
		default:
			return false, nil
		}
	}

	dynamic, err := c.refDesc(r)
	if err != nil {
		return false, err
	}
	switch target.Kind {
	case RefTestAny, RefTestEq:
		return true, nil
	case RefTestI31, RefTestNone:
		return false, nil
	case RefTestStruct:
		return dynamic.Kind == KindStruct, nil
	case RefTestArray:
		return dynamic.Kind == KindArray, nil
	case RefTestDefined:
		if dynamic.Kind != defined.Kind {
			return false, nil
		}
		want := defined.ID
		if canonical != nil {
			want = canonical.types[want]
		}
		for {
			actual := dynamic.ID
			if canonical != nil {
				actual = canonical.types[actual]
			}
			if actual == want {
				return true, nil
			}
			if !dynamic.HasSuper {
				return false, nil
			}
			dynamic = c.types[c.typeIndex[dynamic.Super]]
		}
	default:
		panic("unreachable")
	}
}

func (c *Collector) refTestTargetDesc(target RefTestTarget) (TypeDesc, error) {
	switch target.Kind {
	case RefTestAny, RefTestEq, RefTestI31, RefTestStruct, RefTestArray, RefTestNone:
		return TypeDesc{}, nil
	case RefTestDefined:
		d, err := c.desc(target.Type)
		if err != nil {
			return TypeDesc{}, err
		}
		if d.Kind != KindStruct && d.Kind != KindArray {
			return TypeDesc{}, fmt.Errorf("gc: ref.test target type %d is not a heap object type", target.Type)
		}
		return d, nil
	default:
		return TypeDesc{}, fmt.Errorf("gc: unknown ref.test target kind %d", target.Kind)
	}
}

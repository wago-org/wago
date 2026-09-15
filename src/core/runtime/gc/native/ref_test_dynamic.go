package gc

import (
	"errors"
	"fmt"
)

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
// Exact requires equality with that canonical descriptor rather than admitting
// its declared subtypes.
type RefTestTarget struct {
	Type     TypeID
	Kind     RefTestKind
	Nullable bool
	Exact    bool
}

// ErrCastFailure reports a valid reference whose dynamic type does not match
// the requested cast target. Invalid, stale, forged, and closed references keep
// their more specific collector errors.
var ErrCastFailure = errors.New("gc: cast failure")

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

// RefTest implements the native portion of WebAssembly ref.test. The trusted
// adapter must establish collector ownership before passing compact words.
// This ABI can check liveness and bounds, but raw words do not carry identity
// or generation. Go callers must use the checked parent gc package.
func (c *Collector) RefTest(r Ref, target RefTestTarget) (bool, error) {
	return c.refTest(r, target, nil)
}

// TypeSubtype reports declared collector-domain subtype reachability without
// requiring a live object. Both IDs must name validated descriptors.
func (c *Collector) TypeSubtype(actual, required TypeID) (bool, error) {
	if err := c.errIfClosed(); err != nil {
		return false, err
	}
	dynamic, err := c.desc(actual)
	if err != nil {
		return false, err
	}
	want, err := c.desc(required)
	if err != nil {
		return false, err
	}
	if dynamic.Kind != want.Kind {
		return false, nil
	}
	return c.typeSubtypeIDs(actual, required)
}

// RefTestCanonical applies the same dynamic test while comparing defined types
// through a collector-bound canonicalization map.
func (c *Collector) RefTestCanonical(r Ref, target RefTestTarget, canonical *TypeCanonicalization) (bool, error) {
	if canonical == nil || canonical.collector != c {
		return false, fmt.Errorf("gc: ref.test canonicalization does not belong to collector")
	}
	return c.refTest(r, target, canonical)
}

// RefCast returns the original compact reference when its dynamic type matches
// target. It never rewrites object identity or converts public token bits.
func (c *Collector) RefCast(r Ref, target RefTestTarget) (Ref, error) {
	return c.refCast(r, target, nil)
}

// RefCastCanonical applies the same cast through a collector-bound canonical
// representative map and still returns the original compact reference.
func (c *Collector) RefCastCanonical(r Ref, target RefTestTarget, canonical *TypeCanonicalization) (Ref, error) {
	if canonical == nil || canonical.collector != c {
		return Null(), fmt.Errorf("gc: ref.cast canonicalization does not belong to collector")
	}
	return c.refCast(r, target, canonical)
}

func (c *Collector) refCast(r Ref, target RefTestTarget, canonical *TypeCanonicalization) (Ref, error) {
	matched, err := c.refTest(r, target, canonical)
	if err != nil {
		return Null(), err
	}
	if !matched {
		return Null(), ErrCastFailure
	}
	return r, nil
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
		if target.Exact {
			if canonical == nil {
				return dynamic.ID == defined.ID, nil
			}
			return canonical.types[dynamic.ID] == canonical.types[defined.ID], nil
		}
		if canonical == nil {
			return c.typeSubtypeIDs(dynamic.ID, defined.ID)
		}
		want := canonical.types[defined.ID]
		for {
			actual := canonical.types[dynamic.ID]
			if actual == want {
				return true, nil
			}
			if !dynamic.HasSuper {
				return false, nil
			}
			dynamic = c.types[dynamic.Super]
		}
	default:
		panic("unreachable")
	}
}

func (c *Collector) refTestTargetDesc(target RefTestTarget) (TypeDesc, error) {
	switch target.Kind {
	case RefTestAny, RefTestEq, RefTestI31, RefTestStruct, RefTestArray, RefTestNone:
		if target.Exact {
			return TypeDesc{}, fmt.Errorf("gc: exact ref.test target kind %d is not defined", target.Kind)
		}
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

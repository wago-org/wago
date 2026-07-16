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

// RefTest implements the collector-owned portion of ordinary WebAssembly
// ref.test. It accepts only compact refs owned by this collector, never public
// tokens. Invalid, stale, forged, or closed-collector object refs return an
// error instead of being classified as a failed test.
func (c *Collector) RefTest(r Ref, target RefTestTarget) (bool, error) {
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
		for {
			if dynamic.ID == defined.ID {
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

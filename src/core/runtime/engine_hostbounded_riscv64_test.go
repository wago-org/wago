//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package runtime

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/internal/runtimebridge"
)

func TestProductionRV64HostOptimizationsUnavailable(t *testing.T) {
	access := runtimebridge.GrantHostScalarCall()
	var engine Engine
	if p, err := engine.PrepareScalarHost(access, nil, nil, nil, nil, 0x10001); err != nil || p != nil || engine.PreparedScalarHost() != nil {
		t.Fatal("unavailable prepared bridge changed engine", p, err)
	}
	if p, err := engine.PrepareScalarHostView(access, nil, nil, nil, nil, 0); err != nil || p != nil {
		t.Fatal("unavailable view bridge admitted", p, err)
	}
	prepared := PreparedHostScalarCall{engine: &engine, fixed: true, fixedSlots: 0x10001}
	if err := prepared.EnableBoundedSegments(access); !errors.Is(err, errRV64BoundedHostUnavailable) || prepared.boundedSegments {
		t.Fatal("unavailable bounded proof changed prepared entry", err)
	}
	// These unqualified portals must return before inspecting native addresses,
	// publishing control state or invoking callbacks, even with internal access.
	for _, run := range []func() error{
		func() error {
			return engine.CallWithHostBaseScalarBounded(access, 1, nil, 1, nil, nil, nil, 0x10001, nil)
		},
		func() error { return engine.CallWithHostBaseFixedViewBounded(access, 1, nil, 1, nil, nil, nil, 0, nil) },
		func() error {
			return engine.CallWithHostBaseMultiViewBounded(access, 1, nil, 1, nil, nil, nil, []uint32{0, 0}, nil)
		},
		func() error {
			return engine.CallWithHostBaseScalarBoundedLive(access, 1, nil, 1, nil, nil, nil, 0, nil)
		},
		func() error {
			return engine.CallWithHostBaseFixedViewBoundedLive(access, 1, nil, 1, nil, nil, nil, 0, nil)
		},
		func() error {
			return engine.CallWithHostBaseScalarBoundedContextLive(access, 1, nil, 1, nil, nil, nil, 0, nil, nil, nil)
		},
		func() error {
			return engine.CallWithHostBaseFixedViewBoundedContextLive(access, 1, nil, 1, nil, nil, nil, 0, nil, nil, nil)
		},
	} {
		if err := run(); !errors.Is(err, errRV64BoundedHostUnavailable) {
			t.Fatal("unavailable host entry", err)
		}
	}
	if admitted, err := RegisterNativeScalarLeaf(access, nil, func(v int32) int32 { return v + 1 }, 1); err != nil || admitted || nativeScalarLeafPtr(nil) != 0 {
		t.Fatal("native leaf admitted", admitted, err)
	}
}

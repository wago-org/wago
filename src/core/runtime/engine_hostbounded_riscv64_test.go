//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package runtime

import (
	"encoding/binary"
	"errors"
	"github.com/wago-org/wago/internal/runtimebridge"
	goruntime "runtime"
	"strings"
	"testing"
	"unsafe"
)

func TestProductionRV64BoundedHostAdmission(t *testing.T) {
	access := runtimebridge.GrantHostScalarCall()
	var engine Engine
	prepared := PreparedHostScalarCall{engine: &engine, fixed: true, fixedSlots: 0x10001}
	if err := prepared.EnableBoundedSegments(access); err != nil || !prepared.boundedSegments {
		t.Fatal(err)
	}
	if err := prepared.EnableBoundedSegments(runtimebridge.HostScalarCallAccess{}); err == nil {
		t.Fatal("missing access accepted")
	}
	for _, run := range []func() error{
		func() error { return engine.CallWithHostBaseScalarBounded(access, 1, nil, 1, nil, nil, nil, 0, nil) },
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
		if err := run(); err == nil {
			t.Fatal("invalid entry accepted")
		}
	}
}

func TestProductionRV64BoundedHostScalarAndLive(t *testing.T) {
	for _, live := range []bool{false, true} {
		e, j, args, results, trap, ctrl := hostFixtureRV64(t, 64)
		code := hostRoundtripImportRV64(t, 2, 2, 1, false, 0)
		binary.LittleEndian.PutUint64(args, 0x12345678)
		binary.LittleEndian.PutUint64(args[8:], 0x87654321)
		fixed := func(a, b uint64) uint64 { return a | b<<32 }
		run := func() error {
			if live {
				return e.CallWithHostBaseScalarBoundedLive(runtimebridge.GrantHostScalarCall(), code, args, j.LinMemBase(), trap, results, ctrl, 0x20002, fixed)
			}
			return e.CallWithHostBaseScalarBounded(runtimebridge.GrantHostScalarCall(), code, args, j.LinMemBase(), trap, results, ctrl, 0x20002, fixed)
		}
		if err := run(); err != nil {
			t.Fatal(err)
		}
		if binary.LittleEndian.Uint64(results) != 0x12345678 || binary.LittleEndian.Uint64(results[8:]) != 0x87654321 {
			t.Fatal("scalar pair transport")
		}
		if alloc := testing.AllocsPerRun(32, func() {
			if err := run(); err != nil {
				panic(err)
			}
		}); alloc != 0 {
			t.Fatal("bounded scalar allocations", alloc)
		}
	}
}

func TestProductionRV64BoundedHostViewsAndContext(t *testing.T) {
	e, j, args, results, trap, ctrl := hostFixtureRV64(t, 64)
	code := hostRoundtripImportRV64(t, 3, 3, 1, false, 0)
	for i := range 3 {
		binary.LittleEndian.PutUint64(args[i*8:], uint64(i+10))
	}
	context := &struct {
		delta    uint64
		retained []byte
	}{delta: 31, retained: make([]byte, 1024)}
	view := func(context unsafe.Pointer, in, out []uint64) {
		goruntime.GC()
		delta := (*struct {
			delta    uint64
			retained []byte
		})(context).delta
		for i := range out {
			out[i] = in[i] + delta
		}
	}
	if err := e.CallWithHostBaseFixedViewBoundedContextLive(runtimebridge.GrantHostScalarCall(), code, args, j.LinMemBase(), trap, results, ctrl, 0x30003, unsafe.Pointer(context), view, nil); err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		if binary.LittleEndian.Uint64(results[i*8:]) != uint64(i+41) {
			t.Fatal("context view result", i)
		}
	}
	fixed := func(in, out []uint64) { copy(out, in) }
	run := func() error {
		return e.CallWithHostBaseFixedViewBoundedLive(runtimebridge.GrantHostScalarCall(), code, args, j.LinMemBase(), trap, results, ctrl, 0x30003, fixed)
	}
	if alloc := testing.AllocsPerRun(32, func() {
		if err := run(); err != nil {
			panic(err)
		}
	}); alloc != 0 {
		t.Fatal("bounded view allocations", alloc)
	}
	scalarCode := hostRoundtripImportRV64(t, 1, 1, 1, false, 0)
	if err := e.CallWithHostBaseScalarBoundedContextLive(runtimebridge.GrantHostScalarCall(), scalarCode, args, j.LinMemBase(), trap, results, ctrl, 0x10001, unsafe.Pointer(context), func(ctx unsafe.Pointer, a, b uint64) uint64 {
		return a + (*struct {
			delta    uint64
			retained []byte
		})(ctx).delta
	}, nil); err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint64(results) != 41 {
		t.Fatal("context scalar result")
	}
	goruntime.KeepAlive(context)
}

func TestProductionRV64BoundedHostMultiAndEscapes(t *testing.T) {
	e, j, args, results, trap, ctrl := hostFixtureRV64(t, 64)
	binary.LittleEndian.PutUint64(args, 12)
	multi := hostRoundtripImportRV64(t, 1, 1, 2, false, 0)
	calls := 0
	fixed := func(in, out []uint64) {
		calls++
		out[0] = in[0] + uint64(binary.LittleEndian.Uint32(ctrl[hcImportIdx:])) + 30
	}
	if err := e.CallWithHostBaseMultiViewBounded(runtimebridge.GrantHostScalarCall(), multi, args, j.LinMemBase(), trap, results, ctrl, []uint32{0x10001, 0x10001}, fixed); err != nil || calls != 2 || binary.LittleEndian.Uint64(results) != 43 {
		t.Fatal("multi host", calls, err)
	}
	escaped := hostRoundtripImportRV64(t, 1, 1, 1, false, 1)
	calls = 0
	err := e.CallWithHostBaseFixedViewBounded(runtimebridge.GrantHostScalarCall(), escaped, args, j.LinMemBase(), trap, results, ctrl, 0x10001, fixed)
	if err == nil || !strings.Contains(err.Error(), "admitted import") || calls != 0 {
		t.Fatal("escaped import callback", calls, err)
	}
	wrongShape := hostRoundtripImportRV64(t, 2, 1, 1, false, 0)
	err = e.CallWithHostBaseFixedViewBounded(runtimebridge.GrantHostScalarCall(), wrongShape, args, j.LinMemBase(), trap, results, ctrl, 0x10001, fixed)
	if err == nil || !strings.Contains(err.Error(), "signature") || calls != 0 {
		t.Fatal("escaped signature callback", calls, err)
	}
}

func TestProductionRV64BoundedHostCancelTrapAndReuse(t *testing.T) {
	e, j, args, results, trap, ctrl := hostFixtureRV64(t, 64)
	code := hostRoundtripImportRV64(t, 1, 1, 1, false, 0)
	fixed := func(a, b uint64) uint64 { binary.LittleEndian.PutUint32(trap, uint32(TrapInterrupted)); return 19 }
	err := e.CallWithHostBaseScalarBounded(runtimebridge.GrantHostScalarCall(), code, args, j.LinMemBase(), trap, results, ctrl, 0x10001, fixed)
	var trapped *TrapError
	if !errors.As(err, &trapped) || trapped.Code != TrapInterrupted {
		t.Fatal("cancel", err)
	}
	clear(trap)
	cold := hostRoundtripImportRV64(t, 1, 1, 1, true, 0)
	err = e.CallWithHostBaseScalarBounded(runtimebridge.GrantHostScalarCall(), cold, args, j.LinMemBase(), trap, results, ctrl, 0x10001, func(a, b uint64) uint64 { return 9 })
	if !errors.As(err, &trapped) || trapped.Code != TrapUnreachable {
		t.Fatal("cold trap", err)
	}
	clear(trap)
	if err = e.CallWithHostBaseScalarBounded(runtimebridge.GrantHostScalarCall(), code, args, j.LinMemBase(), trap, results, ctrl, 0x10001, func(a, b uint64) uint64 { return 25 }); err != nil || binary.LittleEndian.Uint64(results) != 25 {
		t.Fatal("reuse", err)
	}
}

func TestProductionRV64BoundedHostPanicReuseAndClose(t *testing.T) {
	e, j, args, results, trap, ctrl := hostFixtureRV64(t, 64)
	code := hostRoundtripImportRV64(t, 1, 1, 1, false, 0)
	access := runtimebridge.GrantHostScalarCall()
	sentinel := errors.New("host callback panic")
	func() {
		defer func() {
			if r := recover(); r != sentinel {
				t.Fatal("callback panic changed", r)
			}
		}()
		_ = e.CallWithHostBaseScalarBounded(access, code, args, j.LinMemBase(), trap, results, ctrl, 0x10001, func(a, b uint64) uint64 { panic(sentinel) })
	}()
	if err := e.CallWithHostBaseScalarBounded(access, code, args, j.LinMemBase(), trap, results, ctrl, 0x10001, func(a, b uint64) uint64 { return 42 }); err != nil || binary.LittleEndian.Uint64(results) != 42 {
		t.Fatal("reuse after panic", err)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	calls := 0
	if err := e.CallWithHostBaseScalarBounded(access, code, args, j.LinMemBase(), trap, results, ctrl, 0x10001, func(a, b uint64) uint64 { calls++; return 42 }); err == nil || calls != 0 {
		t.Fatal("closed engine entered", err, calls)
	}
}

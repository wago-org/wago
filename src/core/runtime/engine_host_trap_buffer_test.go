//go:build (linux || darwin || windows) && (amd64 || arm64) || (linux && riscv64 && !tinygo && !wago_target_tinygo)

package runtime

import (
	"github.com/wago-org/wago/internal/runtimebridge"
	"testing"
)

func TestPrepareHostScalarCallRejectsInvalidState(t *testing.T) {
	jm, err := NewJobMemory(0)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	trap := make([]byte, TrapBufferBytes)
	ctrl := make([]byte, HostCtrlFrameBytes)
	access := runtimebridge.GrantHostScalarCall()

	var nilEngine *Engine
	for name, prepare := range map[string]func() error{
		"access denied": func() error {
			_, err := engine.PrepareHostScalarCall(runtimebridge.HostScalarCallAccess{}, 1, nil, jm, trap, nil, ctrl)
			return err
		},
		"nil engine": func() error {
			_, err := nilEngine.PrepareHostScalarCall(access, 1, nil, jm, trap, nil, ctrl)
			return err
		},
		"zero code": func() error {
			_, err := engine.PrepareHostScalarCall(access, 0, nil, jm, trap, nil, ctrl)
			return err
		},
		"nil memory": func() error {
			_, err := engine.PrepareHostScalarCall(access, 1, nil, nil, trap, nil, ctrl)
			return err
		},
		"short trap": func() error {
			_, err := engine.PrepareHostScalarCall(access, 1, nil, jm, trap[:TrapBufferBytes-1], nil, ctrl)
			return err
		},
		"short control": func() error {
			_, err := engine.PrepareHostScalarCall(access, 1, nil, jm, trap, nil, ctrl[:HostCtrlFrameBytes-1])
			return err
		},
	} {
		if err := prepare(); err == nil {
			t.Fatalf("%s accepted invalid prepared host state", name)
		}
	}
	if err := (*PreparedHostScalarCall)(nil).Call(nil, nil); err == nil {
		t.Fatal("nil prepared host call succeeded")
	}
	prepared, err := engine.PrepareHostScalarCall(access, 1, nil, jm, trap, nil, ctrl)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Call(nil, nil); err == nil {
		t.Fatal("prepared host call accepted a nil dispatcher")
	}
	host := HostCall(func(uintptr, uint32, []uint64, []uint64) {})
	if err := prepared.Call(host, nil); err == nil {
		t.Fatal("prepared host call accepted a nil scalar portal")
	}
	if err := prepared.CallFixed(host, func(uintptr, uint32, uint32, uint64, uint64) (uint64, bool) { return 0, false }, func(uint64, uint64) uint64 { return 0 }); err == nil {
		t.Fatal("non-fixed prepared host call accepted fixed dispatch")
	}
	if _, err := engine.PrepareHostScalarFixedCall(access, 1, nil, jm, trap, nil, ctrl, 3|1<<16); err == nil {
		t.Fatal("fixed prepared host call accepted three parameter slots")
	}
	fixed, err := engine.PrepareHostScalarFixedCall(access, 1, nil, jm, trap, nil, ctrl, 1|1<<16)
	if err != nil {
		t.Fatal(err)
	}
	scalar := ScalarHostCall(func(uintptr, uint32, uint32, uint64, uint64) (uint64, bool) { return 0, false })
	fixedPortal := FixedScalarHostCall(func(uint64, uint64) uint64 { return 0 })
	if err := fixed.CallFixed(nil, scalar, fixedPortal); err == nil {
		t.Fatal("fixed prepared host call accepted a nil host dispatcher")
	}
	if err := fixed.CallFixed(host, nil, fixedPortal); err == nil {
		t.Fatal("fixed prepared host call accepted a nil scalar portal")
	}
	if err := fixed.CallFixed(host, scalar, nil); err == nil {
		t.Fatal("fixed prepared host call accepted a nil fixed portal")
	}
}

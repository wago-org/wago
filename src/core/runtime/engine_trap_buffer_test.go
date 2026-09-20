package runtime

import (
	"bytes"
	"strings"
	"testing"

	"github.com/wago-org/wago/internal/runtimebridge"
)

func TestEngineRejectsIncompleteTrapBuffersBeforeNativeEntry(t *testing.T) {
	engine := &Engine{}
	for length := 0; length < TrapBufferBytes; length++ {
		backing := bytes.Repeat([]byte{0xaa}, TrapBufferBytes)
		trap := backing[:length]
		for name, call := range map[string]func() error{
			"Call":         func() error { return engine.Call(0, nil, nil, trap, nil) },
			"CallPrepared": func() error { return engine.CallPrepared(0, nil, 0, trap, nil) },
		} {
			err := call()
			if err == nil || !strings.Contains(err.Error(), "trap buffer") {
				t.Fatalf("%s trap length %d error = %v", name, length, err)
			}
			if want := bytes.Repeat([]byte{0xaa}, TrapBufferBytes); !bytes.Equal(backing, want) {
				t.Fatalf("%s trap length %d changed backing bytes: %x", name, length, backing)
			}
		}
	}
}

func TestJobMemoryRejectsIncompleteTrapBuffers(t *testing.T) {
	jm, err := NewJobMemory(0)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()

	for length := 0; length < TrapBufferBytes; length++ {
		for name, bind := range map[string]func([]byte) error{
			"BindTrapCell":   jm.BindTrapCell,
			"RebindTrapCell": jm.RebindTrapCell,
		} {
			backing := bytes.Repeat([]byte{0xaa}, TrapBufferBytes)
			err := bind(backing[:length])
			if err == nil || !strings.Contains(err.Error(), "trap buffer") {
				t.Fatalf("%s trap length %d error = %v", name, length, err)
			}
			if want := bytes.Repeat([]byte{0xaa}, TrapBufferBytes); !bytes.Equal(backing, want) {
				t.Fatalf("%s trap length %d changed backing bytes: %x", name, length, backing)
			}
		}
	}
}

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

//go:build linux && riscv64 && wago_guardpage && !tinygo && !wago_target_tinygo

package runtime

import (
	"encoding/binary"
	"errors"
	"testing"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func TestRV64GuardSignalLayout(t *testing.T) {
	if guardRV64PCOffset != 176 || guardRV64HomeOffset != 376 || guardRV64LandingHomeOffset != 216 {
		t.Fatal("Linux RV64 signal layout changed", guardRV64PCOffset, guardRV64HomeOffset, guardRV64LandingHomeOffset)
	}
}

func TestRV64GuardedFaultCommitTrapAndReuse(t *testing.T) {
	if err := InstallGuardTrapHandler(); err != nil {
		t.Fatal(err)
	}
	e, err := NewEngineWithStackBytes(MinNativeStackBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	j, err := AcquireJobMemoryGuarded(wasmPageBytes, 3*wasmPageBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	j.SetStackFence(e.StackLimit())
	// Raw production ABI: read the serialized effective address, then read one
	// byte at that address. The guard handler authenticates pinned X25.
	var a enc.Asm
	a.Ld(enc.X5, enc.X10, 0)
	a.Add(enc.X5, enc.X25, enc.X5)
	a.Lbu(enc.X5, enc.X5, 0)
	a.Sd(enc.X5, enc.X13, 0)
	a.Ret()
	image, err := NewCodeBuffer(len(a.B))
	if err != nil {
		t.Fatal(err)
	}
	defer image.Close()
	if err = image.Append(a.B); err != nil {
		t.Fatal(err)
	}
	if err = image.Seal(); err != nil {
		t.Fatal(err)
	}
	args, trap, result := make([]byte, 8), make([]byte, TrapBufferBytes), make([]byte, 8)
	call := func(address uint64) error {
		binary.LittleEndian.PutUint64(args, address)
		return e.CallGuarded(image.Base(), args, j.LinMemBase(), trap, result, j)
	}
	j.LinearMemory()[0] = 77
	if err = call(0); err != nil || binary.LittleEndian.Uint64(result) != 77 {
		t.Fatal("initial read", err, result)
	}
	j.putU32(offLinMemWasmSize, 2)
	j.putU64(offActualLinMemByteSize64, 2*wasmPageBytes)
	j.putU32(offActualLinMemByteSize, 2*wasmPageBytes)
	if err = call(wasmPageBytes + 17); err != nil || binary.LittleEndian.Uint64(result) != 0 {
		t.Fatal("lazy page commit", err, result)
	}
	if err = call(2 * wasmPageBytes); !guardIsBoundsTrap(err) {
		t.Fatal("out-of-range read", err)
	}
	if err = call(0xffffffff); !guardIsBoundsTrap(err) {
		t.Fatal("large out-of-range read", err)
	}
	if err = call(0); err != nil || binary.LittleEndian.Uint64(result) != 77 {
		t.Fatal("reuse after trap", err, result)
	}
}

func guardIsBoundsTrap(err error) bool {
	var trap *TrapError
	return errors.As(err, &trap) && trap.Code == TrapLinMemOutOfBounds
}

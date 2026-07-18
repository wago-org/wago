//go:build linux && riscv64 && wago_guardpage

package riscv64

import (
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func TestSWARGuardedV128StorePreflightsCompleteWidth(t *testing.T) {
	body := []byte{0}
	body = append(body, swarI32Const(65528)...)
	body = append(body, swarV128Const(0x1111111111111111, 0x2222222222222222)...)
	body = append(body, swarMem(11, 0, 0)...)
	body = append(body, 0x0b)
	m := productionMemoryModule(t, nil, nil, body)
	cm, err := CompileModuleWith(m, CompileOptions{ElideBoundsChecks: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := coreruntime.InstallGuardTrapHandler(); err != nil {
		t.Fatal(err)
	}
	eng, err := coreruntime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	jm, err := coreruntime.NewJobMemoryGuarded(65536, 2*65536)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	before := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	copy(jm.CurrentBytes()[65528:], before)
	arena, err := coreruntime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer arena.Close()
	code, entry, err := coreruntime.MapCode(cm.Code)
	if err != nil {
		t.Fatal(err)
	}
	defer coreruntime.Unmap(code)
	args, results, trap := arena.Alloc(32), arena.Alloc(32), arena.Alloc(8)
	err = eng.CallGuarded(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results, jm)
	if err == nil {
		t.Fatal("expected out-of-bounds trap")
	}
	if got := jm.CurrentBytes()[65528:65536]; string(got) != string(before) {
		t.Fatalf("partial guarded store mutated tail: got %x, want %x", got, before)
	}
}

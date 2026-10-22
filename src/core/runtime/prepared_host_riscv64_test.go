//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package runtime

import (
	"encoding/binary"
	"github.com/wago-org/wago/internal/runtimebridge"
	"testing"
	"unsafe"
)

func TestPreparedHostRV64ContextAndIntegerOwner(t *testing.T) {
	if !preparedScalarHostEnabled {
		t.Skip("prepared host disabled by environment")
	}
	e, j, args, results, trap, ctrl := hostFixtureRV64(t, 64)
	access := runtimebridge.GrantHostScalarCall()
	p, err := e.PrepareScalarHost(access, args, trap, results, ctrl, 0x10001)
	if err != nil || p == nil {
		t.Fatal(p, err)
	}
	code := hostRoundtripImportRV64(t, 1, 1, 1, false, 0)
	binary.LittleEndian.PutUint64(args, 17)
	delta := int64(9)
	callback := func(ctx unsafe.Pointer, a, b uint64) uint64 { return a + uint64(*(*int64)(ctx)) }
	if err := p.Call(code, j.LinMemBase(), unsafe.Pointer(&delta), callback); err != nil || binary.LittleEndian.Uint64(results) != 26 {
		t.Fatal("prepared context", err)
	}
	if err := p.DetachNumericContext(access, j); err != nil {
		t.Fatal(err)
	}
	if err := p.EnableIntegerGuestContext(access); err != nil {
		t.Fatal(err)
	}
	if err := p.CallInteger(code, j.LinMemBase(), unsafe.Pointer(&delta), callback); err != nil || binary.LittleEndian.Uint64(results) != 26 {
		t.Fatal("integer context", err)
	}
	fixed := func(v int32) int32 { return v + 5 }
	owner := p.IntegerI32Owner()
	if owner == nil {
		t.Fatal("integer owner missing")
	}
	owner.Begin()
	outcome := owner.Enter(code, fixed)
	owner.Restore()
	if outcome != 0 || owner.TrapCode() != 0 || binary.LittleEndian.Uint64(results) != 22 {
		t.Fatal("integer owner result", outcome)
	}
	if alloc := testing.AllocsPerRun(32, func() {
		if err := p.CallIntegerI32(code, j.LinMemBase(), fixed); err != nil {
			panic(err)
		}
	}); alloc != 0 {
		t.Fatal("prepared warm allocations", alloc)
	}
	replacement, err := e.PrepareScalarHost(access, args, trap, results, ctrl, 0x10001)
	if err != nil || replacement == nil {
		t.Fatal(err)
	}
	if p.CallIntegerI32(code, j.LinMemBase(), fixed) == nil {
		t.Fatal("replaced prepared owner entered")
	}
}
func TestPreparedHostRV64ViewAndIntegerViewOwner(t *testing.T) {
	if !preparedScalarHostEnabled {
		t.Skip("prepared host disabled by environment")
	}
	e, j, args, results, trap, ctrl := hostFixtureRV64(t, 64)
	access := runtimebridge.GrantHostScalarCall()
	p, err := e.PrepareScalarHostView(access, args, trap, results, ctrl, 0x30003)
	if err != nil || p == nil {
		t.Fatal(err)
	}
	code := hostRoundtripImportRV64(t, 3, 3, 1, false, 0)
	delta := uint64(42)
	for i := range 3 {
		binary.LittleEndian.PutUint64(args[i*8:], uint64(i))
	}
	fixed := func(ctx unsafe.Pointer, in, out []uint64) {
		for i := range out {
			out[i] = in[i] + *(*uint64)(ctx)
		}
	}
	if err := p.CallView(code, j.LinMemBase(), unsafe.Pointer(&delta), fixed); err != nil {
		t.Fatal(err)
	}
	if err := p.DetachNumericContext(access, j); err != nil {
		t.Fatal(err)
	}
	if err := p.EnableIntegerGuestContext(access); err != nil {
		t.Fatal(err)
	}
	owner := p.IntegerViewOwner()
	if owner == nil {
		t.Fatal("integer view owner missing")
	}
	owner.Begin()
	outcome := owner.EnterView(code, unsafe.Pointer(&delta), fixed)
	owner.Restore()
	if outcome != 0 || owner.TrapCode() != 0 {
		t.Fatal("owned integer view", outcome)
	}
	for i := range 3 {
		if binary.LittleEndian.Uint64(results[i*8:]) != uint64(i)+42 {
			t.Fatal("view output", i)
		}
	}
}

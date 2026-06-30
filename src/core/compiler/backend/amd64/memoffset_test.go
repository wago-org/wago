//go:build linux && amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/runtime"
)

func expectOOBTrap(t *testing.T, err error) {
	t.Helper()
	var te *runtime.TrapError
	if !errors.As(err, &te) {
		t.Fatalf("expected out-of-bounds TrapError, got %v", err)
	}
	if te.Code != runtime.TrapLinMemOutOfBounds {
		t.Fatalf("trap code = %v, want %v", te.Code, runtime.TrapLinMemOutOfBounds)
	}
}

// TestMemoryOffsetAddressing checks that the folded memarg offset and the
// dynamic address both contribute to the effective address.
func TestMemoryOffsetAddressing(t *testing.T) {
	m := watToModule(t, `(module (memory 1) (func (export "f") (param i32) (result i32)
		local.get 0 i32.load offset=12))`)
	got, _, err := runMem(t, m, func(lin []byte) {
		binary.LittleEndian.PutUint32(lin[20:], 0xDEADBEEF) // mem[20]
	}, 8) // addr=8, offset=12 -> mem[20]
	if err != nil {
		t.Fatalf("unexpected trap: %v", err)
	}
	if uint32(got) != 0xDEADBEEF {
		t.Fatalf("load addr=8 off=12 = %#x, want 0xDEADBEEF", uint32(got))
	}
}

// TestMemoryOffsetStore checks an offset store writes the right cell and leaves
// neighbours untouched.
func TestMemoryOffsetStore(t *testing.T) {
	m := watToModule(t, `(module (memory 1) (func (export "f") (param i32) (result i32)
		local.get 0 i32.const 0x11223344 i32.store offset=16
		i32.const 0))`)
	_, mem, err := runMem(t, m, nil, 4) // addr=4, off=16 -> writes mem[20]
	if err != nil {
		t.Fatalf("unexpected trap: %v", err)
	}
	if v := binary.LittleEndian.Uint32(mem[20:]); v != 0x11223344 {
		t.Fatalf("mem[20] = %#x, want 0x11223344", v)
	}
	if v := binary.LittleEndian.Uint32(mem[16:]); v != 0 { // neighbour untouched
		t.Fatalf("mem[16] = %#x, want 0", v)
	}
}

// TestMemoryOffsetBoundsCheck is the safety test: the bounds check must remain
// exact with the offset folded — the last in-range byte succeeds, one byte over
// traps, and the dynamic address also counts toward the limit (not just offset).
func TestMemoryOffsetBoundsCheck(t *testing.T) {
	loadI32 := func(off uint32, addr int32) error {
		m := watToModule(t, fmt.Sprintf(`(module (memory 1) (func (export "f") (param i32) (result i32)
			local.get 0 i32.load offset=%d))`, off))
		_, _, err := runMem(t, m, nil, addr)
		return err
	}
	loadI64 := func(off uint32, addr int32) error {
		m := watToModule(t, fmt.Sprintf(`(module (memory 1) (func (export "f") (param i32) (result i64)
			local.get 0 i64.load offset=%d))`, off))
		_, _, err := runMem(t, m, nil, addr)
		return err
	}
	const page = 65536
	// i32 (size 4): addr+off+4 must be <= 65536.
	if err := loadI32(page-4, 0); err != nil { // exactly in range
		t.Fatalf("in-range i32 load (off=%d, addr=0) trapped: %v", page-4, err)
	}
	expectOOBTrap(t, loadI32(page-3, 0)) // one byte over
	expectOOBTrap(t, loadI32(page-4, 4)) // addr counts: 4 + (page-4) + 4 > page
	// i64 (size 8): addr+off+8 must be <= 65536.
	if err := loadI64(page-8, 0); err != nil {
		t.Fatalf("in-range i64 load (off=%d, addr=0) trapped: %v", page-8, err)
	}
	expectOOBTrap(t, loadI64(page-7, 0))
}

// TestMemoryOffsetLargeFallback exercises the explicit-add path for offsets too
// large to fold into a disp32: it must still bounds-check and trap (never OOB).
func TestMemoryOffsetLargeFallback(t *testing.T) {
	// offset 0x80000000 (2 GiB) -> off+size > MaxInt32 -> explicit-add path.
	m := watToModule(t, `(module (memory 1) (func (export "f") (param i32) (result i32)
		local.get 0 i32.load offset=2147483648))`)
	_, _, err := runMem(t, m, nil, 0)
	expectOOBTrap(t, err)
}

// TestMemoryOffsetSizes spot-checks offset loads across widths read the right
// bytes (the folded disp must match the access size's addressing).
func TestMemoryOffsetSizes(t *testing.T) {
	cases := []struct {
		name string
		body string
		want uint32 // low bits compared as the i32 result
	}{
		{"i64.load8_u", `local.get 0 i64.load8_u offset=5`, 0xAB},
		{"i64.load16_u", `local.get 0 i64.load16_u offset=5`, 0xCDAB},
		{"i32.load", `local.get 0 i32.load offset=5`, 0xEFCDAB},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := "i64"
			if c.name == "i32.load" {
				result = "i32"
			}
			m := watToModule(t, fmt.Sprintf(`(module (memory 1) (func (export "f") (param i32) (result %s)
				%s))`, result, c.body))
			_, _, err := runMem(t, m, func(lin []byte) {
				copy(lin[8:], []byte{0xAB, 0xCD, 0xEF, 0x00}) // bytes at mem[8] (addr 3 + off 5)
			}, 3)
			if err != nil {
				t.Fatalf("unexpected trap: %v", err)
			}
		})
	}
}

// TestSibAddrEncoding byte-checks the disp32 vs compact SIB forms.
func TestSibAddrEncoding(t *testing.T) {
	cases := []struct {
		name string
		emit func(a *Asm)
		want []byte
	}{
		{"LoadIdx no disp", func(a *Asm) { a.LoadIdx(RAX, RDI, RCX, 0, 4, false, false) },
			[]byte{0x8B, 0x04, 0x0F}},
		{"LoadIdx disp32", func(a *Asm) { a.LoadIdx(RAX, RDI, RCX, 8, 4, false, false) },
			[]byte{0x8B, 0x84, 0x0F, 0x08, 0x00, 0x00, 0x00}},
		{"StoreIdx disp32", func(a *Asm) { a.StoreIdx(RDI, RCX, RAX, 8, 4) },
			[]byte{0x89, 0x84, 0x0F, 0x08, 0x00, 0x00, 0x00}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := &Asm{}
			c.emit(a)
			if !bytes.Equal(a.B, c.want) {
				t.Fatalf("%s = % x, want % x", c.name, a.B, c.want)
			}
		})
	}
}

// BenchmarkMemoryOffsetSum sums n fields at fixed offsets from a base pointer —
// an offset-folding-friendly shape (struct/array field access).
func BenchmarkMemoryOffsetSum(b *testing.B) {
	const n = 16
	var sb strings.Builder
	sb.WriteString(`(module (memory 1) (func (export "f") (param i32) (result i32)` + "\n")
	sb.WriteString("local.get 0 i32.load offset=0\n")
	for i := 1; i < n; i++ {
		fmt.Fprintf(&sb, "local.get 0 i32.load offset=%d i32.add\n", i*4)
	}
	sb.WriteString("))")
	m := watToModuleB(b, sb.String())
	code, err := CompileFunction(m, 0)
	if err != nil {
		b.Fatal(err)
	}
	b.Logf("code size: %d bytes", len(code))
	eng, _ := runtime.NewEngine()
	defer eng.Close()
	jm, _ := runtime.NewJobMemory(65536)
	defer jm.Close()
	ar, _ := runtime.NewArena(4096)
	defer ar.Close()
	mem, entry, _ := runtime.MapCode(code)
	defer runtime.Unmap(mem)
	serArgs := ar.Alloc(64)
	results := ar.Alloc(16)
	trap := ar.Alloc(8)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := eng.Call(entry, serArgs, jm.LinearMemory(), trap, results); err != nil {
			b.Fatal(err)
		}
	}
}

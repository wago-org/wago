package riscv32

import (
	"encoding/binary"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	rv "github.com/wago-org/wago/src/core/encoder/riscv32"
)

func simdConst(v [16]byte) []byte { return append([]byte{0xfd, 0x0c}, v[:]...) }

func TestCompileV128BeachheadDirectSWAR(t *testing.T) {
	var a, b [16]byte
	for i := range a {
		a[i] = 0xff
		b[i] = 2
	}
	body := []byte{0}
	body = append(body, simdConst(a)...)
	body = append(body, simdConst(b)...)
	body = append(body, 0xfd, 110, 0x0b)
	code, err := CompileV128Beachhead(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(code) == 0 || len(code)%4 != 0 {
		t.Fatalf("code len=%d", len(code))
	}
	if _, err := CompileV128Beachhead([]byte{0, 0xfd, 14, 0x0b}); err == nil {
		t.Fatal("unsupported swizzle accepted")
	}
}

func TestCompileI64Beachhead(t *testing.T) {
	code, err := CompileI64Beachhead([]byte{0, 0x42, 6, 0x42, 7, 0x7e, 0x0b})
	if err != nil {
		t.Fatal(err)
	}
	if len(code) == 0 || len(code)%4 != 0 {
		t.Fatalf("code len=%d", len(code))
	}
	if _, err := CompileI64Beachhead([]byte{0, 0x42, 1, 0x86, 0x0b}); err == nil {
		t.Fatal("unsupported shift accepted")
	}
	localBody := []byte{1, 1, 0x7e, 0x20, 0, 0x42, 5, 0x7c, 0x22, 1, 0x42, 2, 0x7e, 0x0b}
	if _, err := CompileI64Function(1, localBody); err != nil {
		t.Fatal(err)
	}
	if _, err := CompileI64Function(1, []byte{0, 0x20, 2, 0x0b}); err == nil {
		t.Fatal("invalid local accepted")
	}
}

func TestCompileF64BitBeachhead(t *testing.T) {
	body := []byte{0, 0x44}
	var bits [8]byte
	binary.LittleEndian.PutUint64(bits[:], math.Float64bits(1.5))
	body = append(body, bits[:]...)
	body = append(body, 0x9a, 0x0b)
	code, err := CompileF64BitBeachhead(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(code) == 0 || len(code)%4 != 0 {
		t.Fatalf("code len=%d", len(code))
	}
	if _, err := CompileF64BitBeachhead([]byte{0, 0xa0, 0x0b}); err == nil {
		t.Fatal("f64.add accepted by bit beachhead")
	}
}

func TestWideBeachheadsExecuteUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	t.Run("v128", func(t *testing.T) {
		var x, y [16]byte
		for i := range x {
			x[i] = 0xff
			y[i] = 2
		}
		body := []byte{0}
		body = append(body, simdConst(x)...)
		body = append(body, simdConst(y)...)
		body = append(body, 0xfd, 110, 0x0b)
		fn, err := CompileV128Beachhead(body)
		if err != nil {
			t.Fatal(err)
		}
		var entry rv.Asm
		call := entry.Jal(rv.RA)
		entry.MovImm32(rv.A7, 93)
		entry.Ecall()
		if !entry.PatchJAL21(call, len(entry.B)) {
			t.Fatal("call patch")
		}
		runRV32Exit(t, qemu, append(entry.B, fn...), 1)
	})
	t.Run("i64-mul", func(t *testing.T) {
		fn, err := CompileI64Beachhead([]byte{0, 0x42, 6, 0x42, 7, 0x7e, 0x0b})
		if err != nil {
			t.Fatal(err)
		}
		var entry rv.Asm
		call := entry.Jal(rv.RA)
		entry.MovImm32(rv.A7, 93)
		entry.Ecall()
		if !entry.PatchJAL21(call, len(entry.B)) {
			t.Fatal("call patch")
		}
		runRV32Exit(t, qemu, append(entry.B, fn...), 42)
	})
	t.Run("i64-param-local", func(t *testing.T) {
		body := []byte{1, 1, 0x7e, 0x20, 0, 0x42, 5, 0x7c, 0x22, 1, 0x42, 2, 0x7e, 0x0b}
		fn, err := CompileI64Function(1, body)
		if err != nil {
			t.Fatal(err)
		}
		var entry rv.Asm
		entry.MovImm32(rv.A0, 16)
		entry.MovImm32(rv.A1, 0)
		call := entry.Jal(rv.RA)
		entry.MovImm32(rv.A7, 93)
		entry.Ecall()
		if !entry.PatchJAL21(call, len(entry.B)) {
			t.Fatal("call patch")
		}
		runRV32Exit(t, qemu, append(entry.B, fn...), 42)
	})
	t.Run("f64-neg", func(t *testing.T) {
		body := []byte{0, 0x44}
		var b [8]byte
		binary.LittleEndian.PutUint64(b[:], math.Float64bits(1.5))
		body = append(body, b[:]...)
		body = append(body, 0x9a, 0x0b)
		fn, err := CompileF64BitBeachhead(body)
		if err != nil {
			t.Fatal(err)
		}
		var entry rv.Asm
		call := entry.Jal(rv.RA)
		if !entry.Srli(rv.A0, rv.A1, 31) {
			t.Fatal("shift")
		}
		entry.MovImm32(rv.A7, 93)
		entry.Ecall()
		if !entry.PatchJAL21(call, len(entry.B)) {
			t.Fatal("call patch")
		}
		runRV32Exit(t, qemu, append(entry.B, fn...), 1)
	})
}

func runRV32Exit(t *testing.T, qemu string, code []byte, want int) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "wide.elf")
	if err := os.WriteFile(path, rv32ELF(code), 0o755); err != nil {
		t.Fatal(err)
	}
	err := exec.Command(qemu, path).Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != want {
		t.Fatalf("qemu=%v exit=%v want=%d", err, exit, want)
	}
}

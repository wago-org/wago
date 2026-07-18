package riscv32

import (
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	rv "github.com/wago-org/wago/src/core/encoder/riscv32"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
)

func TestHelperThunkValidation(t *testing.T) {
	if code, err := CompileF64HelperThunk(embedded32.F64Add); err != nil || len(code) != 16 {
		t.Fatalf("f64 thunk len=%d err=%v", len(code), err)
	}
	if code, err := CompileI64HelperThunk(embedded32.I64Rotl); err != nil || len(code) != 16 {
		t.Fatalf("i64 thunk len=%d err=%v", len(code), err)
	}
	if code, err := CompileSIMDHelperThunk(174); err != nil || len(code) != 16 {
		t.Fatalf("SIMD thunk len=%d err=%v", len(code), err)
	}
	if _, err := CompileI64HelperThunk(255); err == nil {
		t.Fatal("invalid i64 op accepted")
	}
	if _, err := CompileF64HelperThunk(255); err == nil {
		t.Fatal("invalid f64 op accepted")
	}
	if _, err := CompileSIMDHelperThunk(276); err == nil {
		t.Fatal("invalid SIMD op accepted as helper")
	}
}

func TestHelperThunksExecuteUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	for _, tc := range []struct {
		name string
		op   uint32
		code []byte
	}{
		{name: "f64", op: uint32(embedded32.I64TruncSatF64U)},
		{name: "simd", op: 174},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			if tc.name == "f64" {
				tc.code, err = CompileF64HelperThunk(embedded32.F64Op(tc.op))
			} else {
				tc.code, err = CompileSIMDHelperThunk(tc.op)
			}
			if err != nil {
				t.Fatal(err)
			}
			const base = 0x10000
			const entryLen = 24
			helperOff := entryLen + len(tc.code)
			const helperLen = 8
			tableOff := helperOff + helperLen
			var entry rv.Asm
			entry.MovReg(rv.A0, rv.SP)
			entry.MovImm32(rv.A1, base+uint32(tableOff))
			call := entry.Jal(rv.RA)
			entry.MovImm32(rv.A7, 93)
			entry.Ecall()
			if len(entry.B) != entryLen || !entry.PatchJAL21(call, entryLen) {
				t.Fatalf("entry len=%d", len(entry.B))
			}
			var helper rv.Asm
			if !helper.Lw(rv.A0, rv.A0, embedded32.F64FrameOpOffset) {
				t.Fatal("helper load")
			}
			helper.Ret()
			code := append(entry.B, tc.code...)
			code = append(code, helper.B...)
			var table [8]byte
			binary.LittleEndian.PutUint32(table[0:4], base+uint32(helperOff))
			binary.LittleEndian.PutUint32(table[4:8], base+uint32(helperOff))
			code = append(code, table[:]...)
			path := filepath.Join(t.TempDir(), "helper.elf")
			if err := os.WriteFile(path, rv32ELF(code), 0o755); err != nil {
				t.Fatal(err)
			}
			err = exec.Command(qemu, path).Run()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != int(tc.op) {
				t.Fatalf("qemu=%v exit=%v want=%d", err, exit, tc.op)
			}
		})
	}
}

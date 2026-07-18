package arm32

import (
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	a32 "github.com/wago-org/wago/src/core/encoder/arm32"
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
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	for _, tc := range []struct {
		name string
		op   uint32
	}{
		{name: "f64", op: uint32(embedded32.I64TruncSatF64U)},
		{name: "simd", op: 174},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var thunk []byte
			var err error
			if tc.name == "f64" {
				thunk, err = CompileF64HelperThunk(embedded32.F64Op(tc.op))
			} else {
				thunk, err = CompileSIMDHelperThunk(tc.op)
			}
			if err != nil {
				t.Fatal(err)
			}
			const base = 0x10000
			const entryLen = 24
			helperOff := entryLen + len(thunk)
			const helperLen = 8
			tableOff := helperOff + helperLen
			var entry a32.Asm
			if !entry.MovReg(a32.R0, a32.SP) || !entry.MovImm32(a32.R1, base+uint32(tableOff)) {
				t.Fatal("entry materialization")
			}
			call := entry.Call()
			if !entry.MovImm32(a32.R7, 1) {
				t.Fatal("syscall")
			}
			entry.Svc(0)
			entry.Align4()
			if len(entry.B) != entryLen || !entry.PatchCall(call, entryLen) {
				t.Fatalf("entry len=%d", len(entry.B))
			}
			var helper a32.Asm
			if !helper.Ldr(a32.R0, a32.R0, embedded32.F64FrameOpOffset) {
				t.Fatal("helper load")
			}
			helper.Ret()
			helper.Align4()
			code := append(entry.B, thunk...)
			code = append(code, helper.B...)
			var table [8]byte
			ptr := base + uint32(helperOff) | 1
			binary.LittleEndian.PutUint32(table[0:4], ptr)
			binary.LittleEndian.PutUint32(table[4:8], ptr)
			code = append(code, table[:]...)
			path := filepath.Join(t.TempDir(), "helper.elf")
			if err := os.WriteFile(path, arm32ELF(code), 0o755); err != nil {
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

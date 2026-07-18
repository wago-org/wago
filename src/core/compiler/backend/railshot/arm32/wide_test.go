package arm32

import (
	"encoding/binary"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	a32 "github.com/wago-org/wago/src/core/encoder/arm32"
)

func simdConst(v [16]byte) []byte { return append([]byte{0xfd, 0x0c}, v[:]...) }
func wordSplat(v uint32) (x [16]byte) {
	for i := 0; i < 4; i++ {
		binary.LittleEndian.PutUint32(x[i*4:], v)
	}
	return
}
func vadd(body []byte) []byte { return append(body, 0xfd, 0xae, 0x01) }

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
	local := []byte{0, 0x20, 0}
	local = append(local, simdConst(wordSplat(1))...)
	local = vadd(local)
	local = append(local, 0x22, 0, 0x20, 0)
	local = vadd(local)
	local = append(local, 0x0b)
	if _, err := CompileV128Function(1, local); err != nil {
		t.Fatal(err)
	}
	spill := []byte{1, 1, 0x7b}
	for i := uint32(1); i <= 6; i++ {
		spill = append(spill, simdConst(wordSplat(i))...)
	}
	for i := 0; i < 5; i++ {
		spill = vadd(spill)
	}
	spill = append(spill, 0x0b)
	if _, err := CompileV128Function(0, spill); err != nil {
		t.Fatal(err)
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
	spillBody := []byte{1, 2, 0x7e, 0x42, 1, 0x42, 2, 0x42, 3, 0x42, 4, 0x42, 5, 0x7c, 0x7c, 0x7c, 0x7c, 0x0b}
	if _, err := CompileI64Function(0, spillBody); err != nil {
		t.Fatal(err)
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
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
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
		var entry a32.Asm
		call := entry.Call()
		if !entry.MovImm32(a32.R7, 1) {
			t.Fatal("syscall")
		}
		entry.Svc(0)
		entry.Align4()
		if !entry.PatchCall(call, len(entry.B)) {
			t.Fatal("call patch")
		}
		runARM32Exit(t, qemu, append(entry.B, fn...), 1)
	})
	t.Run("v128-param-local", func(t *testing.T) {
		body := []byte{0, 0x20, 0}
		body = append(body, simdConst(wordSplat(1))...)
		body = vadd(body)
		body = append(body, 0x22, 0, 0x20, 0)
		body = vadd(body)
		body = append(body, 0x0b)
		fn, err := CompileV128Function(1, body)
		if err != nil {
			t.Fatal(err)
		}
		var entry a32.Asm
		for _, r := range []a32.Reg{a32.R0, a32.R1, a32.R2, a32.R3} {
			entry.MovImm32(r, 20)
		}
		call := entry.Call()
		entry.MovImm32(a32.R7, 1)
		entry.Svc(0)
		entry.Align4()
		entry.PatchCall(call, len(entry.B))
		runARM32Exit(t, qemu, append(entry.B, fn...), 42)
	})
	t.Run("v128-spill-reload", func(t *testing.T) {
		body := []byte{1, 1, 0x7b}
		for i := uint32(1); i <= 6; i++ {
			body = append(body, simdConst(wordSplat(i))...)
		}
		for i := 0; i < 5; i++ {
			body = vadd(body)
		}
		body = append(body, 0x0b)
		fn, err := CompileV128Function(0, body)
		if err != nil {
			t.Fatal(err)
		}
		var entry a32.Asm
		call := entry.Call()
		entry.MovImm32(a32.R7, 1)
		entry.Svc(0)
		entry.Align4()
		entry.PatchCall(call, len(entry.B))
		runARM32Exit(t, qemu, append(entry.B, fn...), 21)
	})
	t.Run("i64-mul", func(t *testing.T) {
		fn, err := CompileI64Beachhead([]byte{0, 0x42, 6, 0x42, 7, 0x7e, 0x0b})
		if err != nil {
			t.Fatal(err)
		}
		var entry a32.Asm
		call := entry.Call()
		if !entry.MovImm32(a32.R7, 1) {
			t.Fatal("syscall")
		}
		entry.Svc(0)
		entry.Align4()
		if !entry.PatchCall(call, len(entry.B)) {
			t.Fatal("call patch")
		}
		runARM32Exit(t, qemu, append(entry.B, fn...), 42)
	})
	t.Run("i64-param-local", func(t *testing.T) {
		body := []byte{1, 1, 0x7e, 0x20, 0, 0x42, 5, 0x7c, 0x22, 1, 0x42, 2, 0x7e, 0x0b}
		fn, err := CompileI64Function(1, body)
		if err != nil {
			t.Fatal(err)
		}
		var entry a32.Asm
		entry.MovImm32(a32.R0, 16)
		entry.MovImm32(a32.R1, 0)
		call := entry.Call()
		if !entry.MovImm32(a32.R7, 1) {
			t.Fatal("syscall")
		}
		entry.Svc(0)
		entry.Align4()
		if !entry.PatchCall(call, len(entry.B)) {
			t.Fatal("call patch")
		}
		runARM32Exit(t, qemu, append(entry.B, fn...), 42)
	})
	t.Run("i64-spill-reload", func(t *testing.T) {
		body := []byte{1, 2, 0x7e, 0x42, 1, 0x42, 2, 0x42, 3, 0x42, 4, 0x42, 5, 0x7c, 0x7c, 0x7c, 0x7c, 0x0b}
		fn, err := CompileI64Function(0, body)
		if err != nil {
			t.Fatal(err)
		}
		var entry a32.Asm
		call := entry.Call()
		if !entry.MovImm32(a32.R7, 1) {
			t.Fatal("syscall")
		}
		entry.Svc(0)
		entry.Align4()
		if !entry.PatchCall(call, len(entry.B)) {
			t.Fatal("call patch")
		}
		runARM32Exit(t, qemu, append(entry.B, fn...), 15)
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
		var entry a32.Asm
		call := entry.Call()
		if !entry.LsrImm(a32.R0, a32.R1, 31) || !entry.MovImm32(a32.R7, 1) {
			t.Fatal("entry")
		}
		entry.Svc(0)
		entry.Align4()
		if !entry.PatchCall(call, len(entry.B)) {
			t.Fatal("call patch")
		}
		runARM32Exit(t, qemu, append(entry.B, fn...), 1)
	})
}

func runARM32Exit(t *testing.T, qemu string, code []byte, want int) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "wide.elf")
	if err := os.WriteFile(path, arm32ELF(code), 0o755); err != nil {
		t.Fatal(err)
	}
	err := exec.Command(qemu, path).Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != want {
		t.Fatalf("qemu=%v exit=%v want=%d", err, exit, want)
	}
}

//go:build linux && amd64

package amd64

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// disasm returns objdump's Intel-syntax disassembly of raw machine code.
func disasm(t *testing.T, code []byte) string {
	t.Helper()
	objdump, err := exec.LookPath("objdump")
	if err != nil {
		t.Skip("objdump not on PATH")
	}
	f := filepath.Join(t.TempDir(), "c.bin")
	if err := os.WriteFile(f, code, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(objdump, "-D", "-b", "binary", "-m", "i386:x86-64", "-M", "intel", f).Output()
	if err != nil {
		t.Fatalf("objdump: %v", err)
	}
	return string(out)
}

// TestRegisterResident proves the Valent-Block property: the wasm operand stack
// lives in registers, so the body emits NO per-operation push/pop (only the
// prologue's `push rbp`). The naive stack machine would emit many.
func TestRegisterResident(t *testing.T) {
	m := watToModule(t, `(module (func (export "f") (param i32 i32) (result i32)
		local.get 0 local.get 1 i32.add
		local.get 0 i32.mul
		i32.const 7 i32.add))`)
	code, err := CompileFunction(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	dis := disasm(t, code)
	pushes, pops := 0, 0
	for _, line := range strings.Split(dis, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		mnem := fields[len(fields)-2] // objdump: "addr: bytes  mnem ops"
		// the mnemonic is the first token after the byte columns; scan all tokens
		for _, tok := range fields {
			switch tok {
			case "push":
				pushes++
			case "pop":
				pops++
			}
		}
		_ = mnem
	}
	t.Logf("disassembly:\n%s", dis)
	if pushes != 1 || pops != 0 {
		t.Fatalf("expected exactly 1 push (prologue) and 0 pops; got push=%d pop=%d", pushes, pops)
	}
}

// genSpillWat builds a function that keeps n register-resident temporaries live
// simultaneously, then folds them: result = n*p0 + n(n+1)/2.
func genSpillWat(n int) string {
	var b strings.Builder
	b.WriteString(`(module (func (export "f") (param i32) (result i32)` + "\n")
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "  local.get 0 i32.const %d i32.add\n", i)
	}
	for i := 0; i < n-1; i++ {
		b.WriteString("  i32.add\n")
	}
	b.WriteString("))")
	return b.String()
}

// TestSpillStress forces more live temporaries (n=20) than scratch registers
// (12), exercising the spill-to-frame and reload paths.
func TestSpillStress(t *testing.T) {
	for _, n := range []int{4, 12, 13, 20, 40} {
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			m := watToModule(t, genSpillWat(n))
			const p0 = 3
			got := runI32(t, m, p0)
			want := int32(n*p0 + n*(n+1)/2)
			if got != want {
				t.Fatalf("n=%d: got %d want %d", n, got, want)
			}
		})
	}
}

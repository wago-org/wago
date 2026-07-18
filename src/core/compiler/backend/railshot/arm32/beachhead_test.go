package arm32

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	a32 "github.com/wago-org/wago/src/core/encoder/arm32"
)

func TestCompileBeachheadSupportedBodies(t *testing.T) {
	cases := []struct {
		name   string
		params int
		body   []byte
	}{
		{"const", 0, []byte{0x00, 0x41, 0x2a, 0x0b}},
		{"add", 2, []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b}},
		{"local", 1, []byte{0x01, 0x01, 0x7f, 0x41, 0x07, 0x21, 0x01, 0x20, 0x00, 0x20, 0x01, 0x6c, 0x0b}},
		{"if", 1, []byte{0x01, 0x01, 0x7f, 0x20, 0x00, 0x04, 0x40, 0x41, 0x07, 0x21, 0x01, 0x05, 0x41, 0x09, 0x21, 0x01, 0x0b, 0x20, 0x01, 0x0b}},
		{"compare", 2, []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x4a, 0x0b}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, err := CompileBeachhead(tc.params, tc.body)
			if err != nil {
				t.Fatal(err)
			}
			if len(code) == 0 || len(code)%4 != 0 {
				t.Fatalf("emitted %d bytes", len(code))
			}
		})
	}
}

func TestCompileBeachheadRejectsUnsupportedShape(t *testing.T) {
	cases := []struct {
		name   string
		params int
		body   []byte
	}{
		{"too-many-params", 5, []byte{0x00, 0x0b}},
		{"i64-local", 0, []byte{0x01, 0x01, 0x7e, 0x0b}},
		{"result-block", 0, []byte{0x00, 0x02, 0x7f, 0x0b, 0x0b}},
		{"unsupported-op", 0, []byte{0x00, 0x23, 0x00, 0x0b}},
		{"missing-end", 0, []byte{0x00, 0x41, 0x00}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := CompileBeachhead(tc.params, tc.body); err == nil {
				t.Fatal("compile unexpectedly succeeded")
			}
		})
	}
}

func TestI32ExtendedOpsExecuteUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	tests := []struct {
		name string
		body []byte
		want int
	}{
		{"clz", []byte{0, 0x41, 1, 0x67, 0x0b}, 31},
		{"ctz", []byte{0, 0x41, 16, 0x68, 0x0b}, 4},
		{"popcnt", []byte{0, 0x41, 15, 0x69, 0x0b}, 4},
		{"rotl", []byte{0, 0x41, 1, 0x41, 7, 0x77, 0x0b}, 128},
		{"rotr", []byte{0, 0x41, 0x80, 0x02, 0x41, 1, 0x78, 0x0b}, 128},
		{"extend8_s", []byte{0, 0x41, 0x80, 0x01, 0xc0, 0x41, 24, 0x76, 0x0b}, 255},
		{"extend16_s", []byte{0, 0x41, 0x80, 0x80, 0x02, 0xc1, 0x41, 24, 0x76, 0x0b}, 255},
		{"drop", []byte{0, 0x41, 1, 0x1a, 0x41, 42, 0x0b}, 42},
		{"select_false", []byte{0, 0x41, 7, 0x41, 9, 0x41, 0, 0x1b, 0x0b}, 9},
		{"select_true", []byte{0, 0x41, 7, 0x41, 9, 0x41, 1, 0x1b, 0x0b}, 7},
		{"select_t", []byte{0, 0x41, 7, 0x41, 9, 0x41, 1, 0x1c, 1, 0x7f, 0x0b}, 7},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fn, err := CompileBeachhead(0, tc.body)
			if err != nil {
				t.Fatal(err)
			}
			var wrapper a32.Asm
			call := wrapper.Call()
			mustEncode(t, wrapper.MovImm32(a32.R7, 1))
			wrapper.Svc(0)
			if !wrapper.PatchCall(call, len(wrapper.B)) {
				t.Fatal("wrapper call patch rejected")
			}
			runARM32Exit(t, qemu, append(wrapper.B, fn...), tc.want)
		})
	}
}

func TestCompileBeachheadExecutesUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	fn, err := CompileBeachhead(0, []byte{0x00, 0x41, 0x06, 0x41, 0x07, 0x6c, 0x0b})
	if err != nil {
		t.Fatal(err)
	}
	var wrapper a32.Asm
	call := wrapper.Call()
	mustEncode(t, wrapper.MovImm32(a32.R7, 1))
	wrapper.Svc(0)
	if !wrapper.PatchCall(call, len(wrapper.B)) {
		t.Fatal("wrapper call patch rejected")
	}
	code := append(wrapper.B, fn...)
	path := filepath.Join(t.TempDir(), "arm32-beachhead.elf")
	if err := os.WriteFile(path, arm32ELF(code), 0o755); err != nil {
		t.Fatal(err)
	}
	err = exec.Command(qemu, path).Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 42 {
		t.Fatalf("qemu result %v", err)
	}
}

func mustEncode(t *testing.T, ok bool) {
	t.Helper()
	if !ok {
		t.Fatal("encoding rejected")
	}
}

func arm32ELF(code []byte) []byte {
	const codeOff, base = 0x1000, 0x10000
	buf := bytes.NewBuffer(make([]byte, 0, codeOff+len(code)))
	buf.Write([]byte{0x7f, 'E', 'L', 'F', 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	write := func(v any) { _ = binary.Write(buf, binary.LittleEndian, v) }
	write(uint16(2))
	write(uint16(40))
	write(uint32(1))
	write(uint32(base | 1))
	write(uint32(52))
	write(uint32(0))
	write(uint32(0x05000200))
	write(uint16(52))
	write(uint16(32))
	write(uint16(1))
	write(uint16(0))
	write(uint16(0))
	write(uint16(0))
	write(uint32(1))
	write(uint32(codeOff))
	write(uint32(base))
	write(uint32(base))
	write(uint32(len(code)))
	write(uint32(len(code)))
	write(uint32(5))
	write(uint32(0x1000))
	for buf.Len() < codeOff {
		buf.WriteByte(0)
	}
	buf.Write(code)
	return buf.Bytes()
}

package embedded32

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPico2FirmwareCTransport(t *testing.T) {
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("C compiler not installed")
	}
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller unavailable")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", "..", ".."))
	firmware := filepath.Join(root, "firmware", "pico2")
	binary := filepath.Join(t.TempDir(), "wago-pico2-c-test")
	command := exec.Command(cc,
		"-std=c11", "-Wall", "-Wextra", "-Werror", "-pedantic",
		filepath.Join(firmware, "wago_pico2.c"),
		filepath.Join(firmware, "wago_pico2_test.c"),
		"-o", binary,
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("compile firmware transport: %v\n%s", err, output)
	}
	if output, err := exec.Command(binary).CombinedOutput(); err != nil {
		t.Fatalf("run firmware transport: %v\n%s", err, output)
	}
}

func TestPico2FirmwareCABIConstants(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller unavailable")
	}
	headerPath := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", "..", "..", "firmware", "pico2", "wago_pico2.h"))
	header, err := os.ReadFile(headerPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(header)
	checks := []string{
		"#define WAGO_PICO2_CONTEXT_ABI_BYTES UINT32_C(" + decimal(ContextABISize) + ")",
		"#define WAGO_PICO2_CALL_ABI_BYTES UINT32_C(" + decimal(CallABIBytes) + ")",
		"#define WAGO_PICO2_CONTEXT_TRAP_CELL_OFFSET UINT32_C(" + decimal(ContextTrapCellOffset) + ")",
		"#define WAGO_PICO2_CONTEXT_CANCEL_CELL_OFFSET UINT32_C(" + decimal(ContextCancelCellOffset) + ")",
	}
	for _, check := range checks {
		if !strings.Contains(text, check) {
			t.Fatalf("firmware header is missing %q", check)
		}
	}
}

func decimal(value uint32) string {
	if value == 0 {
		return "0"
	}
	var digits [10]byte
	i := len(digits)
	for value != 0 {
		i--
		digits[i] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[i:])
}

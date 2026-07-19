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

func TestPico2TinyGoHelpers(t *testing.T) {
	if os.Getenv("WAGO_PICO2_TINYGO_HELPERS") != "1" {
		t.Skip("set WAGO_PICO2_TINYGO_HELPERS=1 to cross-compile helper objects")
	}
	tinygo, err := exec.LookPath("tinygo")
	if err != nil {
		t.Skip("TinyGo not installed")
	}
	objcopy, err := exec.LookPath("llvm-objcopy")
	if err != nil {
		t.Skip("llvm-objcopy not installed")
	}
	nm, err := exec.LookPath("nm")
	if err != nil {
		t.Skip("nm not installed")
	}
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller unavailable")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", "..", ".."))
	for _, target := range []string{"pico2", "riscv-qemu"} {
		t.Run(target, func(t *testing.T) {
			raw := filepath.Join(t.TempDir(), "helpers-raw.o")
			object := filepath.Join(t.TempDir(), "helpers.o")
			command := exec.Command(tinygo, "build", "-target="+target,
				"-scheduler=none", "-panic=trap", "-o", raw, "./firmware/pico2/helpers")
			command.Dir = root
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("build %s helpers: %v\n%s", target, err, output)
			}
			arguments := []string{
				"--keep-global-symbol=wago_embedded32_f64",
				"--keep-global-symbol=wago_embedded32_simd_abi",
				"--keep-global-symbol=wago_embedded32_i64",
				"--keep-global-symbol=wago_embedded32_f32",
				raw, object,
			}
			if output, err := exec.Command(objcopy, arguments...).CombinedOutput(); err != nil {
				t.Fatalf("localize %s helpers: %v\n%s", target, err, output)
			}
			output, err := exec.Command(nm, "-g", "--defined-only", object).CombinedOutput()
			if err != nil {
				t.Fatalf("inspect %s helpers: %v\n%s", target, err, output)
			}
			text := string(output)
			for _, symbol := range []string{
				"wago_embedded32_f64", "wago_embedded32_simd_abi",
				"wago_embedded32_i64", "wago_embedded32_f32",
			} {
				if !strings.Contains(text, symbol) {
					t.Fatalf("%s helper object is missing %s\n%s", target, symbol, text)
				}
			}
			if strings.Contains(text, "Reset_Handler") || strings.Contains(text, "malloc") {
				t.Fatalf("%s helper object leaked TinyGo runtime symbols\n%s", target, text)
			}
		})
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
		"#define WAGO_PICO2_CONTEXT_HELPER_TABLE_OFFSET UINT32_C(" + decimal(ContextHelperTableOffset) + ")",
		"#define WAGO_PICO2_HELPER_F64_OFFSET UINT32_C(" + decimal(HelperF64Offset) + ")",
		"#define WAGO_PICO2_HELPER_SIMD_OFFSET UINT32_C(" + decimal(HelperSIMDOffset) + ")",
		"#define WAGO_PICO2_HELPER_I64_OFFSET UINT32_C(" + decimal(HelperI64Offset) + ")",
		"#define WAGO_PICO2_HELPER_F32_OFFSET UINT32_C(" + decimal(HelperF32Offset) + ")",
		"#define WAGO_PICO2_HELPER_TABLE_BYTES UINT32_C(" + decimal(HelperTableBytes) + ")",
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

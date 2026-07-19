package shared

import (
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/runtime/embedded32"
)

func TestRenderPico2FirmwareC(t *testing.T) {
	const base = uint32(0x20040000)
	image := &EmbeddedFirmwareImage{
		Bytes:          make([]byte, 160),
		BaseAddress:    base,
		ContextAddress: base + 32,
		StartAddress:   base + 3,
		TransportFunctions: []embedded32.FirmwareTransportFunction{
			{Address: base + 0x21, Context: base + 32, ParamSlots: 3, ResultSlots: 2},
		},
	}
	for i := range image.Bytes {
		image.Bytes[i] = byte(i)
	}
	binary.LittleEndian.PutUint32(image.Bytes[32+embedded32.ContextHelperTableOffset:], base+128)
	source, err := RenderPico2FirmwareC(image, Pico2FirmwareCOptions{
		Symbol:         "fixture_image",
		Target:         embedded32.TransportTargetArm32,
		MaximumPayload: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	checks := []string{
		"WAGO_PICO2_IMAGE_STORAGE(\".wago_pico2_image.fixture_image\")",
		"uint8_t fixture_image_image[160]",
		".load_address = UINT32_C(0x20040000)",
		"{UINT32_C(0x20040021), UINT32_C(0x20040020), UINT16_C(3), UINT16_C(2)}",
		".context_count = UINT32_C(1)",
	}
	for _, check := range checks {
		if !strings.Contains(text, check) {
			t.Fatalf("generated C is missing %q", check)
		}
	}

	linker, err := RenderPico2FirmwareLinkerScript("fixture_image", "", base, uint32(len(image.Bytes)))
	if err != nil {
		t.Fatal(err)
	}
	if text := string(linker); !strings.Contains(text, ".wago_pico2_image.fixture_image 0x20040000 (NOLOAD)") ||
		!strings.Contains(text, "ASSERT(SIZEOF(.wago_pico2_image.fixture_image) == 160") {
		t.Fatalf("unexpected linker script:\n%s", text)
	}

	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("C compiler not installed")
	}
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller unavailable")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(testFile), "..", "..", "..", "..", "..", ".."))
	firmware := filepath.Join(root, "firmware", "pico2")
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "fixture.c")
	if err := os.WriteFile(sourcePath, source, 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(cc, "-std=c11", "-Wall", "-Wextra", "-Werror", "-pedantic",
		"-I", firmware, "-c", sourcePath, "-o", filepath.Join(dir, "fixture.o"))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("compile generated C: %v\n%s", err, output)
	}
}

func TestRenderPico2LinkedFirmwareCIncludesEveryContext(t *testing.T) {
	const base = uint32(0x20050000)
	provider := &EmbeddedFirmwareImage{ContextAddress: base + 16}
	consumer := &EmbeddedFirmwareImage{ContextAddress: base + 112}
	bundle := &EmbeddedLinkedFirmwareImage{
		Bytes:       make([]byte, 256),
		BaseAddress: base,
		Modules: []EmbeddedLinkedFirmwareModule{
			{Name: "provider", Image: provider},
			{Name: "consumer", Image: consumer},
		},
	}
	binary.LittleEndian.PutUint32(bundle.Bytes[16+embedded32.ContextHelperTableOffset:], base+208)
	binary.LittleEndian.PutUint32(bundle.Bytes[112+embedded32.ContextHelperTableOffset:], base+224)
	source, err := RenderPico2LinkedFirmwareC(bundle, 1, Pico2FirmwareCOptions{
		Symbol:         "linked_fixture",
		Target:         embedded32.TransportTargetRISCV32,
		MaximumPayload: 128,
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, check := range []string{"UINT32_C(0x20050010)", "UINT32_C(0x20050070)", ".context_count = UINT32_C(2)"} {
		if !strings.Contains(text, check) {
			t.Fatalf("generated linked C is missing %q", check)
		}
	}
}

func TestRenderPico2FirmwareCRejectsUnsafeMetadata(t *testing.T) {
	image := &EmbeddedFirmwareImage{Bytes: make([]byte, 128), BaseAddress: 0x20000000, ContextAddress: 0x20000010}
	binary.LittleEndian.PutUint32(image.Bytes[0x10+embedded32.ContextHelperTableOffset:], 0x20000070)
	for _, opts := range []Pico2FirmwareCOptions{
		{Symbol: "bad-name", Target: embedded32.TransportTargetArm32, MaximumPayload: 1},
		{Symbol: "image", Target: 99, MaximumPayload: 1},
		{Symbol: "image", Target: embedded32.TransportTargetArm32},
		{Symbol: "image", Section: "bad section", Target: embedded32.TransportTargetArm32, MaximumPayload: 1},
	} {
		if _, err := RenderPico2FirmwareC(image, opts); err == nil {
			t.Fatalf("accepted options %+v", opts)
		}
	}
}

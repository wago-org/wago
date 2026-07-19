package shared

import (
	"encoding/binary"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/runtime/embedded32"
)

func TestRenderPico2FirmwareGo(t *testing.T) {
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
	for index := range image.Bytes {
		image.Bytes[index] = byte(index)
	}
	binary.LittleEndian.PutUint32(image.Bytes[32+embedded32.ContextHelperTableOffset:], base+128)
	source, err := RenderPico2FirmwareGo(image, Pico2FirmwareGoOptions{
		Package:        "firmware",
		Symbol:         "ApplicationImage",
		Target:         embedded32.TransportTargetArm32,
		MaximumPayload: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "application_image.go", source, parser.AllErrors); err != nil {
		t.Fatalf("parse generated Go: %v\n%s", err, source)
	}
	text := string(source)
	checks := []string{
		"const ApplicationImageInitialImage =",
		"ImageAddress:   0x20040000",
		"Address: 0x20040021",
		"ParamSlots: 3",
		"Contexts:       ApplicationImageContexts[:]",
	}
	for _, check := range checks {
		if !strings.Contains(text, check) {
			t.Fatalf("generated Go is missing %q\n%s", check, text)
		}
	}
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller unavailable")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(testFile), "..", "..", "..", "..", "..", ".."))
	dir := t.TempDir()
	module := "module firmwarefixture\n\ngo 1.22\n\nrequire github.com/wago-org/wago v0.0.0\nreplace github.com/wago-org/wago => " + root + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(module), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "application_image.go"), source, 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "test", "./...")
	command.Dir = dir
	command.Env = append(os.Environ(), "GOWORK=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("compile generated Go: %v\n%s", err, output)
	}
	linker, err := RenderPico2FirmwareMemoryScript("ApplicationImage", base, uint32(len(image.Bytes)))
	if err != nil {
		t.Fatal(err)
	}
	if text := string(linker); !strings.Contains(text, ".wago_pico2_image_ApplicationImage 0x20040000 (NOLOAD)") ||
		!strings.Contains(text, "ASSERT(SIZEOF(.wago_pico2_image_ApplicationImage) == 160") {
		t.Fatalf("unexpected linker script:\n%s", text)
	}
}

func TestRenderPico2LinkedFirmwareGoIncludesEveryContext(t *testing.T) {
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
	source, err := RenderPico2LinkedFirmwareGo(bundle, 1, Pico2FirmwareGoOptions{
		Package:        "firmware",
		Symbol:         "LinkedImage",
		Target:         embedded32.TransportTargetRISCV32,
		MaximumPayload: 128,
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, check := range []string{"0x20050010", "0x20050070", "Contexts:       LinkedImageContexts[:]"} {
		if !strings.Contains(text, check) {
			t.Fatalf("generated linked Go is missing %q\n%s", check, text)
		}
	}
}

func TestRenderPico2FirmwareGoRejectsUnsafeMetadata(t *testing.T) {
	image := &EmbeddedFirmwareImage{Bytes: make([]byte, 128), BaseAddress: 0x20000000, ContextAddress: 0x20000010}
	binary.LittleEndian.PutUint32(image.Bytes[0x10+embedded32.ContextHelperTableOffset:], 0x20000070)
	for _, opts := range []Pico2FirmwareGoOptions{
		{Package: "bad-name", Symbol: "Image", Target: embedded32.TransportTargetArm32, MaximumPayload: 1},
		{Package: "firmware", Symbol: "for", Target: embedded32.TransportTargetArm32, MaximumPayload: 1},
		{Package: "firmware", Symbol: "Image", Target: 99, MaximumPayload: 1},
		{Package: "firmware", Symbol: "Image", Target: embedded32.TransportTargetArm32},
	} {
		if _, err := RenderPico2FirmwareGo(image, opts); err == nil {
			t.Fatalf("accepted options %+v", opts)
		}
	}
}

package spectest

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const release3Revision = "9d36019973201a19f9c9ebb0f10828b2fe2374aa"

func TestRelease3InterpreterBootstrapIsPinned(t *testing.T) {
	repo := filepath.Clean("../..")
	script := filepath.Join(repo, "scripts", "bootstrap-spec-interpreter.sh")
	out, err := exec.Command(script, "--print-revision").CombinedOutput()
	if err != nil {
		t.Fatalf("print official interpreter revision: %v: %s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != release3Revision {
		t.Fatalf("official interpreter revision = %q, want %q", got, release3Revision)
	}

	makefile, err := os.ReadFile(filepath.Join(repo, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(makefile)
	for _, want := range []string{
		"spec3: wabt spec-interpreter",
		"WAGO_SPEC_INTERPRETER=\"$$interpreter\"",
		"WAGO_SPEC_INTERPRETER_REVISION=\"$$interpreter_revision\"",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("Makefile does not lock Release 3 interpreter wiring %q", want)
		}
	}
}

func TestRelease3InterpreterBinaryScriptConverter(t *testing.T) {
	repo := filepath.Clean("../..")
	dir := t.TempDir()
	input := filepath.Join(dir, "sample.bin.wast")
	output := filepath.Join(dir, "sample.json")
	script := `(module definition $M binary "\00\61\73\6d\01\00\00\00")
(module instance $I $M)
(register "I" $I)
(assert_return
  (invoke $I "f" (i32.const 0xffffffff))
  (either (i32.const 0x1) (i32.const 0x2)))
`
	if err := os.WriteFile(input, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	converter := filepath.Join(repo, "scripts", "spec-interpreter-json.py")
	if out, err := exec.Command(converter, input, output).CombinedOutput(); err != nil {
		t.Fatalf("convert binary script: %v: %s", err, out)
	}
	var got struct {
		Source   string `json:"source"`
		Commands []struct {
			Type     string `json:"type"`
			Name     string `json:"name"`
			Module   string `json:"module"`
			Filename string `json:"filename"`
			Either   []struct {
				Type  string `json:"type"`
				Value string `json:"value"`
			} `json:"either"`
		} `json:"commands"`
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Source != "WebAssembly/spec interpreter 3.0.0" || len(got.Commands) != 4 {
		t.Fatalf("converted document = source %q commands %+v", got.Source, got.Commands)
	}
	if got.Commands[0].Type != "module_definition" || got.Commands[0].Name != "$M" || got.Commands[1].Type != "module_instance" || got.Commands[1].Name != "$I" || got.Commands[1].Module != "$M" {
		t.Fatalf("definition/instance commands = %+v", got.Commands[:2])
	}
	if len(got.Commands[3].Either) != 2 || got.Commands[3].Either[1].Value != "2" {
		t.Fatalf("either result patterns = %+v", got.Commands[3].Either)
	}
	wasm, err := os.ReadFile(filepath.Join(dir, got.Commands[0].Filename))
	if err != nil {
		t.Fatal(err)
	}
	if string(wasm) != "\x00asm\x01\x00\x00\x00" {
		t.Fatalf("converted module bytes = %x", wasm)
	}
}

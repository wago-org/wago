package spectest

import (
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

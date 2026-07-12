package wagocli

import (
	"os"
	"strings"
	"testing"
)

func TestUsageDocumentsCommandSurface(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "usage-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	usage(f)
	f.Close()
	b, _ := os.ReadFile(f.Name())
	text := string(b)
	for _, want := range []string{
		"wago is a pure-Go",             // banner
		"Usage: wago",                   // usage line
		"compile and execute an export", // run
		"not implemented",               // build
		"decode and validate a module",  // validate
		"github.com/wago-org/wago",      // footer
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("usage text missing %q:\n%s", want, text)
		}
	}
	// Every top-level command must be listed by name (plugin was folded into pkg).
	for _, cmd := range []string{"run", "add", "rm", "plugin", "auth", "module", "env", "build", "validate", "version"} {
		if !strings.Contains(text, cmd) {
			t.Fatalf("usage text missing command %q:\n%s", cmd, text)
		}
	}
	if strings.Contains(text, "test") {
		t.Fatalf("usage should no longer mention test:\n%s", text)
	}
}

func TestValidateModuleBytesAcceptsEmptyModule(t *testing.T) {
	// Magic + version is a valid empty WebAssembly module.
	mod := []byte{'\x00', 'a', 's', 'm', 0x01, 0x00, 0x00, 0x00}
	if err := validateModuleBytes(mod); err != nil {
		t.Fatalf("validateModuleBytes(empty module): %v", err)
	}
}

func TestValidateModuleBytesRejectsDecodeErrors(t *testing.T) {
	badMagic := []byte{'n', 'o', 'p', 'e', 0x01, 0x00, 0x00, 0x00}
	err := validateModuleBytes(badMagic)
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("validateModuleBytes(bad magic) = %v, want decode error", err)
	}
}

func TestUsageDoesNotAdvertiseValidateDirect(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "usage-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	usage(f)
	f.Close()
	b, _ := os.ReadFile(f.Name())
	removedAlias := "validate" + "-direct"
	if strings.Contains(string(b), removedAlias) {
		t.Fatalf("usage should not mention removed validate alias:\n%s", b)
	}
}

func TestRunHelpCollapsesBooleanPairs(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "help-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	runCommand().printHelp(f, "wago run")
	f.Close()
	b, _ := os.ReadFile(f.Name())
	if !strings.Contains(string(b), "--<no->st-flags") || strings.Contains(string(b), "enable: keep comparison results") {
		t.Fatalf("run help did not collapse optimization pair:\n%s", b)
	}
}

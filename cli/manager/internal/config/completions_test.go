package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompletionSupportsCommonShells(t *testing.T) {
	for _, shell := range []string{"zsh", "bash", "fish"} {
		script, err := Completion(shell)
		if err != nil {
			t.Fatalf("Completion(%q): %v", shell, err)
		}
		if !strings.Contains(script, "wago __complete") {
			t.Errorf("%s completion does not use Wago's command-tree protocol", shell)
		}
	}
}

func TestInstallCompletionIsIdempotent(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "wago.zsh")
	rc := filepath.Join(root, ".zshrc")
	for range 2 {
		if _, err := InstallCompletion("zsh", script, rc); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(rc); !os.IsNotExist(err) {
		t.Fatalf("explicit output unexpectedly edited rc: %v", err)
	}

	// The default install path is what owns shell startup configuration.
	t.Setenv("HOME", root)
	for range 2 {
		if _, err := InstallCompletion("zsh", "", rc); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(data), "# Wago completions"); count != 1 {
		t.Fatalf("completion hook count = %d in %q", count, data)
	}
}

func TestInstalledZshCompletionCanBeSourced(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh is not installed")
	}
	root := t.TempDir()
	rc := filepath.Join(root, ".zshrc")
	t.Setenv("HOME", root)
	if _, err := InstallCompletion("zsh", "", rc); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(zsh, "-f", "-c", `. "$WAGO_TEST_ZSHRC"`)
	command.Env = append(os.Environ(), "WAGO_TEST_ZSHRC="+rc)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("source installed zsh completion: %v\n%s", err, output)
	}
}

func TestBashCompletionPassesNestedCommandWords(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}
	root := t.TempDir()
	writeCompletionTestCommand(t, root)
	script, err := Completion("bash")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "wago.bash")
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(bash, "--noprofile", "--norc", "-c", `. "$1"; COMP_WORDS=(wago version ""); COMP_CWORD=2; _wago_complete; printf '%s\n' "${COMPREPLY[@]}"`, "_", path)
	command.Env = append(os.Environ(), "PATH="+root+string(os.PathListSeparator)+os.Getenv("PATH"))
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("bash nested completion: %v\n%s", err, output)
	}
	if got := strings.TrimSpace(string(output)); got != "install" {
		t.Fatalf("bash nested completion = %q, want install", got)
	}
}

func TestZshCompletionPassesNestedCommandWords(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh is not installed")
	}
	root := t.TempDir()
	writeCompletionTestCommand(t, root)
	script, err := Completion("zsh")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "wago.zsh")
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(zsh, "-f", "-c", `. "$1"; compadd() { shift; print -l -- "$@"; }; _files() {}; invoke() { local -a words=(wago version ""); _wago; }; invoke`, "_", path)
	command.Env = append(os.Environ(), "PATH="+root+string(os.PathListSeparator)+os.Getenv("PATH"))
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("zsh nested completion: %v\n%s", err, output)
	}
	if got := strings.TrimSpace(string(output)); got != "install" {
		t.Fatalf("zsh nested completion = %q, want install", got)
	}
}

func writeCompletionTestCommand(t *testing.T, root string) {
	t.Helper()
	path := filepath.Join(root, "wago")
	script := "#!/bin/sh\n" +
		"[ \"$#\" -eq 3 ] && [ \"$1\" = __complete ] && [ \"$2\" = version ] && [ -z \"$3\" ] || exit 1\n" +
		"printf 'install\\n'\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestCompletionRejectsUnknownShell(t *testing.T) {
	if _, err := Completion("powershell"); err == nil {
		t.Fatal("Completion accepted an unsupported shell")
	}
}

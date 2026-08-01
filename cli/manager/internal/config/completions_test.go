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
		for _, command := range []string{"version", "status", "update", "cache", "config"} {
			if !strings.Contains(script, command) {
				t.Errorf("%s completion does not contain %q", shell, command)
			}
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

func TestCompletionRejectsUnknownShell(t *testing.T) {
	if _, err := Completion("powershell"); err == nil {
		t.Fatal("Completion accepted an unsupported shell")
	}
}

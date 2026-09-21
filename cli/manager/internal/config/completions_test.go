package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	if runtime.GOOS == "windows" && runtime.GOARCH == "arm64" {
		t.Skip("Git Bash is an AMD64 binary and is not reliable under Windows ARM64 emulation")
	}
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

func TestFishCompletionXDGConfigHome(t *testing.T) {
	for _, custom := range []bool{false, true} {
		for _, explicit := range []bool{false, true} {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			t.Setenv("XDG_CONFIG_HOME", "")
			root := filepath.Join(home, ".config")
			if custom {
				root = filepath.Join(home, "xdg")
				t.Setenv("XDG_CONFIG_HOME", root)
			}
			path := ""
			want := filepath.Join(root, "fish", "completions", "wago.fish")
			if explicit {
				path = filepath.Join(home, "explicit.fish")
				want = path
			}
			got, err := InstallCompletion("fish", path, "")
			if err != nil || got != want {
				t.Fatalf("custom=%v explicit=%v: path=%q err=%v, want %q", custom, explicit, got, err, want)
			}
			if data, err := os.ReadFile(want); err != nil || !strings.Contains(string(data), "wago __complete") {
				t.Fatalf("completion=%q err=%v", data, err)
			}
		}
	}
}

func TestFishCompletionRequiresHomeOrXDG(t *testing.T) {
	for _, tc := range []struct {
		name                string
		home, xdg, explicit bool
		wantError           bool
	}{
		{name: "home", home: true},
		{name: "xdg", home: true, xdg: true},
		{name: "xdg_without_home", xdg: true},
		{name: "neither_home_nor_xdg", wantError: true},
		{name: "explicit_without_home", explicit: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			previous, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chdir(root); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.Chdir(previous); err != nil {
					t.Error(err)
				}
			})
			home, xdg, output := "", "", ""
			if tc.home {
				home = filepath.Join(root, "home")
			}
			if tc.xdg {
				xdg = filepath.Join(root, "xdg")
			}
			if tc.explicit {
				output = filepath.Join(root, "explicit.fish")
			}
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			t.Setenv("XDG_CONFIG_HOME", xdg)
			got, err := InstallCompletion("fish", output, "")
			if tc.wantError {
				if err == nil || got != "" {
					t.Errorf("install = %q, %v; want an error and no path", got, err)
				}
				if _, err := os.Stat(filepath.Join(root, ".config")); !os.IsNotExist(err) {
					t.Errorf("installation created a relative config directory: %v", err)
				}
				return
			}
			configRoot := filepath.Join(home, ".config")
			if tc.xdg {
				configRoot = xdg
			}
			want := filepath.Join(configRoot, "fish", "completions", "wago.fish")
			if tc.explicit {
				want = output
			}
			if err != nil || got != want {
				t.Fatalf("install = %q, %v; want %q", got, err, want)
			}
			if data, err := os.ReadFile(want); err != nil || !strings.Contains(string(data), "wago __complete") {
				t.Fatalf("completion = %q, %v", data, err)
			}
		})
	}
}

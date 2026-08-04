package wago

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCmdBootstrapDoesNotUsePowerShellOrCommandSubstitution(t *testing.T) {
	data, err := os.ReadFile("install.cmd")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(data))
	if strings.Contains(text, "powershell") {
		t.Fatal("CMD bootstrap depends on PowerShell")
	}
	if strings.Contains(text, "in (`") {
		t.Fatal("CMD bootstrap uses FOR /F command substitution, which Wine cmd.exe does not support")
	}
}

func TestCmdBootstrapExecutesNativeInstaller(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("cmd.exe is available on native Windows CI")
	}
	tmp := t.TempDir()
	installer := filepath.Join(tmp, "wago-installer.exe")
	command := exec.Command("go", "build", "-o", installer, "./cli/installer")
	command.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build installer: %v\n%s", err, output)
	}
	command = exec.Command("cmd.exe", "/D", "/C", "call install.cmd")
	command.Env = append(os.Environ(),
		"WAGO_INSTALLER="+installer,
		"WAGO_VERSION=parity",
		"WAGO_DRY_RUN=1",
		"WAGO_BIN_DIR=ROOT\\bin",
		"WAGO_SRC_DIR=ROOT\\src",
		"NO_COLOR=1",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("CMD bootstrap: %v\n%s", err, output)
	}
	for _, fragment := range []string{"Wago", "Install location", "Plan", "Dry run"} {
		if !strings.Contains(string(output), fragment) {
			t.Fatalf("CMD bootstrap output missing %q:\n%s", fragment, output)
		}
	}
}

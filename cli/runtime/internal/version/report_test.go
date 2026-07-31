package version

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestVersionReportIncludesDiagnostics(t *testing.T) {
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_MANAGER_VERSION", "manager-test")
	t.Setenv("WAGO_MANAGER_EXECUTABLE", "/opt/wago/bin/wago")
	oldStdout := os.Stdout
	t.Cleanup(func() { os.Stdout = oldStdout })
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = write
	Print("canary-20260729-deadbee", "standard", "normal", "none")
	_ = write.Close()
	output, err := io.ReadAll(read)
	_ = read.Close()
	if err != nil {
		t.Fatal(err)
	}
	text := string(output)
	for _, want := range []string{
		"Wago",
		"channel      canary",
		"release      canary-20260729-deadbee",
		"profile      standard",
		"platform",
		"toolchain",
		"manager      manager-test  /opt/wago/bin/wago",
		"runtime",
		"plugins",
		"features",
		"guard pages",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("version report missing %q:\n%s", want, text)
		}
	}
}

func TestDiagnosticChannel(t *testing.T) {
	for _, tc := range []struct {
		active, release, want string
	}{
		{"canary", "deadbee", "canary"},
		{"", "canary-20260729-deadbee", "canary"},
		{"nightly", "deadbee", "nightly"},
		{"", "nightly-20260729-deadbee", "nightly"},
		{"latest", "v1.0.0", "latest"},
		{"", "v1.0.0", "stable"},
		{"local", "deadbee", "local"},
		{"", "deadbee", "development"},
	} {
		if got := diagnosticChannel(tc.active, tc.release); got != tc.want {
			t.Fatalf("diagnosticChannel(%q, %q) = %q, want %q", tc.active, tc.release, got, tc.want)
		}
	}
}

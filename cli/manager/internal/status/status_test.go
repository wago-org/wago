package status

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago/cli/internal/project"
	managerversion "github.com/wago-org/wago/cli/manager/internal/version"
	"github.com/wago-org/wago/internal/wagopaths"
)

func TestInspectReportsActiveRuntime(t *testing.T) {
	root := t.TempDir()
	dirs := wagopaths.Dirs{
		Config:   filepath.Join(root, "config"),
		Data:     filepath.Join(root, "data"),
		Versions: filepath.Join(root, "versions"),
		Cache:    filepath.Join(root, "cache", "canary"),
		Version:  "canary",
	}
	runtimePath := dirs.RuntimeBinary("nightly", "minimal", "tiny")
	if err := os.MkdirAll(filepath.Dir(runtimePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runtimePath, []byte("runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := managerversion.SetActiveInstallation(dirs, "nightly", wagopaths.ProfileMinimal, wagopaths.BuildTiny); err != nil {
		t.Fatal(err)
	}
	t.Setenv(project.BareEnv, "1")

	report, err := Inspect(dirs, "canary-abc1234", filepath.Join(root, "bin", "wago"))
	if err != nil {
		t.Fatal(err)
	}
	if report.RuntimeVersion != "nightly" || report.RuntimeProfile != "minimal" || report.RuntimeBuild != "tiny" || report.RuntimePath != runtimePath {
		t.Fatalf("runtime report = %#v", report)
	}
	if report.Scope != "bare" {
		t.Fatalf("scope = %q", report.Scope)
	}
}

func TestPrintKeepsStatusCompact(t *testing.T) {
	var output bytes.Buffer
	Print(&output, Report{
		ManagerVersion: "canary-abc1234",
		RuntimeVersion: "nightly",
		RuntimeProfile: "standard",
		RuntimeBuild:   "normal",
		RuntimePath:    "/tmp/wago-runtime",
		Scope:          "local",
		ManifestPath:   "/tmp/project/wago.json",
		Plugins:        2,
		LockState:      "up to date",
	})
	for _, want := range []string{"Wago status", "nightly (standard/normal)", "local", "2 enabled", "up to date"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("output does not contain %q:\n%s", want, output.String())
		}
	}
}

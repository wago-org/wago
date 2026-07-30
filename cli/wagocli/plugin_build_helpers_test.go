package wagocli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPluginBuildHelperParsingAndGitignore(t *testing.T) {
	for _, tc := range []struct{ spec, module, version string }{
		{"example.test/plugin@v1.2.3", "example.test/plugin", "v1.2.3"},
		{"example.test/plugin", "example.test/plugin", ""},
		{"@scope/plugin", "@scope/plugin", ""},
	} {
		module, version := splitModuleVersion(tc.spec)
		if module != tc.module || version != tc.version {
			t.Fatalf("splitModuleVersion(%q) = %q, %q", tc.spec, module, version)
		}
	}
	if plural(1) != "" || plural(0) != "s" || plural(2) != "s" {
		t.Fatal("plural helper changed")
	}
	for _, tc := range []struct {
		value string
		want  bool
	}{{"1", true}, {"TRUE", true}, {"yes", true}, {"on", true}, {"0", false}, {"", false}} {
		t.Setenv("WAGO_TEST_TRUTHY", tc.value)
		if got := truthyEnv("WAGO_TEST_TRUTHY"); got != tc.want {
			t.Fatalf("truthyEnv(%q) = %v", tc.value, got)
		}
	}

	dir := t.TempDir()
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldDir); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
	if err := os.Mkdir(".git", 0o700); err != nil {
		t.Fatal(err)
	}
	ensureGitignore(".wago/")
	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil || string(data) != ".wago/\n" {
		t.Fatalf("created gitignore = %q, %v", data, err)
	}
	ensureGitignore(".wago/")
	data, err = os.ReadFile(".gitignore")
	if err != nil || string(data) != ".wago/\n" {
		t.Fatalf("duplicate gitignore entry = %q, %v", data, err)
	}
}

func TestPackageInstallSummary(t *testing.T) {
	var output bytes.Buffer
	printPackageInstallSummary(&output, []packageInstall{
		{module: "github.com/wago-org/wasi", resolved: "0.0.0"},
		{module: "example.com/acme/log", resolved: "1.2.3"},
	}, 2400*time.Microsecond)
	want := "\n+ wago-org/wasi@0.0.0\n+ example.com/acme/log@1.2.3\n\n2 packages installed [2.4ms]\n"
	if got := output.String(); got != want {
		t.Fatalf("package install summary = %q, want %q", got, want)
	}
}

func TestResolvePackageInstallsSupportsMultiplePackages(t *testing.T) {
	packages, err := resolvePackageInstalls([]string{
		"wago-org/wasi@v1.2.3",
		"github.com/acme/log",
		"wago-org/wasi@v1.2.3",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) != 2 ||
		packages[0].module != "github.com/wago-org/wasi" ||
		packages[0].requested != "v1.2.3" ||
		packages[1].module != "github.com/acme/log" {
		t.Fatalf("resolved packages = %#v", packages)
	}
	if _, err := resolvePackageInstalls([]string{"wago-org/wasi@v1", "wago-org/wasi@v2"}); err == nil {
		t.Fatal("conflicting package versions were accepted")
	}
}

func TestAddCommandAcceptsMultiplePackages(t *testing.T) {
	command := addCommand()
	if command.Args != "<module>[@version]..." {
		t.Fatalf("add args = %q", command.Args)
	}
	ctx, err := command.parse("wago add", []string{"wago-org/wasi", "wago-org/workers@v1.2.3"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ctx.Args) != 2 {
		t.Fatalf("add parsed args = %q", ctx.Args)
	}
}

func TestDisplayModuleVersion(t *testing.T) {
	tests := map[string]string{
		"":                                   "0.0.0",
		"v1.2.3":                             "1.2.3",
		"v0.0.0-20260730120000-0123456789ab": "0.0.0",
		" v2.0.0-beta.1 ":                    "2.0.0-beta.1",
	}
	for input, want := range tests {
		if got := displayModuleVersion(input); got != want {
			t.Errorf("displayModuleVersion(%q) = %q, want %q", input, got, want)
		}
	}
}

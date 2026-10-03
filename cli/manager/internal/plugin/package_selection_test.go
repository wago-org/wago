package plugin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sync/atomic"
	"time"

	"github.com/wago-org/wago/cli/internal/automation"
	"strings"
	"testing"

	"github.com/wago-org/wago/cli/manager/internal/registry"
)

func testInstallPackage() registry.InstallPackage {
	return registry.InstallPackage{
		Module: "github.com/wago-org/wasi",
		Name:   "WASI",
		Subpackages: []registry.InstallSubpackage{
			{Module: "github.com/wago-org/wasi/p1", Name: "Preview 1", Description: "Core Wasm commands.", Stability: "stable"},
			{Module: "github.com/wago-org/wasi/p2", Name: "Preview 2", Description: "Component commands.", Stability: "experimental"},
			{Module: "github.com/wago-org/wasi/unstable", Name: "Unstable", Description: "Legacy snapshot 0.", Stability: "deprecated"},
		},
	}
}

func TestPackageInstallModeOffersEverythingOrSubpackageSelection(t *testing.T) {
	items := packageInstallModeItems(3)
	if len(items) != 2 || items[0].Label != "Everything" || items[0].Value != "all" || items[1].Label != "Choose subpackages" {
		t.Fatalf("items = %#v", items)
	}
}

func TestPackageSubpackageSelectorStartsWithEverySubpackageSelected(t *testing.T) {
	selector, modules := packageSubpackageSelector(testInstallPackage())
	frame := selector.Frame()
	for _, want := range []string{"Subpackages · WASI", "Preview 1", "(stable) Core Wasm commands.", "Preview 2", "Install none"} {
		if !strings.Contains(frame, want) {
			t.Errorf("selector does not contain %q:\n%s", want, frame)
		}
	}
	selected := selectedPackageSubpackages(selector, modules)
	if len(selected) != 3 || selected[2] != "github.com/wago-org/wasi/unstable" {
		t.Fatalf("selected = %q", selected)
	}
	selector.Items[1].On = false
	selected = selectedPackageSubpackages(selector, modules)
	if len(selected) != 2 || selected[0] != "github.com/wago-org/wasi/p1" || selected[1] != "github.com/wago-org/wasi/unstable" {
		t.Fatalf("custom selected = %q", selected)
	}
}

func TestReviewPackageInstallChoicesKeepsConstraintsForChosenProviders(t *testing.T) {
	specs := []string{"github.com/acme/first", "github.com/wago-org/wasi@^0.2.0", "github.com/acme/last"}
	pkg := testInstallPackage()
	got := replacePackageInstallSpec(append([]string(nil), specs...), 1, []string{pkg.Subpackages[0].Module, pkg.Subpackages[1].Module}, "^0.2.0")
	want := []string{"github.com/acme/first", "github.com/wago-org/wasi/p1@^0.2.0", "github.com/wago-org/wasi/p2@^0.2.0", "github.com/acme/last"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("specs = %q, want %q", got, want)
	}
}

func TestNoInputKeepsPackageRootForInstallEverything(t *testing.T) {
	t.Setenv("WAGO_NONINTERACTIVE", "1")
	specs := []string{"github.com/wago-org/wasi@^0.2.0"}
	got, err := reviewPackageInstallChoices(specs, []packageInstallPrompt{{index: 0, constraint: "^0.2.0", pkg: testInstallPackage()}})
	if err != nil || len(got) != 1 || got[0] != specs[0] {
		t.Fatalf("choices = %q, %v", got, err)
	}
}

func TestAllowAllSkipsPackageSelection(t *testing.T) {
	if os.Getenv("WAGO_TEST_ALLOW_ALL_ADD") == "1" {
		automation.Reset()
		pkgAddMany([]string{"github.com/acme/tools@^1.2.0"}, pkgOpts{global: true, grantAll: true})
		return
	}
	var packageRequests, catalogRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(output http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/packages/github.com/acme/tools":
			packageRequests.Add(1)
			_, _ = output.Write([]byte(`{"module":"github.com/acme/tools","displayName":"Acme Tools","subpackages":[{"module":"github.com/acme/tools/log","name":"Logging"},{"module":"github.com/acme/tools/metrics","name":"Metrics"}]}`))
		case "/api/v1/plugins/candidates":
			catalogRequests.Add(1)
			if request.URL.Query().Get("id") != "github.com/acme/tools" || request.URL.Query().Get("range") != "^1.2.0" {
				t.Errorf("root package or constraint changed: %s", request.URL)
			}
			// Stop before fetching code or building: reaching resolution proves that
			// the package chooser was skipped without process-wide --no-input.
			output.WriteHeader(http.StatusBadRequest)
			_, _ = output.Write([]byte(`{"error":"allow-all catalog reached"}`))
		default:
			t.Errorf("unexpected request: %s", request.URL)
			http.NotFound(output, request)
		}
	}))
	defer server.Close()
	t.Setenv("WAGO_TEST_ALLOW_ALL_ADD", "1")
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_REGISTRY", server.URL)
	for _, name := range []string{"WAGO_NONINTERACTIVE", "WAGO_OFFLINE", "WAGO_JSON", "WAGO_DRY_RUN", "WAGO_LOCKED", "WAGO_BARE", "WAGO_GLOBAL"} {
		t.Setenv(name, "")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAllowAllSkipsPackageSelection$")
	cmd.Dir = t.TempDir()
	cmd.Stdin = strings.NewReader("")
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("add waited for input: %v\n%s", ctx.Err(), output)
	}
	if err == nil || !strings.Contains(string(output), "allow-all catalog reached") || packageRequests.Load() != 0 || catalogRequests.Load() != 1 {
		t.Fatalf("add = %v; package requests=%d, catalog requests=%d\n%s", err, packageRequests.Load(), catalogRequests.Load(), output)
	}
}

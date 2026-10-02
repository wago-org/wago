package plugin

import (
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

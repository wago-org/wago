package config

import (
	"bytes"
	"strings"
	"testing"

	"github.com/wago-org/wago/cli/internal/settings"
)

func TestRootItemsExposeConfigSections(t *testing.T) {
	items := rootItems(settings.Default(), settings.ScopeGlobal)
	if len(items) != 5 || items[0].Value != "features" || items[3].Value != "experimental" || items[4].Value != "reset" {
		t.Fatalf("root items = %#v", items)
	}
}

func TestLocalRootClearsOverrides(t *testing.T) {
	items := rootItems(settings.Default(), settings.ScopeLocal)
	if items[4].Label != "Clear local overrides" || !strings.Contains(items[4].Description, "inherit global") {
		t.Fatalf("local reset item = %#v", items[4])
	}
}

func TestCore3FeaturesAreStableAndThreadsRemainExperimental(t *testing.T) {
	stable := map[string]bool{}
	for _, setting := range settings.Features() {
		stable[setting.Key] = true
	}
	if !stable["features.gc"] || !stable["features.exception-handling"] {
		t.Fatalf("stable features = %v, want WasmGC and exception handling", stable)
	}
	foundThreads, foundDragline := false, false
	for _, setting := range settings.Experimental() {
		if setting.Key == "features.threads" {
			foundThreads = true
		}
		if setting.Key == "experimental.dragline" {
			foundDragline = true
			if setting.Label != "Dragline compiler" || !setting.Experimental || !setting.Available {
				t.Fatalf("Dragline preview = %#v", setting)
			}
		}
	}
	if !foundThreads || !foundDragline {
		t.Fatalf("experimental previews: threads=%v Dragline=%v", foundThreads, foundDragline)
	}
}

func TestPrintIncludesExperimentalSectionOnRequest(t *testing.T) {
	var output bytes.Buffer
	Print(&output, settings.Default(), true, settings.ScopeLocal, "./wago.json", []settings.Override{{Key: "features.simd", Base: "false", Value: "true"}})
	for _, want := range []string{"Wago configuration", "WebAssembly features", "Compiler optimizations", "Experimental preview", "dragline", "threads", "override"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, output.String())
		}
	}
}

func TestPrintDiffNamesInheritanceLayer(t *testing.T) {
	var output bytes.Buffer
	PrintDiff(&output, settings.ScopeLocal, []settings.Override{{Key: "features.simd", Base: "false", Value: "true"}})
	for _, want := range []string{"Wago configuration differences", "features.simd", "global false", "local true"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("diff missing %q:\n%s", want, output.String())
		}
	}
}

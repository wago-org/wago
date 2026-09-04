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

func TestExperimentalPreviewIsGeneratedFromRuntimeFeatures(t *testing.T) {
	catalog := settings.Experimental()
	foundGC, foundDragline := false, false
	for _, setting := range catalog {
		if setting.Key == "features.gc" {
			foundGC = true
			if !setting.Experimental {
				t.Fatal("WasmGC should remain experimental")
			}
		}
		if setting.Key == "experimental.dragline" {
			foundDragline = true
			if setting.Label != "Dragline compiler" || !setting.Experimental || !setting.Available {
				t.Fatalf("Dragline preview = %#v", setting)
			}
		}
	}
	if !foundGC || !foundDragline {
		t.Fatalf("experimental previews: WasmGC=%v Dragline=%v", foundGC, foundDragline)
	}
}

func TestPrintIncludesExperimentalSectionOnRequest(t *testing.T) {
	var output bytes.Buffer
	Print(&output, settings.Default(), true, settings.ScopeLocal, "./wago.json", []settings.Override{{Key: "features.simd", Base: "false", Value: "true"}})
	for _, want := range []string{"Wago configuration", "WebAssembly features", "Compiler optimizations", "Experimental preview", "dragline", "gc", "override"} {
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

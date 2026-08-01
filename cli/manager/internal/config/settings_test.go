package config

import (
	"bytes"
	"strings"
	"testing"

	"github.com/wago-org/wago/cli/internal/settings"
)

func TestRootItemsExposeConfigSections(t *testing.T) {
	items := rootItems(settings.Default())
	if len(items) != 5 || items[0].Value != "features" || items[3].Value != "experimental" || items[4].Value != "reset" {
		t.Fatalf("root items = %#v", items)
	}
}

func TestExperimentalPreviewDisablesUnavailableProposals(t *testing.T) {
	catalog := settings.Experimental()
	found := false
	for _, setting := range catalog {
		if setting.Key == "preview.tail-call" {
			found = true
			if setting.Available {
				t.Fatal("tail calls should be preview-only")
			}
		}
	}
	if !found {
		t.Fatal("tail-call preview is missing")
	}
}

func TestPrintIncludesExperimentalSectionOnRequest(t *testing.T) {
	var output bytes.Buffer
	Print(&output, settings.Default(), true)
	for _, want := range []string{"Wago configuration", "WebAssembly features", "Compiler optimizations", "Experimental preview", "tail-call"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, output.String())
		}
	}
}

package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/wago-org/wago"
)

// TestBuildPluginReport checks that a built-in plugin's enriched config
// (provenance + compatibility) is gathered and JSON-marshalable.
func TestBuildPluginReport(t *testing.T) {
	ext, ok := wago.NewExtension("timer")
	if !ok {
		t.Skip("timer plugin not registered")
	}
	rep := buildPluginReport("timer", ext)
	if rep.Plugin != "timer" || rep.ID != "wago.timer" {
		t.Fatalf("identity: plugin=%q id=%q", rep.Plugin, rep.ID)
	}
	if rep.License != "Apache-2.0" {
		t.Errorf("license = %q, want Apache-2.0", rep.License)
	}
	if rep.Repository == "" || len(rep.Tags) == 0 || len(rep.Authors) == 0 {
		t.Errorf("missing provenance: repo=%q tags=%v authors=%v", rep.Repository, rep.Tags, rep.Authors)
	}
	if rep.Private {
		t.Errorf("official timer plugin should not be private")
	}
	if _, ok := rep.Compat.Engines["tinygo"]; !ok {
		t.Errorf("compat = %+v, want a tinygo engine", rep.Compat)
	}
	if rep.Compat.Engines["wago"] == "" {
		t.Errorf("compat = %+v, want a wago engine constraint", rep.Compat)
	}
	if len(rep.Imports) == 0 || len(rep.Capabilities) == 0 {
		t.Errorf("imports=%d caps=%d, want both non-empty", len(rep.Imports), len(rep.Capabilities))
	}

	b, err := json.Marshal(rep)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{`"compatibility"`, `"engines"`, `"tinygo"`, `"license":"Apache-2.0"`, `"tags"`, `"imports"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("json missing %s\n%s", want, b)
		}
	}
}

func TestEngineTerms(t *testing.T) {
	got := engineTerms(map[string]string{"wago": ">=0.1.0", "tinygo": "*", "go": ""})
	want := "go, tinygo, wago >=0.1.0" // sorted; unconstrained render as bare name
	if strings.Join(got, ", ") != want {
		t.Errorf("engineTerms = %q, want %q", strings.Join(got, ", "), want)
	}
}

func TestHasFlag(t *testing.T) {
	got, rest := hasFlag([]string{"timer", "--json"}, "--json")
	if !got || len(rest) != 1 || rest[0] != "timer" {
		t.Fatalf("hasFlag = %v, rest %v", got, rest)
	}
	if got, _ := hasFlag([]string{"timer"}, "--json"); got {
		t.Fatal("hasFlag found --json when absent")
	}
}

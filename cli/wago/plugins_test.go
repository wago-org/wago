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
	if rep.Repository == "" || len(rep.Keywords) == 0 || len(rep.Authors) == 0 {
		t.Errorf("missing provenance: repo=%q keywords=%v authors=%v", rep.Repository, rep.Keywords, rep.Authors)
	}
	if !rep.Compat.TinyGo || rep.Compat.MinWago == "" {
		t.Errorf("compat = %+v, want tinygo + a MinWago", rep.Compat)
	}
	if len(rep.Imports) == 0 || len(rep.Capabilities) == 0 {
		t.Errorf("imports=%d caps=%d, want both non-empty", len(rep.Imports), len(rep.Capabilities))
	}

	b, err := json.Marshal(rep)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{`"compatibility"`, `"tinygo":true`, `"license":"Apache-2.0"`, `"imports"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("json missing %s\n%s", want, b)
		}
	}
}

func TestVersionRange(t *testing.T) {
	cases := []struct{ min, max, want string }{
		{"0.1.0", "", ">=0.1.0"},
		{"", "2.0.0", "<=2.0.0"},
		{"0.1.0", "2.0.0", ">=0.1.0 <=2.0.0"},
		{"", "", ""},
	}
	for _, c := range cases {
		if got := versionRange(c.min, c.max); got != c.want {
			t.Errorf("versionRange(%q,%q) = %q, want %q", c.min, c.max, got, c.want)
		}
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

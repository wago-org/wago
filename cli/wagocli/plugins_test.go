package wagocli

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/wago"
)

type reportTestExtension struct{}

func (reportTestExtension) Info() wago.ExtensionInfo {
	return wago.ExtensionInfo{ID: "test.report", RequiresCapabilities: []wago.PluginCapability{wago.PluginHostImports}}
}

func (reportTestExtension) Register(reg *wago.Registry) error {
	reg.Capability(wago.CapMetricsWrite)
	reg.ImportModule("env").Func("f", func(wago.HostModule, []uint64, []uint64) {}).
		Params(wago.ValI32).Results(wago.ValI64).Capability(wago.CapMetricsWrite).Docs("counts calls")
	return nil
}

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

func TestBuildPluginReportForRegisteredExtension(t *testing.T) {
	rep := buildPluginReport("test.report", reportTestExtension{})
	if rep.Plugin != "test.report" || rep.ID != "test.report" || !reflect.DeepEqual(rep.RequiresCapabilities, []string{string(wago.PluginHostImports)}) {
		t.Fatalf("report identity = %#v", rep)
	}
	if !reflect.DeepEqual(rep.Capabilities, []string{string(wago.CapMetricsWrite)}) {
		t.Fatalf("capabilities = %v", rep.Capabilities)
	}
	if len(rep.Imports) != 1 || rep.Imports[0].Module != "env" || rep.Imports[0].Name != "f" || rep.Imports[0].Capability != string(wago.CapMetricsWrite) || rep.Imports[0].Docs != "counts calls" {
		t.Fatalf("imports = %#v", rep.Imports)
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

func TestPluginPresentationHelpers(t *testing.T) {
	compat := wago.Compatibility{
		Engines:   map[string]string{"wago": ">=1", "tinygo": "*"},
		Platforms: []string{"linux/amd64", "darwin/arm64"},
	}
	if got := compatSummary(compat); got != "engines: tinygo, wago" {
		t.Fatalf("compatSummary = %q", got)
	}
	if got := compatSummary(wago.Compatibility{}); got != "" {
		t.Fatalf("empty compatSummary = %q", got)
	}
	if got := compatDetail(compat); got != "engines: tinygo, wago >=1 · platforms: linux/amd64, darwin/arm64" {
		t.Fatalf("compatDetail = %q", got)
	}
	if got := compatDetail(wago.Compatibility{}); got != "" {
		t.Fatalf("empty compatDetail = %q", got)
	}
	if got := valTypeStrings([]wago.ValType{wago.ValI32, wago.ValExternRef}); strings.Join(got, ",") != "i32,externref" {
		t.Fatalf("valTypeStrings = %v", got)
	}
	if got := valTypeStrings(nil); got != nil {
		t.Fatalf("empty valTypeStrings = %v", got)
	}
	if got := sigStrings([]string{"i32", "i64"}, []string{"f32"}); got != "(i32, i64) -> f32" {
		t.Fatalf("sigStrings = %q", got)
	}
	if got := sigStrings(nil, nil); got != "()" {
		t.Fatalf("void sigStrings = %q", got)
	}
	if got := sigString([]wago.ValType{wago.ValI32}, []wago.ValType{wago.ValI64}); got != "(i32) -> i64" {
		t.Fatalf("sigString = %q", got)
	}
	if got := sigString(nil, nil); got != "()" {
		t.Fatalf("void sigString = %q", got)
	}
	if got := capString(wago.ImportSpec{Capability: wago.CapFilesystemRead, HasCapability: true}); got != string(wago.CapFilesystemRead) {
		t.Fatalf("capString = %q", got)
	}
	if got := capString(wago.ImportSpec{}); got != "" {
		t.Fatalf("empty capString = %q", got)
	}
}

func TestPluginReviewAndScopeHelpers(t *testing.T) {
	if capabilityDoc("host.imports") == "" || capabilityDoc("unknown") != "" {
		t.Fatal("capability documentation lookup mismatch")
	}
	if !depsContainID([]string{"github.com/acme/plugin", "other"}, "acme/plugin") || depsContainID([]string{"acme/plugin"}, "other") {
		t.Fatal("dependency identity lookup mismatch")
	}
	found, rest := hasFlag([]string{"a", "--json", "b", "--json"}, "--json")
	if !found || strings.Join(rest, ",") != "a,b" {
		t.Fatalf("hasFlag = %v, %v", found, rest)
	}
	for _, tc := range []struct {
		global, local, manifest bool
		want                    bool
		err                     bool
	}{
		{true, false, true, true, false},
		{false, true, false, false, false},
		{false, false, true, false, false},
		{false, false, false, true, false},
		{true, true, false, false, true},
	} {
		got, err := scopeGlobal(tc.global, tc.local, tc.manifest)
		if got != tc.want || (err != nil) != tc.err {
			t.Fatalf("scopeGlobal(%v,%v,%v) = %v,%v", tc.global, tc.local, tc.manifest, got, err)
		}
	}
}

func TestResolveScopeUsesCurrentProjectManifest(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if !resolveScope(false, false) || !resolveScope(true, false) || resolveScope(false, true) {
		t.Fatal("scope resolution without manifest changed")
	}
	if err := os.WriteFile(projectFile, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if resolveScope(false, false) || !resolveScope(true, false) || resolveScope(false, true) {
		t.Fatal("scope resolution with manifest changed")
	}
}

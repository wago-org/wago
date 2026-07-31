package wagocli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/internal/wagopaths"
)

// installFake writes a fake installed binary for ver under d.
func installFake(t *testing.T, d wago.Dirs, ver string) {
	t.Helper()
	dir := filepath.Join(d.Versions, ver)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d.VersionBinary(ver), []byte("#!/bin/true\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestVersionManagerState(t *testing.T) {
	t.Setenv("WAGO_HOME", t.TempDir())
	d := wago.DirsFor("test")

	if got := installedVersions(d); len(got) != 0 {
		t.Fatalf("expected no versions, got %v", got)
	}
	installFake(t, d, "0.3.0")
	installFake(t, d, "0.5.0")
	installFake(t, d, "0.10.0")

	got := installedVersions(d)
	want := []string{"0.3.0", "0.5.0", "0.10.0"} // numeric semver order, not lexical
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("installedVersions = %v, want %v", got, want)
	}

	if activeVersion(d) != "" {
		t.Fatal("expected no active version")
	}
	if err := setActiveVersion(d, "0.5.0"); err != nil {
		t.Fatalf("setActiveVersion: %v", err)
	}
	if activeVersion(d) != "0.5.0" {
		t.Fatalf("activeVersion = %q, want 0.5.0", activeVersion(d))
	}
}

func TestProfileInstallationState(t *testing.T) {
	root := t.TempDir()
	d := wagopaths.Dirs{
		Config: filepath.Join(root, "config"), Data: filepath.Join(root, "data"),
		Versions: filepath.Join(root, "data", "versions"), Cache: filepath.Join(root, "cache"),
	}
	for _, profile := range []wagopaths.Profile{wagopaths.ProfileStandard, wagopaths.ProfileMinimal} {
		path := d.RunnerBinary("canary", string(profile))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("runner"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if got := strings.Join(installedProfiles(d, "canary"), ","); got != "standard/normal,minimal/normal" {
		t.Fatalf("installed profiles = %q", got)
	}
	if err := setActiveInstallation(d, "canary", wagopaths.ProfileMinimal, wagopaths.BuildNormal); err != nil {
		t.Fatal(err)
	}
	path, version, profile, build, ok := activeRunner(d)
	if !ok || version != "canary" || profile != wagopaths.ProfileMinimal || build != wagopaths.BuildNormal || path != d.RunnerBinary("canary", string(profile)) {
		t.Fatalf("active runtime = %q, %q, %q, %q, %v", path, version, profile, build, ok)
	}
}

func TestRuntimeBuildsInstallSideBySide(t *testing.T) {
	root := t.TempDir()
	d := wagopaths.Dirs{
		Config: filepath.Join(root, "config"), Data: filepath.Join(root, "data"),
		Versions: filepath.Join(root, "data", "versions"), Cache: filepath.Join(root, "cache"),
	}
	for _, build := range wagopaths.Builds {
		path := d.RuntimeBinary("canary", string(wagopaths.ProfileStandard), string(build))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(build), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := setActiveInstallation(d, "canary", wagopaths.ProfileStandard, wagopaths.BuildTiny); err != nil {
		t.Fatal(err)
	}
	path, version, profile, build, ok := activeRunner(d)
	if !ok || version != "canary" || profile != wagopaths.ProfileStandard || build != wagopaths.BuildTiny {
		t.Fatalf("active runtime = %q, %q, %q, %q, %v", path, version, profile, build, ok)
	}
	if want := d.RuntimeBinary("canary", "standard", "tiny"); path != want {
		t.Fatalf("active path = %q, want %q", path, want)
	}
}

func TestVersionUseDoesNotShowInstallationLocation(t *testing.T) {
	root := t.TempDir()
	d := wagopaths.Dirs{
		Config: filepath.Join(root, "config"), Data: filepath.Join(root, "data"),
		Versions: filepath.Join(root, "data", "versions"), Cache: filepath.Join(root, "cache"),
	}
	path := d.RunnerBinary("canary", string(wagopaths.ProfileMinimal))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("runner"), 0o755); err != nil {
		t.Fatal(err)
	}

	oldStdout := os.Stdout
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = write
	t.Cleanup(func() { os.Stdout = oldStdout })
	vmUse(d, "canary", wagopaths.ProfileMinimal, wagopaths.BuildNormal)
	_ = write.Close()
	os.Stdout = oldStdout
	output, err := io.ReadAll(read)
	_ = read.Close()
	if err != nil {
		t.Fatal(err)
	}
	if text := string(output); !strings.Contains(text, "Using Wago Canary (minimal/normal)") {
		t.Fatalf("version use output missing result:\n%s", text)
	} else if strings.Contains(text, "location") || strings.Contains(text, path) {
		t.Fatalf("version use unexpectedly showed installation location:\n%s", text)
	}
}

func TestInstallShowsInstallationLocation(t *testing.T) {
	root := t.TempDir()
	d := wagopaths.Dirs{
		Config: filepath.Join(root, "config"), Data: filepath.Join(root, "data"),
		Versions: filepath.Join(root, "data", "versions"), Cache: filepath.Join(root, "cache"),
	}
	path := d.RunnerBinary("v0.2.0", string(wagopaths.ProfileStandard))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("runner"), 0o755); err != nil {
		t.Fatal(err)
	}

	oldStdout := os.Stdout
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = write
	t.Cleanup(func() { os.Stdout = oldStdout })
	installVersion(d, "v0.2.0", wagopaths.ProfileStandard, wagopaths.BuildNormal, false, true)
	_ = write.Close()
	os.Stdout = oldStdout
	output, err := io.ReadAll(read)
	_ = read.Close()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Wago v0.2.0 (standard/normal) is already installed", "location", path} {
		if !strings.Contains(string(output), want) {
			t.Fatalf("version install output missing %q:\n%s", want, output)
		}
	}
}

func TestPromptYesNoDefaultsYesAndAcceptsNo(t *testing.T) {
	for _, tt := range []struct {
		input string
		want  bool
	}{
		{"y\n", true},
		{"YES\n", true},
		{"n\n", false},
		{"\n", true},
		{"unexpected\n", false},
	} {
		var out bytes.Buffer
		if got := promptYesNo(strings.NewReader(tt.input), &out, "Use it?"); got != tt.want {
			t.Fatalf("promptYesNo(%q) = %v, want %v", tt.input, got, tt.want)
		}
		if got := out.String(); got != "Use it? [Y/n] " {
			t.Fatalf("prompt output = %q", got)
		}
	}
}

func TestUseInstalledPickerUsesRadioButtonsAndDefaultsYes(t *testing.T) {
	p := useInstalledPicker("canary", wagopaths.ProfileMinimal, wagopaths.BuildTiny)
	if got := p.selected(); got != "yes" {
		t.Fatalf("default selection = %q, want yes", got)
	}
	frame := p.frame()
	for _, want := range []string{"Use Wago Canary (minimal/tiny) now?", "› ◉ Yes", "○ No", "↑/↓ move · enter/→ select · esc cancel"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("use picker missing %q:\n%s", want, frame)
		}
	}
	if strings.Contains(frame, "browse") {
		t.Fatalf("flat radio picker should not advertise drill-down:\n%s", frame)
	}
}

func TestLegacyGlobalPluginsMigrateToSharedIntent(t *testing.T) {
	root := t.TempDir()
	d := wagopaths.Dirs{
		Config: filepath.Join(root, "config"), Data: filepath.Join(root, "data"),
		Versions: filepath.Join(root, "data", "versions"), Cache: filepath.Join(root, "cache"),
	}
	sourceDir := filepath.Join(d.Versions, "canary-source123", "plugins")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := []byte(`{"$schema":"https://wago.sh/v0/schema.json","plugins":{"wago-org/wasi":"^0.0.0"}}`)
	lock := []byte(`{"plugins":{"wago-org/wasi":{"version":"0.0.0","requiredCapabilities":["host.environment"],"capabilities":["host.environment"]}}}`)
	if err := os.WriteFile(filepath.Join(sourceDir, versionPluginManifest), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "wago-lock.json"), lock, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := migrateLegacyGlobalPlugins(d, "canary-source123"); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string][]byte{versionPluginManifest: manifest, "wago-lock.json": lock} {
		got, err := os.ReadFile(filepath.Join(d.Data, name))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("migrated %s = %q, %v; want %q", name, got, err, want)
		}
	}
	if _, err := os.Stat(filepath.Join(sourceDir, versionPluginManifest)); err != nil {
		t.Fatalf("legacy manifest should remain recoverable: %v", err)
	}
}

func TestLegacyPluginReleaseParsing(t *testing.T) {
	for _, test := range []struct {
		name     string
		output   string
		fallback string
		want     string
	}{
		{name: "diagnostic", output: "Wago\n  channel      canary\n  release      canary-20260729-7d8c58a\n", fallback: "canary", want: "canary-20260729-7d8c58a"},
		{name: "legacy", output: "wago v0.2.0 (darwin/arm64)\n", fallback: "canary", want: "v0.2.0"},
		{name: "fallback", output: "unknown\n", fallback: "nightly", want: "nightly"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := releaseFromVersionOutput([]byte(test.output), test.fallback); got != test.want {
				t.Fatalf("release = %q, want %q", got, test.want)
			}
		})
	}
}

func TestOfferUseUpdatedPromptsEvenWhenChannelIsCurrentAndDefaultsYes(t *testing.T) {
	root := t.TempDir()
	d := wagopaths.Dirs{
		Config: filepath.Join(root, "config"), Data: filepath.Join(root, "data"),
		Versions: filepath.Join(root, "data", "versions"), Cache: filepath.Join(root, "cache"),
	}
	path := d.RuntimeBinary("nightly", "standard", "normal")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := setActiveInstallation(d, "nightly", wagopaths.ProfileStandard, wagopaths.BuildNormal); err != nil {
		t.Fatal(err)
	}

	oldStdin, oldStdout := os.Stdin, os.Stdout
	inputRead, inputWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outputRead, outputWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		os.Stdin, os.Stdout = oldStdin, oldStdout
	})
	if _, err := inputWrite.WriteString("\n"); err != nil {
		t.Fatal(err)
	}
	_ = inputWrite.Close()
	os.Stdin, os.Stdout = inputRead, outputWrite
	offerUseUpdated(d, "nightly", wagopaths.ProfileStandard, wagopaths.BuildNormal)
	_ = outputWrite.Close()
	os.Stdin, os.Stdout = oldStdin, oldStdout
	output, err := io.ReadAll(outputRead)
	if err != nil {
		t.Fatal(err)
	}
	if text := string(output); !strings.Contains(text, "Use Wago Nightly (standard/normal) now? [Y/n]") || !strings.Contains(text, "Using Wago Nightly") {
		t.Fatalf("updated-version prompt/output = %q", text)
	}
}

func TestUpdateChannelPickerDefaultsToCurrentChannel(t *testing.T) {
	p := updateChannelPicker("nightly")
	if got := p.selected(); got != "nightly" {
		t.Fatalf("selected channel = %q, want nightly", got)
	}
	frame := p.frame()
	for _, want := range []string{"Update Wago channel", "Canary", "Nightly", "enter/→ select"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("update picker missing %q:\n%s", want, frame)
		}
	}
	if done, cancelled := p.apply(keyRight); !done || cancelled {
		t.Fatalf("right submit = done %v, cancelled %v", done, cancelled)
	}
}

func TestUninstallVersionPickerListsAllVersionsAndCurrent(t *testing.T) {
	root := t.TempDir()
	d := wagopaths.Dirs{
		Config: filepath.Join(root, "config"), Data: filepath.Join(root, "data"),
		Versions: filepath.Join(root, "data", "versions"), Cache: filepath.Join(root, "cache"),
	}
	for _, version := range []string{"canary", "nightly", "v0.2.0"} {
		path := d.RuntimeBinary(version, "standard", "normal")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("runtime"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := setActiveInstallation(d, "nightly", wagopaths.ProfileStandard, wagopaths.BuildNormal); err != nil {
		t.Fatal(err)
	}
	m := uninstallVersionPicker(d, installedVersions(d))
	if len(m.items) != 3 {
		t.Fatalf("uninstall items = %d, want 3", len(m.items))
	}
	frame := m.frame()
	for _, want := range []string{"Uninstall Wago versions", "canary", "nightly", "v0.2.0", "current", "space toggle", "a all", "enter/→ uninstall"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("uninstall picker missing %q:\n%s", want, frame)
		}
	}
}

func TestInstalledVersionPickerShowsCurrentProfileAndBrowsesProfiles(t *testing.T) {
	root := t.TempDir()
	d := wagopaths.Dirs{
		Config: filepath.Join(root, "config"), Data: filepath.Join(root, "data"),
		Versions: filepath.Join(root, "data", "versions"), Cache: filepath.Join(root, "cache"),
	}
	installProfile := func(version string, profile wagopaths.Profile) {
		t.Helper()
		path := d.RunnerBinary(version, string(profile))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("runner"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	installProfile("canary", wagopaths.ProfileStandard)
	installProfile("nightly-20260725-c18b63d", wagopaths.ProfileMinimal)
	if err := setActiveInstallation(d, "canary", wagopaths.ProfileStandard, wagopaths.BuildNormal); err != nil {
		t.Fatal(err)
	}

	p := installedVersionPicker(d, installedVersions(d))
	frame := p.frame()
	for _, want := range []string{
		"Select installed Wago version",
		"› ◉ canary          (standard/normal) →  current",
		"current",
		"○ nightly-c18b63d (minimal/normal)  →",
		"→ select/browse",
	} {
		if !strings.Contains(frame, want) {
			t.Fatalf("installed picker missing %q:\n%s", want, frame)
		}
	}
	if done, cancelled := p.apply(keyRight); done || cancelled {
		t.Fatalf("right arrow unexpectedly finished picker")
	}
	profiles := p.frame()
	for _, want := range []string{
		"Standard", "Minimal",
		"Everything", "Run only",
		"current", "not installed", "←/esc back",
	} {
		if !strings.Contains(profiles, want) {
			t.Fatalf("profile picker missing %q:\n%s", want, profiles)
		}
	}
	if got, want := p.selected(), installedSelectionValue("canary", wagopaths.ProfileStandard, wagopaths.BuildNormal); got != want {
		t.Fatalf("selected profile = %q, want %q", got, want)
	}
	p.apply(keyDown)
	if got, want := p.selected(), installedSelectionValue("canary", wagopaths.ProfileMinimal, wagopaths.BuildNormal); got != want {
		t.Fatalf("switched profile = %q, want %q", got, want)
	}
}

func TestInstalledWagoLabel(t *testing.T) {
	tests := []struct {
		requested string
		resolved  string
		profile   wagopaths.Profile
		build     wagopaths.Build
		want      string
	}{
		{"canary", "canary-20260728-7d8c58a", wagopaths.ProfileStandard, wagopaths.BuildTiny, "Wago Canary (7d8c58a/standard/tiny)"},
		{"nightly-20260725-c18b63d", "nightly-20260725-c18b63d", wagopaths.ProfileMinimal, wagopaths.BuildNormal, "Wago Nightly (c18b63d/minimal/normal)"},
		{"v0.2.0", "v0.2.0", wagopaths.ProfileStandard, wagopaths.BuildNormal, "Wago v0.2.0 (standard/normal)"},
	}
	for _, tt := range tests {
		if got := installedWagoLabel(tt.requested, tt.resolved, tt.profile, tt.build); got != tt.want {
			t.Fatalf("installedWagoLabel(%q, %q, %q) = %q, want %q", tt.requested, tt.resolved, tt.profile, got, tt.want)
		}
	}
}

func TestCompareSemver(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.3.0", "0.5.0", -1},
		{"0.10.0", "0.9.0", 1},
		{"1.0.0", "1.0.0", 0},
		{"v1.2.0", "1.2.0", 0},
		{"1.2.0", "1.2.1", -1},
	}
	for _, c := range cases {
		if got := compareSemver(c.a, c.b); got != c.want {
			t.Fatalf("compareSemver(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestUpdateVersionTarget(t *testing.T) {
	tests := []struct {
		name            string
		active          string
		args            []string
		nightly, canary bool
		want            string
		wantErr         string
	}{
		{name: "active canary", active: "canary", want: "canary"},
		{name: "named nightly", active: "0.5.0", args: []string{"nightly"}, want: "nightly"},
		{name: "active pinned", active: "0.5.0", wantErr: "is pinned"},
		{name: "named pinned", active: "0.5.0", args: []string{"0.6.0"}, wantErr: "is pinned"},
		{name: "nightly", active: "0.5.0", nightly: true, want: "nightly"},
		{name: "canary", active: "0.5.0", canary: true, want: "canary"},
		{name: "missing active", wantErr: "no active version"},
		{name: "both channels", nightly: true, canary: true, wantErr: "cannot be used together"},
		{name: "channel plus version", args: []string{"0.6.0"}, nightly: true, wantErr: "cannot be used with [version]"},
		{name: "too many versions", args: []string{"0.5.0", "0.6.0"}, wantErr: "at most one"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := updateVersionTarget(tt.active, tt.args, tt.nightly, tt.canary)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("updateVersionTarget() error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("updateVersionTarget() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("updateVersionTarget() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInstallPickerHidesImmutableChannelTagsAtTopLevel(t *testing.T) {
	tags := []string{"nightly-20260712-deadbee", "v0.1.4", "canary-cafef00", "nightly-20260711-abcdef0", "v0.2.0", "0.1.0", "canary"}
	if got, want := stableReleaseNames(tags), []string{"v0.2.0", "v0.1.4", "v0.1.0"}; !slices.Equal(got, want) {
		t.Fatalf("stableReleaseNames = %v, want %v", got, want)
	}
	if got, want := channelReleaseNames(tags, "nightly"), []string{"nightly-20260712-deadbee", "nightly-20260711-abcdef0"}; !slices.Equal(got, want) {
		t.Fatalf("channelReleaseNames = %v, want %v", got, want)
	}
	releases := []remoteRelease{
		{TagName: "nightly-20260728-7d8c58a", PublishedAt: "2026-07-28T08:31:22Z"},
		{TagName: "v0.1.4", PublishedAt: "2026-06-30T12:00:00Z"},
		{TagName: "canary-cafef00", PublishedAt: "2026-07-28T00:48:44Z"},
		{TagName: "nightly-20260711-abcdef0", PublishedAt: "2026-07-11T08:31:22Z"},
		{TagName: "v0.2.0", PublishedAt: "2026-07-28T08:31:22Z"},
		{TagName: "0.1.0", PublishedAt: "2026-06-01T12:00:00Z"},
		{TagName: "canary"},
	}
	commits := []remoteCommit{{SHA: "cafef00123456789012345678901234567890123"}, {SHA: "deadbee123456789012345678901234567890123"}}
	commits[0].Commit.Author.Date = "2026-07-28T00:48:44Z"
	commits[1].Commit.Author.Date = "2026-07-27T00:48:44Z"
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	items := versionPickerItemsWithCommits(releases, commits, now)
	if got, want := []string{items[0].children[0].value, items[0].children[1].value, items[0].children[2].value}, []string{"canary", canaryCommitTarget(commits[0].SHA), canaryCommitTarget(commits[1].SHA)}; !slices.Equal(got, want) {
		t.Fatalf("canary picker children = %v, want %v", got, want)
	}
	if got, want := []string{items[2].children[0].value, items[2].children[1].value, items[2].children[2].value, items[2].children[3].value}, []string{"latest", "v0.2.0", "v0.1.4", "0.1.0"}; !slices.Equal(got, want) {
		t.Fatalf("latest picker children = %v, want %v", got, want)
	}
	nightly := items[1].children[1]
	if nightly.label != "nightly-7d8c58a" || nightly.value != "nightly-20260728-7d8c58a" || nightly.desc != "07/28/2026  1d ago" {
		t.Fatalf("nightly picker item = %#v", nightly)
	}
	canary := items[0].children[1]
	if canary.label != "canary-cafef00" || canary.desc != "07/28/2026  1d ago" {
		t.Fatalf("canary picker item = %#v", canary)
	}
	if got := canaryCommitVersion(canary.value); got != "canary-cafef00" {
		t.Fatalf("canary commit install name = %q", got)
	}
	if got := releasePickerLabel("canary"); got != "canary" {
		t.Fatalf("releasePickerLabel(canary) = %q", got)
	}
}

func TestInstallPickerProfilePageReturnsToReleasePage(t *testing.T) {
	releases := []remoteRelease{
		{TagName: "v0.2.0", PublishedAt: "2026-07-27T08:31:22Z"},
	}
	commits := []remoteCommit{{SHA: "7d8c58a123456789012345678901234567890123"}}
	commits[0].Commit.Author.Date = "2026-07-28T08:31:22Z"
	p := newPicker("Install Wago version", installPickerItemsWithCommits(releases, commits, time.Now()))
	p.apply(keyRight) // browse canary releases
	p.apply(keyDown)  // choose the immutable canary build
	if done, cancelled := p.apply(keyAccept); done || cancelled {
		t.Fatalf("release accept unexpectedly finished picker")
	}
	if frame := p.frame(); !strings.Contains(frame, "Choose Wago profile") || !strings.Contains(frame, "Standard") || !strings.Contains(frame, "←/esc back") {
		t.Fatalf("profile page is incomplete:\n%s", frame)
	}
	if done, cancelled := p.apply(keyLeft); done || cancelled || len(p.pages) != 2 {
		t.Fatalf("left did not return to release page: done %v, cancelled %v, pages %d", done, cancelled, len(p.pages))
	}
	p.apply(keyAccept)
	p.apply(keyDown) // Minimal
	if done, cancelled := p.apply(keyAccept); done || cancelled {
		t.Fatalf("profile accept unexpectedly finished picker")
	}
	if frame := p.frame(); !strings.Contains(frame, "Choose Wago build") || !strings.Contains(frame, "Normal") || !strings.Contains(frame, "Tiny") {
		t.Fatalf("build page is incomplete:\n%s", frame)
	}
	p.apply(keyDown) // Tiny
	if done, cancelled := p.apply(keyAccept); !done || cancelled {
		t.Fatalf("build accept = done %v, cancelled %v", done, cancelled)
	}
	version, profile, build, ok := parseInstalledSelection(p.selected())
	if !ok || version != canaryCommitTarget(commits[0].SHA) || profile != wagopaths.ProfileMinimal || build != wagopaths.BuildTiny {
		t.Fatalf("install selection = %q, %q, %q, %v", version, profile, build, ok)
	}
}

func TestRelativeReleaseAgeUsesCompactElapsedUnits(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		published time.Time
		want      string
	}{
		{now.Add(-18 * time.Minute), "18m ago"},
		{now.Add(-18 * time.Hour), "18h ago"},
		{now.Add(-(6*24*time.Hour + 2*time.Hour)), "6d ago"},
		{now.Add(-(400 * 24 * time.Hour)), "1y ago"},
		{now.Add(-(800 * 24 * time.Hour)), "2y ago"},
		{now.Add(18 * time.Minute), "in 18m"},
		{now.Add(800 * 24 * time.Hour), "in 2y"},
	}
	for _, tt := range tests {
		if got := relativeReleaseAge(tt.published, now); got != tt.want {
			t.Fatalf("relativeReleaseAge(%s) = %q, want %q", tt.published, got, tt.want)
		}
	}
}

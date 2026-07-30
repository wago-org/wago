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
	for _, profile := range []wagopaths.Profile{wagopaths.ProfileLite, wagopaths.ProfileMinimal} {
		path := d.RunnerBinary("canary", string(profile))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("runner"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if got := strings.Join(installedProfiles(d, "canary"), ","); got != "lite/normal,minimal/normal" {
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
		path := d.RuntimeBinary("canary", string(wagopaths.ProfileLite), string(build))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(build), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := setActiveInstallation(d, "canary", wagopaths.ProfileLite, wagopaths.BuildTiny); err != nil {
		t.Fatal(err)
	}
	path, version, profile, build, ok := activeRunner(d)
	if !ok || version != "canary" || profile != wagopaths.ProfileLite || build != wagopaths.BuildTiny {
		t.Fatalf("active runtime = %q, %q, %q, %q, %v", path, version, profile, build, ok)
	}
	if want := d.RuntimeBinary("canary", "lite", "tiny"); path != want {
		t.Fatalf("active path = %q, want %q", path, want)
	}
}

func TestVersionUseDoesNotShowInstallationLocation(t *testing.T) {
	root := t.TempDir()
	d := wagopaths.Dirs{
		Config: filepath.Join(root, "config"), Data: filepath.Join(root, "data"),
		Versions: filepath.Join(root, "data", "versions"), Cache: filepath.Join(root, "cache"),
	}
	path := d.RunnerBinary("canary", string(wagopaths.ProfileLite))
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
	vmUse(d, "canary", wagopaths.ProfileLite, wagopaths.BuildNormal)
	_ = write.Close()
	os.Stdout = oldStdout
	output, err := io.ReadAll(read)
	_ = read.Close()
	if err != nil {
		t.Fatal(err)
	}
	if text := string(output); !strings.Contains(text, "Using Wago Canary (lite/normal)") {
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
	installProfile("canary", wagopaths.ProfileLite)
	installProfile("nightly-20260725-c18b63d", wagopaths.ProfileMinimal)
	if err := setActiveInstallation(d, "canary", wagopaths.ProfileLite, wagopaths.BuildNormal); err != nil {
		t.Fatal(err)
	}

	p := installedVersionPicker(d, installedVersions(d))
	frame := p.frame()
	for _, want := range []string{
		"Select installed Wago version",
		"› ◉ canary          (lite/normal)     →  current",
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
		"Standard", "Lite", "Minimal",
		"Everything", "Run, build, and plugins", "Run only",
		"current", "not installed", "←/esc back",
	} {
		if !strings.Contains(profiles, want) {
			t.Fatalf("profile picker missing %q:\n%s", want, profiles)
		}
	}
	if got, want := p.selected(), installedSelectionValue("canary", wagopaths.ProfileLite, wagopaths.BuildNormal); got != want {
		t.Fatalf("selected profile = %q, want %q", got, want)
	}
	p.apply(keyUp)
	if got, want := p.selected(), installedSelectionValue("canary", wagopaths.ProfileStandard, wagopaths.BuildNormal); got != want {
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
		{"canary", "canary-20260728-7d8c58a", wagopaths.ProfileLite, wagopaths.BuildTiny, "Wago Canary (7d8c58a/lite/tiny)"},
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

func TestRemoteVersionNamesHideImmutableChannelTags(t *testing.T) {
	tags := []string{"nightly-20260712-deadbee", "v0.1.4", "canary-cafef00", "nightly-20260711-abcdef0", "v0.2.0", "0.1.0", "canary"}
	if got, want := remoteVersionNames(tags), []string{"canary", "nightly", "latest", "v0.2.0", "v0.1.4", "v0.1.0"}; !slices.Equal(got, want) {
		t.Fatalf("remoteVersionNames = %v, want %v", got, want)
	}
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
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	items := versionPickerItems(releases, now)
	if got, want := []string{items[0].children[0].value, items[0].children[1].value}, []string{"canary", "canary-cafef00"}; !slices.Equal(got, want) {
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
	if got := releasePickerLabel("canary"); got != "canary" {
		t.Fatalf("releasePickerLabel(canary) = %q", got)
	}
	if got, want := formatRemoteVersionList(tags), " canary\n nightly\n latest\n\n v0.2.0\n v0.1.4\n v0.1.0\n"; got != want {
		t.Fatalf("formatRemoteVersionList = %q, want %q", got, want)
	}
}

func TestInstallPickerProfilePageReturnsToReleasePage(t *testing.T) {
	releases := []remoteRelease{
		{TagName: "canary-20260728-7d8c58a", PublishedAt: "2026-07-28T08:31:22Z"},
		{TagName: "v0.2.0", PublishedAt: "2026-07-27T08:31:22Z"},
	}
	p := newPicker("Install Wago version", installPickerItems(releases, time.Now()))
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
	p.apply(keyDown) // Lite
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
	if !ok || version != "canary-20260728-7d8c58a" || profile != wagopaths.ProfileLite || build != wagopaths.BuildTiny {
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

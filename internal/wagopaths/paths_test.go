package wagopaths

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestProfiles(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  Profile
	}{
		{"standard", ProfileStandard},
		{"LITE", ProfileLite},
		{" minimal ", ProfileMinimal},
	} {
		got, err := ParseProfile(tc.value)
		if err != nil || got != tc.want {
			t.Fatalf("ParseProfile(%q) = %q, %v; want %q", tc.value, got, err, tc.want)
		}
	}
	if _, err := ParseProfile("small"); err == nil {
		t.Fatal("unknown profile accepted")
	}
	if ProfileStandard.Description() != "Everything" ||
		ProfileLite.Description() != "Run, build, and plugins" ||
		ProfileMinimal.Description() != "Run only" {
		t.Fatal("profile descriptions changed")
	}
}

func TestRunnerBinarySeparatesProfiles(t *testing.T) {
	t.Setenv("WAGO_HOME", t.TempDir())
	d := DirsFor("manager")
	name := "wago-runtime"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	got := d.RunnerBinary("canary", string(ProfileLite))
	want := filepath.Join(d.Versions, "canary", "lite", name)
	if got != want {
		t.Fatalf("RunnerBinary = %q, want %q", got, want)
	}
	legacyName := "wago-runner"
	if runtime.GOOS == "windows" {
		legacyName += ".exe"
	}
	if got, want := d.LegacyRunnerBinary("canary", string(ProfileLite)), filepath.Join(d.Versions, "canary", "lite", legacyName); got != want {
		t.Fatalf("LegacyRunnerBinary = %q, want %q", got, want)
	}
}

func TestRuntimeBinarySeparatesProfilesAndBuilds(t *testing.T) {
	d := Dirs{Versions: "/versions"}
	got := d.RuntimeBinary("canary", string(ProfileLite), string(BuildTiny))
	name := "wago-runtime"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	want := filepath.Join("/versions", "canary", "lite", "tiny", name)
	if got != want {
		t.Fatalf("RuntimeBinary = %q, want %q", got, want)
	}
}

func TestParseBuild(t *testing.T) {
	if got, err := ParseBuild("Tiny"); err != nil || got != BuildTiny {
		t.Fatalf("ParseBuild(Tiny) = %q, %v", got, err)
	}
	if _, err := ParseBuild("small"); err == nil {
		t.Fatal("ParseBuild(small) succeeded")
	}
}

func TestDarwinUsesDotWagoRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("WAGO_HOME", "")
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "xdg-data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg-config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "xdg-cache"))

	d := dirsFor("canary", "darwin")
	root := filepath.Join(home, ".wago")
	if d.Data != root ||
		d.Config != filepath.Join(root, "config") ||
		d.Versions != filepath.Join(root, "versions") ||
		d.Cache != filepath.Join(root, "cache", "canary") {
		t.Fatalf("Darwin dirs = %#v, want root %q", d, root)
	}
}

func TestLinuxKeepsXDGLayout(t *testing.T) {
	home := t.TempDir()
	data, config, cache := filepath.Join(home, "data"), filepath.Join(home, "config"), filepath.Join(home, "cache")
	t.Setenv("HOME", home)
	t.Setenv("WAGO_HOME", "")
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("XDG_CACHE_HOME", cache)

	d := dirsFor("canary", "linux")
	if d.Data != filepath.Join(data, "wago") ||
		d.Config != filepath.Join(config, "wago") ||
		d.Versions != filepath.Join(data, "wago", "versions") ||
		d.Cache != filepath.Join(cache, "wago", "canary") {
		t.Fatalf("Linux dirs = %#v", d)
	}
}

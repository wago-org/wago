package settings

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/project"
)

func TestBuildProfiles(t *testing.T) {
	for _, test := range []struct {
		core string
		want wago.CoreFeatures
	}{
		{"1.0", wago.CoreFeaturesV1},
		{"2.0", wago.CoreFeaturesV2},
		{"3.0", wago.CoreFeaturesV3 | relaxedSIMD},
	} {
		t.Run(test.core, func(t *testing.T) {
			profile := &BuildProfile{Core: test.core}
			mask, err := profile.FeaturesMask()
			if err != nil || mask != test.want {
				t.Fatalf("mask = %#x, %v, want %#x", mask, err, test.want)
			}
			tags, err := profile.Tags()
			if err != nil || slices.Contains(tags, "wago_no_relaxed_simd") != (test.core != "3.0") ||
				slices.Contains(tags, "wago_no_simd") != (test.core == "1.0") {
				t.Fatalf("tags = %q, %v", tags, err)
			}
		})
	}
	profile := &BuildProfile{Core: "1.0", Features: map[string]bool{"gc": true, "relaxed-simd": true}}
	mask, err := profile.FeaturesMask()
	want := wago.CoreFeaturesV1 | wago.CoreFeatureGC | wago.CoreFeatureTypedFunctionReferences |
		wago.CoreFeatureReferenceTypes | wago.CoreFeatureSIMD | relaxedSIMD
	if err != nil || mask != want {
		t.Fatalf("mask = %#x, %v, want %#x", mask, err, want)
	}
	profile.Features["reference-types"] = false
	if _, err := profile.Tags(); err == nil || !strings.Contains(err.Error(), "reference-types") {
		t.Fatalf("missing dependency error = %v", err)
	}
}

func TestBuildProfileScope(t *testing.T) {
	dir := enterSettingsTestDir(t)
	t.Setenv("WAGO_CONFIG", filepath.Join(t.TempDir(), "settings.json"))
	global := Default()
	if err := Set(&global, "build.core", "2.0", false); err != nil {
		t.Fatal(err)
	}
	if err := Set(&global, "build.features.relaxed-simd", "on", false); err != nil {
		t.Fatal(err)
	}
	if err := Save(global); err != nil {
		t.Fatal(err)
	}
	writeTestManifest(t, dir)

	target, err := Open(false, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := target.Set("build.core", "1.0", false); err != nil {
		t.Fatal(err)
	}
	if err := target.Set("build.features.relaxed-simd", "off", false); err != nil {
		t.Fatal(err)
	}
	if err := target.Save(); err != nil {
		t.Fatal(err)
	}
	manifest, err := project.Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	tags, err := BuildTagsForManifest(manifest)
	if err != nil || !slices.Contains(tags, "wago_no_simd") {
		t.Fatalf("project tags = %q, %v", tags, err)
	}
	target, err = Open(false, true)
	if err != nil {
		t.Fatal(err)
	}
	target.ResetAll()
	if err := target.Save(); err != nil {
		t.Fatal(err)
	}
	manifest, err = project.Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := manifest[localField]; exists {
		t.Fatalf("reset left empty local settings: %#v", manifest)
	}
	tags, err = BuildTagsForManifest(manifest)
	if err != nil || slices.Contains(tags, "wago_no_simd") || slices.Contains(tags, "wago_no_relaxed_simd") {
		t.Fatalf("inherited tags = %q, %v", tags, err)
	}
}

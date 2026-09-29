//go:build wago_profile && (linux || darwin)

package profilecmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/internal/profcapture"
	"github.com/wago-org/wago/profile"
)

func TestAssemblyUsesRelocatedSymbolsAndSampledRegion(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "arguments")
	t.Setenv("WAGO_ANNOTATE_ARGS", log)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := os.WriteFile(filepath.Join(dir, "perf"), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" >> \"$WAGO_ANNOTATE_ARGS\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "perf.jit.data"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	image := &wago.CodeProfileImage{ModuleID: "module", ArtifactID: "artifact", ID: 1, Size: 32,
		Regions: []wago.CodeProfileRegion{
			{Offset: 0, Size: 8, Kind: "entry-adapter", Function: 0, Name: "loop"},
			{Offset: 8, Size: 24, Kind: "guest-body", Function: 0, Name: "loop"},
		},
		Functions: []wago.CodeProfileFunction{{Index: 0, Name: "loop"}},
	}
	c := capture{
		manifest: profcapture.Manifest{Backend: "perf", JITSymbolRoot: "symbols"},
		events:   []wago.CodeProfileEvent{{Kind: "load", Image: image}},
		report: profile.Report{Rows: []profile.Row{{ModuleID: "module", ArtifactID: "artifact", Function: 0,
			PCs: []profile.HotPC{{Offset: 10, Samples: 1}},
		}}},
	}
	if err := annotate(dir, c, "loop", true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(strings.TrimSpace(string(data)), "\n")
	want := []string{"annotate", "--stdio", "-i", filepath.Join(dir, "perf.jit.data"), "--symbol", profile.Symbol(*image, image.Regions[1]), "--symfs", filepath.Join(dir, "symbols")}
	if strings.Join(args, "\n") != strings.Join(want, "\n") {
		t.Fatalf("expected only sampled body with relocated symbols: %q", args)
	}
}

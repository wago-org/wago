//go:build wago_profile

package profcapture

import (
	"encoding/json"
	"github.com/wago-org/wago"
	"github.com/wago-org/wago/profile"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func fixtureModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("identity", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0xb}))),
	)
}
func TestCapturePhasesAndResultOracle(t *testing.T) {
	for _, valid := range []bool{true, false} {
		t.Run(map[bool]string{true: "valid", false: "bad-result"}[valid], func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "capture")
			expected := uint64(37)
			if !valid {
				expected = 12
			}
			w := Workload{ID: "test", Calls: []Call{{Export: "identity", Args: []uint64{37}, Want: []uint64{expected}}}}
			o := Options{Out: dir, Backend: "none", Phase: "execute", Mode: "public", Iterations: 5, Warmup: 1, Bounds: "explicit", Rate: 499}
			err := Run(o, w, fixtureModule())
			if (err == nil) != valid {
				t.Fatal(err)
			}
			b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
			if err != nil {
				t.Fatal(err)
			}
			var m Manifest
			if err = json.Unmarshal(b, &m); err != nil {
				t.Fatal(err)
			}
			if m.Complete != valid {
				t.Fatal(m)
			}
			if valid && (m.Iterations != 5 || m.Invocations != 5 || len(m.Phases) != 6) {
				t.Fatal(m)
			}
			if valid {
				for _, p := range m.Phases {
					switch p.Name {
					case "execute":
						if p.Completed != 5 || p.WorkUnit != "workload iteration" {
							t.Fatal(p)
						}
					case "compile", "instantiate", "close":
						if p.Completed != 1 || p.WorkUnit == "" {
							t.Fatal(p)
						}
					case "initialize":
						if p.Completed != 0 {
							t.Fatal("invented initialization", p)
						}
					}
				}
			}
			if m.Status.Dropped != 0 {
				t.Fatal(m.Status)
			}
		})
	}
}
func TestCaptureConfigurationFailsClosed(t *testing.T) {
	o := Options{Out: "x", Backend: "none", Phase: "execute", Mode: "public", Iterations: 1, Bounds: "explicit", Rate: 499}
	if err := o.Validate(); err != nil {
		t.Fatal(err)
	}
	o.Phase = "execut"
	if err := o.Validate(); err == nil {
		t.Fatal("unknown phase accepted")
	}
	o.Phase = "execute"
	o.Duration = -1
	if err := o.Validate(); err == nil {
		t.Fatal("negative duration accepted")
	}
	if _, _, err := LoadWorkload("", "", "x", "f", "", "", ""); err == nil {
		t.Fatal("missing oracle accepted")
	}
}

func TestRawStackCaptureRequiresExplicitConfiguration(t *testing.T) {
	base := Options{Out: "x", Backend: "perf", Phase: "execute", Mode: "public", Iterations: 1, Bounds: "explicit", Rate: 99, IncludeCode: true, UnwindMaps: true}
	for _, size := range []int{-1, 1, 255, 257, 65529, 65536} {
		o := base
		o.RawStackBytes = size
		if err := o.Validate(); err == nil || !strings.Contains(err.Error(), "eight-byte multiple") {
			t.Fatalf("invalid byte limit %d: %v", size, err)
		}
	}
	for _, size := range []int{256, 8192, 65528} {
		o := base
		o.RawStackBytes = size
		err := o.Validate()
		if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
			if err == nil || !strings.Contains(err.Error(), "Linux/amd64") {
				t.Fatalf("unsupported platform accepted: %v", err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, change := range []func(*Options){
			func(o *Options) { o.Backend = "none" },
			func(o *Options) { o.IncludeCode = false },
			func(o *Options) { o.UnwindMaps = false },
			func(o *Options) { o.ReloadArtifact = true },
			func(o *Options) { o.Phase = "reload" },
		} {
			invalid := o
			change(&invalid)
			if err := invalid.Validate(); err == nil {
				t.Fatalf("accepted invalid raw stack options: %+v", invalid)
			}
		}
	}
	// Metadata alone must not opt an ordinary capture into stack memory.
	if err := base.Validate(); err != nil || base.RawStackBytes != 0 {
		t.Fatal(base, err)
	}
}

func TestDirectGuestTimelineCaptureAndLoss(t *testing.T) {
	for _, mode := range []string{"public", "prepared"} {
		for _, limit := range []int{1, 10} {
			dir := filepath.Join(t.TempDir(), "capture")
			workload := Workload{ID: "identity", Calls: []Call{{Export: "identity", Args: []uint64{37}, Want: []uint64{37}}}}
			options := Options{Out: dir, Backend: "none", Phase: "execute", Mode: mode, Iterations: 2, Warmup: 1, Bounds: "explicit", Rate: 499, Timeline: true, MaxSpans: limit}
			err := Run(options, workload, fixtureModule())
			if (err == nil) != (limit == 10) {
				t.Fatalf("mode %s limit %d: %v", mode, limit, err)
			}
			b, err := os.ReadFile(filepath.Join(dir, "timeline.json"))
			if err != nil {
				t.Fatal(err)
			}
			var report profile.TimelineReport
			if err = json.Unmarshal(b, &report); err != nil {
				t.Fatal(err)
			}
			if limit == 10 {
				if !report.Complete || len(report.Rows) != 6 {
					t.Fatal(report)
				}
				for _, row := range report.Rows {
					if row.InclusiveNS == nil || row.ExclusiveNS == nil {
						t.Fatal(row)
					}
				}
			} else {
				if report.Complete || report.Dropped != 5 || len(report.Rows) != 1 || report.Rows[0].ExclusiveNS != nil {
					t.Fatal(report)
				}
			}
		}
	}
}

func TestArtifactReloadCapture(t *testing.T) {
	for _, phase := range []string{"reload", "execute", "all"} {
		for _, mode := range []string{"public", "prepared"} {
			t.Run(phase+"/"+mode, func(t *testing.T) {
				out := filepath.Join(t.TempDir(), "capture")
				o := Options{Out: out, Backend: "none", Phase: phase, Mode: mode, Iterations: 3, Warmup: 1, Bounds: "explicit", Rate: 99, ReloadArtifact: phase != "reload", IncludeCode: true, SourceMaps: true, UnwindMaps: true}
				w := Workload{ID: "identity", Calls: []Call{{Export: "identity", Args: []uint64{37}, Want: []uint64{37}}}}
				if err := Run(o, w, fixtureModule()); err != nil {
					t.Fatal(err)
				}
				raw, err := os.ReadFile(filepath.Join(out, "manifest.json"))
				if err != nil {
					t.Fatal(err)
				}
				var m Manifest
				if err := json.Unmarshal(raw, &m); err != nil {
					t.Fatal(err)
				}
				if !m.Complete || !m.ReloadArtifact || len(m.ArtifactHash) != 64 || m.ArtifactBytes == 0 || m.Iterations != 3 || m.Invocations != 3 || m.SourceMaps || m.CompilerSites || m.SiteCoverage != "" || m.UnwindMaps || !m.UnwindMapsRequested || m.UnwindCoverage != "unavailable" || m.Status.Dropped != 0 {
					t.Fatalf("invalid reload capture: %+v", m)
				}
				want := []string{"compile", "artifact-prepare", "reload", "instantiate", "initialize", "warmup", "execute", "close"}
				if len(m.Phases) != len(want) {
					t.Fatal(m.Phases)
				}
				for i, p := range m.Phases {
					if p.Name != want[i] || p.End < p.Start || i > 0 && p.Start < m.Phases[i-1].End {
						t.Fatal(m.Phases)
					}
				}
				raw, err = os.ReadFile(filepath.Join(out, "images.json"))
				if err != nil {
					t.Fatal(err)
				}
				var events []wago.CodeProfileEvent
				if err := json.Unmarshal(raw, &events); err != nil {
					t.Fatal(err)
				}
				if len(events) != 2 || events[0].Kind != "load" || events[1].Kind != "retire" || events[0].ImageID != events[1].ImageID {
					t.Fatal(events)
				}
				image := events[0].Image
				if image == nil || image.ModuleID != "" || len(image.Regions) != 1 || image.Regions[0].Kind != "unknown" || len(image.Code) == 0 {
					t.Fatalf("invented metadata or lost artifact image: %+v", image)
				}
			})
		}
	}
}

func TestCaptureUnwindCapabilityIsSeparateFromStacks(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		out := filepath.Join(t.TempDir(), "capture")
		opts := Options{Out: out, Backend: "none", Phase: "execute", Mode: "public", Iterations: 2, Bounds: "explicit", Rate: 99, UnwindMaps: enabled}
		workload := Workload{ID: "identity", Calls: []Call{{Export: "identity", Args: []uint64{37}, Want: []uint64{37}}}}
		if err := Run(opts, workload, fixtureModule()); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(filepath.Join(out, "manifest.json"))
		if err != nil {
			t.Fatal(err)
		}
		var manifest Manifest
		if err := json.Unmarshal(raw, &manifest); err != nil {
			t.Fatal(err)
		}
		wantMaps := enabled && runtime.GOARCH == "amd64"
		wantCoverage := ""
		if enabled {
			wantCoverage = "unsupported"
			if wantMaps {
				wantCoverage = "amd64-fixed-frames-and-adapters"
			}
		}
		if !manifest.Complete || manifest.GuestStacks || manifest.UnwindMaps != wantMaps || manifest.UnwindMapsRequested != enabled || manifest.UnwindCoverage != wantCoverage {
			t.Fatal(manifest)
		}
		raw, err = os.ReadFile(filepath.Join(out, "images.json"))
		if err != nil {
			t.Fatal(err)
		}
		var events []wago.CodeProfileEvent
		if err := json.Unmarshal(raw, &events); err != nil {
			t.Fatal(err)
		}
		if len(events) != 2 || events[0].Image == nil || (len(events[0].Image.Unwind) > 0) != wantMaps || events[0].Image.UnwindCoverage != wantCoverage {
			t.Fatal(events)
		}
	}
}

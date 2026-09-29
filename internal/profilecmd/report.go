package profilecmd

import (
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/internal/jitprofile"
	"github.com/wago-org/wago/internal/profcapture"
	"github.com/wago-org/wago/profile"
)

type capture struct {
	manifest profcapture.Manifest
	events   []wago.CodeProfileEvent
	report   profile.Report
}

func readJSON(path string, v any) error {
	return profile.ReadJSONFile(path, v, profile.DefaultLimits().FileBytes)
}
func loadCapture(dir string) (capture, error) {
	var c capture
	if err := readJSON(filepath.Join(dir, "manifest.json"), &c.manifest); err != nil {
		return c, err
	}
	if c.manifest.Version != 1 {
		return c, fmt.Errorf("unsupported capture schema")
	}
	if c.manifest.JITSymbolRoot != "" && c.manifest.JITSymbolRoot != "symbols" {
		return c, fmt.Errorf("unsupported JIT symbol root")
	}
	if c.manifest.CollectorPending {
		return c, fmt.Errorf("capture is incomplete: capture finalization is pending")
	}
	if !c.manifest.Complete || c.manifest.Status.Dropped != 0 {
		return c, fmt.Errorf("capture is incomplete: %v", c.manifest.Diagnostics)
	}
	limits := profile.DefaultLimits()
	images, ir, err := profile.OpenInput(filepath.Join(dir, "images.json"), limits.FileBytes)
	if err != nil {
		return c, err
	}
	c.events, err = profile.ReadEventsJSON(ir, limits)
	images.Close()
	if err != nil {
		return c, err
	}
	for _, event := range c.events {
		if im := event.Image; im != nil {
			if err := jitprofile.ValidateRegions(im.Regions, im.Size); err != nil {
				return c, err
			}
			if err := jitprofile.ValidateCodeSites(im.CodeSites, im.Size); err != nil {
				return c, err
			}
			if err := jitprofile.ValidateCodeSiteRegions(im.CodeSites, im.Regions); err != nil {
				return c, err
			}
		}
	}
	if c.manifest.Backend == "samply" {
		f, compressed, err := profile.OpenInput(filepath.Join(dir, "samply.json.gz"), limits.FileBytes)
		if err != nil {
			return c, err
		}
		defer f.Close()
		gz, err := gzip.NewReader(compressed)
		if err != nil {
			return c, err
		}
		defer gz.Close()
		c.report, err = profile.ReadSamply(gz, c.events)
		return c, err
	}
	if c.manifest.Backend != "perf" {
		return c, nil
	}
	if c.manifest.Status.Clock != "monotonic" || c.manifest.Event != "cpu-clock:u" {
		return c, fmt.Errorf("unsupported sample clock/event")
	}
	var samples []profile.Sample
	sf, sr, err := profile.OpenInput(filepath.Join(dir, "samples.json"), limits.FileBytes)
	if err == nil {
		samples, err = profile.ReadSamplesJSON(sr, limits)
		sf.Close()
	}
	if os.IsNotExist(err) {
		if err := profile.CheckInput(filepath.Join(dir, "perf.data")); err != nil {
			return c, err
		}
		samples, err = profcapture.ReadPerfSamples(filepath.Join(dir, "perf.data"), limits.Samples)
	}
	if err != nil {
		return c, err
	}
	c.report, err = profile.Resolve(c.events, samples, "nanoseconds")
	return c, err
}
func report(command string, args []string) error {
	f, cfg := reportFlags(command)
	function, assembly, limit, asJSON := cfg.function, cfg.assembly, cfg.limit, cfg.asJSON
	if err := f.Parse(args); err != nil {
		return err
	}
	if *limit < 0 {
		return fmt.Errorf("limit must be nonnegative")
	}
	count := 1
	if command == "diff" {
		count = 2
	}
	if f.NArg() != count {
		return fmt.Errorf("usage: wagoprof %s [flags] capture-directory", command)
	}
	c, err := loadCapture(f.Arg(0))
	if err != nil {
		return err
	}
	if command == "diff" {
		d, err := loadCapture(f.Arg(1))
		if err != nil {
			return err
		}
		return diff(c, d, *asJSON)
	}
	if command == "annotate" {
		if *asJSON {
			if *assembly {
				return fmt.Errorf("--assembly cannot be combined with --json")
			}
			rows, err := annotationRows(c, *function)
			if err != nil {
				return err
			}
			return json.NewEncoder(os.Stdout).Encode(struct {
				Unit        string               `json:"weight_unit,omitempty"`
				GuestStacks bool                 `json:"qualified_guest_stacks"`
				Functions   []functionAnnotation `json:"functions"`
			}{c.report.Unit, c.manifest.GuestStacks, rows})
		}
		return annotate(f.Arg(0), c, *function, *assembly)
	}
	top, err := makeTop(c, *limit)
	if err != nil {
		return err
	}
	return writeTop(os.Stdout, top, *asJSON)
}

func annotate(dir string, c capture, selected string, assembly bool) error {
	if selected == "" {
		return fmt.Errorf("annotate requires --function")
	}
	matched := false
	for _, e := range c.events {
		if e.Image == nil {
			continue
		}
		im := e.Image
		for _, fn := range im.Functions {
			if strconv.Itoa(fn.Index) != selected && fn.Name != selected {
				continue
			}
			matched = true
			b, _ := json.MarshalIndent(fn, "", "  ")
			fmt.Printf("Compiler output for %s / f%d (static counts):\n%s\n", im.ModuleID, fn.Index, b)
			for _, r := range c.report.Rows {
				if r.ModuleID == im.ModuleID && r.ArtifactID == im.ArtifactID && r.Function == fn.Index {
					for _, pc := range r.PCs {
						fmt.Printf("+0x%06x  %7d samples  %12d CPU ns", pc.Offset, pc.Samples, pc.Weight)
						if pc.CompilerSite != nil {
							fmt.Printf("  emitted %s", pc.CompilerSite.Kind)
						}
						if pc.Source != nil {
							fmt.Printf("  Wasm f%d", pc.Source.Function)
							if pc.SourceName != "" {
								fmt.Printf(" (%s)", pc.SourceName)
							}
							fmt.Printf(" +0x%x (compiler-recorded Wasm origin)", pc.Source.WasmOffset)
						}
						for _, caller := range pc.InlineCallers {
							fmt.Printf(" <- inlined at f%d", caller.Function)
							if caller.Name != "" {
								fmt.Printf(" (%s)", caller.Name)
							}
							fmt.Printf(" +0x%x", caller.WasmOffset)
						}
						if pc.Offset < uint64(len(im.Code)) {
							end := min(pc.Offset+8, uint64(len(im.Code)))
							fmt.Printf("  bytes %x", im.Code[pc.Offset:end])
						}
						fmt.Println()
					}
				}
			}
			if assembly {
				if c.manifest.Backend != "perf" {
					return fmt.Errorf("native disassembly requires a perf capture")
				}
				for _, region := range im.Regions {
					if region.Function != fn.Index || region.Kind == "padding" {
						continue
					}
					sampled := false
					for _, row := range c.report.Rows {
						if row.ModuleID != im.ModuleID || row.ArtifactID != im.ArtifactID || row.Function != fn.Index {
							continue
						}
						for _, pc := range row.PCs {
							if pc.Samples > 0 && pc.Offset >= region.Offset && pc.Offset-region.Offset < region.Size {
								sampled = true
								break
							}
						}
					}
					if !sampled {
						// perf annotate prints an alarming "data has no samples"
						// error for an unsampled adapter even when the body is hot.
						// Self attribution is the contract of this report.
						continue
					}
					args := []string{"annotate", "--stdio", "-i", filepath.Join(dir, "perf.jit.data"), "--symbol", profile.Symbol(*im, region)}
					if c.manifest.JITSymbolRoot == "symbols" {
						root, err := filepath.Abs(filepath.Join(dir, "symbols"))
						if err != nil {
							return err
						}
						args = append(args, "--symfs", root)
					}
					if err := profile.CheckInput(filepath.Join(dir, "perf.jit.data")); err != nil {
						return err
					}
					if err := profcapture.PerfAnnotate(args); err != nil {
						return err
					}
				}
			}
		}
	}
	if !matched {
		return fmt.Errorf("no function %q", selected)
	}
	if c.manifest.SourceMaps {
		fmt.Printf("Sparse Wasm origins: %s. Unmapped PCs remain unknown.\n", c.manifest.SourceCoverage)
	} else {
		fmt.Println("No Wasm instruction/source map is present.")
	}
	fmt.Println("Guest call chains are not qualified for this capture.")
	return nil
}

func timelineReport(args []string) error {
	f, jsonOutput := timelineFlags()
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 1 {
		return fmt.Errorf("usage: wagoprof timeline [--json] capture-directory")
	}
	var manifest profcapture.Manifest
	if err := readJSON(filepath.Join(f.Arg(0), "manifest.json"), &manifest); err != nil {
		return err
	}
	if manifest.Version != 1 || manifest.TimelineCoverage == "" {
		return fmt.Errorf("capture has no supported boundary timeline")
	}
	var timeline profile.TimelineReport
	if err := readJSON(filepath.Join(f.Arg(0), "timeline.json"), &timeline); err != nil {
		return err
	}
	if len(timeline.Rows) > profile.DefaultLimits().Events {
		return fmt.Errorf("timeline record limit exceeded")
	}
	if *jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(timeline)
	}
	fmt.Printf("Elapsed boundary time (not CPU time). Coverage: %s\n", manifest.TimelineCoverage)
	fmt.Println("ID  PARENT  INSTANCE  KIND  INCLUSIVE_NS  EXCLUSIVE_NS  OUTCOME")
	for _, row := range timeline.Rows {
		inclusive, exclusive := "unknown", "unknown"
		if row.InclusiveNS != nil {
			inclusive = strconv.FormatUint(*row.InclusiveNS, 10)
		}
		if row.ExclusiveNS != nil {
			exclusive = strconv.FormatUint(*row.ExclusiveNS, 10)
		}
		fmt.Printf("%d  %d  %d  %s  %s  %s  %s\n", row.ID, row.ParentID, row.InstanceID, row.Kind, inclusive, exclusive, row.Outcome)
	}
	for _, diagnostic := range timeline.Diagnostics {
		fmt.Fprintln(os.Stderr, diagnostic)
	}
	if !timeline.Complete {
		return fmt.Errorf("boundary timeline incomplete")
	}
	return nil
}

type reportConfig struct {
	function *string
	assembly *bool
	limit    *int
	asJSON   *bool
}

func reportFlags(command string) (*flag.FlagSet, *reportConfig) {
	cfg := &reportConfig{}
	f := flag.NewFlagSet(command, flag.ContinueOnError)
	cfg.function = f.String("function", "", "full Wasm function index or exact display name")
	cfg.assembly = f.Bool("assembly", false, "use perf annotate for native disassembly")
	cfg.limit = f.Int("limit", 20, "maximum displayed top rows; zero shows all")
	cfg.asJSON = f.Bool("json", false, "print machine-readable report")
	return f, cfg
}

func timelineFlags() (*flag.FlagSet, *bool) {
	f := flag.NewFlagSet("timeline", flag.ContinueOnError)
	jsonOutput := f.Bool("json", false, "emit elapsed boundary report as JSON")
	return f, jsonOutput
}

type functionAnnotation struct {
	ModuleID   string                   `json:"module_id"`
	ArtifactID string                   `json:"artifact_id"`
	Function   wago.CodeProfileFunction `json:"compiler"`
	PCs        []profile.HotPC          `json:"native_pcs"`
	Sites      []wago.CodeProfileSite   `json:"emitted_sites,omitempty"`
}

func annotationRows(c capture, selected string) ([]functionAnnotation, error) {
	if selected == "" {
		return nil, fmt.Errorf("annotate requires --function")
	}
	type identity struct {
		module, artifact string
		function         int
	}
	hot := map[identity][]profile.HotPC{}
	for _, row := range c.report.Rows {
		hot[identity{row.ModuleID, row.ArtifactID, row.Function}] = row.PCs
	}
	seen := map[identity]bool{}
	var rows []functionAnnotation
	for _, event := range c.events {
		if event.Image == nil {
			continue
		}
		im := event.Image
		for _, fn := range im.Functions {
			if strconv.Itoa(fn.Index) != selected && fn.Name != selected {
				continue
			}
			key := identity{im.ModuleID, im.ArtifactID, fn.Index}
			if seen[key] {
				continue
			}
			seen[key] = true
			var sites []wago.CodeProfileSite
			regionAt := 0
			for _, site := range im.CodeSites {
				for regionAt < len(im.Regions) && im.Regions[regionAt].Offset+im.Regions[regionAt].Size <= site.Offset {
					regionAt++
				}
				if regionAt < len(im.Regions) && im.Regions[regionAt].Function == fn.Index {
					sites = append(sites, site)
				}
			}
			rows = append(rows, functionAnnotation{ModuleID: im.ModuleID, ArtifactID: im.ArtifactID, Function: fn, PCs: hot[key], Sites: sites})
		}
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("no function %q", selected)
	}
	return rows, nil
}

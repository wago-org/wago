package profilecmd

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/wago-org/wago/internal/profcapture"
	"github.com/wago-org/wago/profile"
)

type topReport struct {
	SiteSamples  []siteSamples `json:"site_samples,omitempty"`
	ElapsedScope string        `json:"elapsed_scope"`
	profile.Report
	Phase      string              `json:"capture_phase"`
	WorkUnit   string              `json:"work_unit,omitempty"`
	Completed  uint64              `json:"completed_work"`
	ElapsedNS  int64               `json:"phase_elapsed_ns"`
	Phases     []profcapture.Phase `json:"phases"`
	StaticOnly bool                `json:"static_only"`
	CPUPerWork bool                `json:"cpu_per_work_available"`
}

// Old manifests have no phase work counts. Only execution has a safe fallback;
// startup and teardown costs must never depend on subsequent execution counts.
func makeTop(c capture, limit int) (topReport, error) {
	m := c.manifest
	if m.Backend == "samply" {
		m.Phase = "all"
	}
	r := topReport{Report: c.report, Phase: m.Phase, Phases: m.Phases, ElapsedScope: "selected phase"}
	if m.Phase == "all" {
		r.ElapsedScope = "sum of recorded phases; excludes unassigned overhead"
	}
	if m.Phase == "execute" {
		r.WorkUnit, r.Completed, r.ElapsedNS = "workload iteration", m.Iterations, m.ActualNS
	}
	for _, p := range m.Phases {
		if m.Phase == "all" {
			r.ElapsedNS += p.Elapsed
		}
		if p.Name == m.Phase {
			r.ElapsedNS = p.Elapsed
			if p.WorkUnit != "" {
				r.WorkUnit, r.Completed = p.WorkUnit, p.Completed
			}
		}
	}
	r.CPUPerWork = m.Backend == "perf" && r.Unit == "nanoseconds" && m.PhaseIsolation == "collector-start-stop" && r.Completed > 0 && r.WorkUnit != ""
	r.StaticOnly = m.Backend != "perf" && m.Backend != "samply"
	if r.StaticOnly {
		seen := make(map[string]bool)
		for _, e := range c.events {
			if e.Image != nil {
				for _, fn := range e.Image.Functions {
					key := fmt.Sprintf("%s/%s/%d", e.Image.ModuleID, e.Image.ArtifactID, fn.Index)
					if seen[key] {
						continue
					}
					if len(r.Rows) >= profile.DefaultLimits().Rows {
						return topReport{}, fmt.Errorf("static report row limit exceeded")
					}
					seen[key] = true
					f := fn
					r.Rows = append(r.Rows, profile.Row{ModuleID: e.Image.ModuleID, ArtifactID: e.Image.ArtifactID, Function: fn.Index, Name: fn.Name, Kind: "function", Static: &f})
				}
			}
		}
	}
	r.Rows = append([]profile.Row(nil), r.Rows...)
	if r.CPUPerWork {
		for i := range r.Rows {
			v := float64(r.Rows[i].Weight) / float64(r.Completed)
			r.Rows[i].CPUPerWork = &v
		}
	}
	if limit > 0 && len(r.Rows) > limit {
		r.Rows = r.Rows[:limit]
	}
	return r, nil
}
func writeTop(w io.Writer, r topReport, asJSON bool) error {
	if asJSON {
		return json.NewEncoder(w).Encode(r)
	}
	if _, err := fmt.Fprintf(w, "Capture phase: %s; %.3f ms elapsed (%s)\n", r.Phase, float64(r.ElapsedNS)/1e6, r.ElapsedScope); err != nil {
		return err
	}
	if r.WorkUnit != "" {
		if _, err := fmt.Fprintf(w, "Completed %s: %d\n", r.WorkUnit, r.Completed); err != nil {
			return err
		}
	}
	if r.Phase == "all" {
		for _, p := range r.Phases {
			if _, err := fmt.Fprintf(w, "  %s: %.3f ms elapsed; %d %s\n", p.Name, float64(p.Elapsed)/1e6, p.Completed, p.WorkUnit); err != nil {
				return err
			}
		}
	}
	if r.StaticOnly {
		if _, err := fmt.Fprintln(w, "No native samples available. Static compiler output follows; these are not measured hotspots."); err != nil {
			return err
		}
	} else if r.Unit == "observations" {
		if _, err := fmt.Fprintf(w, "%d observations; %d unmapped/non-guest. May include off-CPU observations; all phases were collected.\n", r.Samples, r.UnknownSamples); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintf(w, "%d native samples; %d unknown/non-guest samples. Flat self attribution only. Total CPU: %.3f ms; unknown/non-guest CPU: %.3f ms.\n", r.Samples, r.UnknownSamples, float64(r.Weight)/1e6, float64(r.UnknownWeight)/1e6); err != nil {
			return err
		}
		if _, err := fmt.Fprint(w, "samples     CPU ms"); err != nil {
			return err
		}
		if r.CPUPerWork {
			if _, err := fmt.Fprintf(w, "   CPU ns/%s", r.WorkUnit); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w, "   function / native bytes / static spills / reloads / checks"); err != nil {
			return err
		}
	}
	for _, row := range r.Rows {
		if !r.StaticOnly {
			if _, err := fmt.Fprintf(w, "%7d", row.Samples); err != nil {
				return err
			}
			if r.Unit == "nanoseconds" {
				if _, err := fmt.Fprintf(w, " %10.3f", float64(row.Weight)/1e6); err != nil {
					return err
				}
			}
			if r.CPUPerWork {
				if _, err := fmt.Fprintf(w, " %18.1f", float64(row.Weight)/float64(r.Completed)); err != nil {
					return err
				}
			}
		}
		if _, err := fmt.Fprintf(w, "   f%-5d %-28s", row.Function, row.Name); err != nil {
			return err
		}
		if f := row.Static; f != nil {
			if _, err := fmt.Fprintf(w, " %d bytes / %d frame bytes / %d static spills / %d reloads / %d checks", f.NativeBytes, f.FrameBytes, f.Spills, f.Reloads, f.BoundsChecks); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	if len(r.SiteSamples) != 0 {
		observation := "samples"
		if r.Unit == "observations" {
			observation = "observations"
		}
		if _, err := fmt.Fprintf(w, "Native %s at compiler sites (all functions; not dynamic operation counts):\n", observation); err != nil {
			return err
		}
		for _, site := range r.SiteSamples {
			name := site.Category
			if site.Kind != "" {
				name = site.Kind
			}
			if _, err := fmt.Fprintf(w, "  %7d %s  %12d %s  %s\n", site.Samples, observation, site.Weight, r.Unit, name); err != nil {
				return err
			}
		}
	}
	return nil
}

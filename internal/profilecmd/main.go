// wagoprof records reproducible Wasm workloads and exports native JIT metadata.
package profilecmd

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/wago-org/wago/internal/profcapture"
)

func Run(args []string, prefix []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: wagoprof record|top|annotate|diff|timeline [options]")
	}
	switch args[0] {
	case "record", "capture":
		return record(args[0], args[1:], prefix)
	case "timeline":
		return timelineReport(args[1:])
	case "top", "annotate", "diff":
		return report(args[0], args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}
func record(command string, args []string, prefix []string) error {
	f, cfg := recordFlags(command)
	if err := f.Parse(args); err != nil {
		return err
	}
	o := cfg.options
	o.CommandPrefix = append([]string(nil), prefix...)
	explicitDuration := false
	f.Visit(func(v *flag.Flag) {
		if v.Name == "duration" {
			explicitDuration = true
		}
	})
	if o.Iterations > 0 && !explicitDuration {
		o.Duration = 0
	}
	if f.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	if err := o.Validate(); err != nil {
		return err
	}
	w, b, err := profcapture.LoadWorkload(*cfg.catalog, *cfg.workload, *cfg.module, *cfg.export, *cfg.init, *cfg.argv, *cfg.want)
	if err != nil {
		return err
	}
	if o.Backend == "perf" && command == "record" {
		err = profcapture.RecordPerf(o, args)
	} else if o.Backend == "samply" && command == "record" {
		err = profcapture.RecordSamply(o, args)
	} else {
		err = profcapture.Run(o, w, b)
	}
	if err == nil && command == "record" {
		_, err = fmt.Fprintf(os.Stdout, "Capture saved to %s\n", o.Out)
	}
	return err
}

type recordConfig struct {
	options                                             profcapture.Options
	catalog, workload, module, export, init, argv, want *string
}

func recordFlags(command string) (*flag.FlagSet, *recordConfig) {
	f := flag.NewFlagSet(command, flag.ContinueOnError)
	cfg := &recordConfig{}
	o := &cfg.options
	f.StringVar(&o.Out, "out", "", "new local capture directory")
	f.StringVar(&o.Samply, "samply", "samply", "Samply executable for the samply backend")
	f.StringVar(&o.Backend, "backend", "none", "none, pprof (Go CPU only), perf, perf-map, or samply")
	f.StringVar(&o.Phase, "phase", "execute", "all, compile, reload, instantiate, initialize, execute, close")
	f.StringVar(&o.Mode, "mode", "public", "public (named Invoke) or prepared (resolved WasmFunc)")
	f.DurationVar(&o.Duration, "duration", 15*time.Second, "recording workload duration; use 0 with --iterations")
	f.Uint64Var(&o.Iterations, "iterations", 0, "fixed complete workload iterations")
	f.Uint64Var(&o.Warmup, "warmup", 5, "warmup iterations before capture")
	f.BoolVar(&o.Timeline, "timeline", false, "record elapsed synchronous native boundaries and host re-entry across all phases")
	f.IntVar(&o.MaxSpans, "max-spans", 65536, "maximum retained boundary spans")
	f.BoolVar(&o.UnwindMaps, "unwind-maps", false, "retain experimental AMD64 fixed-frame recovery metadata; does not record stack samples")
	f.IntVar(&o.RawStackBytes, "stack-bytes", 0, "opt in to raw stack memory: 256..65528 bytes per perf sample, multiple of 8 (Linux/amd64); requires unwind-maps and include-code; 0 disables")
	f.StringVar(&o.JITDir, "jit-dir", "", "internal perf JIT discovery directory")
	f.BoolVar(&o.SourceMaps, "source-maps", false, "retain Wasm lowering origins, static inline ancestry, and emitted compiler sites")
	f.BoolVar(&o.ReloadArtifact, "reload-artifact", false, "execute a freshly serialized and reloaded artifact; implied by --phase=reload")
	f.BoolVar(&o.IncludeCode, "include-code", false, "include native bytes; required for perf jitdump")
	f.StringVar(&o.Bounds, "bounds", "explicit", "explicit or signals")
	f.IntVar(&o.Rate, "rate", 99, "requested native samples/second; pprof uses Go's 100 Hz")
	f.StringVar(&o.Control, "control", "", "internal perf control FIFO")
	f.StringVar(&o.Ack, "ack", "", "internal perf acknowledgement FIFO")
	cfg.catalog = f.String("catalog", defaultCatalog(), "shared corpus catalog")
	cfg.workload = f.String("workload", "", "corpus benchmark preset, e.g. json-as")
	cfg.module = f.String("module", "", "arbitrary Wasm module (default import environment: env.abort)")
	cfg.export = f.String("export", "", "export to invoke")
	cfg.init = f.String("init", "", "optional initialization export")
	cfg.argv = f.String("args", "", "comma-separated uint64 ABI slots")
	cfg.want = f.String("want", "", "exact expected result slots; [] for void")
	return f, cfg
}

func defaultCatalog() string {
	for _, path := range []string{filepath.Join("corpus", "catalog.json"), filepath.Join("..", "corpus", "catalog.json")} {
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			return path
		}
	}
	return filepath.Join("corpus", "catalog.json")
}

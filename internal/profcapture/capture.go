package profcapture

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"slices"
	"time"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/profile"
)

type Options struct {
	Supervised        bool // internal child/parent completion handshake
	CollectionTimeout time.Duration
	ConversionTimeout time.Duration
	// CommandPrefix routes collector subprocesses through the current CLI.
	CommandPrefix []string
	Samply        string
	Out           string
	Backend       string
	Phase         string
	Mode          string
	Duration      time.Duration
	Iterations    uint64
	Warmup        uint64
	IncludeCode   bool
	SourceMaps    bool
	UnwindMaps    bool
	RawStackBytes int
	// JITDir is assigned by the perf parent for its private stack-capture files.
	JITDir         string
	ReloadArtifact bool
	Timeline       bool
	MaxSpans       int
	Bounds         string
	Rate           int
	Control        string
	Ack            string
}
type Phase struct {
	Completed uint64 `json:"completed_work"`
	WorkUnit  string `json:"work_unit,omitempty"`
	Name      string `json:"name"`
	Start     int64  `json:"start_ns"`
	End       int64  `json:"end_ns"`
	Elapsed   int64  `json:"elapsed_ns"`
}
type Manifest struct {
	ParentSupervised     bool                   `json:"parent_supervised,omitempty"`
	CollectionTimeoutNS  int64                  `json:"collection_timeout_ns"`
	ConversionTimeoutNS  int64                  `json:"conversion_timeout_ns"`
	CompilerSites        bool                   `json:"native_compiler_sites"`
	SiteCoverage         string                 `json:"compiler_site_coverage,omitempty"`
	RawStackBytes        int                    `json:"raw_stack_bytes_limit"`
	StackCollection      string                 `json:"stack_collection,omitempty"`
	JITSymbolRoot        string                 `json:"jit_symbol_root,omitempty"`
	UnwindMapsRequested  bool                   `json:"unwind_maps_requested"`
	UnwindMaps           bool                   `json:"native_unwind_maps"`
	UnwindCoverage       string                 `json:"unwind_map_coverage,omitempty"`
	ReloadArtifact       bool                   `json:"artifact_reloaded"`
	ArtifactHash         string                 `json:"artifact_sha256,omitempty"`
	ArtifactBytes        int                    `json:"artifact_bytes,omitempty"`
	SourceMapsRequested  bool                   `json:"source_maps_requested"`
	SourceCoverage       string                 `json:"source_map_coverage,omitempty"`
	TimelineCoverage     string                 `json:"timeline_coverage,omitempty"`
	CPUModel             string                 `json:"cpu_model"`
	OSVersion            string                 `json:"os_version"`
	RateAccounting       string                 `json:"sampling_rate_accounting"`
	Version              int                    `json:"version"`
	Complete             bool                   `json:"complete"`
	CollectorPending     bool                   `json:"collector_pending,omitempty"`
	Diagnostics          []string               `json:"diagnostics,omitempty"`
	Revision             string                 `json:"wago_revision"`
	Dirty                string                 `json:"wago_dirty"`
	GoVersion            string                 `json:"go_version"`
	Target               string                 `json:"target"`
	CPUs                 int                    `json:"logical_cpus"`
	Workload             string                 `json:"workload"`
	ModuleHash           string                 `json:"module_sha256"`
	WorkloadHash         string                 `json:"workload_contract_sha256"`
	SemanticChecks       []string               `json:"semantic_checks,omitempty"`
	SemanticReturnChecks []string               `json:"semantic_return_oracle_checks,omitempty"`
	SemanticInputWrites  bool                   `json:"semantic_input_writes_in_execute,omitempty"`
	MemoryValidation     bool                   `json:"memory_oracle_checks_in_execute,omitempty"`
	Backend              string                 `json:"backend"`
	CollectorVersion     string                 `json:"collector_version"`
	Event                string                 `json:"event"`
	RequestedRate        int                    `json:"requested_rate_hz"`
	Phase                string                 `json:"capture_phase"`
	PhaseIsolation       string                 `json:"phase_isolation"`
	Mode                 string                 `json:"invocation_mode"`
	RequestedNS          int64                  `json:"requested_duration_ns"`
	ActualNS             int64                  `json:"execution_duration_ns"`
	Iterations           uint64                 `json:"completed_iterations"`
	Invocations          uint64                 `json:"completed_invocations"`
	Warmup               uint64                 `json:"warmup_iterations"`
	Phases               []Phase                `json:"phases"`
	Config               map[string]any         `json:"effective_configuration"`
	CodeIncluded         bool                   `json:"native_code_included"`
	GuestStacks          bool                   `json:"qualified_guest_stacks"`
	InlineSources        bool                   `json:"static_inline_ancestry"`
	SourceMaps           bool                   `json:"wasm_instruction_maps"`
	Status               wago.CodeProfileStatus `json:"metadata_status"`
}

func (o Options) Validate() error {
	if o.CollectionTimeout < 0 || o.ConversionTimeout < 0 {
		return fmt.Errorf("safety timeouts must be positive (zero uses defaults)")
	}
	if err := o.validateStackCapture(); err != nil {
		return err
	}
	if o.MaxSpans < 0 {
		return fmt.Errorf("max-spans must be non-negative")
	}
	if o.Out == "" {
		return fmt.Errorf("--out is required")
	}
	if o.Duration < 0 || o.Iterations == 0 && o.Duration <= 0 {
		return fmt.Errorf("choose a positive duration or iteration count")
	}
	if o.Iterations != 0 && o.Duration != 0 {
		return fmt.Errorf("choose duration or iterations, not both")
	}
	if o.Rate < 1 || o.Rate > 10000 {
		return fmt.Errorf("sampling rate must be 1..10000 Hz")
	}
	switch o.Backend {
	case "none", "pprof", "perf", "perf-map", "samply":
	default:
		return fmt.Errorf("unsupported backend %q", o.Backend)
	}
	switch o.Phase {
	case "all", "compile", "reload", "instantiate", "initialize", "execute", "close":
	default:
		return fmt.Errorf("unsupported phase %q", o.Phase)
	}
	if o.Mode != "public" && o.Mode != "prepared" {
		return fmt.Errorf("mode must be public or prepared")
	}
	if o.Bounds != "explicit" && o.Bounds != "signals" {
		return fmt.Errorf("bounds must be explicit or signals")
	}
	if o.Backend == "perf" && !o.IncludeCode {
		return fmt.Errorf("perf jitdump requires --include-code")
	}
	return nil
}

func (o Options) validateStackCapture() error {
	if o.RawStackBytes == 0 {
		return nil
	}
	if o.RawStackBytes < 256 || o.RawStackBytes > 65528 || o.RawStackBytes%8 != 0 {
		return fmt.Errorf("stack-bytes must be 0 or an eight-byte multiple from 256 through 65528")
	}
	if o.Backend != "perf" || runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return fmt.Errorf("raw stack capture currently requires the perf backend on Linux/amd64")
	}
	if !o.UnwindMaps || !o.IncludeCode {
		return fmt.Errorf("raw stack capture requires --unwind-maps and --include-code")
	}
	if o.ReloadArtifact || o.Phase == "reload" {
		return fmt.Errorf("raw guest stack capture requires fresh compilation; reloaded artifacts have no unwind maps")
	}
	return nil
}
func writeJSON(path string, v any) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	e := json.NewEncoder(f)
	e.SetIndent("", "  ")
	return errors.Join(e.Encode(v), f.Close())
}

// Run creates a self-contained capture in a new private directory. Any workload,
// collector or exporter error produces a failed manifest and a non-nil error.
func Run(o Options, w Workload, wasm []byte) (result error) {
	if err := o.Validate(); err != nil {
		return err
	}
	if o.RawStackBytes != 0 && o.JITDir == "" {
		return fmt.Errorf("raw stack capture must be started through the record command")
	}
	if err := os.Mkdir(o.Out, 0700); err != nil {
		return err
	}
	m := Manifest{Version: 1, UnwindMapsRequested: o.UnwindMaps, SourceMapsRequested: o.SourceMaps, Revision: "unknown", Dirty: "unknown", GoVersion: runtime.Version(), Target: runtime.GOOS + "/" + runtime.GOARCH, CPUs: runtime.NumCPU(), Workload: w.ID, ModuleHash: w.Hash, Backend: o.Backend, Phase: o.Phase, PhaseIsolation: "collector-start-stop", Mode: o.Mode, RequestedNS: int64(o.Duration), Warmup: o.Warmup, CodeIncluded: o.IncludeCode, RequestedRate: o.Rate}
	m.CollectionTimeoutNS, m.ConversionTimeoutNS = int64(o.collectionTimeout()), int64(o.conversionTimeout())
	m.ParentSupervised = o.Supervised
	m.ReloadArtifact = o.ReloadArtifact || o.Phase == "reload"
	m.RawStackBytes = o.RawStackBytes
	if o.RawStackBytes != 0 {
		m.StackCollection = "perf-dwarf"
		m.Diagnostics = append(m.Diagnostics, "raw stack memory is included in perf.data; bounded reads and unsupported unwind transitions may truncate call chains; complete guest stacks are not established and built-in reports remain flat")
	}
	m.WorkloadHash = workloadHash(w)
	for _, check := range w.semantic {
		m.SemanticChecks = append(m.SemanticChecks, check.ID)
		if len(check.Expect.Return) != 0 {
			m.SemanticReturnChecks = append(m.SemanticReturnChecks, check.ID)
		}
		m.SemanticInputWrites = m.SemanticInputWrites || check.Invoke.Input != "" || check.Invoke.Vectors != nil
		m.MemoryValidation = m.MemoryValidation || len(check.Expect.Memory) > 0 || check.Invoke.Vectors != nil
	}
	m.CPUModel, m.OSVersion = hostIdentity()
	m.RateAccounting = "requested rate only; raw collector samples retain actual observations"
	if b, ok := debug.ReadBuildInfo(); ok {
		for _, s := range b.Settings {
			switch s.Key {
			case "vcs.revision":
				m.Revision = s.Value
			case "vcs.modified":
				m.Dirty = s.Value
			}
		}
	}
	if BuildRevision != "" {
		m.Revision = BuildRevision
		m.Dirty = BuildDirty
	}
	if m.Revision == "unknown" {
		m.Diagnostics = append(m.Diagnostics, "build revision unavailable; build with scripts/build-profiler.sh for reproducible provenance")
	}
	if o.Timeline {
		m.TimelineCoverage = "named-resolved-typed-and-reserved-export-invocations; synchronous-native-boundaries-and-host-reentry; all workload phases; instance-instantiation-initialization-logical-close-and-physical-release"
	}
	session := wago.NewCodeProfile(wago.CodeProfileOptions{IncludeCode: o.IncludeCode, SourceMaps: o.SourceMaps, UnwindMaps: o.UnwindMaps, TraceBoundaries: o.Timeline, TraceLifecycle: o.Timeline, MaxSpans: o.MaxSpans})
	defer session.Close()
	var compiled *wago.Compiled
	var instance *wago.Instance
	var jit *profile.JITFile
	var perfMap *profile.PerfMap
	var mapFile *os.File
	var cpu *os.File
	var cursor uint64
	recording := false
	drain := func() error {
		events, status := session.Read(cursor)
		if status.Dropped != 0 {
			return fmt.Errorf("code metadata lost %d records", status.Dropped)
		}
		if len(events) > 0 {
			cursor = events[len(events)-1].Sequence
		}
		if jit != nil {
			return jit.Write(events)
		}
		if perfMap != nil {
			return perfMap.Write(events)
		}
		return nil
	}
	start := func() error {
		if recording {
			return nil
		}
		switch o.Backend {
		case "pprof":
			var err error
			cpu, err = os.OpenFile(filepath.Join(o.Out, "cpu.pprof"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				return err
			}
			if err = pprof.StartCPUProfile(cpu); err != nil {
				return err
			}
		case "perf":
			if err := control(o.Control, o.Ack, "enable"); err != nil {
				return err
			}
			recording = true
			if jit != nil {
				if err := jit.RefreshMarker(); err != nil {
					return err
				}
			}
		}
		recording = true
		return nil
	}
	stop := func() error {
		if !recording {
			return nil
		}
		recording = false
		switch o.Backend {
		case "pprof":
			pprof.StopCPUProfile()
			err := cpu.Close()
			cpu = nil
			return err
		case "perf":
			return control(o.Control, o.Ack, "disable")
		}
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			result = errors.Join(result, fmt.Errorf("capture panic: %v", r))
		}
		result = errors.Join(result, stop())
		if instance != nil {
			result = errors.Join(result, instance.Close())
		}
		if compiled != nil {
			result = errors.Join(result, compiled.Close())
		}
		result = errors.Join(result, drain())
		if jit != nil {
			result = errors.Join(result, jit.Close())
		}
		if mapFile != nil {
			result = errors.Join(result, mapFile.Close())
		}
		if cpu != nil {
			result = errors.Join(result, cpu.Close())
		}
		events, status := session.Read(0)
		if perfMap != nil {
			f, err := os.OpenFile(filepath.Join(o.Out, "symbols.map"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err == nil {
				err = errors.Join(profile.NewPerfMap(f).Write(events), f.Close())
			}
			result = errors.Join(result, err)
		}
		if o.UnwindMaps {
			m.UnwindCoverage = "unavailable"
		}
		for _, event := range events {
			if event.Image != nil && event.Image.SiteCoverage != "" {
				m.SiteCoverage = event.Image.SiteCoverage
				m.CompilerSites = m.CompilerSites || len(event.Image.CodeSites) != 0
			}
			if event.Image != nil && event.Image.UnwindCoverage != "" {
				m.UnwindCoverage = event.Image.UnwindCoverage
				m.UnwindMaps = m.UnwindMaps || len(event.Image.Unwind) > 0
			}
			if event.Image != nil && len(event.Image.Sources) > 0 {
				m.SourceMaps = true
				for _, r := range event.Image.Sources {
					if r.InlineParent != 0 {
						m.InlineSources = true
						break
					}
				}
				m.SourceCoverage = event.Image.SourceCoverage
			}
		}
		m.Status = status
		if status.Dropped > 0 {
			result = errors.Join(result, fmt.Errorf("metadata loss: %d", status.Dropped))
		}
		result = errors.Join(result, writeJSON(filepath.Join(o.Out, "images.json"), events))
		if o.Timeline {
			spans := session.Spans()
			result = errors.Join(result, writeJSON(filepath.Join(o.Out, "boundaries.json"), spans))
			timeline, err := profile.AnalyzeTimeline(spans, status)
			result = errors.Join(result, err)
			if err == nil {
				result = errors.Join(result, writeJSON(filepath.Join(o.Out, "timeline.json"), timeline))
				if !timeline.Complete {
					result = errors.Join(result, fmt.Errorf("incomplete boundary timeline: %v", timeline.Diagnostics))
				}
			}
		}
		m.Complete = result == nil
		// The external collector still has to exit, retain its raw data, and
		// finish conversion. Only the parent may publish overall completion.
		m.CollectorPending = m.Complete && (o.Supervised || o.Backend == "perf" || o.Backend == "samply")
		if m.CollectorPending {
			m.Complete = false
		}
		if result != nil {
			m.Diagnostics = append(m.Diagnostics, result.Error())
		}
		result = errors.Join(result, writeJSON(filepath.Join(o.Out, "manifest.json"), m))
	}()
	switch o.Backend {
	case "perf":
		var err error
		jitDir := o.Out
		if o.RawStackBytes != 0 {
			jitDir = o.JITDir
		}
		jit, err = profile.OpenJITDump(jitDir)
		if err != nil {
			return err
		}
		m.Event = "cpu-clock:u"
		m.CollectorVersion = collectorVersion()
	case "perf-map", "samply":
		m.Event = "external collector unspecified"
		m.CollectorVersion = "external"
		m.PhaseIsolation = "external-collector-uncontrolled"
		if o.Backend == "samply" {
			m.Event = "samply observations (may include off-CPU)"
			m.CollectorVersion = commandVersion(o.Samply, "--version")
		}
		m.Diagnostics = append(m.Diagnostics, "perf-map has no collector handshake or address-reuse support")
		tempDir := os.TempDir()
		if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
			tempDir = "/tmp"
		}
		path := filepath.Join(tempDir, fmt.Sprintf("perf-%d.map", os.Getpid()))
		var err error
		mapFile, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		perfMap = profile.NewPerfMap(mapFile)
	case "pprof":
		m.Event = "Go CPU samples"
		m.RequestedRate = 100
		m.CollectorVersion = runtime.Version()
		m.Diagnostics = append(m.Diagnostics, "Go CPU profiling does not provide qualified guest PC attribution or guest call chains")
	case "none":
		m.Event = "none"
		m.PhaseIsolation = "no-sampling"
		m.CollectorVersion = "none"
	}
	cfg := wago.NewRuntimeConfig().WithCodeProfile(session)
	if o.Bounds == "signals" {
		cfg = cfg.WithBoundsChecks(wago.BoundsChecksSignalsBased)
	} else {
		cfg = cfg.WithBoundsChecks(wago.BoundsChecksExplicit)
	}
	m.Config = map[string]any{"bounds": o.Bounds, "features": cfg.CoreFeatures(), "native_stack_bytes": cfg.NativeStackBytes(), "optimizations": cfg.OptimizationInfos(), "result_validation": "every invocation", "import_environment": "env.abort traps; all other imports must be supplied by module"}
	if len(w.semantic) != 0 {
		m.Config["result_validation"] = "catalog return and memory oracles after every invocation"
		m.Config["invocation_count_scope"] = "workload calls; semantic pointer queries excluded"
	}
	var warmupCompleted uint64
	phase := func(name string, fn func() error) error {
		capture := o.Phase == name
		if capture {
			if err := start(); err != nil {
				return err
			}
		}
		started := time.Now()
		p := Phase{Name: name, Start: started.UnixNano()}
		before := m.Iterations
		err := fn()
		ended := time.Now()
		p.End = ended.UnixNano()
		p.Elapsed = int64(ended.Sub(started))
		switch name {
		case "execute":
			p.WorkUnit, p.Completed = "workload iteration", m.Iterations-before
		case "warmup":
			p.WorkUnit, p.Completed = "workload iteration", warmupCompleted
		case "artifact-prepare":
			p.WorkUnit = "artifact preparation"
		case "compile":
			p.WorkUnit = "compilation"
		case "reload":
			p.WorkUnit = "reload"
		case "instantiate":
			p.WorkUnit = "instantiation"
		case "initialize":
			p.WorkUnit = "initialization"
		case "close":
			p.WorkUnit = "teardown"
		}
		if p.WorkUnit != "" && name != "execute" && name != "warmup" && err == nil && (name != "initialize" || w.Init != "" || len(w.semantic) > 0) {
			p.Completed = 1
		}
		m.Phases = append(m.Phases, p)
		if name == "execute" {
			m.ActualNS = p.Elapsed
		}
		if capture {
			err = errors.Join(err, stop())
		}
		return errors.Join(err, drain())
	}
	if o.Phase == "all" {
		if err := start(); err != nil {
			return err
		}
	}
	compileConfig := cfg
	if m.ReloadArtifact {
		// Artifacts do not serialize diagnostic metadata. Do not expose the
		// preparatory compiler's metadata as if it belonged to the loaded image.
		compileConfig = cfg.WithCodeProfile(nil)
	}
	if err := phase("compile", func() error { var err error; compiled, err = wago.Compile(compileConfig, wasm); return err }); err != nil {
		return err
	}
	if m.ReloadArtifact {
		var artifact []byte
		if err := phase("artifact-prepare", func() error {
			var err error
			artifact, err = compiled.MarshalBinary()
			if err != nil {
				return err
			}
			sum := sha256.Sum256(artifact)
			m.ArtifactHash = hex.EncodeToString(sum[:])
			m.ArtifactBytes = len(artifact)
			err = compiled.Close()
			compiled = nil
			return err
		}); err != nil {
			return err
		}
		if err := phase("reload", func() error {
			var err error
			// Only bytes produced by this run are admitted to the trusted loader.
			compiled, err = wago.LoadTrustedArtifact(artifact)
			return err
		}); err != nil {
			return err
		}
		artifact = nil
		if err := compiled.AttachCodeProfile(session); err != nil {
			return err
		}
		m.Diagnostics = append(m.Diagnostics, "reloaded artifact has no serialized compiler/source metadata; its native body is attributed as unknown")
	}
	imports := wago.NewImports()
	imports.HostFunc("env", "abort", func(wago.HostCall) { panic("guest env.abort") })
	if err := phase("instantiate", func() error {
		var err error
		instance, err = wago.Instantiate(compiled, wago.InstantiateOptions{Imports: imports})
		return err
	}); err != nil {
		return err
	}
	var calls []checkedCall
	if err := phase("initialize", func() error {
		if w.Init != "" {
			_, err := instance.Invoke(w.Init)
			if err != nil {
				return err
			}
		}
		var err error
		calls, err = prepareCheckedCalls(instance, w)
		return err
	}); err != nil {
		return err
	}
	prepared := make([]*wago.WasmFunc, len(calls))
	if o.Mode == "prepared" {
		for i, c := range calls {
			var err error
			prepared[i], err = instance.WasmFunc(c.Export)
			if err != nil {
				return err
			}
		}
	}
	run := func(count bool) error {
		for i := range calls {
			c := &calls[i]
			if err := c.initialize(instance); err != nil {
				return err
			}
			var out []uint64
			var err error
			if o.Mode == "prepared" {
				out, err = prepared[i].Invoke(c.Args...)
			} else {
				out, err = instance.Invoke(c.Export, c.Args...)
			}
			if err != nil {
				return fmt.Errorf("%s: %w", c.Export, err)
			}
			if c.returnOracle && !slices.Equal(out, c.Want) {
				return fmt.Errorf("%s: result validation failed", c.Export)
			}
			if err := c.validateMemory(instance); err != nil {
				return err
			}
			if count {
				m.Invocations++
			}
		}
		return nil
	}
	if err := phase("warmup", func() error {
		for i := uint64(0); i < o.Warmup; i++ {
			if err := run(false); err != nil {
				return err
			}
			warmupCompleted++
		}
		return nil
	}); err != nil {
		return err
	}
	if err := phase("execute", func() error {
		deadline := time.Now().Add(o.Duration)
		for (o.Iterations != 0 && m.Iterations < o.Iterations) || (o.Iterations == 0 && time.Now().Before(deadline)) {
			if err := run(true); err != nil {
				return err
			}
			m.Iterations++
		}
		return nil
	}); err != nil {
		return err
	}
	if err := phase("close", func() error {
		err := errors.Join(instance.Close(), compiled.Close())
		instance = nil
		compiled = nil
		return err
	}); err != nil {
		return err
	}
	return nil
}

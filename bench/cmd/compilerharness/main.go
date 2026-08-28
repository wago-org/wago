// Command compilerharness measures external same-Wasm compilers using a strict
// JSON command manifest. Engines run in alternating order each round and every
// raw result records process wall and CPU time, peak RSS, and artifact bytes.
package main

import (
	"bytes"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/wago-org/wago/src/wago"
)

const (
	configVersion = 2
	reportVersion = 3
)

type config struct {
	Version uint32         `json:"version"`
	Engines []engineConfig `json:"engines"`
}

type engineConfig struct {
	Name        string   `json:"name"`
	Command     string   `json:"command,omitempty"`
	Builtin     string   `json:"builtin,omitempty"`
	Target      string   `json:"target,omitempty"`
	Workers     int      `json:"workers,omitempty"`
	Args        []string `json:"args,omitempty"`
	VersionArgs []string `json:"version_args"`
	Required    bool     `json:"required,omitempty"`
}

type resolvedEngine struct {
	name             string
	command          string
	args             []string
	executableSHA256 string
	builtin          bool
}

type report struct {
	Version    uint32         `json:"version"`
	GOOS       string         `json:"goos"`
	GOARCH     string         `json:"goarch"`
	WasmPath   string         `json:"wasm_path"`
	WasmSHA256 [32]byte       `json:"wasm_sha256"`
	Rounds     int            `json:"rounds"`
	Engines    []engineReport `json:"engines"`
	Runs       []runReport    `json:"runs"`
}

type engineReport struct {
	Name             string `json:"name"`
	Command          string `json:"command"`
	ExecutableSHA256 string `json:"executable_sha256,omitempty"`
	Available        bool   `json:"available"`
	Version          string `json:"tool_version,omitempty"`
	Reason           string `json:"reason,omitempty"`
}

type runReport struct {
	Round           int    `json:"round"`
	Order           int    `json:"order"`
	Engine          string `json:"engine"`
	WallNanos       int64  `json:"wall_nanos"`
	CPUUserNanos    int64  `json:"cpu_user_nanos"`
	CPUSystemNanos  int64  `json:"cpu_system_nanos"`
	CPUTotalNanos   int64  `json:"cpu_total_nanos"`
	PeakRSSBytes    uint64 `json:"peak_rss_bytes"`
	ArtifactBytes   int64  `json:"artifact_bytes"`
	NativeCodeBytes uint64 `json:"native_code_bytes"`
}

func main() {
	configPath := flag.String("config", "external-compilers.json", "strict compiler command manifest")
	rounds := flag.Int("rounds", 6, "alternating measured rounds")
	outPath := flag.String("out", "", "write JSON report here instead of stdout")
	appendOut := flag.Bool("append", false, "append one JSON report to -out (JSON Lines)")
	worker := flag.String("internal-worker", "", "internal compiler worker")
	workerTarget := flag.String("internal-target", "compat", "internal compiler target")
	workerCount := flag.Int("internal-workers", 0, "internal compiler function workers")
	flag.Parse()
	if *worker != "" {
		if flag.NArg() != 2 {
			fail("internal worker", errors.New("expected input Wasm and output artifact paths"))
		}
		if err := runBuiltinCompiler(*worker, *workerTarget, *workerCount, flag.Arg(0), flag.Arg(1)); err != nil {
			fail("internal worker", err)
		}
		return
	}
	if flag.NArg() != 1 || *rounds < 1 {
		fmt.Fprintln(os.Stderr, "usage: compilerharness [-config commands.json] [-rounds 6] [-out report.json] module.wasm")
		os.Exit(2)
	}

	cfg, err := readConfig(*configPath)
	if err != nil {
		fail("read config", err)
	}
	wasmPath, err := filepath.Abs(flag.Arg(0))
	if err != nil {
		fail("resolve module", err)
	}
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		fail("read module", err)
	}
	tempDir, err := os.MkdirTemp("", "wago-compiler-harness-")
	if err != nil {
		fail("create temporary directory", err)
	}
	defer os.RemoveAll(tempDir)

	report := report{Version: reportVersion, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, WasmPath: wasmPath, WasmSHA256: sha256.Sum256(wasmBytes), Rounds: *rounds}
	available := make([]resolvedEngine, 0, len(cfg.Engines))
	for _, engine := range cfg.Engines {
		resolved, version, lookupErr := resolveEngine(engine)
		row := engineReport{Name: engine.Name, Command: resolved.command, ExecutableSHA256: resolved.executableSHA256, Version: version}
		if lookupErr != nil {
			row.Reason = lookupErr.Error()
			report.Engines = append(report.Engines, row)
			if engine.Required {
				fail("locate required engine "+engine.Name, lookupErr)
			}
			continue
		}
		row.Available = true
		report.Engines = append(report.Engines, row)
		available = append(available, resolved)
	}
	if len(available) == 0 {
		fail("locate compilers", errors.New("no configured engine is available"))
	}

	for round := 0; round < *rounds; round++ {
		for order := range available {
			engine := available[(round+order)%len(available)]
			artifact := filepath.Join(tempDir, fmt.Sprintf("%02d-%02d-%s.bin", round, order, engine.name))
			args, err := expandArgs(engine.args, wasmPath, artifact)
			if err != nil {
				fail("expand engine "+engine.name, err)
			}
			cmd := exec.Command(engine.command, args...)
			var stderr bytes.Buffer
			cmd.Stdout = io.Discard
			cmd.Stderr = &stderr
			started := time.Now()
			err = cmd.Run()
			wall := time.Since(started)
			if err != nil {
				fail("run engine "+engine.name, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String())))
			}
			info, err := os.Stat(artifact)
			if err != nil {
				fail("stat artifact for "+engine.name, err)
			}
			codeBytes, err := nativeCodeBytes(artifact, engine.builtin)
			if err != nil {
				fail("measure native code for "+engine.name, err)
			}
			report.Runs = append(report.Runs, newRunReport(round, order, engine.name, wall, cmd.ProcessState, info.Size(), codeBytes))
		}
	}

	var output = os.Stdout
	if *outPath != "" {
		if *appendOut {
			output, err = os.OpenFile(*outPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
		} else {
			output, err = os.Create(*outPath)
		}
		if err != nil {
			fail("create report", err)
		}
		defer output.Close()
	} else if *appendOut {
		fail("configure output", errors.New("-append requires -out"))
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		fail("write report", err)
	}
}

func resolveEngine(engine engineConfig) (resolvedEngine, string, error) {
	if engine.Builtin != "" {
		self, err := os.Executable()
		if err != nil {
			return resolvedEngine{name: engine.Name}, "", err
		}
		digest, err := executableSHA256(self)
		if err != nil {
			return resolvedEngine{name: engine.Name, command: self}, "", err
		}
		return resolvedEngine{
			name: engine.Name, command: self,
			args:             []string{"-internal-worker=" + engine.Builtin, "-internal-target=" + normalizedBuiltinTarget(engine.Target), "-internal-workers=" + strconv.Itoa(engine.Workers), "{wasm}", "{artifact}"},
			executableSHA256: digest,
			builtin:          true,
		}, builtinCompilerVersion(engine.Builtin), nil
	}
	path, err := exec.LookPath(engine.Command)
	if err != nil {
		return resolvedEngine{name: engine.Name, command: engine.Command}, "", err
	}
	digest, err := executableSHA256(path)
	if err != nil {
		return resolvedEngine{name: engine.Name, command: path}, "", err
	}
	version := ""
	if len(engine.VersionArgs) != 0 {
		output, versionErr := exec.Command(path, engine.VersionArgs...).CombinedOutput()
		if versionErr != nil {
			return resolvedEngine{name: engine.Name, command: path}, "", fmt.Errorf("fingerprint: %w: %s", versionErr, bytes.TrimSpace(output))
		}
		version = strings.TrimSpace(string(output))
	}
	return resolvedEngine{name: engine.Name, command: path, args: engine.Args, executableSHA256: digest}, version, nil
}

func executableSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("hash executable: %w", err)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func normalizedBuiltinTarget(target string) string {
	if target == "" {
		return "compat"
	}
	return target
}

func builtinCompilerVersion(name string) string {
	version := name + " " + runtime.Version()
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" && setting.Value != "" {
				return version + " " + setting.Value
			}
		}
	}
	return version
}

func runBuiltinCompiler(name, target string, workers int, wasmPath, artifactPath string) error {
	var compiler wago.CompilerEngine
	switch name {
	case "dragline":
		compiler = wago.CompilerDragline
	case "railshot":
		compiler = wago.CompilerRailshot
	default:
		return fmt.Errorf("unknown builtin compiler %q", name)
	}
	source, err := os.ReadFile(wasmPath)
	if err != nil {
		return fmt.Errorf("read Wasm: %w", err)
	}
	if workers < 0 {
		return fmt.Errorf("function workers must be non-negative")
	}
	cfg := wago.NewRuntimeConfig().WithCompiler(compiler).WithBoundsChecks(wago.BoundsChecksExplicit).WithFunctionWorkers(workers)
	switch target {
	case "compat":
	case "native":
		cfg = cfg.WithTarget(wago.TargetNative)
	default:
		return fmt.Errorf("unknown %s target %q", name, target)
	}
	compiled, err := cfg.Compile(source)
	if err != nil {
		return fmt.Errorf("compile: %w", err)
	}
	defer compiled.Close()
	artifact, err := compiled.MarshalBinary()
	if err != nil {
		return fmt.Errorf("encode artifact: %w", err)
	}
	if err := os.WriteFile(artifactPath, artifact, 0o644); err != nil {
		return fmt.Errorf("write artifact: %w", err)
	}
	if err := os.WriteFile(artifactPath+".native-code-bytes", []byte(strconv.Itoa(compiled.CodeSize())), 0o644); err != nil {
		return fmt.Errorf("write native code size: %w", err)
	}
	return nil
}

func newRunReport(round, order int, engine string, wall time.Duration, state *os.ProcessState, artifactBytes int64, nativeCodeBytes uint64) runReport {
	user := state.UserTime().Nanoseconds()
	system := state.SystemTime().Nanoseconds()
	return runReport{
		Round: round, Order: order, Engine: engine, WallNanos: wall.Nanoseconds(),
		CPUUserNanos: user, CPUSystemNanos: system, CPUTotalNanos: user + system,
		PeakRSSBytes: peakRSS(state), ArtifactBytes: artifactBytes, NativeCodeBytes: nativeCodeBytes,
	}
}

func nativeCodeBytes(path string, builtin bool) (uint64, error) {
	if builtin {
		value, err := os.ReadFile(path + ".native-code-bytes")
		if err != nil {
			return 0, err
		}
		bytes, err := strconv.ParseUint(string(value), 10, 64)
		if err != nil {
			return 0, err
		}
		return bytes, nil
	}
	file, err := elf.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	symbols, err := file.Symbols()
	if err != nil {
		return 0, err
	}
	var total uint64
	for _, symbol := range symbols {
		if elf.ST_TYPE(symbol.Info) == elf.STT_FUNC && symbol.Section != elf.SHN_UNDEF {
			total += symbol.Size
		}
	}
	return total, nil
}

func readConfig(path string) (config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return config{}, err
	}
	var cfg config
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return config{}, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		if err == nil {
			return config{}, errors.New("trailing JSON value")
		}
		return config{}, fmt.Errorf("trailing data: %w", err)
	}
	if cfg.Version != configVersion {
		return config{}, fmt.Errorf("version %d unsupported (want %d)", cfg.Version, configVersion)
	}
	seen := make(map[string]bool, len(cfg.Engines))
	for i, engine := range cfg.Engines {
		if engine.Name == "" {
			return config{}, fmt.Errorf("engine %d has empty name", i)
		}
		if seen[engine.Name] {
			return config{}, fmt.Errorf("duplicate engine name %q", engine.Name)
		}
		seen[engine.Name] = true
		if engine.Builtin != "" {
			if engine.Command != "" || len(engine.Args) != 0 || len(engine.VersionArgs) != 0 {
				return config{}, fmt.Errorf("engine %s builtin cannot declare command, args, or version_args", engine.Name)
			}
			if engine.Builtin != "dragline" && engine.Builtin != "railshot" {
				return config{}, fmt.Errorf("engine %s has unknown builtin %q", engine.Name, engine.Builtin)
			}
			if engine.Workers < 0 {
				return config{}, fmt.Errorf("engine %s has negative worker count", engine.Name)
			}
			if target := normalizedBuiltinTarget(engine.Target); target != "compat" && target != "native" {
				return config{}, fmt.Errorf("engine %s has unknown builtin target %q", engine.Name, target)
			}
			continue
		}
		if engine.Command == "" {
			return config{}, fmt.Errorf("engine %s has empty command", engine.Name)
		}
		if engine.Target != "" {
			return config{}, fmt.Errorf("engine %s external command cannot declare a builtin target", engine.Name)
		}
		if engine.Workers != 0 {
			return config{}, fmt.Errorf("engine %s external command cannot declare workers", engine.Name)
		}
		if _, err := expandArgs(engine.Args, "input.wasm", "output.bin"); err != nil {
			return config{}, fmt.Errorf("engine %s: %w", engine.Name, err)
		}
	}
	return cfg, nil
}

func expandArgs(args []string, wasmPath, artifactPath string) ([]string, error) {
	out := make([]string, len(args))
	wasmSeen, artifactSeen := false, false
	for i, arg := range args {
		if strings.Contains(arg, "{wasm}") {
			wasmSeen = true
		}
		if strings.Contains(arg, "{artifact}") {
			artifactSeen = true
		}
		out[i] = strings.ReplaceAll(strings.ReplaceAll(arg, "{wasm}", wasmPath), "{artifact}", artifactPath)
	}
	if !wasmSeen || !artifactSeen {
		return nil, errors.New("args must contain {wasm} and {artifact} placeholders")
	}
	return out, nil
}

func fail(operation string, err error) {
	fmt.Fprintf(os.Stderr, "compilerharness: %s: %v\n", operation, err)
	os.Exit(1)
}

// Package standalone builds native executables containing a Wasm command.
package standalone

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/wago-org/wago/cli/internal/automation"
	managerplugin "github.com/wago-org/wago/cli/manager/internal/plugin"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type Target struct {
	OS, Arch string
}

func (t Target) String() string { return t.OS + "/" + t.Arch }

func (t Target) supportsCore3() bool {
	return (t.OS == "linux" && (t.Arch == "amd64" || t.Arch == "arm64")) ||
		(t.OS == "darwin" && t.Arch == "arm64")
}

func ParseTarget(value string) (Target, error) {
	if value == "" {
		return Target{OS: runtime.GOOS, Arch: runtime.GOARCH}, nil
	}
	parts := strings.Split(value, "/")
	if len(parts) != 2 {
		return Target{}, fmt.Errorf("target must be os/arch (for example linux/amd64)")
	}
	target := Target{OS: parts[0], Arch: parts[1]}
	if (target.OS != "darwin" && target.OS != "linux" && target.OS != "windows") ||
		(target.Arch != "amd64" && target.Arch != "arm64") {
		return Target{}, fmt.Errorf("unsupported target %s; use darwin, linux, or windows with amd64 or arm64", value)
	}
	return target, nil
}

type Request struct {
	Input, Output          string
	Invoke                 string
	Core                   int
	DeferredBoundsChecking bool
	FunctionWorkers        int
	Optimizations          map[string]bool
	Target                 Target
	Verbose                bool
	KeepSymbols            bool
	TinyGo                 bool
}

type Result struct {
	Output  string
	Target  Target
	Plugins int
}

func DefaultOutput(input string, target Target) string {
	extension := filepath.Ext(input)
	output := strings.TrimSuffix(input, extension)
	if target.OS == "windows" {
		output += ".exe"
	}
	return output
}

func Build(request Request) (Result, error) {
	source, err := os.ReadFile(request.Input)
	if err != nil {
		return Result{}, err
	}
	module, err := wasm.DecodeModule(source)
	if err != nil {
		return Result{}, fmt.Errorf("decode: %w", err)
	}
	if err := wasm.ValidateModule(module); err != nil {
		return Result{}, fmt.Errorf("validate: %w", err)
	}
	if request.Target.OS == "" {
		request.Target, err = ParseTarget("")
		if err != nil {
			return Result{}, err
		}
	}
	if request.TinyGo {
		if !request.Target.supportsTinyGo() {
			return Result{}, fmt.Errorf("TinyGo standalone executables are not supported for target %s", request.Target)
		}
	}
	if request.Core == 3 && !request.Target.supportsCore3() {
		return Result{}, fmt.Errorf("WebAssembly Core 3 is not supported for target %s", request.Target)
	}
	host := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
	if request.Target != host {
		return Result{}, fmt.Errorf("precompiled standalone builds require the native target %s; got %s", host, request.Target)
	}
	if request.Output == "" {
		request.Output = DefaultOutput(request.Input, request.Target)
	}
	input, err := filepath.Abs(request.Input)
	if err != nil {
		return Result{}, err
	}
	output, err := filepath.Abs(request.Output)
	if err != nil {
		return Result{}, err
	}
	if filepath.Clean(input) == filepath.Clean(output) {
		return Result{}, fmt.Errorf("output path must differ from input")
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return Result{}, err
	}
	buildDir, err := os.MkdirTemp("", "wago-standalone-*")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(buildDir)
	inputs, err := managerplugin.PrepareStandalone(buildDir, request.Verbose)
	if err != nil {
		return Result{}, fmt.Errorf("prepare plugins: %w", err)
	}
	selections, err := json.Marshal(inputs.Build.Selections)
	if err != nil {
		return Result{}, fmt.Errorf("encode plugin configuration: %w", err)
	}
	if err := os.WriteFile(filepath.Join(buildDir, "module.wasm"), source, 0o644); err != nil {
		return Result{}, err
	}
	mainPath := filepath.Join(buildDir, "main.go")
	if err := os.WriteFile(mainPath, mainSource(inputs.Build.ProviderImports, selections, request.Invoke, request.Core, request.DeferredBoundsChecking, request.FunctionWorkers, request.Optimizations, false), 0o644); err != nil {
		return Result{}, err
	}
	environment := append(os.Environ(),
		"CGO_ENABLED=0",
		"GOOS="+request.Target.OS,
		"GOARCH="+request.Target.Arch,
	)
	if err := runGo(buildDir, environment, request.Verbose, "mod", "tidy"); err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(mainPath, artifactCompilerSource(inputs.Build.ProviderImports, selections, request.Invoke, request.Core, request.DeferredBoundsChecking, request.FunctionWorkers, request.Optimizations), 0o644); err != nil {
		return Result{}, err
	}
	helperArgs := []string{"run"}
	if request.TinyGo {
		// The helper itself uses standard Go, but its output executes under TinyGo.
		// Select final-runtime capabilities such as cooperative interruption while
		// retaining the standard compiler needed to generate the artifact.
		helperArgs = append(helperArgs, "-tags=wago_target_tinygo")
	}
	helperArgs = append(helperArgs, ".")
	if err := runGo(buildDir, environment, request.Verbose, helperArgs...); err != nil {
		return Result{}, fmt.Errorf("precompile standalone artifact: %w", err)
	}
	if err := os.WriteFile(mainPath, mainSource(inputs.Build.ProviderImports, selections, request.Invoke, request.Core, request.DeferredBoundsChecking, request.FunctionWorkers, request.Optimizations, true), 0o644); err != nil {
		return Result{}, err
	}
	if request.TinyGo {
		args := []string{"build", "-scheduler=tasks", "-opt=z", "-gc=conservative", "-tags=wago_precompiled"}
		if !request.KeepSymbols {
			args = append(args, "-no-debug")
		}
		args = append(args, "-o", output, ".")
		if err := runTool("tinygo", buildDir, environment, request.Verbose, args...); err != nil {
			return Result{}, err
		}
		if !request.KeepSymbols {
			if err := stripTinyGo(buildDir, environment, request.Verbose, output, request.Target); err != nil {
				return Result{}, err
			}
		}
		return Result{Output: output, Target: request.Target, Plugins: len(inputs.Build.Selections)}, nil
	}
	args := []string{"build", "-buildvcs=false", "-trimpath", "-tags=wago_precompiled"}
	if !request.KeepSymbols {
		args = append(args, "-ldflags=-s -w")
	}
	args = append(args, "-o", output, ".")
	if err := runGo(buildDir, environment, request.Verbose, args...); err != nil {
		return Result{}, err
	}
	return Result{Output: output, Target: request.Target, Plugins: len(inputs.Build.Selections)}, nil
}

func mainSource(providerImports []string, selections []byte, invoke string, core int, deferredBoundsChecking bool, functionWorkers int, optimizations map[string]bool, precompiled bool) []byte {
	providerImports = append([]string(nil), providerImports...)
	sort.Strings(providerImports)
	var source bytes.Buffer
	source.WriteString("package main\n\nimport (\n\t_ \"embed\"\n\t\"encoding/json\"\n\t\"os\"\n\n")
	source.WriteString("\twago \"github.com/wago-org/wago\"\n")
	source.WriteString("\t\"github.com/wago-org/wago/cli/standalone\"\n")
	for index, providerImport := range providerImports {
		fmt.Fprintf(&source, "\tprovider%d %q\n", index, providerImport)
	}
	artifact := "module.wasm"
	run := "Run"
	if precompiled {
		artifact = "module.wago"
		run = "RunArtifact"
	}
	fmt.Fprintf(&source, ")\n\n//go:embed %s\nvar module []byte\n\n", artifact)
	fmt.Fprintf(&source, "var selectionJSON = []byte(%q)\n\n", selections)
	source.WriteString("func pluginSet() wago.PluginSet {\n\tvar selections []wago.PluginSelection\n\tif err := json.Unmarshal(selectionJSON, &selections); err != nil { panic(err) }\n\tvar providers []wago.PluginProvider\n")
	for index := range providerImports {
		fmt.Fprintf(&source, "\tproviders = append(providers, provider%d.Providers()...)\n", index)
	}
	source.WriteString("\treturn wago.PluginSet{Providers: providers, Selections: selections}\n}\n\n")
	fmt.Fprintf(&source, "var options = standalone.Options{Invoke: %q, Core: %d, DeferBoundsChecks: %t, FunctionWorkers: %d, OptimizationKnobs: map[string]bool{", invoke, core, deferredBoundsChecking, functionWorkers)
	names := make([]string, 0, len(optimizations))
	for name := range optimizations {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(&source, "%q: %t, ", name, optimizations[name])
	}
	source.WriteString("}}\n\n")
	fmt.Fprintf(&source, "func main() { os.Exit(standalone.%s(module, pluginSet(), options, os.Args)) }\n", run)
	return source.Bytes()
}

func artifactCompilerSource(providerImports []string, selections []byte, invoke string, core int, deferredBoundsChecking bool, functionWorkers int, optimizations map[string]bool) []byte {
	source := mainSource(providerImports, selections, invoke, core, deferredBoundsChecking, functionWorkers, optimizations, false)
	source = bytes.Replace(source, []byte("\t\"os\"\n"), []byte("\t\"os\"\n\t\"fmt\"\n"), 1)
	source = bytes.Replace(source,
		[]byte("func main() { os.Exit(standalone.Run(module, pluginSet(), options, os.Args)) }"),
		[]byte("func main() { artifact, err := standalone.CompileArtifact(module, pluginSet(), options); if err == nil { err = os.WriteFile(\"module.wago\", artifact, 0o644) }; if err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) } }"), 1)
	return source
}

func runGo(dir string, environment []string, verbose bool, args ...string) error {
	return runTool("go", dir, environment, verbose, args...)
}

func runTool(tool, dir string, environment []string, verbose bool, args ...string) error {
	command := exec.Command(tool, args...)
	command.Dir = dir
	command.Env = environment
	automation.ConfigureCommand(command)
	if verbose {
		command.Stdout, command.Stderr = os.Stderr, os.Stderr
		return command.Run()
	}
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w\n%s", tool, args[0], err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (t Target) supportsTinyGo() bool {
	return (t.OS == "linux" && (t.Arch == "amd64" || t.Arch == "arm64")) ||
		(t.OS == "darwin" && t.Arch == "arm64")
}

func stripTinyGo(dir string, environment []string, verbose bool, output string, target Target) error {
	switch target.OS {
	case "darwin":
		return runTool("strip", dir, environment, verbose, "-x", output)
	case "linux":
		return runTool("strip", dir, environment, verbose, "-s", output)
	default:
		return fmt.Errorf("TinyGo stripping is not supported for target %s", target)
	}
}

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
	Input, Output string
	Target        Target
	Verbose       bool
	KeepSymbols   bool
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
	config, err := json.Marshal(inputs.Plugins)
	if err != nil {
		return Result{}, fmt.Errorf("encode plugin configuration: %w", err)
	}
	if err := os.WriteFile(filepath.Join(buildDir, "module.wasm"), source, 0o644); err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(filepath.Join(buildDir, "main.go"), mainSource(inputs.Dependencies, config), 0o644); err != nil {
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
	args := []string{"build", "-buildvcs=false", "-trimpath"}
	if !request.KeepSymbols {
		args = append(args, "-ldflags=-s -w")
	}
	args = append(args, "-o", output, ".")
	if err := runGo(buildDir, environment, request.Verbose, args...); err != nil {
		return Result{}, err
	}
	return Result{Output: output, Target: request.Target, Plugins: len(inputs.Dependencies)}, nil
}

func mainSource(dependencies []string, pluginConfig []byte) []byte {
	dependencies = append([]string(nil), dependencies...)
	sort.Strings(dependencies)
	var source bytes.Buffer
	source.WriteString("package main\n\nimport (\n\t_ \"embed\"\n\t\"os\"\n\n")
	source.WriteString("\t\"github.com/wago-org/wago/cli/standalone\"\n")
	for _, dependency := range dependencies {
		fmt.Fprintf(&source, "\t_ %q\n", dependency+"/register")
	}
	source.WriteString(")\n\n//go:embed module.wasm\nvar module []byte\n\n")
	fmt.Fprintf(&source, "var pluginConfig = []byte(%q)\n\n", pluginConfig)
	source.WriteString("func main() { os.Exit(standalone.Run(module, pluginConfig, os.Args)) }\n")
	return source.Bytes()
}

func runGo(dir string, environment []string, verbose bool, args ...string) error {
	command := exec.Command("go", args...)
	command.Dir = dir
	command.Env = environment
	automation.ConfigureCommand(command)
	if verbose {
		command.Stdout, command.Stderr = os.Stderr, os.Stderr
		return command.Run()
	}
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("go %s: %w\n%s", args[0], err, strings.TrimSpace(string(output)))
	}
	return nil
}

// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

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

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/automation"
	managerplugin "github.com/wago-org/wago/cli/manager/internal/plugin"
	"github.com/wago-org/wago/internal/wagopaths"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type Target struct {
	OS, Arch string
}

func (t Target) String() string { return t.OS + "/" + t.Arch }

func (t Target) supportsCore3() bool {
	return (t.OS == "linux" && (t.Arch == "amd64" || t.Arch == "arm64" || t.Arch == "riscv64")) ||
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
		(target.Arch != "amd64" && target.Arch != "arm64" && !(target.OS == "linux" && target.Arch == "riscv64")) {
		return Target{}, fmt.Errorf("unsupported target %s; use darwin, linux, or windows with amd64 or arm64, or linux/riscv64", value)
	}
	return target, nil
}

type Request struct {
	Input, Output          string
	Invoke                 string
	Core                   int
	Features               wago.CoreFeatures
	FeaturesSet            bool
	BuildTags              []string
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

var (
	findTool = exec.LookPath
)

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
	if _, err := wasm.DecodeModule(source); err != nil {
		return Result{}, fmt.Errorf("decode: %w", err)
	}
	// The generator validates under the selected profile and target build tags.
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
	if err := requireToolchain("go"); err != nil {
		return Result{}, err
	}
	if request.TinyGo {
		if err := requireToolchain("tinygo"); err != nil {
			return Result{}, err
		}
	}
	if request.Core == 3 && !request.Target.supportsCore3() {
		return Result{}, fmt.Errorf("WebAssembly Core 3 is not supported for target %s", request.Target)
	}
	host := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
	if request.Target != host {
		return Result{}, fmt.Errorf("standalone builds require the native target %s; got %s", host, request.Target)
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
	var selections []byte
	hasPlugins := len(inputs.Build.ProviderImports) != 0 || len(inputs.Build.Selections) != 0
	if hasPlugins {
		selections, err = json.Marshal(inputs.Build.Selections)
		if err != nil {
			return Result{}, fmt.Errorf("encode plugin configuration: %w", err)
		}
	}
	if err := os.WriteFile(filepath.Join(buildDir, "module.wasm"), source, 0o644); err != nil {
		return Result{}, err
	}
	mainPath := filepath.Join(buildDir, "main.go")
	if err := os.WriteFile(mainPath, mainSource(inputs.Build.ProviderImports, selections, request, generateModule), 0o644); err != nil {
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
	nativeRuntime, err := prepareNativeRuntime(buildDir, environment)
	if err != nil {
		return Result{}, err
	}
	tags := strings.Join(append([]string{"wago_standalone_generator"}, request.BuildTags...), ",")
	if request.TinyGo {
		tags += ",wago_target_tinygo"
	}
	if err := runGo(buildDir, environment, request.Verbose, "run", "-trimpath", "-tags="+tags, "."); err != nil {
		return Result{}, fmt.Errorf("compile standalone machine code: %w", err)
	}
	tagsForRuntime, err := linkStandaloneLauncher(buildDir, nativeRuntime, mainPath, inputs.Build.ProviderImports, selections, request)
	if err != nil {
		return Result{}, err
	}
	runtimeTags := "-tags=" + strings.Join(append(tagsForRuntime, request.BuildTags...), ",")
	if request.TinyGo {
		args := []string{"build", "-scheduler=tasks", "-opt=z", "-gc=conservative", runtimeTags}
		args = append(args, wagopaths.TinyGoTargetFlags(request.Target.OS, request.Target.Arch)...)
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
	args := []string{"build", "-buildvcs=false", "-trimpath", runtimeTags}
	if !request.KeepSymbols {
		args = append(args, "-ldflags=-s -w")
	}
	args = append(args, "-o", output, ".")
	if err := runGo(buildDir, environment, request.Verbose, args...); err != nil {
		return Result{}, err
	}
	return Result{Output: output, Target: request.Target, Plugins: len(inputs.Build.Selections)}, nil
}

func linkStandaloneLauncher(buildDir, nativeRuntime, mainPath string, providerImports []string, selections []byte, request Request) ([]string, error) {
	hasPlugins := len(providerImports) != 0 || len(selections) != 0
	if !hasPlugins {
		// The generator can prove some launchers terminate before reaching the
		// compiled image. Older generators leave this optional source absent.
		source, err := os.ReadFile(filepath.Join(buildDir, "standalone_diagnostic.go.txt"))
		if err == nil {
			return nil, os.WriteFile(mainPath, source, 0o644)
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
	}
	generated := map[string][]byte{}
	names := []string{"standalone_module.go"}
	hasProfile := false
	if !hasPlugins {
		// Generators before image specialization emit only the module and dispatcher.
		profile, err := os.ReadFile(filepath.Join(buildDir, "standalone_profile.go.txt"))
		if err == nil {
			generated["standalone_profile.go"] = profile
			hasProfile = true
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	if hasProfile {
		registers, err := os.ReadFile(filepath.Join(buildDir, "standalone_registers.h.txt"))
		if err == nil {
			generated["standalone_registers.h"] = registers
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	} else {
		names = append(names, "standalone_dispatcher.go")
	}
	for _, name := range names {
		body, err := os.ReadFile(filepath.Join(buildDir, name+".txt"))
		if err != nil {
			return nil, err
		}
		generated[name] = body
	}
	if err := linkNativeModule(nativeRuntime, generated); err != nil {
		return nil, err
	}
	var launcher []byte
	mode := launchCompiled
	usesGeneratedLauncher := false
	if hasProfile {
		mode = launchWithoutPlugins
		// A generator with an image profile can supply its own launcher.
		generated, err := os.ReadFile(filepath.Join(buildDir, "standalone_launcher.go.txt"))
		if err == nil {
			launcher = generated
			usesGeneratedLauncher = true
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	if !usesGeneratedLauncher {
		launcher = mainSource(providerImports, selections, request, mode)
	}
	if err := os.WriteFile(mainPath, launcher, 0o644); err != nil {
		return nil, err
	}
	tagsForRuntime := []string{"wago_standalone"}
	if hasProfile {
		tagsForRuntime = append(tagsForRuntime, "wago_standalone_no_plugins", "wago_standalone_generated")
		markers := []string{"standalone_no_reference_store", "standalone_no_collector"}
		if usesGeneratedLauncher {
			markers = append(markers, "standalone_one_shot")
		}
		for _, marker := range markers {
			if _, err := os.Stat(filepath.Join(buildDir, marker+".txt")); err == nil {
				tagsForRuntime = append(tagsForRuntime, "wago_"+marker)
			} else if !os.IsNotExist(err) {
				return nil, err
			}
		}
	}
	return tagsForRuntime, nil
}

func requireToolchain(name string) error {
	if _, err := findTool(name); err == nil {
		return nil
	}
	label, website := "Go", "https://go.dev/dl/"
	if name == "tinygo" {
		label, website = "TinyGo", "https://tinygo.org/getting-started/install/"
	}
	return fmt.Errorf(`%s is required to build this executable but was not found on PATH.

Install %s and ensure %q is available on PATH: %s`, label, label, name, website)
}

type mainMode uint8

const (
	generateModule mainMode = iota
	launchCompiled
	launchWithoutPlugins
)

func mainSource(providerImports []string, selections []byte, request Request, mode mainMode) []byte {
	providerImports = append([]string(nil), providerImports...)
	sort.Strings(providerImports)
	hasPlugins := len(providerImports) != 0 || len(selections) != 0
	var source bytes.Buffer
	source.WriteString("package main\n\nimport (\n\t\"os\"\n")
	if mode == generateModule {
		source.WriteString("\t_ \"embed\"\n")
	}
	if hasPlugins {
		source.WriteString("\t\"encoding/json\"\n")
	}
	source.WriteString("\n\twago \"github.com/wago-org/wago\"\n")
	source.WriteString("\t\"github.com/wago-org/wago/cli/standalone\"\n")
	for index, providerImport := range providerImports {
		fmt.Fprintf(&source, "\tprovider%d %q\n", index, providerImport)
	}
	source.WriteString(")\n\n")
	if mode == generateModule {
		source.WriteString("//go:embed module.wasm\nvar module []byte\n\n")
	}
	pluginSet := "wago.PluginSet{}"
	if hasPlugins {
		pluginSet = "pluginSet()"
		fmt.Fprintf(&source, "var selectionJSON = []byte(%q)\n\n", selections)
		source.WriteString("func pluginSet() wago.PluginSet {\n\tvar selections []wago.PluginSelection\n\tif err := json.Unmarshal(selectionJSON, &selections); err != nil { panic(err) }\n\tvar providers []wago.PluginProvider\n")
		for index := range providerImports {
			fmt.Fprintf(&source, "\tproviders = append(providers, provider%d.Providers()...)\n", index)
		}
		source.WriteString("\treturn wago.PluginSet{Providers: providers, Selections: selections}\n}\n\n")
	}
	fmt.Fprintf(&source, "var options = standalone.Options{Invoke: %q, Core: %d, DeferBoundsChecks: %t, FunctionWorkers: %d, Features: %d, FeaturesSet: %t", request.Invoke, request.Core, request.DeferredBoundsChecking, request.FunctionWorkers, request.Features, request.FeaturesSet)
	if mode != launchWithoutPlugins && len(request.Optimizations) != 0 {
		source.WriteString(", OptimizationKnobs: map[string]bool{")
		names := make([]string, 0, len(request.Optimizations))
		for name := range request.Optimizations {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Fprintf(&source, "%q: %t, ", name, request.Optimizations[name])
		}
		source.WriteString("}")
	}
	source.WriteString("}\n\n")
	switch {
	case mode == generateModule:
		fmt.Fprintf(&source, "func main() {\n\tsources, err := standalone.GenerateModule(module, %s, options)\n", pluginSet)
		source.WriteString("\tif err == nil {\n\t\tfor name, source := range sources {\n\t\t\tif err = os.WriteFile(name+\".txt\", source, 0644); err != nil { break }\n\t\t}\n\t}\n")
		source.WriteString("\tif err != nil { println(err.Error()); os.Exit(1) }\n}\n")
	case mode == launchWithoutPlugins:
		source.WriteString("func main() { os.Exit(standalone.RunCompiledWithoutPlugins(wago.StandaloneCompiled(), options, os.Args)) }\n")
	default:
		fmt.Fprintf(&source, "func main() { os.Exit(standalone.RunCompiled(wago.StandaloneCompiled(), %s, options, os.Args)) }\n", pluginSet)
	}
	return source.Bytes()
}

func runGo(dir string, environment []string, verbose bool, args ...string) error {
	return runTool("go", dir, environment, verbose, args...)
}

func runTool(tool, dir string, environment []string, verbose bool, args ...string) error {
	if tool == "go" || tool == "tinygo" {
		if err := requireToolchain(tool); err != nil {
			return err
		}
	}
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

// Package compile defines wago compile.
package compile

import (
	"fmt"
	"strings"

	"github.com/wago-org/wago/cli/internal/command"
	internalparallel "github.com/wago-org/wago/cli/internal/parallel"
	"github.com/wago-org/wago/cli/internal/settings"
	"github.com/wago-org/wago/cli/internal/ui"
)

const deferredBoundsCheckingFlag = "deferred-bounds-checking"

type Options struct {
	Input, Output, Target, Invoke string
	Core                          string
	Parallel                      string
	Plugins                       string
	DeferredBoundsChecking        *bool
	Optimizations                 map[string]bool
	Global, Local, Bare           bool
	Verbose                       bool
}

type Environment interface {
	Compile(Options)
}

func Command(environment Environment) *command.Cmd {
	knobs := compileKnobFlags()
	flags := []command.Flag{
		{Name: "output", Short: "o", Arg: "<file>", Help: "output executable path"},
		{Name: "target", Arg: "<os/arch>", Help: "target platform (default: current platform)"},
		{Name: "invoke", Short: "e", Arg: "<name>", Help: "exported function to call"},
		{Name: "core", Arg: "<version>", Help: "WebAssembly core feature set: 2 (default) | 3"},
		internalparallel.Flag(),
		{Name: "plugin", Arg: "<names>", Help: "comma-separated extra plugins to enable"},
		{Name: "plugins", Arg: "<names>", Help: "alias for --plugin"},
		{Name: "global", Short: "g", Bool: true, Help: "include shared user-wide plugins"},
		{Name: "local", Bool: true, Help: "include this project's plugins"},
		{Name: "bare", Bool: true, Help: "build without plugins"},
		{Name: "verbose", Short: "v", Bool: true, Help: "show Go build output"},
	}
	parserFlags := append(append([]command.Flag(nil), flags...), knobs...)
	return &command.Cmd{
		Name:       "compile",
		Summary:    "build a standalone executable from a WebAssembly module",
		Args:       "<file>",
		Automation: command.DryRun,
		Flags:      flags,
		Knobs:      knobs,
		Normalize: func(args []string) ([]string, error) {
			return internalparallel.NormalizeArgs(args, parserFlags, false)
		},
		Long: "The executable embeds the module and selected plugin configuration. By default it\n" +
			"calls _start; use --invoke to bake in another exported function. Core features,\n" +
			"parallelism, and optimization knobs are fixed at build time. Use --target\n" +
			"linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64,\n" +
			"or windows/arm64 to cross-compile with the matching Wago backend.",
		Run: func(context *command.Ctx) {
			if len(context.Args) != 1 {
				ui.Usage("compile: need exactly one <file>")
			}
			plugins := joinPluginLists(context.Str("plugin"), context.Str("plugins"))
			deferred, optimizations := compileKnobOverrides(context)
			environment.Compile(Options{
				Input: context.Args[0], Output: context.Str("output"), Target: context.Str("target"), Invoke: context.Str("invoke"),
				Core: context.Str("core"), Parallel: context.Str("parallel"), Plugins: plugins, DeferredBoundsChecking: deferred, Optimizations: optimizations,
				Global: context.Bool("global"), Local: context.Bool("local"), Bare: context.Bool("bare"),
				Verbose: context.Bool("verbose"),
			})
		},
	}
}

func compileKnobFlags() []command.Flag {
	configured, hasConfig, _ := settings.LoadConfigured()
	deferred := true
	if hasConfig {
		deferred = configured.Runtime.DeferredBoundsChecking
	}
	flags := pairedFlag(deferredBoundsCheckingFlag, deferred, "skip provably redundant explicit bounds checks")
	for _, knob := range settings.OptimizationCatalog() {
		name := strings.TrimPrefix(knob.Key, "optimizations.")
		on := knob.Default
		if hasConfig {
			if value, ok := configured.Optimizations[name]; ok {
				on = value
			}
		}
		flags = append(flags, pairedFlag(name, on, knob.Description)...)
	}
	return flags
}

func pairedFlag(name string, on bool, description string) []command.Flag {
	state := "off"
	if on {
		state = "on"
	}
	return []command.Flag{
		{Name: name, Bool: true, Help: fmt.Sprintf("(default: %s) %s", state, description)},
		{Name: "no-" + name, Bool: true},
	}
}

func compileKnobOverrides(context *command.Ctx) (*bool, map[string]bool) {
	var deferred *bool
	if value, set := pairedOverride(context, deferredBoundsCheckingFlag); set {
		deferred = &value
	}
	overrides := map[string]bool{}
	for _, knob := range settings.OptimizationCatalog() {
		name := strings.TrimPrefix(knob.Key, "optimizations.")
		if value, set := pairedOverride(context, name); set {
			overrides[name] = value
		}
	}
	return deferred, overrides
}

func pairedOverride(context *command.Ctx, name string) (bool, bool) {
	on, off := context.Bool(name), context.Bool("no-"+name)
	if on && off {
		ui.Usage("compile: conflicting --%s and --no-%s", name, name)
	}
	if on {
		return true, true
	}
	if off {
		return false, true
	}
	return false, false
}

func joinPluginLists(values ...string) string {
	plugins := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			plugins = append(plugins, value)
		}
	}
	return strings.Join(plugins, ",")
}

package handoff

import (
	"fmt"
	"runtime"

	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/settings"
)

// RuntimeCommands describes the stable standard-runtime command surface used
// when no Active Runtime is available to describe its exact compiled profile.
// The manager owns no copy of this surface; routing, automation, completion,
// and fallback schema all consume this Runtime Handoff description.
func RuntimeCommands() []*command.Cmd {
	parallel := command.Flag{Name: "parallel", Short: "p", Arg: "[workers]", Help: "parallel function validation and compilation"}
	profileFlags := runtimeProfileFlags()
	knobs := runtimeCompilationKnobs()
	runFlags := []command.Flag{
		{Name: "invoke", Short: "e", Arg: "<name>", Help: "exported function to call"},
		{Name: "allow-native-artifact", Bool: true, Help: "execute a trusted .wago native-code artifact"},
	}
	runFlags = append(runFlags, runtimeWatchFlags()...)
	runFlags = append(runFlags,
		command.Flag{Name: "core", Arg: "<version>", Help: "WebAssembly core feature set: 2 | 3 (default: best supported)"},
		command.Flag{Name: "target", Arg: "<mode>", Help: "compiler target: compat | native (default compat)"},
		command.Flag{Name: "objective", Arg: "<name>", Help: "compiler objective: speed | balanced | size (default speed)"},
		command.Flag{Name: "native-stack", Arg: "<size>", Help: "native execution stack capacity in bytes or KiB, MiB, GiB"},
		command.Flag{Name: "gc-heap", Arg: "<size>", Help: "throughput GC heap capacity in bytes or KiB, MiB, GiB"},
		command.Flag{Name: "gc-nursery", Arg: "<size>", Help: "throughput GC nursery capacity in bytes or KiB, MiB, GiB"},
		parallel,
	)
	runFlags = append(runFlags, runtimeBackendFlags()...)
	runFlags = append(runFlags, profileFlags...)
	buildFlags := []command.Flag{
		{Name: "output", Short: "o", Arg: "<file>", Help: "output path"},
		{Name: "target", Arg: "<mode>", Help: "compiler target: compat | native (default compat)"},
		{Name: "objective", Arg: "<name>", Help: "compiler objective: speed | balanced | size (default speed)"},
		parallel,
	}
	buildFlags = append(buildFlags, runtimeBackendFlags()...)
	buildFlags = append(buildFlags, profileFlags...)
	return []*command.Cmd{
		{Name: "run", Summary: "compile and execute a WebAssembly module (default)", Args: "<file> [args...]", Flags: runFlags, Knobs: cloneFlags(knobs), PassThrough: true},
		{Name: "module", Aliases: []string{"mod"}, Summary: "inspect a module's imports, exports, and required capabilities", Children: []*command.Cmd{
			{Name: "imports", Summary: "list a module's imports", Args: "<file>", Automation: command.JSONOutput},
			{Name: "exports", Summary: "list a module's exports and types", Args: "<file>", Automation: command.JSONOutput},
			{Name: "capabilities", Aliases: []string{"caps"}, Summary: "list required capabilities", Args: "<file>", Automation: command.JSONOutput},
		}},
		{Name: "build", Summary: "precompile a WebAssembly module to a .wago artifact", Args: "<file>", Flags: buildFlags, Knobs: cloneFlags(knobs), Automation: command.DryRun},
		{Name: "validate", Aliases: []string{"check"}, Summary: "decode and validate a module", Args: "<file>", Flags: []command.Flag{parallel}, Automation: command.JSONOutput},
	}
}

func runtimeBackendFlags() []command.Flag {
	return []command.Flag{
		{Name: "backend", Arg: "<name>", Help: "compiler backend: railshot | dragline (default railshot)"},
		{Name: "compiler-fallback", Arg: "<name>", Help: "whole-module fallback: none | railshot (default none)"},
		{Name: "railshot", Bool: true, Help: "use the Railshot compiler"},
		{Name: "dragline", Bool: true, Help: "use the Dragline compiler (strict; no fallback)"},
	}
}

func runtimeProfileFlags() []command.Flag {
	return []command.Flag{
		{Name: "local", Bool: true, Help: "use this project's plugins"},
		{Name: "global", Short: "g", Bool: true, Help: "use shared user-wide plugins"},
		{Name: "bare", Bool: true, Help: "run without plugins"},
	}
}

func runtimeCompilationKnobs() []command.Flag {
	configured, hasConfig, _ := settings.LoadConfigured()
	deferred := true
	if hasConfig {
		deferred = configured.Runtime.DeferredBoundsChecking
	}
	flags := pairedRuntimeFlag("deferred-bounds-checking", deferred, "skip provably redundant explicit bounds checks")
	for _, knob := range settings.OptimizationsForArch(runtime.GOARCH) {
		on := knob.Default
		if hasConfig {
			on = knob.Value(configured)
		}
		flags = append(flags, pairedRuntimeFlag(knob.Name(), on, knob.Description)...)
	}
	return flags
}

func pairedRuntimeFlag(name string, on bool, description string) []command.Flag {
	state := "off"
	if on {
		state = "on"
	}
	return []command.Flag{
		{Name: name, Bool: true, Help: fmt.Sprintf("(default: %s) %s", state, description)},
		{Name: "no-" + name, Bool: true},
	}
}

func cloneFlags(flags []command.Flag) []command.Flag { return append([]command.Flag(nil), flags...) }

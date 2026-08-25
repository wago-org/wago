// Package run implements wago run.
package run

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/settings"
	"github.com/wago-org/wago/cli/internal/ui"
	"github.com/wago-org/wago/cli/runtime/internal/artifactcache"
)

// Environment supplies only profile-specific flags, optimization knobs, and
// runtime construction. All command behavior remains in this package.
type Environment interface {
	ProfileFlags() []command.Flag
	LoadRuntime(*wago.RuntimeConfig, []string) *wago.Runtime
	ArtifactCache() artifactcache.Cache
}

func Command(environment Environment) *command.Cmd {
	flags := []command.Flag{{Name: "invoke", Short: "e", Arg: "<name>", Help: "exported function to call"}}
	flags = append(flags, watchFlags()...)
	flags = append(flags,
		command.Flag{Name: "core", Arg: "<version>", Help: "WebAssembly core feature set: 2 | 3 (default: best supported)"},
		ParallelFlag(),
	)
	flags = append(flags, environment.ProfileFlags()...)
	knobs := append(DeferredBoundsCheckingFlags(), OptimizationFlags()...)
	parserFlags := append(append([]command.Flag(nil), flags...), knobs...)
	implementation := implementation{environment: environment}
	return &command.Cmd{
		Name: "run", Summary: "compile and execute a WebAssembly module (default)",
		Args: "<file> [args...]", Flags: flags, Knobs: knobs, PassThrough: true,
		Normalize: func(args []string) ([]string, error) {
			return NormalizeParallelArgs(args, parserFlags, false)
		},
		Long: "<file> is raw .wasm or a precompiled .wago. Args after the file are typed by the\n" +
			"signature; override per-arg with a suffix:  42   7:i64   3.5:f64\n" +
			"Wago flags may appear before or after <file>; use -- before colliding guest flags.\n" +
			"Selected Core 3 features default on where supported; use --core 2 for strict Release 2\n" +
			"or --core 3 for the complete release. Use -p for\n" +
			"adaptive validation/compile parallelism, or -p8 / -p 8 / --parallel=8 to force a\n" +
			"worker maximum. Advanced compiler controls are listed in `wago run --help-optimizations`.",
		Run: implementation.Run,
	}
}

type implementation struct {
	environment Environment
}

func (cmd implementation) Run(ctx *command.Ctx) {
	if runWatch(ctx) {
		return
	}
	deferredBoundsChecking, err := DeferredBoundsOverride(ctx)
	if err != nil {
		ui.Usage("run: %v", err)
	}
	positionals := ctx.Args
	if len(positionals) == 0 {
		ui.Usage("run: need a <file>")
	}
	optimizations, err := OptimizationOverrides(ctx)
	if err != nil {
		ui.Usage("run: %v", err)
	}
	selection, err := settings.ResolveCompilation(settings.CompilationRequest{
		Arch: runtime.GOARCH, Core: ctx.Str("core"), Parallel: ctx.Str("parallel"),
		DeferredBoundsChecking: deferredBoundsChecking, Optimizations: optimizations,
	})
	if err != nil {
		if settings.IsCompilationSettingsError(err) {
			ui.Fatal("run: %v", err)
		}
		ui.Usage("run: %v", err)
	}
	config := selection.RuntimeConfig()
	runtime := cmd.environment.LoadRuntime(config, positionals)
	defer runtime.Close()
	module := mustLoadModule(positionals[0], config, runtime, cmd.environment.ArtifactCache())
	compiled := module.Compiled()
	export := mustResolveExport(compiled, ctx.Str("invoke"))

	if export == "_start" {
		runStart(runtime, module)
		return
	}
	params, results, _ := compiled.Signature(export)
	values := mustParseArgs(positionals[1:], params)
	instance, err := runtime.Instantiate(context.Background(), module)
	if err != nil {
		ui.Fatal("%v", friendlyInstantiationError(err))
	}
	defer instance.Close()
	result, err := instance.Invoke(export, values...)
	if err != nil {
		ui.Fatal("%s %s", ui.Red("trap:"), trapReason(err))
	}
	fmt.Println(format(export, values, result, params, results))
}

func runStart(runtime *wago.Runtime, module *wago.Module) {
	instance, err := runtime.Instantiate(context.Background(), module)
	if err != nil {
		ui.Fatal("%v", friendlyInstantiationError(err))
	}
	defer instance.Close()
	if _, err := instance.Invoke("_start"); err != nil {
		var exit *wago.ExitError
		if errors.As(err, &exit) {
			instance.Close()
			os.Exit(int(exit.Code))
		}
		ui.Fatal("%s %s", ui.Red("trap:"), trapReason(err))
	}
}

func friendlyInstantiationError(err error) error {
	if !errors.Is(err, wago.ErrMissingImport) {
		return err
	}
	const prefix = `module imports "`
	message := err.Error()
	start := strings.Index(message, prefix)
	if start < 0 {
		return err
	}
	importName := message[start+len(prefix):]
	end := strings.IndexByte(importName, '"')
	if end < 0 {
		return err
	}
	return fmt.Errorf("no installed plugin provides this host import\n\n  %s\n\nAdd a plugin that provides it", importName[:end])
}

func trapReason(err error) string {
	var trap *wago.TrapError
	if errors.As(err, &trap) {
		return strings.TrimPrefix(trap.Error(), "wasm trap: ")
	}
	return err.Error()
}

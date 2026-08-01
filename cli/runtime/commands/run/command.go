// Package run implements wago run.
package run

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/ui"
	"github.com/wago-org/wago/cli/runtime/internal/artifactcache"
)

// Environment supplies only profile-specific flags, optimization knobs, and
// runtime construction. All command behavior remains in this package.
type Environment interface {
	ProfileFlags() []command.Flag
	LoadRuntime(*wago.RuntimeConfig, string) *wago.Runtime
	ArtifactCache() artifactcache.Cache
}

func Command(environment Environment) *command.Cmd {
	flags := []command.Flag{
		{Name: "invoke", Short: "e", Arg: "<name>", Help: "exported function to call"},
		{Name: "watch", Short: "w", Bool: true, Help: "rerun when the module changes"},
		{Name: "watch-interval", Arg: "<duration>", Help: "watch polling interval (default 200ms)"},
		{Name: "core", Arg: "<version>", Help: "WebAssembly core feature set: 2 (default) | 3"},
		ParallelFlag(),
	}
	flags = append(flags, environment.ProfileFlags()...)
	knobs := append(DeferredBoundsCheckingFlags(), OptimizationFlags()...)
	parserFlags := append(append([]command.Flag(nil), flags...), knobs...)
	implementation := implementation{environment: environment}
	return &command.Cmd{
		Name: "run", Summary: "compile and execute a WebAssembly module (default)",
		Args: "<file> [args...]", Flags: flags, Knobs: knobs, PassThrough: true,
		Normalize: func(args []string) ([]string, error) {
			return NormalizeParallelArgs(args, parserFlags, true)
		},
		Long: "<file> is raw .wasm or a precompiled .wago. Args after the file are typed by the\n" +
			"signature; override per-arg with a suffix:  42   7:i64   3.5:f64\n" +
			"Use --core 3 for the complete opt-in WebAssembly Core 3 feature set. Use -p for\n" +
			"adaptive validation/compile parallelism, or -p8 / -p 8 / --parallel=8 to force a\n" +
			"worker maximum. Optimization knobs are listed in `wago run --help`.",
		Run: implementation.Run,
	}
}

type implementation struct {
	environment Environment
}

func (cmd implementation) Run(ctx *command.Ctx) {
	if ctx.Bool("watch") {
		if len(ctx.Args) == 0 {
			ui.Usage("run: need a <file>")
		}
		watchModule(ctx.Args[0], ctx.Str("watch-interval"))
		return
	}
	defaults, configured, err := LoadDefaults()
	if err != nil {
		ui.Fatal("run: %v", err)
	}
	ApplyOptimizationDefaults(defaults, configured)
	ApplyOptimizationFlags(ctx)
	deferredDefault := true
	if configured {
		deferredDefault = defaults.Runtime.DeferredBoundsChecking
	}
	deferredBoundsChecking, err := DeferredBoundsChecking(ctx, deferredDefault)
	if err != nil {
		ui.Usage("run: %v", err)
	}
	positionals := ctx.Args
	if len(positionals) == 0 {
		ui.Usage("run: need a <file>")
	}
	wago.SetGuestArgs(positionals)
	config, err := Config(ctx.Str("core"), deferredBoundsChecking, ResolveParallel(ctx.Str("parallel"), defaults, configured))
	if err != nil {
		ui.Usage("run: %v", err)
	}
	config = ApplyFeatureDefaults(config, defaults, configured)
	runtime := cmd.environment.LoadRuntime(config, ctx.Str("plugin"))
	defer runtime.Close()
	module := mustLoadModule(positionals[0], config, runtime, cmd.environment.ArtifactCache())
	compiled := module.Compiled()
	export := mustResolveExport(compiled, ctx.Str("invoke"))

	if export == "_start" {
		runStart(runtime, module, compiled)
		return
	}
	params, results, _ := compiled.Signature(export)
	values := mustParseArgs(positionals[1:], params)
	imports := autoHosts(compiled, true, runtime.HostImports())
	instance, err := runtime.Instantiate(context.Background(), module, wago.WithImports(imports))
	if err != nil {
		ui.Fatal("%v", err)
	}
	defer instance.Close()
	result, err := instance.Invoke(export, values...)
	if err != nil {
		ui.Fatal("%s %s", ui.Red("trap:"), trapReason(err))
	}
	fmt.Println(format(export, values, result, params, results))
}

func runStart(runtime *wago.Runtime, module *wago.Module, compiled *wago.Compiled) {
	imports := autoHosts(compiled, false, runtime.HostImports())
	instance, err := runtime.Instantiate(context.Background(), module, wago.WithImports(imports))
	if err != nil {
		ui.Fatal("%v", err)
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

func trapReason(err error) string {
	var trap *wago.TrapError
	if errors.As(err, &trap) {
		return strings.TrimPrefix(trap.Error(), "wasm trap: ")
	}
	return err.Error()
}

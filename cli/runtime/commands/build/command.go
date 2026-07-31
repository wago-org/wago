// Package build implements wago build.
package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/ui"
	runcmd "github.com/wago-org/wago/cli/runtime/commands/run"
)

type Environment interface {
	ProfileFlags() []command.Flag
	LoadRuntime(*wago.RuntimeConfig, string) *wago.Runtime
}

func Command(environment Environment) *command.Cmd {
	flags := []command.Flag{
		{Name: "output", Short: "o", Arg: "<file>", Help: "output path (default: input name with .wago extension)"},
		runcmd.ParallelFlag(),
	}
	flags = append(flags, environment.ProfileFlags()...)
	knobs := append(runcmd.DeferredBoundsCheckingFlags(), runcmd.OptimizationFlags()...)
	parserFlags := append(append([]command.Flag(nil), flags...), knobs...)
	implementation := implementation{environment: environment}
	return &command.Cmd{
		Name: "build", Summary: "precompile a WebAssembly module to a .wago artifact",
		Automation: command.DryRun,
		Args:       "<file>", Flags: flags, Knobs: knobs,
		Normalize: func(args []string) ([]string, error) {
			return runcmd.NormalizeParallelArgs(args, parserFlags, false)
		},
		Long: ".wago artifacts are host-architecture-specific and must be rebuilt after incompatible Wago upgrades.",
		Run:  implementation.Run,
	}
}

type implementation struct {
	environment Environment
}

func (cmd implementation) Run(c *command.Ctx) {
	runcmd.ApplyOptimizationFlags(c)
	deferredBoundsChecking, err := runcmd.DeferredBoundsChecking(c)
	if err != nil {
		ui.Usage("build: %v", err)
	}
	input := singleFileArg(c.Args)
	output := c.Str("output")
	if output == "" {
		ext := filepath.Ext(input)
		output = strings.TrimSuffix(input, ext) + ".wago"
	}
	if automation.DryRun() {
		automation.PrintPlan("build artifact", map[string]any{"input": input, "output": output, "parallel": c.Str("parallel"), "deferredBoundsChecking": deferredBoundsChecking})
		return
	}
	source, err := os.ReadFile(input)
	if err != nil {
		ui.Fatal("build: %v", err)
	}
	if wago.IsCompiled(source) {
		ui.Fatal("build: %s is already a .wago artifact", input)
	}
	cfg, err := runcmd.Config(deferredBoundsChecking, c.Str("parallel"))
	if err != nil {
		ui.Usage("build: %v", err)
	}
	rt := cmd.environment.LoadRuntime(cfg, c.Str("plugin"))
	defer rt.Close()
	module, err := rt.Compile(source)
	if err != nil {
		ui.Fatal("build: %v", err)
	}
	artifact, err := module.Compiled().MarshalBinary()
	if err != nil {
		ui.Fatal("build: %v", err)
	}
	if filepath.Clean(output) == filepath.Clean(input) {
		ui.Usage("build: output path must differ from input")
	}
	if err := os.WriteFile(output, artifact, 0o644); err != nil {
		ui.Fatal("build: %v", err)
	}
	fmt.Printf("%s built %s\n", ui.Cyan("✓"), output)
}

func singleFileArg(args []string) string {
	if len(args) != 1 {
		ui.Usage("build: need exactly one <file>")
	}
	return args[0]
}

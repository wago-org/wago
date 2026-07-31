// Package build implements wago build.
package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/ui"
	runcmd "github.com/wago-org/wago/cli/runtime/commands/run"
)

type Environment interface {
	ProfileFlags() []command.Flag
	LoadRuntime(*wago.RuntimeConfig, string) *wago.Runtime
}

func Command(environment Environment) *command.Cmd {
	flags := append([]command.Flag{
		{Name: "output", Short: "o", Arg: "<file>", Help: "output path (default: input name with .wago extension)"},
		{Name: "bounds", Arg: "<mode>", Help: "bounds checks: defer (default) | all"},
		runcmd.ParallelFlag(),
	}, runcmd.OptimizationFlags()...)
	flags = append(flags, environment.ProfileFlags()...)
	implementation := implementation{environment: environment}
	return &command.Cmd{
		Name: "build", Summary: "precompile a WebAssembly module to a .wago artifact",
		Args: "<file>", Flags: flags,
		Normalize: func(args []string) ([]string, error) {
			return runcmd.NormalizeParallelArgs(args, flags, false)
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
	input := singleFileArg(c.Args)
	source, err := os.ReadFile(input)
	if err != nil {
		ui.Fatal("build: %v", err)
	}
	if wago.IsCompiled(source) {
		ui.Fatal("build: %s is already a .wago artifact", input)
	}
	cfg, err := runcmd.Config(c.Str("bounds"), c.Str("parallel"))
	if err != nil {
		ui.Fatal("build: %v", err)
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
	output := c.Str("output")
	if output == "" {
		ext := filepath.Ext(input)
		output = strings.TrimSuffix(input, ext) + ".wago"
	}
	if filepath.Clean(output) == filepath.Clean(input) {
		ui.Fatal("build: output path must differ from input")
	}
	if err := os.WriteFile(output, artifact, 0o644); err != nil {
		ui.Fatal("build: %v", err)
	}
	fmt.Printf("%s built %s\n", ui.Cyan("✓"), output)
}

func singleFileArg(args []string) string {
	if len(args) != 1 {
		ui.Fatal("build: need exactly one <file>")
	}
	return args[0]
}

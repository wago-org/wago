//go:build !wago_manager

package wagocli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/internal/functionworkers"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// buildCommand precompiles raw wasm into Wago's host-specific .wago format.
func buildCommand() *Cmd {
	flags := append([]Flag{
		{Name: "output", Short: "o", Arg: "<file>", Help: "output path (default: input name with .wago extension)"},
		{Name: "bounds", Arg: "<mode>", Help: "bounds checks: defer (default) | all"},
		parallelFlag(),
	}, optKnobFlags()...)
	flags = append(flags, runProfileFlags()...)
	return &Cmd{
		Name:      "build",
		Summary:   "precompile a WebAssembly module to a .wago artifact",
		Args:      "<file>",
		Flags:     flags,
		Normalize: func(args []string) ([]string, error) { return normalizeParallelArgs(args, flags, false) },
		Long:      ".wago artifacts are host-architecture-specific and must be rebuilt after incompatible Wago upgrades.",
		Run:       buildExec,
	}
}

func buildExec(c *Ctx) {
	prepareRunPlugins()
	applyOptFlags(c)
	input := singleFileArg("build", c.Args)
	source, err := os.ReadFile(input)
	if err != nil {
		fatal("build: %v", err)
	}
	if wago.IsCompiled(source) {
		fatal("build: %s is already a .wago artifact", input)
	}
	cfg, err := runConfig(c.Str("bounds"), c.Str("parallel"))
	if err != nil {
		fatal("build: %v", err)
	}
	rt := loadRunRuntime(cfg, c.Str("plugin"))
	defer rt.Close()
	module, err := rt.Compile(source)
	if err != nil {
		fatal("build: %v", err)
	}
	artifact, err := module.Compiled().MarshalBinary()
	if err != nil {
		fatal("build: %v", err)
	}
	output := c.Str("output")
	if output == "" {
		ext := filepath.Ext(input)
		output = strings.TrimSuffix(input, ext) + ".wago"
	}
	if filepath.Clean(output) == filepath.Clean(input) {
		fatal("build: output path must differ from input")
	}
	if err := os.WriteFile(output, artifact, 0o644); err != nil {
		fatal("build: %v", err)
	}
	fmt.Printf("%s built %s\n", cyan("✓"), output)
}

// validateCommand decodes and validates a module without running it.
func validateCommand() *Cmd {
	flags := []Flag{parallelFlag()}
	return &Cmd{
		Name:      "validate",
		Summary:   "decode and validate a module",
		Args:      "<file>",
		Flags:     flags,
		Normalize: func(args []string) ([]string, error) { return normalizeParallelArgs(args, flags, false) },
		Long:      "Use -p for adaptive parallel function validation, or -p8 / -p 8 / --parallel=8 to force a worker maximum.",
		Run: func(c *Ctx) {
			file := singleFileArg("validate", c.Args)
			src, err := os.ReadFile(file)
			if err != nil {
				fatal("%v", err)
			}
			policy, err := parallelPolicy(c.Str("parallel"))
			if err != nil {
				fatal("validate: %v", err)
			}
			if err := validateModuleBytesWithPolicy(src, policy); err != nil {
				fatal("%v", err)
			}
		},
	}
}

// singleFileArg returns the sole positional or fatals with a usage hint.
func singleFileArg(cmd string, args []string) string {
	if len(args) != 1 {
		fatal("%s: need exactly one <file>", cmd)
	}
	return args[0]
}

func validateModuleBytes(src []byte) error {
	return validateModuleBytesWithPolicy(src, 1)
}

func validateModuleBytesWithPolicy(src []byte, policy int) error {
	m, err := wasm.DecodeModule(src)
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	bodyBytes := 0
	for i := range m.Code {
		bodyBytes += len(m.Code[i].BodyBytes)
	}
	workers := functionworkers.Resolve(policy, len(m.Code), bodyBytes)
	if err := wasm.ValidateModuleWithWorkers(m, workers); err != nil {
		return fmt.Errorf("validate: %w", err)
	}
	return nil
}

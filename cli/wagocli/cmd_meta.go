package wagocli

import (
	"fmt"
	"os"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/internal/functionworkers"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// envCommand prints wago's resolved on-disk directories.
func envCommand() *Cmd {
	return &Cmd{
		Name:    "env",
		Summary: "print resolved config/cache/data directories",
		Run: func(*Ctx) {
			d := wago.DirsFor(versionString())
			fmt.Printf("%s %s\n", dim("WAGO_VERSION"), d.Version)
			fmt.Printf("%s %s\n", dim("WAGO_CONFIG  "), d.Config)
			fmt.Printf("%s %s\n", dim("WAGO_DATA    "), d.Data)
			fmt.Printf("%s %s\n", dim("WAGO_VERSIONS"), d.Versions)
			fmt.Printf("%s %s\n", dim("WAGO_CACHE   "), d.Cache)
		},
	}
}

// buildCommand is a placeholder for the not-yet-implemented AOT builder.
func buildCommand() *Cmd {
	return &Cmd{
		Name:    "build",
		Summary: "not implemented",
		Run:     func(*Ctx) { fatal("build: not implemented") },
	}
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

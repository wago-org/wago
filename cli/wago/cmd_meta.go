package main

import (
	"fmt"
	"os"

	"github.com/wago-org/wago"
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
	return &Cmd{
		Name:    "validate",
		Summary: "decode and validate a module",
		Args:    "<file>",
		Run: func(c *Ctx) {
			file := singleFileArg("validate", c.Args)
			src, err := os.ReadFile(file)
			if err != nil {
				fatal("%v", err)
			}
			if err := validateModuleBytes(src); err != nil {
				fatal("validate: %v", err)
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
	m, err := wasm.DecodeModule(src)
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		return fmt.Errorf("validate: %w", err)
	}
	return nil
}

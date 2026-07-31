// Package validate implements wago validate.
package validate

import (
	"fmt"
	"os"

	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/ui"
	runcmd "github.com/wago-org/wago/cli/runtime/commands/run"
	"github.com/wago-org/wago/internal/functionworkers"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func Command() *command.Cmd {
	flags := []command.Flag{runcmd.ParallelFlag()}
	return &command.Cmd{
		Name: "validate", Aliases: []string{"check"}, Summary: "decode and validate a module",
		Args: "<file>", Flags: flags,
		Automation: command.JSONOutput,
		Normalize: func(args []string) ([]string, error) {
			return runcmd.NormalizeParallelArgs(args, flags, false)
		},
		Long: "Use -p for adaptive parallel function validation, or -p8 / -p 8 / --parallel=8 to force a worker maximum.",
		Run:  run,
	}
}

func run(c *command.Ctx) {
	if len(c.Args) != 1 {
		ui.Usage("validate: need exactly one <file>")
	}
	src, err := os.ReadFile(c.Args[0])
	if err != nil {
		ui.Fatal("%v", err)
	}
	policy, err := runcmd.ParallelPolicy(c.Str("parallel"))
	if err != nil {
		ui.Usage("validate: %v", err)
	}
	if err := ModuleBytesWithPolicy(src, policy); err != nil {
		ui.Fatal("%v", err)
	}
	if automation.JSON() {
		ui.PrintJSON(map[string]any{"valid": true, "file": c.Args[0], "bytes": len(src), "workers": policy})
	}
}

func ModuleBytes(src []byte) error {
	return ModuleBytesWithPolicy(src, 1)
}

func ModuleBytesWithPolicy(src []byte, policy int) error {
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

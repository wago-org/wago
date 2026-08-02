// Package initcmd defines wago init.
package initcmd

import (
	"fmt"
	"os"

	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/tui"
	"github.com/wago-org/wago/cli/internal/ui"
)

func Command() *command.Cmd {
	return &command.Cmd{
		Name: "init", Summary: "initialize and configure a local Wago project",
		Automation: command.DryRun,
		Flags: []command.Flag{
			{Name: "run", Bool: true, Help: "create a minimal project for running WebAssembly"},
			{Name: "plugin", Bool: true, Help: "configure a publishable Wago plugin"},
			{Name: "name", Short: "n", Arg: "<name>", Help: "human-readable project name"},
			{Name: "description", Short: "d", Arg: "<text>", Help: "project description"},
			{Name: "plugins", Short: "p", Arg: "<spec,...>", Help: "initial plugin constraints"},
			{Name: "module", Short: "m", Arg: "<path>", Help: "publishable Go module path"},
			{Name: "version", Short: "v", Arg: "<version>", Help: "package semantic version"},
			{Name: "license", Short: "l", Arg: "<spdx>", Help: "package SPDX license"},
			{Name: "repository", Short: "r", Arg: "<url>", Help: "public HTTPS repository"},
			{Name: "homepage", Arg: "<url>", Help: "project homepage"},
			{Name: "category", Short: "c", Arg: "<slug>", Help: "registry category"},
			{Name: "tags", Short: "t", Arg: "<tag,...>", Help: "registry discovery tags"},
			{Name: "author", Short: "a", Arg: "<name>", Help: "package author"},
			{Name: "stability", Short: "s", Arg: "<level>", Help: "experimental, stable, or deprecated"},
			{Name: "yes", Short: "y", Bool: true, Help: "accept inferred defaults for omitted answers"},
		},
		Run: func(ctx *command.Ctx) {
			if automation.Locked() && !automation.DryRun() {
				ui.Fatal("init: --locked prevents changing wago.json")
			}
			if automation.DryRun() {
				mode, err := explicitMode(ctx)
				if err != nil {
					ui.Usage("init: %v", err)
				}
				if mode == "" {
					mode = modeRun
				}
				automation.PrintPlan("initialize project", map[string]any{"mode": mode, "name": ctx.Str("name"), "plugins": ctx.Str("plugins")})
				return
			}
			result, err := run(ctx, os.Stdin, os.Stdout, tui.StdinIsTTY())
			if err != nil {
				ui.Fatal("init: %v", err)
			}
			if result.Cancelled {
				fmt.Println("Cancelled.")
				return
			}
			verb := "Updated"
			if result.Created {
				verb = "Initialized"
			}
			target := "for running WebAssembly"
			if result.Mode == modePlugin {
				target = "for a Wago plugin"
			}
			fmt.Printf("%s %s wago.json %s\n", ui.Cyan("✓"), verb, target)
			if result.Plugins > 0 {
				fmt.Printf("  %d plugin%s configured\n", result.Plugins, plural(result.Plugins))
			}
		},
	}
}

func plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

// Package initcmd defines wago init.
package initcmd

import (
	"fmt"
	"os"

	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/tui"
	"github.com/wago-org/wago/cli/internal/ui"
)

func Command() *command.Cmd {
	return &command.Cmd{
		Name: "init", Summary: "initialize and configure a local Wago project",
		Flags: []command.Flag{
			{Name: "quick", Short: "q", Bool: true, Help: "create a minimal project without prompting"},
			{Name: "full", Short: "f", Bool: true, Help: "configure project details and plugins"},
			{Name: "kind", Short: "k", Arg: "<kind>", Help: "project kind: application or plugin"},
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
			fmt.Printf("%s %s wago.json (%s setup)\n", ui.Cyan("✓"), verb, result.Mode)
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

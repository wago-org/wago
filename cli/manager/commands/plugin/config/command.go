// Package config defines atomic plugin configuration updates.
package config

import (
	"os"
	"strings"

	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/ui"
	plugin "github.com/wago-org/wago/cli/manager/commands/plugin"
)

type Options struct {
	ID            string
	JSON          []byte
	Global, Local bool
}

type Environment interface{ ConfigurePlugin(Options) }

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name: "config", Summary: "validate and atomically update a plugin's configuration", Args: "<plugin-id> [json]",
		Automation: command.DryRun,
		Flags:      []command.Flag{plugin.GlobalFlag(), plugin.LocalFlag(), {Name: "file", Short: "f", Arg: "<path>", Help: "read JSON configuration from a file"}},
		Run: func(ctx *command.Ctx) {
			if len(ctx.Args) < 1 || len(ctx.Args) > 2 {
				ui.Usage("plugin config: need <plugin-id> and optional [json]")
			}
			if ctx.Str("file") != "" && len(ctx.Args) == 2 {
				ui.Usage("plugin config: choose [json] or --file, not both")
			}
			data := []byte(`{}`)
			if len(ctx.Args) == 2 {
				data = []byte(strings.TrimSpace(ctx.Args[1]))
			}
			if file := ctx.Str("file"); file != "" {
				var err error
				data, err = os.ReadFile(file)
				if err != nil {
					ui.Fatal("plugin config: %v", err)
				}
			}
			environment.ConfigurePlugin(Options{ID: ctx.Args[0], JSON: data, Global: ctx.Bool("global"), Local: ctx.Bool("local")})
		},
	}
}

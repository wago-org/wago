package rebuild

import (
	"github.com/wago-org/wago/cli/internal/command"
	plugin "github.com/wago-org/wago/cli/manager/commands/plugin"
)

type Options struct {
	Global  bool
	Local   bool
	Verbose bool
}

type Environment interface {
	RebuildPlugins(Options)
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name:    "rebuild",
		Summary: "rebuild the plugin-enabled runtime from the lockfile",
		Flags: []command.Flag{
			plugin.GlobalFlag(),
			plugin.LocalFlag(),
			{Name: "verbose", Short: "v", Bool: true, Help: "stream Go build output"},
		},
		Run: func(ctx *command.Ctx) {
			environment.RebuildPlugins(Options{
				Global: ctx.Bool("global"), Local: ctx.Bool("local"), Verbose: ctx.Bool("verbose"),
			})
		},
	}
}

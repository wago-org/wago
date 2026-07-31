package outdated

import (
	"github.com/wago-org/wago/cli/internal/command"
	plugin "github.com/wago-org/wago/cli/manager/commands/plugin"
)

type Options struct {
	Global bool
	Local  bool
}

type Environment interface {
	OutdatedPlugins(Options)
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name:       "outdated",
		Summary:    "list plugins with newer releases",
		Automation: command.JSONOutput,
		Flags:      []command.Flag{plugin.GlobalFlag(), plugin.LocalFlag()},
		Run: func(ctx *command.Ctx) {
			environment.OutdatedPlugins(Options{Global: ctx.Bool("global"), Local: ctx.Bool("local")})
		},
	}
}

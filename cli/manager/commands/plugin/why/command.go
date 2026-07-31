package why

import (
	"github.com/wago-org/wago/cli/internal/command"
	plugin "github.com/wago-org/wago/cli/manager/commands/plugin"
)

type Options struct {
	Name          string
	Global, Local bool
}
type Environment interface {
	WhyPlugin(Options)
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name:    "why",
		Summary: "explain why a plugin is enabled",
		Args:    "<name>",
		Flags:   []command.Flag{plugin.GlobalFlag(), plugin.LocalFlag()},
		Run: func(ctx *command.Ctx) {
			environment.WhyPlugin(Options{
				Name: ctx.One("<name>"), Global: ctx.Bool("global"), Local: ctx.Bool("local"),
			})
		},
	}
}

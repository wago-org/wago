package verify

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
	VerifyPlugins(Options)
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name:    "verify",
		Summary: "verify plugin constraints, lockfile, and module checksums",
		Flags: []command.Flag{
			plugin.GlobalFlag(),
			plugin.LocalFlag(),
			{Name: "verbose", Short: "v", Bool: true, Help: "stream Go verification output"},
		},
		Run: func(ctx *command.Ctx) {
			environment.VerifyPlugins(Options{
				Global: ctx.Bool("global"), Local: ctx.Bool("local"), Verbose: ctx.Bool("verbose"),
			})
		},
	}
}

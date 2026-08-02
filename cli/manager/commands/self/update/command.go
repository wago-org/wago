// Package update defines wago self update.
package update

import "github.com/wago-org/wago/cli/internal/command"

type Environment interface {
	Update(force bool)
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name: "update", Aliases: []string{"up", "upgrade"}, Summary: "update Wago on its current release channel",
		Automation: command.DryRun,
		Flags:      []command.Flag{{Name: "force", Short: "f", Bool: true, Help: "reinstall even when the commit matches"}},
		Run:        func(ctx *command.Ctx) { environment.Update(ctx.Bool("force")) },
	}
}

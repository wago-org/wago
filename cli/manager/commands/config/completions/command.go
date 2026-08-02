// Package completions defines wago config completions.
package completions

import (
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/ui"
)

type Options struct {
	Shell, Output string
	RC            string
	Install       bool
}

type Environment interface {
	Completions(Options)
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name:       "completions",
		Aliases:    []string{"completion"},
		Summary:    "print or install shell completions",
		Automation: command.DryRun,
		Args:       "<shell>",
		Flags: []command.Flag{
			{Name: "install", Short: "i", Bool: true, Help: "install completion for the selected shell"},
			{Name: "output", Short: "o", Arg: "<file>", Help: "write to a specific file"},
			{Name: "rc", Arg: "<file>", Help: "shell startup file to configure with --install"},
		},
		Run: func(ctx *command.Ctx) {
			shell := ctx.One("<shell>")
			if shell != "zsh" && shell != "bash" && shell != "fish" {
				ui.Usage("config completions: unsupported shell %q (want zsh, bash, or fish)", shell)
			}
			environment.Completions(Options{
				Shell:   shell,
				Output:  ctx.Str("output"),
				RC:      ctx.Str("rc"),
				Install: ctx.Bool("install"),
			})
		},
	}
}

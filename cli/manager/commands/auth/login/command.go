// Package login defines wago auth login.
package login

import (
	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/ui"
)

type Options struct {
	Link, Code, WithToken bool
	Token                 string
}

type Environment interface {
	Login(Options)
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name:       "login",
		Summary:    "log in to the registry",
		Automation: command.DryRun,
		Flags: []command.Flag{
			{Name: "link", Short: "l", Bool: true, Help: "show one-time authorization with copy and browser shortcuts"},
			{Name: "code", Short: "c", Bool: true, Help: "log in with a one-time code (headless/remote)"},
			{Name: "token", Short: "t", Arg: "<t>", Help: "use this API token directly"},
			{Name: "with-token", Bool: true, Help: "read an API token from stdin (for CI)"},
		},
		Long: "With no flag, login asks whether to use a browser link or a one-time code.",
		Run: func(c *command.Ctx) {
			options := Options{
				Link:      c.Bool("link"),
				Code:      c.Bool("code"),
				Token:     c.Str("token"),
				WithToken: c.Bool("with-token"),
			}
			methods := 0
			for _, selected := range []bool{options.Code, options.Link, options.Token != "", options.WithToken} {
				if selected {
					methods++
				}
			}
			if methods > 1 {
				ui.Usage("login: choose only one of --code, --link, --token, or --with-token")
			}
			if automation.NoInput() && !options.Code && !options.Link && options.Token == "" && !options.WithToken {
				ui.Usage("auth login: --no-input requires --link, --code, --token, or --with-token")
			}
			environment.Login(options)
		},
	}
}

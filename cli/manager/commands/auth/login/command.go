// Package login defines wago auth login.
package login

import (
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
		Name:    "login",
		Summary: "log in to the registry",
		Flags: []command.Flag{
			{Name: "link", Bool: true, Help: "log in via a browser link on this machine"},
			{Name: "code", Bool: true, Help: "log in with a one-time code (headless/remote)"},
			{Name: "token", Arg: "<t>", Help: "use this API token directly"},
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
			if options.Code && options.Link {
				ui.Fatal("login: choose either --code or --link, not both")
			}
			environment.Login(options)
		},
	}
}

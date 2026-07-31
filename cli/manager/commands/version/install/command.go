// Package install defines wago version install.
package install

import "github.com/wago-org/wago/cli/internal/command"

type Options struct {
	Versions                []string
	Latest, Nightly, Canary bool
	Profile, Build          string
}

type Environment interface {
	InstallRequested(Options)
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name: "install", Aliases: []string{"add"},
		Summary: "browse and install a release or commit",
		Args:    "[version]",
		Flags: []command.Flag{
			{Name: "latest", Bool: true, Help: "install the latest release"},
			{Name: "nightly", Bool: true, Help: "install nightly"},
			{Name: "canary", Bool: true, Help: "install the latest canary"},
			{Name: "profile", Arg: "<name>", Help: "standard or minimal"},
			{Name: "build", Arg: "<name>", Help: "normal or tiny"},
		},
		Run: func(c *command.Ctx) {
			environment.InstallRequested(Options{
				Versions: c.Args,
				Latest:   c.Bool("latest"),
				Nightly:  c.Bool("nightly"),
				Canary:   c.Bool("canary"),
				Profile:  c.Str("profile"),
				Build:    c.Str("build"),
			})
		},
	}
}

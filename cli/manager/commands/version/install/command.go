// Package install defines wago version install.
package install

import (
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/ui"
)

type Options struct {
	Versions                []string
	Latest, Nightly, Canary bool
	Profile, Build          string
	Use                     string
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
			{Name: "version", Arg: "<version>", Help: "install an exact release or commit"},
			{Name: "latest", Bool: true, Help: "install the latest release"},
			{Name: "nightly", Bool: true, Help: "install nightly"},
			{Name: "canary", Bool: true, Help: "install the latest canary"},
			{Name: "profile", Arg: "<name>", Help: "standard or minimal"},
			{Name: "build", Arg: "<name>", Help: "normal or tiny"},
			{Name: "use", Bool: true, Help: "make the installed runtime active without prompting"},
			{Name: "no-use", Bool: true, Help: "do not make the installed runtime active"},
		},
		Run: func(c *command.Ctx) {
			versions := append([]string(nil), c.Args...)
			if value := c.Str("version"); value != "" {
				if len(versions) != 0 {
					ui.Fatal("version install: use [version] or --version, not both")
				}
				versions = []string{value}
			}
			use := ""
			if c.Bool("use") {
				use = "yes"
			}
			if c.Bool("no-use") {
				if use != "" {
					ui.Fatal("version install: choose --use or --no-use")
				}
				use = "no"
			}
			environment.InstallRequested(Options{
				Versions: versions,
				Latest:   c.Bool("latest"),
				Nightly:  c.Bool("nightly"),
				Canary:   c.Bool("canary"),
				Profile:  c.Str("profile"),
				Build:    c.Str("build"),
				Use:      use,
			})
		},
	}
}

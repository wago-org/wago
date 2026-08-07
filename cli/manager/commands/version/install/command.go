// Package install defines wago version install.
package install

import (
	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/ui"
	"github.com/wago-org/wago/internal/wagopaths"
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
		Summary:    "browse and install a release or commit",
		Automation: command.DryRun,
		Args:       "[version]",
		Flags: []command.Flag{
			{Name: "version", Short: "v", Arg: "<version>", Help: "install an exact release or commit"},
			{Name: "latest", Short: "l", Bool: true, Help: "install the latest release"},
			{Name: "nightly", Short: "n", Bool: true, Help: "install nightly"},
			{Name: "canary", Short: "c", Bool: true, Help: "install the latest canary"},
			{Name: "profile", Short: "p", Arg: "<name>", Help: "standard or minimal"},
			{Name: "build", Short: "b", Arg: "<name>", Help: "normal or tiny"},
			{Name: "use", Short: "u", Bool: true, Help: "make the installed runtime active without prompting"},
			{Name: "no-use", Bool: true, Help: "do not make the installed runtime active"},
		},
		Run: func(c *command.Ctx) {
			versions := append([]string(nil), c.Args...)
			if value := c.Str("version"); value != "" {
				if len(versions) != 0 {
					ui.Usage("version install: use [version] or --version, not both")
				}
				versions = []string{value}
			}
			channels := 0
			for _, selected := range []bool{c.Bool("latest"), c.Bool("nightly"), c.Bool("canary")} {
				if selected {
					channels++
				}
			}
			if len(versions) > 1 || channels > 1 || (len(versions) != 0 && channels != 0) {
				ui.Usage("version install: choose exactly one version or release channel")
			}
			if value := c.Str("profile"); value != "" {
				if _, err := wagopaths.ParseProfile(value); err != nil {
					ui.Usage("version install: %v", err)
				}
			}
			if value := c.Str("build"); value != "" {
				if _, err := wagopaths.ParseBuild(value); err != nil {
					ui.Usage("version install: %v", err)
				}
			}
			use := ""
			if c.Bool("use") {
				use = "yes"
			}
			if c.Bool("no-use") {
				if use != "" {
					ui.Usage("version install: choose --use or --no-use")
				}
				use = "no"
			}
			if automation.NoInput() {
				if len(versions) == 0 && !c.Bool("latest") && !c.Bool("nightly") && !c.Bool("canary") {
					ui.Usage("version install: --no-input requires [version], --latest, --nightly, or --canary")
				}
				if use == "" && !automation.DryRun() {
					ui.Usage("version install: --no-input requires --use or --no-use")
				}
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

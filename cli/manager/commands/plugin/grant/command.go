// Package grant defines wago plugin grant.
package grant

import (
	"strings"

	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/ui"
	plugin "github.com/wago-org/wago/cli/manager/commands/plugin"
)

type Options struct {
	Name          string
	Global, Local bool
	Capabilities  []string
	All, DenyAll  bool
}

type Environment interface {
	Grant(Options)
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name: "grant", Summary: "review and edit a plugin's granted capabilities", Args: "<name>",
		Flags: []command.Flag{
			plugin.GlobalFlag(), plugin.LocalFlag(),
			{Name: "allow", Arg: "<cap,...>", Help: "grant a comma-separated capability set without a prompt"},
			{Name: "all", Bool: true, Help: "grant every requested capability without a prompt"},
			{Name: "deny-all", Bool: true, Help: "remove every grant without a prompt"},
		},
		Run: func(c *command.Ctx) {
			selected := 0
			for _, explicit := range []bool{c.Str("allow") != "", c.Bool("all"), c.Bool("deny-all")} {
				if explicit {
					selected++
				}
			}
			if selected > 1 {
				ui.Fatal("plugin grant: choose only one of --allow, --all, or --deny-all")
			}
			var capabilities []string
			for _, value := range strings.Split(c.Str("allow"), ",") {
				if value = strings.TrimSpace(value); value != "" {
					capabilities = append(capabilities, value)
				}
			}
			environment.Grant(Options{
				Name: c.One("<name>"), Global: c.Bool("global"), Local: c.Bool("local"),
				Capabilities: capabilities, All: c.Bool("all"), DenyAll: c.Bool("deny-all"),
			})
		},
	}
}

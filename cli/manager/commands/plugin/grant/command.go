// Package grant defines wago plugin grant.
package grant

import (
	"strings"

	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/project"
	"github.com/wago-org/wago/cli/internal/ui"
	plugin "github.com/wago-org/wago/cli/manager/commands/plugin"
)

type Options struct {
	Name          string
	Global, Local bool
	Authorities   []string
	All, DenyAll  bool
	Scopes        map[string]map[string]project.AuthorityScope
}

type Environment interface {
	Grant(Options)
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name: "grant", Summary: "review and edit a plugin's authority grants", Args: "[plugin-id]",
		Automation: command.DryRun,
		Long: "--scopes accepts one strict JSON object keyed first by full Plugin ID, then by exact Authority.\n" +
			`Example: --scopes '{"github.com/acme/plugin":{"host.import.define":{"modules":["env"]}}}'`,
		Flags: []command.Flag{
			plugin.GlobalFlag(), plugin.LocalFlag(),
			{Name: "allow", Arg: "<authority,...>", Help: "grant only these comma-separated authorities without prompting"},
			{Name: "all", Short: "a", Bool: true, Help: "grant every requested authority without prompting"},
			{Name: "deny-all", Bool: true, Help: "deny every requested authority without a prompt"},
			{Name: "scopes", Arg: "<json>", Help: "set exact narrower Authority scopes from one JSON document"},
		},
		Run: func(c *command.Ctx) {
			name := c.Optional("[plugin-id]")
			selected := 0
			for _, explicit := range []bool{c.Str("allow") != "", c.Bool("all"), c.Bool("deny-all")} {
				if explicit {
					selected++
				}
			}
			if selected > 1 {
				ui.Usage("plugin grant: choose only one of --allow, --all, or --deny-all")
			}
			if automation.NoInput() && selected == 0 && c.Str("scopes") == "" {
				ui.Usage("plugin grant: --no-input requires --allow, --all, --deny-all, or --scopes")
			}
			scopes, err := plugin.ParseAuthorityScopeOverrides(c.Str("scopes"))
			if err != nil {
				ui.Usage("plugin grant: %v", err)
			}
			var authorities []string
			for _, value := range strings.Split(c.Str("allow"), ",") {
				if value = strings.TrimSpace(value); value != "" {
					authorities = append(authorities, value)
				}
			}
			environment.Grant(Options{
				Name: name, Global: c.Bool("global"), Local: c.Bool("local"),
				Authorities: authorities, All: c.Bool("all"), DenyAll: c.Bool("deny-all"), Scopes: scopes,
			})
		},
	}
}

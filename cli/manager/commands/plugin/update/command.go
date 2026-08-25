// Package update defines wago plugin update.
package update

import (
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/project"
	"github.com/wago-org/wago/cli/internal/ui"
	plugin "github.com/wago-org/wago/cli/manager/commands/plugin"
)

type Options struct {
	Module                        string
	Global, Local, Force, Verbose bool
	AcceptContracts               bool
	Authorities                   []string
	GrantAll, DenyAll             bool
	Scopes                        map[string]map[string]project.AuthorityScope
}

type Environment interface {
	UpdatePlugins(Options)
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name: "update", Aliases: []string{"up", "upgrade"},
		Summary: "update plugins to their latest versions, then rebuild", Args: "[module]",
		Automation: command.DryRun,
		Flags: []command.Flag{
			plugin.GlobalFlag(), plugin.LocalFlag(),
			{Name: "force", Short: "f", Bool: true, Help: "update and rebuild even when commit hashes match"},
			{Name: "verbose", Short: "v", Bool: true, Help: "stream the underlying go output"},
			{Name: "allow", Arg: "<authority,...>", Help: "grant only these reviewed authorities without prompting"},
			{Name: "allow-all", Bool: true, Help: "grant every reviewed authority without prompting"},
			{Name: "deny-all", Bool: true, Help: "deny every reviewed authority without prompting"},
			{Name: "scopes", Arg: "<json>", Help: "set narrower scopes by Plugin ID and exact Authority"},
			{Name: "accept-contracts", Bool: true, Help: "accept the proposed exact contract bindings without prompting"},
		},
		Run: func(c *command.Ctx) {
			explicitChoices := 0
			for _, selected := range []bool{c.Str("allow") != "", c.Bool("allow-all"), c.Bool("deny-all")} {
				if selected {
					explicitChoices++
				}
			}
			if explicitChoices > 1 {
				ui.Usage("plugin update: choose only one of --allow, --allow-all, or --deny-all")
			}
			scopes, err := plugin.ParseAuthorityScopeOverrides(c.Str("scopes"))
			if err != nil {
				ui.Usage("plugin update: %v", err)
			}
			environment.UpdatePlugins(Options{
				Module:          c.Optional("[module]"),
				Global:          c.Bool("global"),
				Local:           c.Bool("local"),
				Force:           c.Bool("force"),
				Verbose:         c.Bool("verbose"),
				Authorities:     plugin.SplitCommaList(c.Str("allow")),
				GrantAll:        c.Bool("allow-all"),
				DenyAll:         c.Bool("deny-all"),
				Scopes:          scopes,
				AcceptContracts: c.Bool("accept-contracts"),
			})
		},
	}
}
